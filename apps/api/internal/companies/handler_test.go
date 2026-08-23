package companies

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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping companies handler tests")
	return nil
}

func testHandler(pool *pgxpool.Pool) *Handler {
	return New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

var slugCounter atomic.Uint64

// slug mirrors apps/api/internal/jobs/handler_test.go's own — unique per
// call (not just per test) so concurrent runs and a same-second repeat
// never collide on company.slug's unique constraint.
func slug(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	n := slugCounter.Add(1)
	return "companies-test-" + time.Now().UTC().Format("150405") + "-" + hex32(h.Sum32()) + "-" + hex32(uint32(n))
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

// insertCompanyWithJob inserts a company plus one job (open or closed, per
// jobStatus) — the minimum SelectCompanies/GetCompanyByID's joins need.
// Unlike apps/api/internal/jobs' own fixture, this has no job_score row:
// neither query here joins job_score at all.
func insertCompanyWithJob(t *testing.T, pool *pgxpool.Pool, name, jobStatus string) string {
	t.Helper()
	ctx := context.Background()
	s := slug(t)

	var companyID string
	err := pool.QueryRow(ctx, `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::citext, $2::text, $2::citext, 'seed') returning id
	`, s, name).Scan(&companyID)
	if err != nil {
		t.Fatalf("insert company: %v", err)
	}

	url := "https://example.test/" + s
	var sourceID string
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
	var jobID string
	err = pool.QueryRow(ctx, `
		insert into job (
			job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, apply_url, is_software, paid, status,
			role_family, seniority, work_mode
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4, digest($4, 'sha256'),
			digest($4, 'sha256'), 'Software Engineering Intern', 'software engineering intern', $4,
			TRUE, 'paid', $5, 'swe.general', 'internship', 'remote'
		) returning id
	`, groupID, companyID, sourceID, canonicalURL, jobStatus).Scan(&jobID)
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

	return companyID
}

func TestList_OnlyReturnsCompaniesWithAtLeastOneOpenJob(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	withOpen := "Open Co " + slug(t)
	withClosed := "Closed Co " + slug(t)
	insertCompanyWithJob(t, pool, withOpen, "open")
	insertCompanyWithJob(t, pool, withClosed, "closed")

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/companies", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp companyListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var sawOpen, sawClosed bool
	for _, item := range resp.Data {
		if item.Name == withOpen {
			sawOpen = true
			if item.OpenJobCount != 1 {
				t.Errorf("open_job_count = %d, want 1", item.OpenJobCount)
			}
		}
		if item.Name == withClosed {
			sawClosed = true
		}
	}
	if !sawOpen {
		t.Error("company with an open job is missing from the list")
	}
	if sawClosed {
		t.Error("company with only a closed job should not appear — it has zero open software jobs")
	}
}

func TestList_QueryFiltersByName(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	s := slug(t)
	match := "Zzyzx Robotics " + s
	other := "Totally Different Corp " + s
	insertCompanyWithJob(t, pool, match, "open")
	insertCompanyWithJob(t, pool, other, "open")

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/companies?q=Zzyzx", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp companyListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range resp.Data {
		if item.Name == other {
			t.Errorf("q=Zzyzx matched %q, which doesn't contain that substring", other)
		}
	}
}

func TestDetail_ReturnsCompanyByID(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	name := "Detail Test Co " + slug(t)
	companyID := insertCompanyWithJob(t, pool, name, "open")

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/companies/"+companyID, nil)
	r.SetPathValue("id", companyID)
	w := httptest.NewRecorder()
	h.Detail(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var detail companyDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != name {
		t.Errorf("name = %q, want %q", detail.Name, name)
	}
	if detail.OpenJobCount != 1 {
		t.Errorf("open_job_count = %d, want 1", detail.OpenJobCount)
	}
}

func TestDetail_UnknownIDIs404(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/companies/00000000-0000-0000-0000-000000000000", nil)
	r.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	w := httptest.NewRecorder()
	h.Detail(w, r)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
