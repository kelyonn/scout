package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/adapters/ats/greenhouse"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

// ensureSeeded makes the bootstrap data infra/seed/seed.sql provides
// (single app_user, its user_profile, the active weight_version) present
// without assuming `make seed` has run — `make dev-db`, which a contributor
// running this package's tests directly is just as likely to have used
// instead, only migrates. Idempotent, mirroring seed.sql's own ON CONFLICT
// DO NOTHING guards, so it is safe to run alongside a real seed.
func ensureSeeded(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		insert into app_user (email, display_name)
		select 'ingest-test@scout.local', 'Ingest Test'
		where not exists (select 1 from app_user)
	`)
	if err != nil {
		t.Fatalf("ensureSeeded: app_user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		insert into user_profile (user_id)
		select id from app_user
		where not exists (select 1 from user_profile where user_id = app_user.id)
		limit 1
	`)
	if err != nil {
		t.Fatalf("ensureSeeded: user_profile: %v", err)
	}

	_, err = pool.Exec(ctx, `
		insert into weight_version (version, weights, source, active)
		select 'v1-hand-tuned-2026-08', $1::jsonb, 'hand_tuned', true
		where not exists (select 1 from weight_version where active)
	`, `{
		"priority": {"overall_match": 0.24, "company_quality": 0.14, "learning_opportunity": 0.12,
			"interview_probability": 0.10, "competition_estimate": 0.10, "compensation": 0.09,
			"growth_potential": 0.07, "engineering_culture": 0.06, "ease_of_applying": 0.05,
			"deadline_urgency": 0.03},
		"overall_match": {"skill_match": 0.40, "resume_match": 0.30, "role_family_fit": 0.20, "seniority_fit": 0.10},
		"location_multipliers": {"1": 1.20, "2": 1.05, "3": 1.12, "4": 0.90},
		"freshness_half_life_days": 14, "freshness_floor": 0.55
	}`)
	if err != nil {
		t.Fatalf("ensureSeeded: weight_version: %v", err)
	}
}

func insertIngestTestCompany(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	slug := fmt.Sprintf("ingest-test-co-%d", time.Now().UnixNano())
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::text, $1::text, $1::text, 'seed')
		returning id
	`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from company where id = $1::uuid`, id)
	})
	return id
}

