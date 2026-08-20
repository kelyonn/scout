// Package scheduler implements the collector's scheduling loop: docs/06-ingestion-pipeline.md
// section 3, wired to the politeness gate (section 4), the fetcher (section
// 5), change detection layers 1-2 (section 6), and — once WithPipeline is
// called — Layer 3 and observation writing (sections 8-9, in ingest.go) and
// docs/06 section 10's per-status-code handling for 401/403, 404, 429, and
// TLS errors (errorclass.go). 5xx, timeouts, DNS failures, and connection
// refusals still go through the single coarse "transient failure" path —
// see outcomeFromResult's comment for why that coarsening is the safe
// default for the codes this package doesn't distinguish.
//
// Without WithPipeline, a poll cycle's outcome is entirely about the source
// row itself: did the fetch succeed, did the content change, when should
// the next attempt be — nothing is written for anything resembling a job
// posting. That's still true for any source_kind with no registered
// adapter, which today is everything except Greenhouse.
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
	"github.com/kelyon/scout/apps/collector/internal/interval"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/metrics"
	"github.com/kelyon/scout/packages/queue"
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
// packages/fetch each keep that escape hatch private to their
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
	// pipeline is nil until WithPipeline is called (ingest.go) — see that
	// file's package comment for what changes once it's set.
	pipeline *Pipeline
	// queue is nil until WithQueue is called. Nil means "not configured":
	// new jobs are still written exactly as before, just with no embed job
	// enqueued for apps/brain to pick up — the same coarsening-is-safe
	// posture pipeline itself uses.
	queue *queue.Client
	// shadowBatchLimit bounds one ReviewShadowSources pass, same reasoning
	// as batchLimit for RunOnce.
	shadowBatchLimit int32
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
		pool:             pool,
		gate:             gate,
		fetcher:          fetcher,
		log:              log,
		batchLimit:       DefaultBatchLimit,
		concurrency:      DefaultConcurrency,
		shadowBatchLimit: shadowReviewBatchLimit,
	}
}

// WithQueue enables enqueueing embed jobs for apps/brain to consume. Without
// it, new jobs are written exactly as WithPipeline alone already does, just
// with no embedding ever computed for them.
func (s *Scheduler) WithQueue(q *queue.Client) *Scheduler { s.queue = q; return s }

// WithBatchLimit overrides DefaultBatchLimit.
func (s *Scheduler) WithBatchLimit(n int32) *Scheduler { s.batchLimit = n; return s }

// WithConcurrency overrides DefaultConcurrency.
func (s *Scheduler) WithConcurrency(n int) *Scheduler { s.concurrency = n; return s }

// WithShadowBatchLimit overrides shadowReviewBatchLimit — primarily for
// tests that want ReviewShadowSources to look at only their own fixtures,
// the same reasoning WithBatchLimit's own comment gives for RunOnce.
func (s *Scheduler) WithShadowBatchLimit(n int32) *Scheduler { s.shadowBatchLimit = n; return s }

