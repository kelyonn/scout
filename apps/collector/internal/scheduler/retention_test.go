package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/kelyon/scout/packages/db/gen"
)

// Retention tests run inside a transaction that is always rolled back, against
// the shared dev database. Fixtures are dated 2000 and every grace window in
// testRetention is a year, so only the fixtures — never live rows — qualify as
// cold, and nothing is ever committed.
func testRetention() RetentionConfig {
	year := 15 * 365 * 24 * time.Hour
	return RetentionConfig{
		ClosedGrace: year, IrrelevantGrace: year, StaleOpenAge: year, UnseenAge: year,
		JobRawPayloadAge: year, ObservationPayloadAge: year, QueueFinalizedAge: year, QueueStaleAge: year,
		BatchSize: 2, MaxBatches: 10, // tiny batches exercise the drain loop
	}
}

type retentionFixture struct {
	user                                         string
	coldGroup, savedGroup, notifiedGroup, hotGrp string
	coldJob, savedJob, notifiedJob, hotJob       string
	closedJob, extraColdJob1, extraColdJob2      string
}

// software sets both is_software and in_scope (ADR-020 made them distinct
// columns, but every family this fixture exercises is swe.*-equivalent, so
// the two are always the same value here).
func (f *retentionFixture) job(ctx context.Context, t *testing.T, tx pgx.Tx, company, source, group string, software bool, status, seen string) string {
	t.Helper()
	var id string
	err := tx.QueryRow(ctx, `
		insert into job (job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, description_html, description_text, description_stripped,
			requirements_text, apply_url, is_software, in_scope, status, first_seen_at, last_seen_at, posted_at,
			simhash, raw_payload)
		values ($1, $2, $3, 'https://x.test/'||gen_random_uuid(), gen_random_bytes(16), gen_random_bytes(16),
			'Engineer', 'engineer', '<p>hi</p>', 'hi', 'hi', 'reqs', 'https://x.test/apply', $4, $4, $5::job_status,
			$6::timestamptz, $6::timestamptz, $6::timestamptz, 42, '{"k":"v"}')
		returning id`, group, company, source, software, status, seen).Scan(&id)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into job_score (job_id, user_id, weight_version, overall_match, skill_match, resume_match,
			company_quality, compensation, learning_opportunity, engineering_culture, growth_potential,
			interview_probability, competition_estimate, ease_of_applying, deadline_urgency, priority,
			explanation, score_inputs)
		select $1, $2, version, 50,50,50,50,50,50,50,50,50,50,50,50, 77, 'because', '{"a":1}'
		from weight_version limit 1`, id, f.user); err != nil {
		t.Fatalf("insert job_score: %v", err)
	}
	return id
}

func setupRetention(ctx context.Context, t *testing.T, tx pgx.Tx) *retentionFixture {
	t.Helper()
	f := &retentionFixture{}
	var company, source string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tx.QueryRow(ctx, `insert into app_user (email) values ('retention-test@example.invalid') returning id`).Scan(&f.user))
	must(tx.QueryRow(ctx, `insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ('retention-test', 'Retention Test', 'retention test', 'seed') returning id`).Scan(&company))
	must(tx.QueryRow(ctx, `insert into source (kind, url, url_hash)
		select (enum_range(null::source_kind))[1], 'https://retention.test', gen_random_bytes(16) returning id`).Scan(&source))
	for _, g := range []*string{&f.coldGroup, &f.savedGroup, &f.notifiedGroup, &f.hotGrp} {
		must(tx.QueryRow(ctx, `insert into job_group (company_id) values ($1) returning id`, company).Scan(g))
	}
	old := "2000-01-01T00:00:00Z"
	f.coldJob = f.job(ctx, t, tx, company, source, f.coldGroup, false, "open", old)
	f.extraColdJob1 = f.job(ctx, t, tx, company, source, f.coldGroup, false, "open", old)
	f.extraColdJob2 = f.job(ctx, t, tx, company, source, f.coldGroup, false, "open", old)
	f.closedJob = f.job(ctx, t, tx, company, source, f.coldGroup, true, "closed", old)
	f.savedJob = f.job(ctx, t, tx, company, source, f.savedGroup, false, "open", old)
	f.notifiedJob = f.job(ctx, t, tx, company, source, f.notifiedGroup, false, "open", old)
	f.hotJob = f.job(ctx, t, tx, company, source, f.hotGrp, true, "open", time.Now().UTC().Format(time.RFC3339))

	must2 := func(_ any, err error) { t.Helper(); must(err) }
	must2(tx.Exec(ctx, `insert into user_job_state (user_id, job_group_id, state, notes, rating)
		values ($1, $2, (enum_range(null::application_state))[2], 'my private note', 4)`, f.user, f.savedGroup))
	must2(tx.Exec(ctx, `insert into notification (user_id, job_group_id, trigger, urgency, payload)
		values ($1, $2, (enum_range(null::notification_trigger))[1], 'instant', '{}')`, f.user, f.notifiedGroup))
	return f
}

func heavy(ctx context.Context, t *testing.T, tx pgx.Tx, id string) (hasText bool, simhash int64, payload string, score string) {
	t.Helper()
	var sh *int64
	if err := tx.QueryRow(ctx, `select description_text is not null or description_html is not null
		or description_stripped is not null or requirements_text is not null, simhash, raw_payload::text from job where id=$1`, id).
		Scan(&hasText, &sh, &payload); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if sh != nil {
		simhash = *sh
	}
	if err := tx.QueryRow(ctx, `select score_inputs::text from job_score where job_id=$1`, id).Scan(&score); err != nil {
		t.Fatalf("read score: %v", err)
	}
	return
}

func TestRetention_CompactsColdJobsAndNeverProtectedOnes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	f := setupRetention(ctx, t, tx)
	var res RetentionResult
	if err := applyRetention(ctx, db.New(tx), testRetention(), &res); err != nil {
		t.Fatalf("applyRetention: %v", err)
	}

	// 4 cold jobs in the unprotected group (3 non-software + 1 closed). BatchSize 2
	// means the drain loop had to run more than once to get all of them.
	if res.JobsCompacted != 4 {
		t.Errorf("JobsCompacted = %d, want 4", res.JobsCompacted)
	}
	for name, id := range map[string]string{"cold": f.coldJob, "extra1": f.extraColdJob1, "extra2": f.extraColdJob2, "closed": f.closedJob} {
		has, sim, payload, score := heavy(ctx, t, tx, id)
		if has || payload != "{}" || score != "{}" {
			t.Errorf("%s job not compacted: bulky=%v payload=%s score=%s", name, has, payload, score)
		}
		if sim != 42 {
			t.Errorf("%s job lost its simhash (%d): dedup identity must survive compaction", name, sim)
		}
	}

	for name, id := range map[string]string{"saved": f.savedJob, "notified": f.notifiedJob, "hot": f.hotJob} {
		has, _, payload, score := heavy(ctx, t, tx, id)
		if !has || payload == "{}" || score == "{}" {
			t.Errorf("%s job must stay whole: bulky=%v payload=%s score=%s", name, has, payload, score)
		}
	}

	// Identity and user data are untouched.
	var state, notes string
	var rating int
	if err := tx.QueryRow(ctx, `select state::text, notes, rating from user_job_state where job_group_id=$1`, f.savedGroup).
		Scan(&state, &notes, &rating); err != nil || notes != "my private note" || rating != 4 {
		t.Errorf("saved state row damaged: state=%s notes=%q rating=%d err=%v", state, notes, rating, err)
	}
	var groups, jobs int
	_ = tx.QueryRow(ctx, `select count(*) from job_group where id = any($1)`,
		[]string{f.coldGroup, f.savedGroup, f.notifiedGroup, f.hotGrp}).Scan(&groups)
	_ = tx.QueryRow(ctx, `select count(*) from job where id = any($1)`,
		[]string{f.coldJob, f.extraColdJob1, f.extraColdJob2, f.closedJob, f.savedJob, f.notifiedJob, f.hotJob}).Scan(&jobs)
	if groups != 4 || jobs != 7 {
		t.Errorf("retention deleted identity rows: groups=%d (want 4) jobs=%d (want 7)", groups, jobs)
	}

	// Idempotent: a second pass has nothing left to do.
	var again RetentionResult
	if err := applyRetention(ctx, db.New(tx), testRetention(), &again); err != nil {
		t.Fatal(err)
	}
	if again.JobsCompacted != 0 || again.ScoresTrimmed != 0 {
		t.Errorf("second pass should be a no-op, got %+v", again)
	}
}

// A group becomes protected by ANY of its members' sibling state: protecting
// by group, not by job, is what keeps a saved opportunity's other source
// listings whole.
func TestRetention_ProtectionIsByGroup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	f := setupRetention(ctx, t, tx)
	if _, err := tx.Exec(ctx, `insert into user_job_state (user_id, job_group_id) values ($1, $2)`, f.user, f.coldGroup); err != nil {
		t.Fatal(err)
	}
	var res RetentionResult
	if err := applyRetention(ctx, db.New(tx), testRetention(), &res); err != nil {
		t.Fatal(err)
	}
	if res.JobsCompacted != 0 {
		t.Errorf("JobsCompacted = %d, want 0: every group now has user state or a notification", res.JobsCompacted)
	}
}

func TestRetention_ObservationsScrubbedRowsKept(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	f := setupRetention(ctx, t, tx)
	var source string
	if err := tx.QueryRow(ctx, `select primary_source_id from job where id=$1`, f.hotJob).Scan(&source); err != nil {
		t.Fatal(err)
	}
	// 2000 has no partition, so it lands in raw_observation_default (rolled back with the tx).
	ins := func(jobID *string, hash string) {
		if _, err := tx.Exec(ctx, `insert into raw_observation (source_id, observed_at, url, canonical_url,
			canonical_url_hash, content_hash, payload, job_id)
			values ($1, '2000-01-01', 'u', 'u', decode('00','hex'), decode($2,'hex'), '{"big":"payload"}', $3)`,
			source, hash, jobID); err != nil {
			t.Fatalf("insert observation: %v", err)
		}
	}
	ins(&f.hotJob, "01") // processed -> scrub
	ins(nil, "02")       // no job, no error, not processed -> must keep payload

	var res RetentionResult
	if err := applyRetention(ctx, db.New(tx), testRetention(), &res); err != nil {
		t.Fatal(err)
	}
	if res.PayloadsScrubbed != 1 {
		t.Errorf("PayloadsScrubbed = %d, want 1", res.PayloadsScrubbed)
	}
	var rows, full int
	_ = tx.QueryRow(ctx, `select count(*), count(*) filter (where payload <> '{}') from raw_observation
		where source_id=$1 and observed_at='2000-01-01'`, source).Scan(&rows, &full)
	if rows != 2 || full != 1 {
		t.Errorf("rows=%d full=%d, want 2 rows kept (idempotency keys) and exactly 1 unprocessed payload intact", rows, full)
	}
}

func TestRetention_QueuePruning(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, r := range []struct{ state, finalized string }{
		{"completed", "now() - interval '30 years'"}, // old, finished -> pruned
		{"completed", "now()"},                       // recent, finished -> kept
		{"available", ""},                            // old, unclaimed -> pruned
	} {
		q := `insert into river_job (state, kind, args, created_at, finalized_at) values ($1::river_job_state, 'retention_test', '{}', now() - interval '30 years', `
		if r.finalized == "" {
			q += `NULL)`
		} else {
			q += r.finalized + `)`
		}
		if _, err := tx.Exec(ctx, q, r.state); err != nil {
			t.Fatalf("insert river_job: %v", err)
		}
	}
	var res RetentionResult
	if err := applyRetention(ctx, db.New(tx), testRetention(), &res); err != nil {
		t.Fatal(err)
	}
	if res.QueueFinalizedPruned != 1 || res.QueueStalePruned != 1 {
		t.Errorf("finalized=%d stale=%d, want 1 and 1", res.QueueFinalizedPruned, res.QueueStalePruned)
	}
	var left int
	_ = tx.QueryRow(ctx, `select count(*) from river_job where kind='retention_test'`).Scan(&left)
	if left != 1 {
		t.Errorf("left = %d, want 1 (the recent completed row)", left)
	}
}

func TestDefaultRetention_AggressiveOnlyTightens(t *testing.T) {
	d, a := DefaultRetention(), DefaultRetention().Aggressive()
	if a.ClosedGrace > d.ClosedGrace || a.IrrelevantGrace > d.IrrelevantGrace ||
		a.StaleOpenAge > d.StaleOpenAge || a.ObservationPayloadAge > d.ObservationPayloadAge ||
		a.QueueStaleAge > d.QueueStaleAge {
		t.Errorf("Aggressive() must never lengthen a window: default=%+v aggressive=%+v", d, a)
	}
}

// Retention rewrites data, so a Scheduler built without WithRetention must not
// run it — otherwise any test or tool that calls RunMaintenance against a
// shared database would compact its rows.
func TestRunRetention_OptIn(t *testing.T) {
	s := New(nil, nil, nil, testLogger()) // nil pool: any query would panic
	s.runRetention(context.Background())
	if s.retention != nil {
		t.Fatal("retention must be nil unless WithRetention is called")
	}
	if got := New(nil, nil, nil, testLogger()).WithRetention(DefaultRetention()).retention; got == nil {
		t.Fatal("WithRetention must enable retention")
	}
}
