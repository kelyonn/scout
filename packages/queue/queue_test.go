package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a pool against a reachable Postgres, or skips. Same
// shape as apps/collector/internal/scheduler's own testPool.
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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping queue tests")
	return nil
}

func TestEnqueueEmbed_TransactionalWithCommit(t *testing.T) {
	pool := testPool(t)
	c, err := New(pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const jobID = "00000000-0000-0000-0000-0000000000e1"
	if enqueueErr := c.EnqueueEmbed(ctx, tx, jobID); enqueueErr != nil {
		t.Fatalf("EnqueueEmbed: %v", enqueueErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river_job where kind = 'embed' and args->>'job_id' = $1`, jobID)
	})

	var queueName, kind string
	var args []byte
	err = pool.QueryRow(ctx, `select queue, kind, args from river_job where kind = 'embed' and args->>'job_id' = $1`, jobID).
		Scan(&queueName, &kind, &args)
	if err != nil {
		t.Fatalf("query inserted job: %v", err)
	}
	if queueName != QueueEmbed {
		t.Errorf("queue = %q, want %q", queueName, QueueEmbed)
	}
	if kind != "embed" {
		t.Errorf("kind = %q, want embed", kind)
	}
	var got EmbedArgs
	if err := json.Unmarshal(args, &got); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if got.JobID != jobID {
		t.Errorf("args.JobID = %q, want %q", got.JobID, jobID)
	}
}

// TestEnqueueEmbed_RolledBackWithTransaction is the property ADR-003 exists
// for: enqueue is transactional with the caller's own write, so a rollback
// takes the queued job with it — no observation ever exists without a job
// pending for it, and vice versa.
func TestEnqueueEmbed_RolledBackWithTransaction(t *testing.T) {
	pool := testPool(t)
	c, err := New(pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	const jobID = "00000000-0000-0000-0000-0000000000e2"
	if enqueueErr := c.EnqueueEmbed(ctx, tx, jobID); enqueueErr != nil {
		t.Fatalf("EnqueueEmbed: %v", enqueueErr)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	var count int
	err = pool.QueryRow(ctx, `select count(*) from river_job where kind = 'embed' and args->>'job_id' = $1`, jobID).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("river_job rows after rollback = %d, want 0", count)
	}
}

func TestEnqueueBrainDeep(t *testing.T) {
	pool := testPool(t)
	c, err := New(pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const jobID = "00000000-0000-0000-0000-0000000000e3"
	if enqueueErr := c.EnqueueBrainDeep(ctx, tx, jobID, TaskDedupStage3); enqueueErr != nil {
		t.Fatalf("EnqueueBrainDeep: %v", enqueueErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river_job where kind = 'brain_deep' and args->>'job_id' = $1`, jobID)
	})

	var queueName string
	var args []byte
	err = pool.QueryRow(ctx, `select queue, args from river_job where kind = 'brain_deep' and args->>'job_id' = $1`, jobID).
		Scan(&queueName, &args)
	if err != nil {
		t.Fatalf("query inserted job: %v", err)
	}
	if queueName != QueueBrainDeep {
		t.Errorf("queue = %q, want %q", queueName, QueueBrainDeep)
	}
	var got BrainDeepArgs
	if err := json.Unmarshal(args, &got); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if got.Task != TaskDedupStage3 {
		t.Errorf("args.Task = %q, want %q", got.Task, TaskDedupStage3)
	}
}

func TestEnqueueEmbedResume(t *testing.T) {
	pool := testPool(t)
	c, err := New(pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if enqueueErr := c.EnqueueEmbedResume(ctx, tx); enqueueErr != nil {
		t.Fatalf("EnqueueEmbedResume: %v", enqueueErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	var queueName, kind string
	err = pool.QueryRow(ctx,
		`select queue, kind from river_job where kind = 'embed_resume' order by id desc limit 1`).
		Scan(&queueName, &kind)
	if err != nil {
		t.Fatalf("query inserted job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river_job where kind = 'embed_resume'`)
	})
	if queueName != QueueEmbed {
		t.Errorf("queue = %q, want %q — embed_resume shares the embed queue, not a dedicated one", queueName, QueueEmbed)
	}
	if kind != "embed_resume" {
		t.Errorf("kind = %q, want embed_resume", kind)
	}
}

// TestEnqueueEmbedResume_RolledBackWithTransaction mirrors
// TestEnqueueEmbed_RolledBackWithTransaction — River's InsertTx is only
// durable if tx actually commits.
func TestEnqueueEmbedResume_RolledBackWithTransaction(t *testing.T) {
	pool := testPool(t)
	c, err := New(pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if enqueueErr := c.EnqueueEmbedResume(ctx, tx); enqueueErr != nil {
		t.Fatalf("EnqueueEmbedResume: %v", enqueueErr)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	var count int
	if countErr := pool.QueryRow(ctx, `select count(*) from river_job where kind = 'embed_resume'`).Scan(&count); countErr != nil {
		t.Fatalf("count: %v", countErr)
	}
	if count != 0 {
		t.Errorf("found %d embed_resume jobs after rollback, want 0", count)
	}
}