// Run blocks, calling RunOnce every tickInterval and ReviewShadowSources
// every ShadowReviewInterval, until ctx is cancelled. A tick that errors is
// logged and does not stop the loop — the scheduler's entire job is to keep
// trying on a schedule, and a single bad tick (a transient DB blip) must not
// be the thing that stops discovery instead of whatever caused the blip.
func (s *Scheduler) Run(ctx context.Context, tickInterval time.Duration) {
	if tickInterval <= 0 {
		tickInterval = DefaultTickInterval
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	shadowTicker := time.NewTicker(ShadowReviewInterval)
	defer shadowTicker.Stop()

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
		case <-shadowTicker.C:
			n, err := s.ReviewShadowSources(ctx)
			if err != nil {
				s.log.Warn("shadow review failed", "err", err)
				continue
			}
			if n > 0 {
				s.log.Info("shadow review", "reviewed", n)
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
		s.recordOutcome(ctx, row, outcome{success: false}, 0, 0)
		return
	}

	// ResultAllow past this point. release MUST be called exactly once,
	// whatever happens next — it frees the per-domain concurrency slot the
	// decision reserved (apps/collector/internal/politeness).
	defer release(ctx)

	fetchStart := time.Now()
	result, err := s.fetchResult(ctx, row, src)
	fetchDuration := time.Since(fetchStart)
	if err != nil {
		metrics.ObserveSourceFetch(string(row.Kind), metrics.FetchStatusFailure, fetchDuration)
		if isTLSError(err) {
			// docs/06 section 10: "TLS error: Persistent. Fail immediately,
			// alert — usually a real config change." Alerted at Error
			// (routine fetch failures below log at Warn); still goes
			// through the normal failure/circuit-breaker path since there
			// is no dedicated schema state for "TLS is broken" to route it
			// to instead.
			s.log.Error("TLS error fetching source", "source_id", src.ID, "err", err)
		} else {
			s.log.Warn("fetch failed", "source_id", src.ID, "err", err)
		}
		s.recordOutcome(ctx, row, outcome{success: false}, 0, 0)
		return
	}
	metrics.ObserveSourceFetch(string(row.Kind), fetchStatusFor(result), fetchDuration)

	switch classifyStatus(result.StatusCode) {
	case actionQuarantine:
		s.quarantine(ctx, row.ID, result.StatusCode)
		s.recordOutcome(ctx, row, outcome{success: false}, 0, 0)
		return

	case actionNotFound:
		s.recordOutcome(ctx, row, outcome{success: false}, 0, 0)
		// docs/06 section 10: three consecutive 404s means the board is
		// gone. Approximated against the existing consecutive_failures
		// counter (see errorclass.go's package comment) rather than a
		// dedicated 404-streak column.
		if int(row.ConsecutiveFailures)+1 >= notFoundRetireThreshold {
			s.retire(ctx, row.ID)
		}
		return

	case actionRateLimited:
		s.handleRateLimited(ctx, row, result)
		return
	}

	jobsFound, newJobs := int64(0), int64(0)
	out := outcomeFromResult(row, result)
	if out.success && !result.NotModified && out.contentChanged {
		jobsFound, newJobs = s.runIngestion(ctx, row, result)
	}

	s.recordOutcome(ctx, row, out, jobsFound, newJobs)
}

// fetchStatusFor classifies a fetch result for metrics.ObserveSourceFetch's
// status label. Not the same partition as classifyStatus just above this
// method's own call site — that one drives circuit-breaker/retry behavior
// (quarantine, not-found, rate-limited as distinct actions); this one only
// needs "did this poll come back with something," the docs/16 section 4
// scout_source_fetch_total shape.
func fetchStatusFor(result *fetch.Result) metrics.FetchStatus {
	switch {
	case result.NotModified:
		return metrics.FetchStatusNotModified
	case result.StatusCode >= 200 && result.StatusCode < 300:
		return metrics.FetchStatusSuccess
	default:
		return metrics.FetchStatusFailure
	}
}

// fetchResult performs pollOne's one HTTP fetch. The default, and correct
// choice for every adapter this project had before Workday, is a single
// conditional GET to src.URL through the scheduler's own injectable
// Fetcher — that is what makes the rest of this file's fetch outcome
// entirely fakeable in tests without a real network call. A source whose
// registered adapter implements padapter.OwnFetcher (Workday: POST, a JSON
// search body, paginated — see that interface's own comment) is fetched
// through the adapter's Fetch instead, since no GET-shaped request
// produces a usable response for it at all.
func (s *Scheduler) fetchResult(ctx context.Context, row db.SelectDueSourcesRow, src source.Source) (*fetch.Result, error) {
	if s.pipeline != nil {
		if ad, ok := s.pipeline.Adapters[padapter.SourceKind(row.Kind)]; ok {
			if of, ok := ad.(padapter.OwnFetcher); ok && of.RequiresOwnFetch() {
				adapterSrc, err := toAdapterSource(row)
				if err != nil {
					return nil, fmt.Errorf("build adapter source: %w", err)
				}
				raw, err := ad.Fetch(ctx, adapterSrc, padapter.FetchHints{
					IfNoneMatch:     derefStr(row.LastEtag),
					IfModifiedSince: derefStr(row.LastModified),
				})
				if err != nil {
					return nil, fmt.Errorf("adapter fetch: %w", err)
				}
				return resultFromRaw(raw), nil
			}
		}
	}

	result, err := s.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		Mode:            fetch.ModeStandard,
		IfNoneMatch:     derefStr(row.LastEtag),
		IfModifiedSince: derefStr(row.LastModified),
	})
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	return result, nil
}