// cleanupJobData deletes everything runIngestion writes for companyID, in
// the only order the schema's FKs allow: job_group.representative_job_id
// references job(id) with no cascade, and job.job_group_id references
// job_group(id) with no cascade either, so the representative reference
// must be broken before job rows can go, and job rows must be gone before
// their job_group can. Without this, a job row from one test run survives
// into the next (job_score cascades off job, but job itself does not
// cascade off company), and Stage 1 dedup — correctly — treats the next
// run's identical fixture URLs as already-seen jobs rather than new ones.
func cleanupJobData(t *testing.T, pool *pgxpool.Pool, companyID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `update job_group set representative_job_id = NULL where company_id = $1::uuid`, companyID); err != nil {
		t.Logf("cleanup: clear representative_job_id: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from job where company_id = $1::uuid`, companyID); err != nil {
		t.Logf("cleanup: delete job: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from job_group where company_id = $1::uuid`, companyID); err != nil {
		t.Logf("cleanup: delete job_group: %v", err)
	}
}

func insertGreenhouseSource(t *testing.T, pool *pgxpool.Pool, companyID pgtype.UUID) pgtype.UUID {
	t.Helper()
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/ingest-test-%d/jobs", time.Now().UnixNano())
	// priority_tier 1 and an epoch next_poll_at: see insertTestSource's
	// comment in scheduler_test.go for why this fixture must outrank the
	// 1,400+ real seeded sources in the shared local dev database.
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash,
		                     max_rps, max_concurrency, base_interval_s, min_interval_s, max_interval_s, next_poll_at, priority_tier)
		values ($1, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'),
		        1000, 10, 900, 300, 86400, '1970-01-01'::timestamptz, 1)
		returning id
	`, companyID, url).Scan(&id)
	if err != nil {
		t.Fatalf("insert greenhouse source: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, id)
	})
	return id
}

func testPipeline() *Pipeline {
	return &Pipeline{
		Adapters: map[padapter.SourceKind]padapter.Adapter{
			padapter.SourceKindGreenhouse: greenhouse.New(nil), // never calls Fetch — see the comment on runIngestion
		},
		Gazetteer: taxonomy.LoadGazetteer(),
		Roles:     taxonomy.LoadRolePatterns(),
		Skills:    taxonomy.LoadSkills(),
	}
}

// TestRunOnce_EndToEnd_GreenhouseFixture is the plan's own verification
// step: seed a source, run the scheduler once against a recorded Greenhouse
// response, and confirm a real job, job_score, and raw_observation appear —
// docs/06 sections 8-9 and docs/09's scoring, exercised together rather
// than each package in isolation.
func TestRunOnce_EndToEnd_GreenhouseFixture(t *testing.T) {
	pool := testPool(t)
	ensureSeeded(t, pool)

	companyID := insertIngestTestCompany(t, pool)
	sourceID := insertGreenhouseSource(t, pool, companyID)
	// Registered after both inserts' own cleanups, so t.Cleanup's LIFO order
	// runs this first — job/job_group must be gone before company/source
	// cleanup tries to delete rows they still reference.
	t.Cleanup(func() { cleanupJobData(t, pool, companyID) })

	body, err := os.ReadFile("../../../../adapters/ats/greenhouse/fixtures/standard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: 200, Body: body, FetchedAt: time.Now()}}
	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// WithBatchLimit(1): the local dev database this test runs against
	// (testPool, not an isolated schema) carries 1,500+ real seeded sources,
	// virtually all already "due" since the real collector has never polled
	// them for real. Without this, RunOnce claims hundreds of them alongside
	// this test's own fixture — and since several of those real rows are
	// also kind='ats_greenhouse', they race this test's own source to insert
	// the exact same 3 canonical URLs from standard.json (job_canonical_url_idx
	// is unique), so this test's own sourceID can lose that race and end up
	// with 0 job rows even though RunOnce succeeded. insertGreenhouseSource's
	// tier-1/epoch fixture plus this batch limit together guarantee the
	// claimed batch is exactly this test's own row.
	sched := New(pool, gate, fetcher, log).WithPipeline(testPipeline()).WithBatchLimit(1)

	n, err := sched.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("RunOnce claimed %d sources, want 1", n)
	}

	var jobCount int
	err = pool.QueryRow(context.Background(),
		`select count(*) from job where primary_source_id = $1::uuid`, sourceID).Scan(&jobCount)
	if err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobCount != 3 {
		t.Fatalf("job rows = %d, want 3 (standard.json has 3 postings)", jobCount)
	}

	var scoreCount int
	err = pool.QueryRow(context.Background(), `
		select count(*)
		from job_score js
		join job j on j.id = js.job_id
		where j.primary_source_id = $1::uuid
	`, sourceID).Scan(&scoreCount)
	if err != nil {
		t.Fatalf("count job_scores: %v", err)
	}
	if scoreCount != 3 {
		t.Fatalf("job_score rows = %d, want 3", scoreCount)
	}

	var obsCount int
	err = pool.QueryRow(context.Background(),
		`select count(*) from raw_observation where source_id = $1::uuid`, sourceID).Scan(&obsCount)
	if err != nil {
		t.Fatalf("count raw_observations: %v", err)
	}
	if obsCount != 3 {
		t.Fatalf("raw_observation rows = %d, want 3", obsCount)
	}

	// Confirm location tiering resolved correctly against real fixture
	// data. This does NOT also assert Bengaluru's priority exceeds
	// Mumbai's — docs/09 section 3.13's "always ranks higher" property
	// (properly tested in apps/collector/internal/scoring's own
	// TestBengaluruAlwaysRanksHigher, with two jobs synthetically
	// identical except for location) only holds when location is the
	// *only* difference. These are two genuinely different real postings
	// with different extracted skills and titles, so skill_match and
	// role_family_fit legitimately differ between them too — after the
	// scoring renormalisation fix, those now carry enough relative weight
	// that a large skill_match gap can outweigh a 1.20-vs-1.05 location
	// multiplier gap. That is correct scoring behavior, not a location bug.
	var bengaluruTier, mumbaiTier int16
	err = pool.QueryRow(context.Background(), `
		select location_tier from job where primary_source_id = $1::uuid and title = 'Software Engineering Intern, Summer 2027'
	`, sourceID).Scan(&bengaluruTier)
	if err != nil {
		t.Fatalf("query bengaluru job: %v", err)
	}
	err = pool.QueryRow(context.Background(), `
		select location_tier from job where primary_source_id = $1::uuid and title = 'Data Engineer Intern'
	`, sourceID).Scan(&mumbaiTier)
	if err != nil {
		t.Fatalf("query mumbai job: %v", err)
	}

	if bengaluruTier != 1 {
		t.Errorf("Bengaluru job location_tier = %d, want 1", bengaluruTier)
	}
	if mumbaiTier != 2 {
		t.Errorf("Mumbai job location_tier = %d, want 2", mumbaiTier)
	}

	// Re-running against unchanged content must not create duplicate jobs
	// — Layer 2 change detection should skip ingestion entirely on the
	// second poll.
	_, err = pool.Exec(context.Background(),
		`update source set next_poll_at = now() - interval '1 minute' where id = $1::uuid`, sourceID)
	if err != nil {
		t.Fatalf("reset next_poll_at: %v", err)
	}
	if _, runErr := sched.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("second RunOnce: %v", runErr)
	}
	err = pool.QueryRow(context.Background(),
		`select count(*) from job where primary_source_id = $1::uuid`, sourceID).Scan(&jobCount)
	if err != nil {
		t.Fatalf("count jobs after second poll: %v", err)
	}
	if jobCount != 3 {
		t.Errorf("job rows after unchanged re-poll = %d, want still 3 (no duplicates)", jobCount)
	}
}

// fakeOwnFetchAdapter is a minimal padapter.Adapter + padapter.OwnFetcher
// double for TestRunOnce_RoutesOwnFetcherAdapterThroughItsOwnFetch below.
// It stands in for adapters/ats/workday without a real network call —
// Workday's own Fetch always ends in a.fetcher.Fetch against a concrete
// *fetch.Fetcher, which (like every adapter in this project) cannot be
// pointed at anything fake from outside its own package. What this test
// needs to prove lives entirely in the scheduler's dispatch logic
// (fetchResult), not in Workday's specific request-building — a fake
// that only tracks whether it was called, and with what hints, is enough.
type fakeOwnFetchAdapter struct {
	fetchCalled bool
	gotHints    padapter.FetchHints
}

func (f *fakeOwnFetchAdapter) Kind() padapter.SourceKind { return padapter.SourceKindWorkday }

func (f *fakeOwnFetchAdapter) RequiresOwnFetch() bool { return true }

func (f *fakeOwnFetchAdapter) Fetch(_ context.Context, _ padapter.Source, hints padapter.FetchHints) (*padapter.RawResponse, error) {
	f.fetchCalled = true
	f.gotHints = hints
	return &padapter.RawResponse{StatusCode: 200, Body: []byte("fake"), FetchedAt: time.Now()}, nil
}

func (f *fakeOwnFetchAdapter) Parse(_ context.Context, _ padapter.Source, _ *padapter.RawResponse) ([]schema.Posting, error) {
	return []schema.Posting{{
		URL: "https://acme.example/jobs/1", TitleRaw: "Software Engineering Intern",
		CompanyNameRaw: "Acme", LocationRaw: "Bengaluru, India", Adapter: "fake-own-fetch",
	}}, nil
}

func (f *fakeOwnFetchAdapter) Validate(_ context.Context, _ padapter.Source, _ []schema.Posting) error {
	return nil
}

// erroringFetcher fails any call — used below to prove the scheduler's
// generic conditional-GET path (s.fetcher) is never reached for a source
// whose adapter implements padapter.OwnFetcher.
type erroringFetcher struct{}

func (erroringFetcher) Fetch(context.Context, fetch.Request) (*fetch.Result, error) {
	return nil, fmt.Errorf("s.fetcher.Fetch must not be called for an OwnFetcher-routed source")
}

// TestRunOnce_RoutesOwnFetcherAdapterThroughItsOwnFetch is the regression
// test for the bug this task found while wiring up Workday-based GCC
// sources: pollOne always performed its own generic conditional GET and
// handed the result straight to Parse, never calling a registered
// adapter's own Fetch at all (confirmed live — Workday's CXS endpoint
// returns HTTP 400 to a plain GET, since it requires a POST with a JSON
// search body; a Workday source would 400 on every poll, forever). Fixed
// via padapter.OwnFetcher (see that interface's own comment) and
// Scheduler.fetchResult. This proves both halves: the adapter's Fetch is
// actually called (with the row's ETag/Last-Modified as hints), and the
// scheduler's own generic fetcher is not — erroringFetcher would fail the
// whole poll if it were.
func TestRunOnce_RoutesOwnFetcherAdapterThroughItsOwnFetch(t *testing.T) {
	pool := testPool(t)
	ensureSeeded(t, pool)

	companyID := insertIngestTestCompany(t, pool)
	url := fmt.Sprintf("https://acme.wd5.myworkdayjobs.com/wday/cxs/acme/External/jobs?t=%d", time.Now().UnixNano())
	// priority_tier 1 and an epoch next_poll_at: see insertTestSource's
	// comment in scheduler_test.go for why this fixture must outrank the
	// 1,400+ real seeded sources in the shared local dev database.
	var sourceID pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash, last_etag, last_modified,
		                     max_rps, max_concurrency, base_interval_s, min_interval_s, max_interval_s, next_poll_at, priority_tier)
		values ($1, 'ats_workday', 'active', 'permitted', $2, digest($2, 'sha256'), 'prior-etag', 'prior-last-modified',
		        1000, 10, 900, 300, 86400, '1970-01-01'::timestamptz, 1)
		returning id
	`, companyID, url).Scan(&sourceID)
	if err != nil {
		t.Fatalf("insert workday source: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, sourceID) })
	t.Cleanup(func() { cleanupJobData(t, pool, companyID) })

	fakeAdapter := &fakeOwnFetchAdapter{}
	pipeline := &Pipeline{
		Adapters:  map[padapter.SourceKind]padapter.Adapter{padapter.SourceKindWorkday: fakeAdapter},
		Gazetteer: taxonomy.LoadGazetteer(),
		Roles:     taxonomy.LoadRolePatterns(),
		Skills:    taxonomy.LoadSkills(),
	}

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	// WithBatchLimit(1) — see the identical comment in
	// TestRunOnce_EndToEnd_GreenhouseFixture above. This test needs it even
	// more: the GCC seed data is heavily kind='ats_workday', so an unbounded
	// batch routes several real sources through the same fakeAdapter
	// concurrently, and whichever's hints happen to be captured last is not
	// guaranteed to be this test's own row — found live as
	// "hints.IfNoneMatch = '', want prior-etag" once real due data existed.
	sched := New(pool, gate, erroringFetcher{}, testLogger()).WithPipeline(pipeline).WithBatchLimit(1)

	n, err := sched.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("RunOnce claimed %d sources, want 1", n)
	}

	if !fakeAdapter.fetchCalled {
		t.Fatal("adapter's own Fetch was never called")
	}
	if fakeAdapter.gotHints.IfNoneMatch != "prior-etag" {
		t.Errorf("hints.IfNoneMatch = %q, want %q", fakeAdapter.gotHints.IfNoneMatch, "prior-etag")
	}
	if fakeAdapter.gotHints.IfModifiedSince != "prior-last-modified" {
		t.Errorf("hints.IfModifiedSince = %q, want %q", fakeAdapter.gotHints.IfModifiedSince, "prior-last-modified")
	}

	var jobCount int
	err = pool.QueryRow(context.Background(),
		`select count(*) from job where company_id = $1::uuid`, companyID).Scan(&jobCount)
	if err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobCount != 1 {
		t.Errorf("job rows = %d, want 1 (the fake adapter's one Parse result)", jobCount)
	}
}

