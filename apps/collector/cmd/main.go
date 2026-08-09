// Command collector polls sources and writes raw observations.
//
// The scheduler (docs/06-ingestion-pipeline.md section 3), the politeness
// gate (section 4), and the fetcher (section 5) all run here now. What is
// still missing is an adapter: Layer 3 change detection (a structural diff
// via an adapter's Parse) and observation writing (sections 8 and 9) need
// one, and none exists yet — Greenhouse, Lever, and Ashby land with the rest
// of P1. Until then, a poll cycle updates the source row itself (did it
// succeed, did the content change, when to try next) and nothing else; see
// the package comment on apps/collector/internal/scheduler for the exact
// boundary.
//
// Every outbound HTTP request that fetches a source must go through
// PolitenessGate.Allow(). See docs/14-legal-compliance.md, and the heartbeat
// package comment for why the heartbeat is not one of those requests.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/apps/collector/internal/fetch"
	"github.com/kelyon/scout/apps/collector/internal/heartbeat"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
	"github.com/kelyon/scout/apps/collector/internal/scheduler"
	"github.com/kelyon/scout/apps/collector/internal/source"
)

// defaultLivenessPath sits on the container's tmpfs, which is the only writable
// path in a read-only container.
const defaultLivenessPath = "/tmp/collector-alive"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// `collector healthcheck` is what the container healthcheck runs. The image
	// is distroless: no shell, no curl, nothing else that could probe it.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := heartbeat.Fresh(livenessPath(), heartbeat.DefaultLivenessMaxAge); err != nil {
			log.Error("unhealthy", "err", err)
			os.Exit(1)
		}
		return
	}

	// run() returns rather than calling os.Exit itself, so every defer it
	// registers (closing Redis, closing the database pool) unwinds before this
	// process actually exits. See the identical reasoning that used to live
	// here before the scheduler existed — it still applies, with more defers
	// now depending on it.
	if err := run(log); err != nil {
		log.Error("exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	log.Info("collector starting", "fixtures_only", os.Getenv("SCOUT_FIXTURES_ONLY"))

	// SCOUT-LEGAL-003: every request identifies Scout honestly, with a contact
	// URL and an operator email, so a site operator's first move is an email
	// rather than a block. Both are required, not defaulted, because a
	// placeholder here would mean shipping a User-Agent that lies. Read once
	// here rather than inside each constructor below, since both the
	// politeness gate's robots checker and the fetcher need the same identity.
	contactURL := os.Getenv("SCOUT_COLLECTOR_CONTACT_URL")
	operatorEmail := os.Getenv("SCOUT_COLLECTOR_OPERATOR_EMAIL")
	if contactURL == "" || operatorEmail == "" {
		return errors.New(
			"SCOUT_COLLECTOR_CONTACT_URL and SCOUT_COLLECTOR_OPERATOR_EMAIL must both be set (SCOUT-LEGAL-003)")
	}

	gate, rdb, err := buildPolitenessGate(log, contactURL, operatorEmail)
	if err != nil {
		// Fail closed: a collector that cannot construct its own compliance
		// gate must not run at all, rather than run and hope nothing tries to
		// fetch before someone notices.
		return fmt.Errorf("configure politeness gate: %w", err)
	}
	defer func() { _ = rdb.Close() }()

	if err = selfCheckPolitenessGate(context.Background(), gate); err != nil {
		// AGENTS.md rule 1 has no exceptions, so a gate that fails to prove it
		// enforces that rule is treated exactly like one that failed to
		// construct at all: refuse to start, rather than run in a state where
		// the one guarantee this binary exists to uphold is unverified.
		return fmt.Errorf("politeness gate self-check: %w", err)
	}
	log.Info("politeness gate armed", "self_check", "passed")

	pool, err := buildDatabasePool(context.Background())
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer pool.Close()

	fetcher := fetch.New(contactURL, operatorEmail)
	sched := scheduler.New(pool, gate, fetcher, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		heartbeat.New(os.Getenv("SCOUT_HEALTHCHECK_URL"), log).
			WithLiveness(livenessPath()).
			Run(ctx, healthcheckInterval(log))
	}()

	// Local development never fetches live sources — SCOUT_FIXTURES_ONLY is
	// the default in .env.example, and the collector honors it here rather
	// than relying on the local source table simply being empty. The
	// scheduler has no fixture-replay mode yet (that lands with the adapters
	// that need it), so the only correct behavior today is not to start it at
	// all, not to start it and trust nothing due happens to be seeded.
	if os.Getenv("SCOUT_FIXTURES_ONLY") == "true" {
		log.Warn("scheduler disabled: SCOUT_FIXTURES_ONLY=true")
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sched.Run(ctx, scheduler.DefaultTickInterval)
		}()
	}

	wg.Wait()

	log.Info("collector stopped")
	return nil
}

