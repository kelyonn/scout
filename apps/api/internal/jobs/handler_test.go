package jobs

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/packages/queue"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	candidates := []string{
		os.Getenv("SCOUT_TEST_DATABASE_URL"),
		"postgres://scout:scout_local_dev_only@localhost:5433/scout?sslmode=disable",
		"postgres://scout:scout_ci@localhost:5432/scout?sslmode=disable",
	}
	for _, url := range candidates {
		if url == "" {
			continue
		}
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err = pool.Ping(ctx)
		cancel()
		if err != nil {
			pool.Close()
			continue
		}
		t.Cleanup(pool.Close)
		return pool
	}
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping jobs handler tests")
	return nil
}

func testHandler(pool *pgxpool.Pool) *Handler {
	q, err := queue.New(pool)
	if err != nil {
		panic(err)
	}
	return New(pool, q, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// slug returns a short, deterministic identifier per test name, so
// concurrent test runs (or a leftover row from a previous crashed run)
// never collide on a company slug — company.slug is unique.
var slugCounter atomic.Uint64

// slug is unique per call, not just per test — several tests insert more
// than one fixture (e.g. one internship, one senior row to prove a filter
// actually excludes something), and those calls share the same t.Name()
// within the same second.
func slug(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	n := slugCounter.Add(1)
	return "jobsfeed-test-" + time.Now().UTC().Format("150405") + "-" + hex32(h.Sum32()) + "-" + hex32(uint32(n))
}

func hex32(n uint32) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = hexDigits[n&0xF]
		n >>= 4
	}
	return string(b)
}

type jobFixture struct {
	companyID, sourceID, groupID, jobID string
}

