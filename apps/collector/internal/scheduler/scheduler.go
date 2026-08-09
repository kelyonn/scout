// Package scheduler implements the collector's scheduling loop: docs/06-ingestion-pipeline.md
// section 3, wired to the politeness gate (section 4), the fetcher (section
// 5), and change detection layers 1-2 (section 6).
//
// # What this package does not do
//
// Layer 3 change detection (a structural diff via an adapter's Parse) and
// observation writing (docs/06 sections 8 and 9) are not here. Both need an
// adapter, and none exists yet — Greenhouse, Lever, and Ashby land with the
// rest of P1. Until then, a poll cycle's outcome is entirely about the
// source row itself: did the fetch succeed, did the content change, when
// should the next attempt be. Nothing is written anywhere for anything
// resembling a job posting.
//
// The full per-status-code error classification in docs/06 section 10 —
// quarantine on 401/403, retire after three 404s, honor Retry-After and halve
// max_rps on 429 — is also not implemented. This package's classification is
// deliberately coarser: a 2xx or 304 is success, everything else (a network
// error, a timeout, an SSRF refusal, any other status) counts toward the
// circuit breaker exactly like a transient failure. That is the safe
// direction to be coarse in — a source that should have been quarantined
// instead backs off through the circuit breaker's own 6-hour ceiling, which
// is worse than the specific handling but never worse than not backing off at
// all. The specific per-code handling is deferred to when a real adapter
// makes the distinction actually matter.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/changedetect"
	"github.com/kelyon/scout/apps/collector/internal/fetch"
	"github.com/kelyon/scout/apps/collector/internal/interval"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/source"
)

// DefaultBatchLimit is docs/06 section 3.2's literal value: "LIMIT 200 ...
// bounds the batch so one scheduler tick cannot flood the fetcher."
const DefaultBatchLimit = 200

// DefaultTickInterval is docs/06 section 3.2: "The scheduler's hot loop,
// running every 10 seconds."
const DefaultTickInterval = 10 * time.Second

// DefaultConcurrency bounds how many sources this process fetches at once
// within a single batch. Not a value docs/06 states directly — the
// politeness gate's own per-domain rate and concurrency limits are the real
// throttle (apps/collector/internal/politeness), and this number exists only
// so a 200-source batch does not open 200 goroutines and 200 outbound
// connections simultaneously against whatever hosts happen to share this
// tick. 20 is chosen to keep that number modest without serializing the
// batch, which at up to 30s per fetch would make a 200-source batch take
// minutes rather than the ~10s the next tick expects.
const DefaultConcurrency = 20

// claimWindow bounds how long a claimed-but-not-yet-completed row is
// reserved before it becomes eligible again — the same self-healing-on-crash
// property as apps/collector/internal/politeness's concurrency slot TTL,
// applied via the column this table already has (next_poll_at) rather than a
// second piece of state. Generous against the fetcher's own longest timeout
// (60s, ModeRendered) for the same reason that TTL is 5 minutes.
const claimWindow = 5 * time.Minute

// deferRetryDelay is how soon a politeness-gate DEFER (rate budget,
// concurrency cap, or crawl-delay not yet elapsed) is retried. Short, because
// none of those conditions indicate anything wrong with the source — they are
// the collector's own throttling working as intended, and the source's own
// adaptive interval (apps/collector/internal/interval) is what governs how
// often it should genuinely be polled; this delay only governs how soon a
// *this-tick* miss gets another chance.
const deferRetryDelay = 60 * time.Second

// Scheduler selects due sources and drives them through the politeness gate
// and the fetcher.
//
// gate and fetcher are the two small interfaces below rather than the
// concrete *politeness.Gate and *fetch.Fetcher types, so a test can supply a
// scripted fake without needing either package's own test-only, deliberately
// unexported SSRF-bypass dialer (apps/collector/internal/robots and
// apps/collector/internal/fetch each keep that escape hatch private to their
// own package on purpose — see the [dialFunc] comment in fetch.go). Production
// wiring in apps/collector/cmd passes the real *politeness.Gate and
// *fetch.Fetcher, which satisfy these interfaces with no adapter needed.
type Scheduler struct {
	pool        *pgxpool.Pool
	gate        Gate
	fetcher     Fetcher
	log         *slog.Logger
	batchLimit  int32
	concurrency int
}

