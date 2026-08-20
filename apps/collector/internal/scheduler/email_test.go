package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/apps/collector/internal/emailalert"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/packages/fetch"
)

func TestIngestEmailAlert_NoPipelineIsANoop(t *testing.T) {
	sched := New(nil, &fakeGate{}, &fakeFetcher{}, testLogger())
	isNew, err := sched.IngestEmailAlert(context.Background(), "linkedin", emailalert.ExtractedPosting{
		TitleRaw: "Backend Intern", CompanyNameRaw: "Acme", TrackingURL: "https://example.com/jobs/1",
	})
	if err != nil {
		t.Fatalf("IngestEmailAlert: %v", err)
	}
	if isNew {
		t.Error("isNew = true with no pipeline configured, want false")
	}
}

func TestIngestEmailAlert_RejectsIncompletePosting(t *testing.T) {
	sched := New(nil, &fakeGate{}, &fakeFetcher{}, testLogger()).WithPipeline(testPipeline())
	if _, err := sched.IngestEmailAlert(context.Background(), "linkedin", emailalert.ExtractedPosting{
		TitleRaw: "", CompanyNameRaw: "Acme", TrackingURL: "https://example.com/jobs/1",
	}); err == nil {
		t.Error("want an error for a posting with no title")
	}
	if _, err := sched.IngestEmailAlert(context.Background(), "linkedin", emailalert.ExtractedPosting{
		TitleRaw: "Backend Intern", CompanyNameRaw: "", TrackingURL: "https://example.com/jobs/1",
	}); err == nil {
		t.Error("want an error for a posting with no company name")
	}
}

// cleanupEmailCompany deletes everything IngestEmailAlert wrote for a
// company created purely for this test — same FK order concern
// cleanupJobData already documents (job_group before job before company),
// plus source, which HTTP-sourced tests never had to delete because
// insertGreenhouseSource registers its own cleanup; this path creates its
// source itself, so nothing else will.
func cleanupEmailCompany(t *testing.T, pool *pgxpool.Pool, companyID pgtype.UUID) {
	t.Helper()
	cleanupJobData(t, pool, companyID)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `delete from source where company_id = $1::uuid`, companyID); err != nil {
		t.Logf("cleanup: delete source: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from company where id = $1::uuid`, companyID); err != nil {
		t.Logf("cleanup: delete company: %v", err)
	}
}

// TestIngestEmailAlert_EndToEnd proves the whole email-alert write path:
// company and source rows get created (with legal_posture = 'email_only',
// keeping SelectDueSources from ever polling them — see
// packages/db/queries/emailalert.sql's own comment), the tracking link
// gets resolved to a canonical URL through the fake fetcher's FinalURL,
// and the posting comes out the other end of processPosting as a real job
// with a job_score, exactly like an HTTP-sourced posting would.
func TestIngestEmailAlert_EndToEnd(t *testing.T) {
	pool := testPool(t)
	ensureSeeded(t, pool)

	companyName := fmt.Sprintf("Acme Email Test %d", time.Now().UnixNano())
	canonicalURL := fmt.Sprintf("https://boards.greenhouse.io/acme/jobs/%d", time.Now().UnixNano())
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: 200, FinalURL: canonicalURL, FetchedAt: time.Now()}}
	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}

	sched := New(pool, gate, fetcher, testLogger()).WithPipeline(testPipeline())

	posting := emailalert.ExtractedPosting{
		TitleRaw:       "Backend Engineering Intern",
		CompanyNameRaw: companyName,
		LocationRaw:    "Bengaluru, Karnataka",
		TrackingURL:    "https://www.linkedin.com/comm/jobs/view/123456/?trackingId=abc",
	}

	isNew, err := sched.IngestEmailAlert(context.Background(), "linkedin", posting)
	if err != nil {
		t.Fatalf("IngestEmailAlert: %v", err)
	}
	if !isNew {
		t.Fatal("isNew = false on first ingest, want true")
	}

	var companyID pgtype.UUID
	var slug, discoveredVia string
	err = pool.QueryRow(context.Background(),
		`select id, slug::text, discovered_via from company where canonical_name = $1`, companyName,
	).Scan(&companyID, &slug, &discoveredVia)
	if err != nil {
		t.Fatalf("query company: %v", err)
	}
	t.Cleanup(func() { cleanupEmailCompany(t, pool, companyID) })
	if discoveredVia != "email_alert" {
		t.Errorf("discovered_via = %q, want email_alert", discoveredVia)
	}

	var legalPosture, kind string
	err = pool.QueryRow(context.Background(),
		`select legal_posture::text, kind::text from source where company_id = $1::uuid`, companyID,
	).Scan(&legalPosture, &kind)
	if err != nil {
		t.Fatalf("query source: %v", err)
	}
	if legalPosture != "email_only" {
		t.Errorf("legal_posture = %q, want email_only (this is what keeps SelectDueSources from polling it)", legalPosture)
	}
	if kind != "email_alert" {
		t.Errorf("kind = %q, want email_alert", kind)
	}

	var jobCount int
	err = pool.QueryRow(context.Background(),
		`select count(*) from job where company_id = $1::uuid and title = $2`, companyID, posting.TitleRaw,
	).Scan(&jobCount)
	if err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("job rows = %d, want 1", jobCount)
	}

	var scoreCount int
	err = pool.QueryRow(context.Background(), `
		select count(*) from job_score js join job j on j.id = js.job_id where j.company_id = $1::uuid
	`, companyID).Scan(&scoreCount)
	if err != nil {
		t.Fatalf("count job_score: %v", err)
	}
	if scoreCount != 1 {
		t.Errorf("job_score rows = %d, want 1", scoreCount)
	}

	// Re-ingesting the identical posting must resolve to the same company,
	// the same source, and the same job — not a duplicate. slug's
	// on-conflict and url_hash's own unique index are what make the
	// company/source halves of this idempotent; Stage 1 dedup on
	// canonical_url is what makes the job half of it idempotent. The
	// second call also hits raw_observation's per-partition unique index
	// on (source_id, content_hash) — a "genuine duplicate", exactly like
	// re-observing an unchanged HTTP posting — which IngestEmailAlert
	// treats as a non-fatal skip (see its own comment), not an error.
	isNewAgain, err := sched.IngestEmailAlert(context.Background(), "linkedin", posting)
	if err != nil {
		t.Fatalf("second IngestEmailAlert: %v", err)
	}
	if isNewAgain {
		t.Error("isNew = true on second ingest of the identical posting, want false (dedup should have caught it)")
	}
	err = pool.QueryRow(context.Background(),
		`select count(*) from job where company_id = $1::uuid and title = $2`, companyID, posting.TitleRaw,
	).Scan(&jobCount)
	if err != nil {
		t.Fatalf("count jobs after re-ingest: %v", err)
	}
	if jobCount != 1 {
		t.Errorf("job rows after re-ingest = %d, want still 1 (no duplicate)", jobCount)
	}
}

func TestSlugifyCompanyName(t *testing.T) {
	cases := map[string]string{
		"Acme Corp":              "acme-corp",
		"  Société Générale  ":   "soci-t-g-n-rale",
		"Foo & Bar, Inc.":        "foo-bar-inc",
		"already-slugged":        "already-slugged",
		"UPPER   CASE    SPACES": "upper-case-spaces",
	}
	for in, want := range cases {
		if got := slugifyCompanyName(in); got != want {
			t.Errorf("slugifyCompanyName(%q) = %q, want %q", in, got, want)
		}
	}
}