// buildPolitenessGate wires the compliance gate from environment
// configuration. It never falls back to a permissive default when
// configuration is missing — see the call site's comment on why a
// misconfigured gate stops the process rather than starting one that cannot
// enforce SCOUT-LEGAL-001 through 006.
func buildPolitenessGate(log *slog.Logger, contactURL, operatorEmail string) (*politeness.Gate, *redis.Client, error) {
	redisURL := os.Getenv("SCOUT_REDIS_URL")
	if redisURL == "" {
		return nil, nil, errors.New("SCOUT_REDIS_URL is not set")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse SCOUT_REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opt)

	checker := robots.New(robots.NewRedisCache(rdb), contactURL, operatorEmail)
	limiter := ratelimit.New(rdb)
	return politeness.New(checker, limiter, rdb, log), rdb, nil
}

// buildDatabasePool wires the connection pool the scheduler reads and writes
// source rows through. Fails closed like every other piece of startup
// configuration in this file — a scheduler with no database is not a
// degraded scheduler, it is not a scheduler.
func buildDatabasePool(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("SCOUT_DATABASE_URL")
	if databaseURL == "" {
		return nil, errors.New("SCOUT_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// selfCheckPolitenessGate proves, at process startup and before anything else
// runs, that AGENTS.md rule 1 actually holds in this build: a source marked
// prohibited must be refused, with zero request ever reaching the network.
// This is the same invariant
// apps/collector/internal/politeness/gate_test.go's
// TestProhibitedSourceMakesZeroRequests exists to prove — checked again here
// because that test proves the code was correct as of the last commit that ran
// it, not that the binary running right now still is.
func selfCheckPolitenessGate(ctx context.Context, gate *politeness.Gate) error {
	probe := source.Source{
		ID:             "startup-self-check",
		Kind:           "self_check",
		LegalPosture:   source.PostureProhibited,
		URL:            "https://prohibited.invalid/self-check",
		MaxRPS:         1,
		MaxConcurrency: 1,
	}

	decision, release := gate.Allow(ctx, probe)
	if decision.Result != politeness.ResultRefuse || release != nil {
		return fmt.Errorf("a prohibited source was not refused (result=%s)", decision.Result)
	}
	return nil
}

func livenessPath() string {
	if v := os.Getenv("SCOUT_LIVENESS_FILE"); v != "" {
		return v
	}
	return defaultLivenessPath
}

func healthcheckInterval(log *slog.Logger) time.Duration {
	v := os.Getenv("SCOUT_HEALTHCHECK_INTERVAL")
	if v == "" {
		return heartbeat.DefaultInterval
	}

	parsed, err := time.ParseDuration(v)
	if err != nil {
		// Not fatal: a mistyped interval should not take the collector down, and
		// the default is the documented value anyway.
		log.Warn("ignoring unparseable SCOUT_HEALTHCHECK_INTERVAL", "value", v, "err", err)
		return heartbeat.DefaultInterval
	}
	return parsed
}