// Gate is the subset of *politeness.Gate the scheduler needs.
type Gate interface {
	Allow(ctx context.Context, src source.Source) (politeness.Decision, politeness.Release)
}

// Fetcher is the subset of *fetch.Fetcher the scheduler needs.
type Fetcher interface {
	Fetch(ctx context.Context, r fetch.Request) (*fetch.Result, error)
}

// New constructs a Scheduler with the documented defaults. Use the With*
// options to override them, primarily for tests that want a small batch and
// low concurrency for determinism.
func New(pool *pgxpool.Pool, gate Gate, fetcher Fetcher, log *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:        pool,
		gate:        gate,
		fetcher:     fetcher,
		log:         log,
		batchLimit:  DefaultBatchLimit,
		concurrency: DefaultConcurrency,
	}
}

// WithBatchLimit overrides DefaultBatchLimit.
func (s *Scheduler) WithBatchLimit(n int32) *Scheduler { s.batchLimit = n; return s }

// WithConcurrency overrides DefaultConcurrency.
func (s *Scheduler) WithConcurrency(n int) *Scheduler { s.concurrency = n; return s }

// Run blocks, calling RunOnce every tickInterval, until ctx is cancelled.
// A tick that errors is logged and does not stop the loop — the scheduler's
// entire job is to keep trying on a schedule, and a single bad tick (a
// transient DB blip) must not be the thing that stops discovery instead of
// whatever caused the blip.
func (s *Scheduler) Run(ctx context.Context, tickInterval time.Duration) {
	if tickInterval <= 0 {
		tickInterval = DefaultTickInterval
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.RunOnce(ctx)
			if err != nil {
				s.log.Warn("scheduler tick failed", "err", err)
				continue
			}
			if n > 0 {
				s.log.Info("scheduler tick", "claimed", n)
			}
		}
	}
}

// RunOnce claims one batch of due sources and polls them with bounded
// concurrency, returning how many were claimed.
func (s *Scheduler) RunOnce(ctx context.Context) (int, error) {
	rows, err := s.claimBatch(ctx)
	if err != nil {
		return 0, fmt.Errorf("scheduler: claim batch: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	for _, row := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func(row db.SelectDueSourcesRow) {
			defer wg.Done()
			defer func() { <-sem }()
			s.pollOne(ctx, row)
		}(row)
	}
	wg.Wait()

	return len(rows), nil
}