// notFoundRetireThreshold is docs/06 section 10's literal "three
// consecutive 404s".
const notFoundRetireThreshold = 3

// quarantine implements docs/06 section 10's 401/403 handling: access
// changed, not a transient blip, so polling stops until a human reviews it.
func (s *Scheduler) quarantine(ctx context.Context, id pgtype.UUID, statusCode int) {
	s.log.Error("quarantining source: access denied", "source_id", id.String(), "status", statusCode)
	if err := db.New(s.pool).QuarantineSource(ctx, id); err != nil {
		s.log.Warn("quarantine failed", "source_id", id.String(), "err", err)
	}
}

// retire implements docs/06 section 10's "mark retired after 3 consecutive
// 404s — board deleted."
func (s *Scheduler) retire(ctx context.Context, id pgtype.UUID) {
	s.log.Warn("retiring source: repeated 404", "source_id", id.String())
	if err := db.New(s.pool).RetireSource(ctx, id); err != nil {
		s.log.Warn("retire failed", "source_id", id.String(), "err", err)
	}
}

// handleRateLimited implements docs/06 section 10's 429 handling: honor
// Retry-After if the response gave one (falling back to the normal backoff
// path if it didn't — a 429 with no Retry-After is still a 429), and halve
// max_rps permanently, since the source just told us its previous rate was
// too fast.
func (s *Scheduler) handleRateLimited(ctx context.Context, row db.SelectDueSourcesRow, result *fetch.Result) {
	s.log.Warn("rate limited (429)", "source_id", row.ID.String(), "retry_after", result.RetryAfter)

	q := db.New(s.pool)
	if err := q.HalveMaxRPS(ctx, row.ID); err != nil {
		s.log.Warn("halve max_rps failed", "source_id", row.ID.String(), "err", err)
	}

	nextPollAt := time.Now().Add(deferRetryDelay)
	if t, ok := parseRetryAfter(result.RetryAfter, time.Now()); ok {
		nextPollAt = t
	}
	s.reschedule(ctx, row.ID, nextPollAt)
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
func (s *Scheduler) recordOutcome(ctx context.Context, row db.SelectDueSourcesRow, out outcome, jobsFound, newJobs int64) {
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
	yieldRatio, err := q.UpdateSourceAfterPoll(ctx, db.UpdateSourceAfterPollParams{
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
		JobsFound:           jobsFound,
		NewJobs:             newJobs,
	})
	if err != nil {
		s.log.Warn("record poll outcome failed", "source_id", row.ID.String(), "err", err)
		return
	}
	// docs/16-observability.md section 3.2's per-source-kind yield signal —
	// last-writer-wins across every source of one kind sharing this gauge
	// is fine here: yield_ratio is already an EMA smoothed at the source
	// level, so the dashboard reads it as "yield of whichever source in
	// this kind polled most recently," a reasonable proxy at this
	// project's scale (packages/metrics' own comment notes the full
	// per-source_id cardinality control from the spec isn't implemented).
	metrics.SourceYieldRatio.WithLabelValues(string(row.Kind)).Set(float64(yieldRatio))
}