// insertJobFixture inserts a company/source/job_group/job/job_score row
// tuple directly — the minimum a row needs to satisfy SelectJobFeed's
// joins and WHERE clause (status='open', is_software=TRUE, paid='paid').
// Cleaned up in t.Cleanup regardless of pass/fail.
func insertJobFixture(t *testing.T, pool *pgxpool.Pool, opts fixtureOpts) jobFixture {
	t.Helper()
	ctx := context.Background()
	s := slug(t)

	var companyID string
	err := pool.QueryRow(ctx, `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::citext, $1::text, $1::citext, 'seed') returning id
	`, s).Scan(&companyID)
	if err != nil {
		t.Fatalf("insert company: %v", err)
	}

	url := "https://example.test/" + s
	var sourceID string
	// next_poll_at explicit and far in the future — the default (now()) makes
	// this row a candidate for apps/collector/internal/scheduler's RunOnce,
	// a real source of flaky cross-package failures there since `go test
	// ./...` runs packages concurrently against the same shared test DB.
	err = pool.QueryRow(ctx, `
		insert into source (company_id, kind, status, legal_posture, url, url_hash, next_poll_at)
		values ($1::uuid, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'), now() + interval '10 years')
		returning id
	`, companyID, url).Scan(&sourceID)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}

	var groupID string
	if groupErr := pool.QueryRow(ctx, `insert into job_group (company_id) values ($1::uuid) returning id`, companyID).
		Scan(&groupID); groupErr != nil {
		t.Fatalf("insert job_group: %v", groupErr)
	}

	canonicalURL := url + "/job"
	title := opts.title
	if title == "" {
		title = "Software Engineering Intern"
	}
	roleFamily := opts.roleFamily
	if roleFamily == "" {
		roleFamily = "swe.general"
	}
	seniority := opts.seniority
	if seniority == "" {
		seniority = "internship"
	}
	workMode := opts.workMode
	if workMode == "" {
		workMode = "remote"
	}

	var jobID string
	err = pool.QueryRow(ctx, `
		insert into job (
			job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, apply_url, is_software, paid, status,
			role_family, seniority, work_mode
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4, digest($4, 'sha256'),
			digest($4, 'sha256'), $5, $6, $4, TRUE, 'paid', 'open',
			$7::role_family, $8::seniority, $9::work_mode
		) returning id
	`, groupID, companyID, sourceID, canonicalURL, title, title, roleFamily, seniority, workMode).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	if _, repErr := pool.Exec(ctx, `update job_group set representative_job_id = $1::uuid where id = $2::uuid`,
		jobID, groupID); repErr != nil {
		t.Fatalf("set representative: %v", repErr)
	}

	var userID string
	if userErr := pool.QueryRow(ctx, `select id from app_user order by created_at asc limit 1`).Scan(&userID); userErr != nil {
		t.Fatalf("get sole user (run `make seed`): %v", userErr)
	}

	_, err = pool.Exec(ctx, `
		insert into job_score (
			job_id, user_id, weight_version, overall_match, skill_match, resume_match, company_quality,
			compensation, learning_opportunity, engineering_culture, growth_potential, interview_probability,
			competition_estimate, ease_of_applying, deadline_urgency, priority, location_multiplier,
			freshness_multiplier, score_inputs
		) values (
			$1::uuid, $2::uuid, $3, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, $4::smallint, 1.0, 1.0, '{}'
		)
	`, jobID, userID, weightVersion, opts.priority)
	if err != nil {
		t.Fatalf("insert job_score: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		// user_job_state cascades on job_group deletion (its FK), but
		// user_job_state_event deliberately has none (docs/03's own
		// schema: an append-only history table, never joined against by
		// FK) — cleaned up explicitly here so state_test.go's fixtures
		// don't leak rows into the next run.
		_, _ = pool.Exec(ctx, `delete from user_job_state_event where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `update job_group set representative_job_id = NULL where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job_group where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from source where company_id = $1::uuid`, companyID)
		_, _ = pool.Exec(ctx, `delete from company where id = $1::uuid`, companyID)
	})

	return jobFixture{companyID: companyID, sourceID: sourceID, groupID: groupID, jobID: jobID}
}

type fixtureOpts struct {
	title      string
	roleFamily string
	seniority  string
	workMode   string
	priority   int16
}

func doList(t *testing.T, h *Handler, rawQuery string) feedResponse {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/jobs?"+rawQuery, nil)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp feedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func containsGroupID(items []feedItem, groupID string) bool {
	for _, it := range items {
		if it.JobGroupID == groupID {
			return true
		}
	}
	return false
}

func TestList_ReturnsInsertedJob(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 80})

	resp := doList(t, h, "min_priority=1&limit=100")

	if !containsGroupID(resp.Data, fx.groupID) {
		t.Errorf("expected job_group %s in feed, got %d items", fx.groupID, len(resp.Data))
	}
}

func TestList_FiltersBySeniority(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	intern := insertJobFixture(t, pool, fixtureOpts{seniority: "internship", priority: 70})
	senior := insertJobFixture(t, pool, fixtureOpts{seniority: "senior", priority: 70})

	resp := doList(t, h, "seniority=internship&limit=200")

	if !containsGroupID(resp.Data, intern.groupID) {
		t.Error("expected the internship fixture in a seniority=internship filtered feed")
	}
	if containsGroupID(resp.Data, senior.groupID) {
		t.Error("did not expect the senior fixture in a seniority=internship filtered feed")
	}
}

func TestList_FiltersByRoleFamily(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	backend := insertJobFixture(t, pool, fixtureOpts{roleFamily: "swe.backend", priority: 60})
	ml := insertJobFixture(t, pool, fixtureOpts{roleFamily: "swe.ml", priority: 60})

	resp := doList(t, h, "role_family=swe.backend&limit=200")

	if !containsGroupID(resp.Data, backend.groupID) {
		t.Error("expected the swe.backend fixture in a role_family=swe.backend filtered feed")
	}
	if containsGroupID(resp.Data, ml.groupID) {
		t.Error("did not expect the swe.ml fixture in a role_family=swe.backend filtered feed")
	}
}

func TestList_MinPriorityExcludesLowerScored(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	low := insertJobFixture(t, pool, fixtureOpts{priority: 10})
	high := insertJobFixture(t, pool, fixtureOpts{priority: 95})

	resp := doList(t, h, "min_priority=50&limit=200")

	if containsGroupID(resp.Data, low.groupID) {
		t.Error("did not expect a priority=10 job when min_priority=50")
	}
	if !containsGroupID(resp.Data, high.groupID) {
		t.Error("expected the priority=95 job when min_priority=50")
	}
}

func TestList_ResultsAreSortedByPriorityDescending(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	insertJobFixture(t, pool, fixtureOpts{priority: 55})
	insertJobFixture(t, pool, fixtureOpts{priority: 99})
	insertJobFixture(t, pool, fixtureOpts{priority: 77})

	resp := doList(t, h, "min_priority=1&limit=200")

	for i := 1; i < len(resp.Data); i++ {
		if resp.Data[i].Priority > resp.Data[i-1].Priority {
			t.Fatalf("results not sorted descending by priority at index %d: %d > %d",
				i, resp.Data[i].Priority, resp.Data[i-1].Priority)
		}
	}
}

func TestList_CursorPaginationCoversAllResultsWithNoDuplicates(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	var groupIDs []string
	var companyFilter string
	for i := 0; i < 5; i++ {
		fx := insertJobFixture(t, pool, fixtureOpts{priority: int16(60 + i)})
		groupIDs = append(groupIDs, fx.groupID)
		companyFilter += "&company_id=" + fx.companyID
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 20 {
			t.Fatal("paginated more than 20 times — likely an infinite loop")
		}
		// company_id scopes this to exactly the 5 fixtures above — without
		// it, this walks every real job already in a dev database (there
		// can be thousands), which is what actually caused the "infinite
		// loop" this comment used to report.
		q := "min_priority=1&limit=2" + companyFilter
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		resp := doList(t, h, q)
		for _, it := range resp.Data {
			if seen[it.JobGroupID] {
				t.Errorf("job_group %s appeared on more than one page", it.JobGroupID)
			}
			seen[it.JobGroupID] = true
		}
		if !resp.Page.HasMore {
			break
		}
		if resp.Page.NextCursor == "" {
			t.Fatal("has_more=true but next_cursor is empty")
		}
		cursor = resp.Page.NextCursor
	}

	for _, id := range groupIDs {
		if !seen[id] {
			t.Errorf("job_group %s was never seen across any page", id)
		}
	}
}

func TestList_InvalidCursorIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/jobs?cursor=not-valid-base64!!!", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestList_DefaultLimitIsApplied(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/jobs", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp feedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) > defaultLimit {
		t.Errorf("got %d items, want at most defaultLimit=%d", len(resp.Data), defaultLimit)
	}
}

func TestList_LimitIsCappedAtMax(t *testing.T) {
	if got := parseLimit("99999"); got != maxLimit {
		t.Errorf("parseLimit(99999) = %d, want maxLimit=%d", got, maxLimit)
	}
}

func TestList_FiltersByCompanyID(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	a := insertJobFixture(t, pool, fixtureOpts{priority: 60})
	b := insertJobFixture(t, pool, fixtureOpts{priority: 60})

	resp := doList(t, h, "min_priority=1&limit=200&company_id="+a.companyID)

	if !containsGroupID(resp.Data, a.groupID) {
		t.Error("expected fixture a's job when filtering by its own company_id")
	}
	if containsGroupID(resp.Data, b.groupID) {
		t.Error("did not expect fixture b's job when filtering by fixture a's company_id")
	}
}

func TestList_UnpaidExcludedByDefault(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	ctx := context.Background()

	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	if _, err := pool.Exec(ctx, `update job set paid = 'unpaid' where id = $1::uuid`, fx.jobID); err != nil {
		t.Fatalf("set unpaid: %v", err)
	}

	resp := doList(t, h, "min_priority=1&limit=200")

	if containsGroupID(resp.Data, fx.groupID) {
		t.Error("an explicitly unpaid job should not appear in the feed")
	}
}

func doDetail(t *testing.T, h *Handler, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/jobs/"+groupID, nil)
	r.SetPathValue("group_id", groupID)
	w := httptest.NewRecorder()
	h.Detail(w, r)
	return w
}

func TestDetail_ReturnsFullJobWithAllScores(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{title: "Backend Intern", priority: 88})

	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp detailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Title != "Backend Intern" {
		t.Errorf("Title = %q, want %q", resp.Title, "Backend Intern")
	}
	if resp.SourceCount < 1 {
		t.Errorf("SourceCount = %d, want at least 1", resp.SourceCount)
	}
	if resp.Scores == nil {
		t.Fatal("expected Scores to be populated — the fixture inserts a job_score row")
	}
	if resp.Scores.Priority != 88 {
		t.Errorf("Scores.Priority = %d, want 88", resp.Scores.Priority)
	}
	// docs/12 section 4.3: "All thirteen scores are shown, always."
	if resp.Scores.OverallMatch == nil || resp.Scores.SkillMatch == nil ||
		resp.Scores.ResumeMatch == nil || resp.Scores.CompanyQuality == nil ||
		resp.Scores.Compensation == nil || resp.Scores.LearningOpportunity == nil ||
		resp.Scores.EngineeringCulture == nil || resp.Scores.GrowthPotential == nil ||
		resp.Scores.InterviewProbability == nil || resp.Scores.CompetitionEstimate == nil ||
		resp.Scores.EaseOfApplying == nil || resp.Scores.DeadlineUrgency == nil {
		t.Errorf("expected all thirteen subscores present, got: %+v", resp.Scores)
	}
}

func TestDetail_UnknownGroupIDIs404(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	w := doDetail(t, h, "00000000-0000-0000-0000-000000000000")
	if w.Code != 404 {
		t.Errorf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

func TestDetail_InvalidUUIDIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	w := doDetail(t, h, "not-a-uuid")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDetail_NoScoreRowLeavesScoresNil(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 0})

	// Delete the job_score row the fixture inserts — proving a job that
	// genuinely has no score yet (e.g. scoring hasn't run) reports
	// scores: null rather than a fabricated all-zero block a client could
	// mistake for a real (if bad) score.
	if _, err := pool.Exec(context.Background(),
		`delete from job_score where job_id = $1::uuid`, fx.jobID); err != nil {
		t.Fatalf("delete job_score: %v", err)
	}

	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp detailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Scores != nil {
		t.Errorf("expected Scores = nil with no job_score row, got %+v", resp.Scores)
	}
}

func TestDetail_NoSummaryYetEnqueuesOneAndMarksPending(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from river_job where kind = 'brain_deep' and args->>'job_id' = $1`, fx.jobID)
	})

	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp detailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AISummary != nil {
		t.Errorf("expected ai_summary = nil for a fresh fixture, got %q", *resp.AISummary)
	}
	if !resp.SummaryPending {
		t.Error("expected summary_pending = true when ai_summary is not yet generated")
	}

	var queuedCount int
	err := pool.QueryRow(context.Background(), `
		select count(*) from river_job
		where kind = 'brain_deep' and args->>'task' = 'summarize' and args->>'job_id' = $1
	`, fx.jobID).Scan(&queuedCount)
	if err != nil {
		t.Fatalf("query river_job: %v", err)
	}
	if queuedCount != 1 {
		t.Errorf("expected exactly 1 summarize job enqueued, got %d", queuedCount)
	}
}

