package search

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping search handler tests")
	return nil
}

var slugCounter atomic.Uint64

func slug(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	n := slugCounter.Add(1)
	return fmt.Sprintf("search-test-%s-%08x-%08x", time.Now().UTC().Format("150405"), h.Sum32(), n)
}

// insertSearchableJob inserts a company/source/job_group/job tuple with a
// real title/description so job.search_vector (a generated column) has
// something to match against.
func insertSearchableJob(t *testing.T, pool *pgxpool.Pool, title, description string) string {
	t.Helper()
	ctx := context.Background()
	s := slug(t)

	var companyID string
	if err := pool.QueryRow(ctx, `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::citext, $1::text, $1::citext, 'seed') returning id
	`, s).Scan(&companyID); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	url := "https://example.test/" + s
	var sourceID string
	// next_poll_at explicit and far in the future — the default (now()) makes
	// this row a candidate for apps/collector/internal/scheduler's RunOnce,
	// a real source of flaky cross-package failures there since `go test
	// ./...` runs packages concurrently against the same shared test DB.
	if err := pool.QueryRow(ctx, `
		insert into source (company_id, kind, status, legal_posture, url, url_hash, next_poll_at)
		values ($1::uuid, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'), now() + interval '10 years')
		returning id
	`, companyID, url).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	var groupID string
	if err := pool.QueryRow(ctx, `insert into job_group (company_id) values ($1::uuid) returning id`, companyID).
		Scan(&groupID); err != nil {
		t.Fatalf("insert job_group: %v", err)
	}

	canonicalURL := url + "/job"
	canonicalHash := sha256.Sum256([]byte(canonicalURL))
	contentHash := sha256.Sum256([]byte(canonicalURL + "-content"))
	var jobID string
	err := pool.QueryRow(ctx, `
		insert into job (
			job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, description_text, apply_url, is_software, paid, status,
			role_family, seniority, work_mode
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4, $5,
			$6, $7, $8, $9, $4, TRUE, 'paid', 'open',
			'swe.backend', 'internship', 'remote'
		) returning id
	`, groupID, companyID, sourceID, canonicalURL, canonicalHash[:], contentHash[:], title, title, description).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	if _, err := pool.Exec(ctx, `update job_group set representative_job_id = $1::uuid where id = $2::uuid`,
		jobID, groupID); err != nil {
		t.Fatalf("set representative: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `update job_group set representative_job_id = NULL where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job_group where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from source where company_id = $1::uuid`, companyID)
		_, _ = pool.Exec(ctx, `delete from company where id = $1::uuid`, companyID)
	})

	return groupID
}

func doSearch(t *testing.T, h *Handler, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/search?"+rawQuery, nil)
	w := httptest.NewRecorder()
	h.Search(w, r)
	return w
}

func TestSearch_MatchesOnTitle(t *testing.T) {
	pool := testPool(t)
	h := New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	groupID := insertSearchableJob(t, pool, "Distributed Systems Backend Intern",
		"Work on Kafka-based event pipelines and Go microservices.")

	w := doSearch(t, h, "q=distributed+systems")
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, item := range resp.Data {
		if item.JobGroupID == groupID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected job matching title to appear in search results, got %d items", len(resp.Data))
	}
	if resp.ModeServed != "keyword" {
		t.Errorf("ModeServed = %q, want %q", resp.ModeServed, "keyword")
	}
}

func TestSearch_UnrelatedQueryDoesNotMatch(t *testing.T) {
	pool := testPool(t)
	h := New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	groupID := insertSearchableJob(t, pool, "Frontend Engineering Intern",
		"Build React components for our customer dashboard.")

	w := doSearch(t, h, "q=zzz_totally_unrelated_nonsense_zzz")
	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, item := range resp.Data {
		if item.JobGroupID == groupID {
			t.Error("did not expect an unrelated query to match this job")
		}
	}
}

func TestSearch_EmptyQueryIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	w := doSearch(t, h, "")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSearch_MatchesOnCompanyName(t *testing.T) {
	pool := testPool(t)
	h := New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	ctx := context.Background()
	groupID := insertSearchableJob(t, pool, "Software Engineering Intern", "Generic description.")

	var companyID, uniqueName string
	if err := pool.QueryRow(ctx, `
		select c.id, c.canonical_name from job_group jg join company c on c.id = jg.company_id where jg.id = $1::uuid
	`, groupID).Scan(&companyID, &uniqueName); err != nil {
		t.Fatalf("lookup company: %v", err)
	}

	w := doSearch(t, h, "q="+uniqueName)
	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, item := range resp.Data {
		if item.JobGroupID == groupID {
			found = true
		}
	}
	if !found {
		t.Error("expected a query matching the company's slug/name to find its job")
	}
}