// claimBatch selects the due batch and reserves it, all inside one short
// transaction — see the comment on the ClaimSources query in
// packages/db/queries/source.sql for why the reservation is a second write
// rather than something the row lock alone provides. The transaction commits
// (and its row locks release) before any fetching happens; fetching a batch
// of sources over the network is not something that should ever happen
// inside an open Postgres transaction.
func (s *Scheduler) claimBatch(ctx context.Context) ([]db.SelectDueSourcesRow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	q := db.New(tx)

	rows, err := q.SelectDueSources(ctx, s.batchLimit)
	if err != nil {
		return nil, fmt.Errorf("select due sources: %w", err)
	}
	if len(rows) == 0 {
		if err = tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty claim: %w", err)
		}
		return nil, nil
	}

	ids := make([]pgtype.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}

	err = q.ClaimSources(ctx, db.ClaimSourcesParams{
		Ids:          ids,
		ClaimedUntil: pgtype.Timestamptz{Time: time.Now().Add(claimWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("claim sources: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return rows, nil
}

// pollOne runs one source through the gate and, if allowed, the fetcher, then
// persists the outcome. Errors are logged rather than returned: RunOnce
// processes a batch concurrently and one source's failure — which is an
// entirely expected, routine outcome for a poll, not a bug — must never
// affect any other source in the same batch.
func (s *Scheduler) pollOne(ctx context.Context, row db.SelectDueSourcesRow) {
	src := toSource(row)

	decision, release := s.gate.Allow(ctx, src)

	switch decision.Result {
	case politeness.ResultDefer, politeness.ResultSkip:
		s.reschedule(ctx, row.ID, time.Now().Add(deferRetryDelay))
		return

	case politeness.ResultRefuse:
		// A REFUSE this late (SelectDueSources's own predicate already
		// excludes prohibited and email_only sources) means robots.txt now
		// disallows a path it used to allow, or the URL itself is
		// unparseable. Either way: no request was made, and treating it as a
		// failure for circuit-breaker purposes is the same safe-direction
		// coarsening described in the package comment.
		s.recordOutcome(ctx, row, outcome{success: false})
		return
	}

	// ResultAllow past this point. release MUST be called exactly once,
	// whatever happens next — it frees the per-domain concurrency slot the
	// decision reserved (apps/collector/internal/politeness).
	defer release(ctx)

	result, err := s.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		Mode:            fetch.ModeStandard,
		IfNoneMatch:     derefStr(row.LastEtag),
		IfModifiedSince: derefStr(row.LastModified),
	})
	if err != nil {
		s.log.Warn("fetch failed", "source_id", src.ID, "err", err)
		s.recordOutcome(ctx, row, outcome{success: false})
		return
	}

	s.recordOutcome(ctx, row, outcomeFromResult(row, result))
}

// reschedule implements a politeness-gate DEFER or SKIP — see
// packages/db/queries/source.sql's RescheduleSource for why this touches only
// next_poll_at.
func (s *Scheduler) reschedule(ctx context.Context, id pgtype.UUID, nextPollAt time.Time) {
	q := db.New(s.pool)
	err := q.RescheduleSource(ctx, db.RescheduleSourceParams{
		ID:         id,
		NextPollAt: pgtype.Timestamptz{Time: nextPollAt, Valid: true},
	})
	if err != nil {
		s.log.Warn("reschedule failed", "source_id", id.String(), "err", err)
	}
}

// outcome is what pollOne learned from one attempt, reduced to exactly what
// UpdateSourceAfterPoll needs.
type outcome struct {
	success        bool
	contentChanged bool
	newHash        []byte
	etag           string
	lastModified   string
}

// outcomeFromResult classifies a completed fetch. See the package comment for
// why "success" is 2xx-or-304 and nothing finer-grained yet.
func outcomeFromResult(row db.SelectDueSourcesRow, result *fetch.Result) outcome {
	success := result.NotModified || (result.StatusCode >= 200 && result.StatusCode < 300)

	out := outcome{
		success:      success,
		etag:         result.ETag,
		lastModified: result.LastModified,
	}

	if result.NotModified {
		// A compliant server sends no body on 304; there is nothing to hash,
		// and the prior hash is still correct because nothing changed.
		return out
	}

	if success {
		out.contentChanged = changedetect.Changed(result.Body, row.LastContentHash)
		out.newHash = changedetect.Hash(result.Body)
	}

	return out
}

// recordOutcome computes the circuit breaker transition and the next
// adaptive interval, then writes both plus the change-detection result in one
// UpdateSourceAfterPoll call.
func (s *Scheduler) recordOutcome(ctx context.Context, row db.SelectDueSourcesRow, out outcome) {
	now := time.Now()

	failures, circuitOpenUntil := nextCircuitState(int(row.ConsecutiveFailures), out.success, now)

	next := interval.Compute(interval.Inputs{
		BaseSeconds:           int(row.BaseIntervalS),
		MinSeconds:            int(row.MinIntervalS),
		MaxSeconds:            int(row.MaxIntervalS),
		HiringPattern:         interval.HiringPattern(row.HiringPattern),
		YieldRatio:            float64(row.YieldRatio),
		DaysSinceLastActivity: daysSinceLastActivity(row.LastChangedAt, now),
		ConsecutiveFailures:   failures,
		Now:                   now,
	})
	next = interval.Jitter(next, rngFor(row.ID))

	q := db.New(s.pool)
	err := q.UpdateSourceAfterPoll(ctx, db.UpdateSourceAfterPollParams{
		ID:                  row.ID,
		NextPollAt:          pgtype.Timestamptz{Time: now.Add(next), Valid: true},
		CurrentIntervalS:    int32(next / time.Second), //nolint:gosec // interval.Compute clamps to source.max_interval_s, which is itself int32 in the schema
		LastEtag:            nullableStr(out.etag),
		LastModified:        nullableStr(out.lastModified),
		LastContentHash:     out.newHash,
		ContentChanged:      out.contentChanged,
		ConsecutiveFailures: int16(failures), //nolint:gosec // circuitOpenThreshold-based backoff never approaches int16's range
		CircuitOpenUntil:    toTimestamptz(circuitOpenUntil),
		Success:             out.success,
	})
	if err != nil {
		s.log.Warn("record poll outcome failed", "source_id", row.ID.String(), "err", err)
	}
}