// TestInsertObservation_DuplicateContentHashDoesNotPoisonTransaction is a
// regression test for a real failure found during P1 live verification
// against Stripe's Greenhouse board: a second raw_observation with the
// same (source_id, content_hash) — e.g. the same posting re-observed with
// unchanged content — hits the partition's own unique index as a genuine
// Postgres error (raw_observation cannot use ON CONFLICT here at all; see
// that query's own comment for why). Inside a single shared transaction,
// that error poisons every statement after it until rollback, silently
// dropping the rest of a poll's postings rather than just the one
// duplicate. The fix is processPosting's per-posting savepoint: this test
// proves a duplicate rolls back only its own savepoint (via a direct
// nested-transaction Begin/InsertObservation/InsertObservation/Rollback
// sequence, the same shape processPosting uses) while a sibling statement
// in the OUTER transaction commits successfully.
func TestInsertObservation_DuplicateContentHashDoesNotPoisonTransaction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	companyID := insertTestCompanyForObservation(t, tx, pool)
	sourceID := insertTestSourceForObservation(t, tx, pool, companyID)

	contentHash := []byte("identical-content-hash-for-this-test")
	base := db.InsertObservationParams{
		SourceID:         sourceID,
		Url:              "https://example.test/job/a",
		CanonicalUrl:     "https://example.test/job/a",
		CanonicalUrlHash: []byte("hash-a"),
		ContentHash:      contentHash,
		Payload:          []byte(`{}`),
	}

	// First posting: its own savepoint, inserts cleanly, commits.
	sp1, sp1Err := tx.Begin(ctx)
	if sp1Err != nil {
		t.Fatalf("begin savepoint 1: %v", sp1Err)
	}
	if _, insertErr := db.New(sp1).InsertObservation(ctx, base); insertErr != nil {
		t.Fatalf("first InsertObservation: %v", insertErr)
	}
	if commitErr := sp1.Commit(ctx); commitErr != nil {
		t.Fatalf("commit savepoint 1: %v", commitErr)
	}

	// Second posting: same (source_id, content_hash) — the exact collision
	// found live. Its own savepoint takes the real constraint-violation
	// error and rolls back; that must not affect the outer transaction.
	dup := base
	dup.Url = "https://example.test/job/b"
	dup.CanonicalUrl = "https://example.test/job/b"
	dup.CanonicalUrlHash = []byte("hash-b")

	sp2, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("begin savepoint 2: %v", err)
	}
	if _, dupErr := db.New(sp2).InsertObservation(ctx, dup); dupErr == nil {
		t.Fatal("expected a real constraint-violation error for the duplicate content_hash")
	}
	if err := sp2.Rollback(ctx); err != nil {
		t.Fatalf("rollback savepoint 2: %v", err)
	}

	// The critical assertion: the OUTER transaction must still be usable
	// after savepoint 2's rollback — a poisoned outer transaction would
	// fail this with "current transaction is aborted" (SQLSTATE 25P02),
	// which is exactly what happened in production before this fix.
	var jobGroupCount int
	if err := tx.QueryRow(ctx,
		`select count(*) from source where id = $1::uuid`, sourceID).Scan(&jobGroupCount); err != nil {
		t.Fatalf("query after a sibling savepoint's rollback should still succeed, got: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// insertTestCompanyForObservation inserts via tx (the test's own
// transaction, which it commits on purpose to prove the fix) but registers
// cleanup against pool — tx will already be closed by the time t.Cleanup
// runs, and without this the row leaks into the shared dev database
// permanently. This exact leak happened once already (an earlier version
// of this helper had no cleanup at all) and polluted
// TestRunOnce_EndToEnd_GreenhouseFixture in a later test run by leaving a
// status='active' source the scheduler picked up alongside the real one.
func insertTestCompanyForObservation(t *testing.T, tx pgx.Tx, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	slug := fmt.Sprintf("obs-test-co-%d", time.Now().UnixNano())
	var id pgtype.UUID
	err := tx.QueryRow(context.Background(), `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::text, $1::text, $1::text, 'seed')
		returning id
	`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from company where id = $1::uuid`, id) })
	return id
}

// insertTestSourceForObservation deliberately seeds status='pending_review'
// — this test drives InsertObservation directly, never through
// SelectDueSources, so there is no reason for the scheduler to be able to
// pick this row up, and 'active' is exactly what let the leaked-row bug
// above cross-contaminate an unrelated test.
func insertTestSourceForObservation(t *testing.T, tx pgx.Tx, pool *pgxpool.Pool, companyID pgtype.UUID) pgtype.UUID {
	t.Helper()
	url := fmt.Sprintf("https://example.test/%d/board", time.Now().UnixNano())
	var id pgtype.UUID
	err := tx.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash)
		values ($1, 'ats_greenhouse', 'pending_review', 'permitted', $2, digest($2, 'sha256'))
		returning id
	`, companyID, url).Scan(&id)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, id) })
	return id
}