func TestDetail_RepeatedViewsDoNotEnqueueDuplicateSummaryJobs(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from river_job where kind = 'brain_deep' and args->>'job_id' = $1`, fx.jobID)
	})

	for i := 0; i < 3; i++ {
		w := doDetail(t, h, fx.groupID)
		if w.Code != 200 {
			t.Fatalf("view %d: status = %d, body = %s", i, w.Code, w.Body.String())
		}
	}

	var queuedCount int
	err := pool.QueryRow(context.Background(), `
		select count(*) from river_job
		where kind = 'brain_deep' and args->>'task' = 'summarize' and args->>'job_id' = $1
	`, fx.jobID).Scan(&queuedCount)
	if err != nil {
		t.Fatalf("query river_job: %v", err)
	}
	if queuedCount != 1 {
		t.Errorf("expected exactly 1 summarize job across 3 views while it's still outstanding, got %d", queuedCount)
	}
}

func TestDetail_ExistingSummaryIsReturnedAndNothingIsEnqueued(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from river_job where kind = 'brain_deep' and args->>'job_id' = $1`, fx.jobID)
	})

	const wantSummary = "Acme builds widgets. This backend role involves Go services. Requires a CS background. Pay: not disclosed."
	_, err := pool.Exec(context.Background(),
		`update job set ai_summary = $1, ai_summary_model = 'test-model', ai_summary_generated_at = now() where id = $2::uuid`,
		wantSummary, fx.jobID)
	if err != nil {
		t.Fatalf("set ai_summary: %v", err)
	}

	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp detailResponse
	if decodeErr := json.Unmarshal(w.Body.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if resp.AISummary == nil || *resp.AISummary != wantSummary {
		t.Errorf("AISummary = %v, want %q", resp.AISummary, wantSummary)
	}
	if resp.SummaryPending {
		t.Error("expected summary_pending = false when a summary already exists")
	}

	var queuedCount int
	err = pool.QueryRow(context.Background(), `
		select count(*) from river_job
		where kind = 'brain_deep' and args->>'task' = 'summarize' and args->>'job_id' = $1
	`, fx.jobID).Scan(&queuedCount)
	if err != nil {
		t.Fatalf("query river_job: %v", err)
	}
	if queuedCount != 0 {
		t.Errorf("expected no summarize job enqueued when a summary already exists, got %d", queuedCount)
	}
}

func TestDetail_IncludesCompanyInfo(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 40})

	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp detailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Company.ID != fx.companyID {
		t.Errorf("Company.ID = %q, want %q", resp.Company.ID, fx.companyID)
	}
}
