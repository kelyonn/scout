// Command collector polls sources, writes raw observations, and runs each
// posting through normalize/classify/dedup/score.
//
// The scheduler (docs/06-ingestion-pipeline.md section 3), the politeness
// gate (section 4), the fetcher (section 5), change detection (section 6),
// and — via buildPipeline below — Layer 3 and observation writing (sections
// 8-9) all run here. The adapter registry covers every P1/P2/P3 adapter
// (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Recruitee,
// Teamtailor, Workday); any source_kind without a registered adapter is
// still change-detected and scheduled correctly, just with nothing written
// downstream of Layer 2 — see the package comment on
// apps/collector/internal/scheduler for the exact boundary.
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
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/adapters/ats/ashby"
	"github.com/kelyon/scout/adapters/ats/greenhouse"
	"github.com/kelyon/scout/adapters/ats/lever"
	"github.com/kelyon/scout/adapters/ats/recruitee"
	"github.com/kelyon/scout/adapters/ats/smartrecruiters"
	"github.com/kelyon/scout/adapters/ats/teamtailor"
	"github.com/kelyon/scout/adapters/ats/workable"
	"github.com/kelyon/scout/adapters/ats/workday"
	"github.com/kelyon/scout/apps/collector/internal/emailalert"
	"github.com/kelyon/scout/apps/collector/internal/heartbeat"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
	"github.com/kelyon/scout/apps/collector/internal/scheduler"
	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/logging"
	"github.com/kelyon/scout/packages/metrics"
	"github.com/kelyon/scout/packages/queue"
	"github.com/kelyon/scout/packages/taxonomy"
)

// defaultLivenessPath sits on the container's tmpfs, which is the only writable
// path in a read-only container.
const defaultLivenessPath = "/tmp/collector-alive"

func main() {
	log := slog.New(logging.Scrub(slog.NewJSONHandler(os.Stdout, nil)))

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

	queueClient, err := queue.New(pool)
	if err != nil {
		return fmt.Errorf("configure queue: %w", err)
	}

	fetcher := fetch.New(contactURL, operatorEmail)
	sched := scheduler.New(pool, gate, fetcher, log).
		WithPipeline(buildPipeline(fetcher)).
		WithQueue(queueClient)

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

	// docs/16-observability.md — Prometheus scrapes this directly; see
	// packages/metrics' own comment on why it's unauthenticated.
	wg.Add(1)
	go func() {
		defer wg.Done()
		metrics.Serve(ctx, metricsAddr(), log)
	}()

	// scout_queue_depth / scout_queue_oldest_job_age_seconds (docs/16
	// section 4) — read directly from river_job on a short ticker, not
	// event-driven, since nothing in this process observes a River queue
	// draining (apps/brain's consumer is the one that dequeues, in a
	// different process and language).
	wg.Add(1)
	go func() {
		defer wg.Done()
		runQueueDepthLoop(ctx, pool, log)
	}()

	// Local development never fetches live sources — SCOUT_FIXTURES_ONLY is
	// the default in .env.example, and the collector honors it here rather
	// than relying on the local source table simply being empty. There is
	// still no fixture-replay mode for the scheduler itself (adapter-level
	// fixtures live in each adapter's own package and are exercised by its
	// tests, not by running the live scheduler against them), so the only
	// correct behavior today is not to start the scheduler at all, not to
	// start it and trust nothing due happens to be seeded.
	if os.Getenv("SCOUT_FIXTURES_ONLY") == "true" {
		log.Warn("scheduler disabled: SCOUT_FIXTURES_ONLY=true")
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sched.Run(ctx, scheduler.DefaultTickInterval)
		}()

		// Same fixtures-only gate as the HTTP scheduler above: this writes
		// real job rows through the identical pipeline, just from a
		// different source (docs/06's email-alert ingestion), so local
		// development skips it for the same reason.
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler := func(ctx context.Context, provider string, posting emailalert.ExtractedPosting) error {
				if _, err := sched.IngestEmailAlert(ctx, provider, posting); err != nil {
					return fmt.Errorf("ingest email alert: %w", err)
				}
				return nil
			}
			emailalert.NewPoller(emailIMAPConfig(), handler, log).Run(ctx, emailalert.DefaultInterval)
		}()
	}

	wg.Wait()

	log.Info("collector stopped")
	return nil
}

