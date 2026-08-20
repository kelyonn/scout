// Package metrics defines Scout's Prometheus metrics — docs/16-observability.md
// section 4's catalog and ADR-011's stack decision, scoped to the subset
// that feeds the Overview dashboard (docs/16 section 8, dashboard 1): SLO
// compliance, discovery rate, notification latency, LLM budget burn, and
// source health. The remaining five dashboards and the full metrics
// catalog are documented, not built — see this package's own comment on
// each metric group for what's included and HANDOFF.md for what isn't.
//
// One process-wide registry (the default, via promauto) rather than one
// per service: each binary (api, collector, notifier) only ever
// registers the metrics it actually emits, so there is no risk of one
// service's dashboard panel silently reading another's zero-valued
// series — a metric simply doesn't exist in a process that never
// registers it.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// --- Ingestion (apps/collector) ---

// SourceFetchTotal is docs/16 section 4's scout_source_fetch_total,
// narrowed to (source_kind, status) — http_status is omitted to keep this
// package's first pass simple; source_id-level cardinality control (top
// 200 by yield) from that section's own "cardinality control" note is not
// implemented, so this is source_kind-level only, coarser than the full
// spec.
var SourceFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scout_source_fetch_total",
	Help: "Source fetches, by source_kind and outcome.",
}, []string{"source_kind", "status"})

// SourceFetchDuration is scout_source_fetch_duration_seconds.
var SourceFetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "scout_source_fetch_duration_seconds",
	Help:    "Source fetch duration, by source_kind.",
	Buckets: prometheus.DefBuckets,
}, []string{"source_kind"})

// SourceYieldRatio is scout_source_yield_ratio — docs/16 section 3.2's
// per-source-kind yield signal, one of the "four signals that matter
// most." Gauge rather than counter: apps/collector/internal/interval
// already computes a 0-1 yield ratio per source for its own scheduling
// purposes; this exposes the same number for the dashboard rather than
// deriving it a second way.
var SourceYieldRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "scout_source_yield_ratio",
	Help: "Rolling yield ratio (new jobs / polls) per source_kind.",
}, []string{"source_kind"})

// JobsDiscoveredTotal is scout_jobs_discovered_total — section 3.1's
// aggregate discovery-rate signal, and the numerator of the "week over
// week" ratio that catches a broken adapter or a pipeline stall.
var JobsDiscoveredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scout_jobs_discovered_total",
	Help: "Genuinely new jobs written, by role_family, location_tier, and source_kind.",
}, []string{"role_family", "location_tier", "source_kind"})

// --- Pipeline (apps/collector, apps/notifier) ---

// QueueDepth is scout_queue_depth — a gauge a periodic updater sets from
// a direct river_job count, not derived from River's own client library
// (which has no simple "depth" accessor); see packages/queue's own
// QueueDepth query.
var QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "scout_queue_depth",
	Help: "River jobs in available/scheduled/retryable state, by queue.",
}, []string{"queue"})

// QueueOldestJobAge is scout_queue_oldest_job_age_seconds — docs/16
// section 4's own reasoning for why this matters more than depth alone:
// "a queue with 10,000 jobs all enqueued 5 seconds ago is healthy, and a
// queue with 3 jobs enqueued 40 minutes ago is stuck."
var QueueOldestJobAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "scout_queue_oldest_job_age_seconds",
	Help: "Age of the oldest available job, by queue. 0 when the queue is empty.",
}, []string{"queue"})

// --- Notifications (apps/notifier) ---

// NotificationTotal is scout_notification_total.
var NotificationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scout_notification_total",
	Help: "Notifications evaluated, by trigger and outcome.",
}, []string{"trigger", "outcome"})

// NotificationLatency is scout_notification_latency_seconds — the
// primary SLO (docs/16 section 3.3): posted_at (or, more practically
// here, job-discovery time) to sent_at.
var NotificationLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "scout_notification_latency_seconds",
	Help: "Time from job discovery to notification sent, by trigger and channel.",
	// Wider than DefBuckets' top (10s): the SLO itself is minutes
	// (p50 <=10min, p95 <=30min), so this needs buckets reaching well
	// past DefBuckets' 10s ceiling to say anything about it.
	Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 900, 1800, 3600},
}, []string{"trigger", "channel"})

// --- API (apps/api) ---

// APIRequestDuration is scout_api_request_duration_seconds — the API
// latency SLO (p95 <=300ms).
var APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "scout_api_request_duration_seconds",
	Help:    "HTTP request duration, by route and status.",
	Buckets: prometheus.DefBuckets,
}, []string{"route", "status"})

// Handler returns the /metrics HTTP handler every service exposes on its
// existing HTTP surface (apps/api) or the dedicated listener Serve opens
// (apps/collector, apps/notifier, neither of which otherwise serves
// HTTP). Unauthenticated, like /health — Prometheus scrapes it directly,
// and the whole host is Tailscale-only with no public ingress
// (ADR-014), so this is inside the same trust boundary Postgres and
// Redis already are.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Serve runs a dedicated /metrics listener until ctx is cancelled, for a
// binary (collector, notifier) with no other HTTP surface to hang it off
// of. Failure to bind is logged, not fatal — a service that cannot serve
// its own metrics should keep doing its actual job, not stop pipeline
// work because a monitoring endpoint is unavailable.
func Serve(ctx context.Context, addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Warn("metrics server failed", "addr", addr, "err", err)
		}
	case <-ctx.Done():
		// ctx is already Done() here; a fresh Background() is the only way
		// to get a non-cancelled context for the shutdown's own timeout.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // see the comment above
			log.Warn("metrics server shutdown failed", "err", err)
		}
	}
}

// FetchStatus classifies a source fetch outcome for SourceFetchTotal's
// status label — small, closed set rather than a raw error string, to
// keep the label's cardinality bounded regardless of what a specific
// fetch failure says.
type FetchStatus string

// The three outcomes ObserveSourceFetch records against SourceFetchTotal's
// status label.
const (
	FetchStatusSuccess     FetchStatus = "success"
	FetchStatusNotModified FetchStatus = "not_modified"
	FetchStatusFailure     FetchStatus = "failure"
)

// ObserveSourceFetch records one fetch attempt's outcome and duration —
// a small helper so every call site increments the counter and the
// histogram together rather than risking one without the other.
func ObserveSourceFetch(sourceKind string, status FetchStatus, duration time.Duration) {
	SourceFetchTotal.WithLabelValues(sourceKind, string(status)).Inc()
	SourceFetchDuration.WithLabelValues(sourceKind).Observe(duration.Seconds())
}

// fmtCount is a tiny formatting helper kept local to this package rather
// than pulled from strconv at every call site — used only by callers
// building label values from integers (e.g. location_tier).
func fmtCount(n int) string { return fmt.Sprintf("%d", n) }

// LocationTierLabel formats a location tier (1-4, or 0/unknown) for
// JobsDiscoveredTotal's location_tier label — kept here so every call
// site uses the identical string form instead of each formatting an int
// its own way.
func LocationTierLabel(tier int) string { return fmtCount(tier) }
