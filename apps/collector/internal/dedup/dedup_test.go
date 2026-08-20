package dedup

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/schema"
)

// testPool mirrors apps/collector/internal/scheduler's helper of the same
// name — skip rather than fail so `go test ./...` stays green without the
// local stack running.
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

	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping dedup tests")
	return nil
}

func insertTestCompany(t *testing.T, tx pgx.Tx) pgtype.UUID {
	t.Helper()
	slug := fmt.Sprintf("test-co-%d", time.Now().UnixNano())
	var id pgtype.UUID
	err := tx.QueryRow(context.Background(), `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::text, $1::text, $1::text, 'seed')
		returning id
	`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}
	return id
}

func insertTestSource(t *testing.T, tx pgx.Tx, companyID pgtype.UUID) pgtype.UUID {
	t.Helper()
	url := fmt.Sprintf("https://example.test/%d/board", time.Now().UnixNano())
	var id pgtype.UUID
	err := tx.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash)
		values ($1, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'))
		returning id
	`, companyID, url).Scan(&id)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}
	return id
}

func testPosting(canonicalURL string) schema.NormalizedJob {
	return schema.NormalizedJob{
		CanonicalURL:     canonicalURL,
		CanonicalURLHash: []byte(fmt.Sprintf("hash-%s", canonicalURL)),
		ContentHash:      []byte(fmt.Sprintf("content-%s", canonicalURL)),
		Title:            "Backend Engineer Intern",
		NormalizedTitle:  "backend engineer intern",
		ApplyURL:         canonicalURL,
		RoleFamily:       schema.RoleSWEBackend,
		Seniority:        schema.SeniorityInternship,
		IsSoftware:       true,
		WorkMode:         schema.WorkOnsite,
		Paid:             schema.PaidUnknown,
	}
}

func TestResolve_NewJobCreatesGroup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	companyID := insertTestCompany(t, tx)
	sourceID := insertTestSource(t, tx, companyID)

	posting := testPosting(fmt.Sprintf("https://example.test/job/%d", time.Now().UnixNano()))
	result, err := Resolve(ctx, q, posting, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !result.IsNewJob {
		t.Error("expected IsNewJob = true for a never-seen posting")
	}

	job, err := q.GetJobByID(ctx, result.JobID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if job.JobGroupID != result.JobGroupID {
		t.Error("job's job_group_id should match the returned JobGroupID")
	}
}

func TestResolve_ReobservationAttachesInsteadOfDuplicating(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	companyID := insertTestCompany(t, tx)
	sourceID := insertTestSource(t, tx, companyID)

	posting := testPosting(fmt.Sprintf("https://example.test/job/%d", time.Now().UnixNano()))

	first, err := Resolve(ctx, q, posting, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	second, err := Resolve(ctx, q, posting, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	if second.IsNewJob {
		t.Error("second Resolve of the same posting should not create a new job")
	}
	if second.JobID != first.JobID || second.JobGroupID != first.JobGroupID {
		t.Error("second Resolve should return the same job and job_group as the first")
	}

	job, err := q.GetJobByID(ctx, first.JobID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if job.ObservationCount != 2 {
		t.Errorf("ObservationCount = %d, want 2 after a re-observation", job.ObservationCount)
	}
}