// buildPipeline wires normalize/classify/dedup/score's shared dependencies
// and the adapter registry. Panics on malformed taxonomy data for the same
// reason packages/taxonomy's loaders do: a broken data file is a build-time
// bug this binary should refuse to start with, not a runtime condition to
// degrade around.
func buildPipeline(fetcher *fetch.Fetcher) *scheduler.Pipeline {
	return &scheduler.Pipeline{
		Adapters: map[padapter.SourceKind]padapter.Adapter{
			padapter.SourceKindGreenhouse:      greenhouse.New(fetcher),
			padapter.SourceKindLever:           lever.New(fetcher),
			padapter.SourceKindAshby:           ashby.New(fetcher),
			padapter.SourceKindWorkable:        workable.New(fetcher),
			padapter.SourceKindSmartRecruiters: smartrecruiters.New(fetcher),
			padapter.SourceKindRecruitee:       recruitee.New(fetcher),
			padapter.SourceKindTeamtailor:      teamtailor.New(fetcher),
			padapter.SourceKindWorkday:         workday.New(fetcher),
		},
		Gazetteer: taxonomy.LoadGazetteer(),
		Roles:     taxonomy.LoadRolePatterns(),
		Skills:    taxonomy.LoadSkills(),
	}
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

// metricsAddr is the collector's dedicated /metrics listener — it has no
// other HTTP surface (unlike apps/api) for Prometheus to scrape.
func metricsAddr() string {
	if v := os.Getenv("SCOUT_METRICS_ADDR"); v != "" {
		return v
	}
	return ":9090"
}

// queueDepthInterval is how often runQueueDepthLoop refreshes
// scout_queue_depth/scout_queue_oldest_job_age_seconds — frequent enough
// for the Overview dashboard to track a genuine stall within a couple of
// minutes, cheap enough (two small aggregate queries) not to matter
// against the Postgres load the rest of this binary already produces.
const queueDepthInterval = 30 * time.Second

// runQueueDepthLoop polls river_job for both queues until ctx is
// cancelled. A read failure is logged and skipped rather than fatal —
// same coarsening-is-safe posture as every other background loop in this
// binary: a monitoring gauge going briefly stale must never take down
// actual pipeline work.
func runQueueDepthLoop(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	queues := []string{queue.QueueEmbed, queue.QueueBrainDeep}

	refresh := func() {
		for _, q := range queues {
			d, err := queue.QueueDepth(ctx, pool, q)
			if err != nil {
				log.Warn("queue depth check failed", "queue", q, "err", err)
				continue
			}
			metrics.QueueDepth.WithLabelValues(q).Set(float64(d.Count))
			metrics.QueueOldestJobAge.WithLabelValues(q).Set(d.OldestAgeSeconds)
		}
	}

	refresh()
	ticker := time.NewTicker(queueDepthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

// emailIMAPConfig reads the email-alert account from environment
// variables. All empty is a valid, "not configured" value — Poller.Enabled
// reports false and Run returns without polling, the same
// not-configured-is-quiet posture as buildPolitenessGate's peers
// (heartbeat.New, and this package's own telegram-adjacent config helpers).
// An unparseable port falls back to 993 (IMAPS) rather than failing
// startup, since a misconfigured optional integration should degrade, not
// take the whole collector down — the healthcheckInterval helper below does
// the same for its own duration env var.
func emailIMAPConfig() emailalert.Config {
	port := 993
	if v := os.Getenv("SCOUT_EMAIL_IMAP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			port = parsed
		}
	}
	return emailalert.Config{
		Host:     os.Getenv("SCOUT_EMAIL_IMAP_HOST"),
		Port:     port,
		Username: os.Getenv("SCOUT_EMAIL_IMAP_USER"),
		Password: os.Getenv("SCOUT_EMAIL_IMAP_PASSWORD"),
		Mailbox:  os.Getenv("SCOUT_EMAIL_IMAP_MAILBOX"),
	}
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
