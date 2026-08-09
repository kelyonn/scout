package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/fetch"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testPool returns a pool against a reachable Postgres, or skips. Mirrors the
// testRedis helpers used across the collector's other internal packages —
// same shape, same reasoning: skip rather than fail so a contributor without
// the local stack running still gets a green `go test ./...` everywhere else.
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

	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping scheduler tests")
	return nil
}

// testSourceOpts configures insertTestSource; zero value is a sane, fast,
// currently-due default source.
type testSourceOpts struct {
	kind                                                   string
	legalPosture                                           string
	maxRPS                                                 float64
	maxConcurrency, baseInterval, minInterval, maxInterval int
}

func defaultTestSourceOpts() testSourceOpts {
	return testSourceOpts{
		kind:           "rss",
		legalPosture:   "permitted",
		maxRPS:         1000, // fast: tests should not wait on the rate limiter
		maxConcurrency: 10,
		baseInterval:   900,
		minInterval:    300,
		maxInterval:    86400,
	}
}

// insertTestSource inserts a minimal, valid, currently-due source row and
// returns its ID. A raw INSERT rather than a sqlc query: this is test
// fixture setup, not application logic, and there is no CreateSource query
// because nothing in the collector creates sources yet — that arrives with
// the source registry.
func insertTestSource(t *testing.T, pool *pgxpool.Pool, o testSourceOpts) pgtype.UUID {
	t.Helper()

	url := fmt.Sprintf("https://example.test/%d/feed.xml", time.Now().UnixNano())

	// digest(..., 'sha256') is pgcrypto's function (infra/migrations/000001),
	// not a bare sha256() — Postgres has no built-in hash function of that
	// name.
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into source (kind, status, legal_posture, url, url_hash, max_rps, max_concurrency,
		                     base_interval_s, min_interval_s, max_interval_s, next_poll_at)
		values ($1, 'active', $2, $3, digest($3, 'sha256'), $4, $5, $6, $7, $8, now() - interval '1 minute')
		returning id
	`, o.kind, o.legalPosture, url, o.maxRPS, o.maxConcurrency, o.baseInterval, o.minInterval, o.maxInterval).
		Scan(&id)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, id)
	})

	return id
}

func getTestSource(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) db.Source {
	t.Helper()
	row, err := db.New(pool).GetSourceByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSourceByID: %v", err)
	}
	return row
}

// fakeGate lets tests script the politeness decision returned to every
// caller, without needing a real robots.txt fetch or Redis-backed rate
// limiter — those are apps/collector/internal/politeness's own,
// already-covered responsibility. It cannot be built from ssrf.DialContext-
// guarded internals reaching into another package's private test dialer,
// which is the whole reason Scheduler depends on the small Gate/Fetcher
// interfaces rather than the concrete politeness and fetch types.
//
// decision is set once at construction and never mutated afterward, so
// concurrent reads from RunOnce's worker pool need no lock — only released
// and calls, which every worker writes to, are atomic.
type fakeGate struct {
	decision politeness.Decision
	released atomic.Int32
	calls    atomic.Int32
}

func (g *fakeGate) Allow(_ context.Context, _ source.Source) (politeness.Decision, politeness.Release) {
	g.calls.Add(1)
	if g.decision.Result != politeness.ResultAllow {
		return g.decision, nil
	}
	return g.decision, func(context.Context) { g.released.Add(1) }
}

// fakeFetcher lets tests script the fetch outcome per call.
type fakeFetcher struct {
	result *fetch.Result
	err    error
	calls  atomic.Int32
}

func (f *fakeFetcher) Fetch(_ context.Context, _ fetch.Request) (*fetch.Result, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
