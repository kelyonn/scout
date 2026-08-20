package stream

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/packages/realtime"
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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping stream integration tests")
	return nil
}

// TestBroker_ReceivesRealPostgresNotify is the one test that actually
// exercises LISTEN/NOTIFY end to end — everything in broker_test.go tests
// the in-process fan-out, which never touches Postgres at all and would
// stay green even if listenOnce's LISTEN/Hijack/WaitForNotification
// plumbing were completely broken.
func TestBroker_ReceivesRealPostgresNotify(t *testing.T) {
	pool := testPool(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx, pool, log)

	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	// Run's LISTEN takes a moment to establish after Subscribe returns —
	// there is no synchronous "now listening" signal by design (the
	// broker doesn't need one for real use), so this polls by retrying
	// the NOTIFY rather than adding test-only coordination surface to it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := realtime.NotifyJobNew(ctx, tx, realtime.JobNew{
			JobGroupID: "integration-test-group", Priority: 88, Title: "Integration Test Job",
		}); err != nil {
			t.Fatalf("NotifyJobNew: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}

		select {
		case ev := <-ch:
			if ev.Name != "job.new" {
				t.Fatalf("event name = %q, want job.new", ev.Name)
			}
			if !strings.Contains(ev.Data, "integration-test-group") {
				t.Fatalf("event data = %q, want it to contain the job_group_id", ev.Data)
			}
			return
		case <-time.After(300 * time.Millisecond):
			// Not listening yet — retry the NOTIFY.
		}
	}
	t.Fatal("never received the notification within the deadline")
}

// TestBroker_NotifyBeforeCommitIsNotDelivered proves the exact guarantee
// apps/collector's ingest.go relies on: a NOTIFY sent inside a
// transaction that then rolls back must never reach a subscriber.
func TestBroker_NotifyBeforeCommitIsNotDelivered(t *testing.T) {
	pool := testPool(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx, pool, log)

	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	// Give the LISTEN a moment to establish, using a real committed
	// notify as the readiness signal, then drain it.
	for {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		_ = realtime.NotifyJobNew(ctx, tx, realtime.JobNew{JobGroupID: "readiness-ping"})
		_ = tx.Commit(ctx)
		select {
		case ev := <-ch:
			if strings.Contains(ev.Data, "readiness-ping") {
				goto ready
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
ready:

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := realtime.NotifyJobNew(ctx, tx, realtime.JobNew{JobGroupID: "should-never-arrive"}); err != nil {
		t.Fatalf("NotifyJobNew: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	select {
	case ev := <-ch:
		t.Fatalf("received an event from a rolled-back transaction: %+v", ev)
	case <-time.After(time.Second):
		// Correct: nothing arrived.
	}
}
