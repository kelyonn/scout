# ADR-011: OpenTelemetry into a self-hosted Grafana stack

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout runs unattended. Its most dangerous failure mode is not a crash — a crash
is loud and gets fixed. It is **silent degradation**: an adapter that starts
returning zero results because a site changed its HTML, a source whose circuit
breaker tripped three weeks ago, a classifier quietly misrouting a whole role
family. In every one of those cases the system looks healthy, notifications keep
arriving, and the user is silently missing opportunities.

Detecting silent degradation is the primary requirement. Crash detection is
secondary and easy.

Constraints: single node with ~1GB of RAM available for observability, and a
budget of roughly ₹0.

## Options considered

### Option A — Datadog / New Relic / Honeycomb

**For:** excellent products, zero operational burden, superb query experience.
**Against:** Datadog is ~$15/host/month plus per-metric and per-GB-log charges;
realistically $50–150/month for our cardinality. That is 5–10x the entire
infrastructure budget. Honeycomb's free tier (20M events/month) is genuinely
generous and would work for traces alone, but does not cover metrics and logs.

### Option B — Logs only, no metrics or traces

**For:** simplest. `docker logs` and `grep` cost nothing.
**Against:** cannot answer "is source yield declining?" without a metrics store.
Silent degradation is precisely the class of problem logs are worst at, because
nothing is being logged — that is what makes it silent.

### Option C — Prometheus + Grafana only

**For:** lightweight, ubiquitous, excellent alerting.
**Against:** no log aggregation and no distributed tracing. Debugging "why did
this specific job take 40 seconds" requires correlating across four services,
which needs traces.

### Option D — Self-hosted Grafana stack (Prometheus, Loki, Tempo, Grafana)

**For:** metrics, logs, and traces in one UI with click-through correlation —
from a metric spike to the logs in that window to the trace of a slow request.
Loki indexes only labels rather than full log content, so it is dramatically
cheaper in memory than Elasticsearch-based logging. Total footprint is about
900MB. Free.
**Against:** four more containers to run and upgrade. Loki's query language is
less powerful than full-text log search. Retention is bounded by local disk.

### Option E — Grafana Cloud free tier

**For:** managed, with a free tier covering 10k metrics series, 50GB logs, and
50GB traces — which is genuinely enough for us.
**Against:** telemetry leaves the host, and if the host is down we can still see
why, which is a real advantage. But it is a dependency on a vendor's free tier
continuing to exist, and our observability then depends on outbound network
health.

## Decision

**Option D**, instrumented with **OpenTelemetry** throughout, with Grafana Cloud
as a documented fallback if local resource pressure becomes a problem.

OpenTelemetry is the key choice, more than the backend. Instrumenting with OTel
means the backend is swappable: if we later move to Grafana Cloud, Honeycomb, or
Datadog, we change a collector endpoint rather than reinstrumenting three
services.

### Stack

| Component | Role | Memory | Retention |
| --- | --- | --- | --- |
| OTel Collector | Receives all telemetry, processes, routes | 128MB | — |
| Prometheus | Metrics | 384MB | 30 days |
| Loki | Logs | 256MB | 14 days |
| Tempo | Traces | 128MB | 7 days |
| Grafana | Dashboards, alerting | 128MB | — |
| Uptime Kuma | External black-box probes | 64MB | 90 days |
| Sentry (hosted, free) | Error tracking with stack traces | — | 30 days |

**Why Sentry alongside this.** Sentry does one thing the Grafana stack does
poorly: error grouping with full stack traces, release attribution, and
regression detection. Its free tier (5k errors/month) is ample. The
Grafana stack tells you *that* error rate rose; Sentry tells you *which line*.

**Why Uptime Kuma.** Every other component runs on the host it is monitoring. If
the host dies, so does the monitoring. Uptime Kuma runs on a separate free-tier
instance elsewhere and probes from outside — the only component that can report
"the whole thing is down."

### Instrumentation standards

**Traces.** One trace per pipeline run, propagated across service boundaries
through the job queue via a `trace_id` column on the job payload. This is the
piece that makes cross-service debugging possible: a single trace shows fetch →
normalize → classify → dedup → embed → score → notify with per-stage timing, even
though those steps happen in different processes, in different languages, minutes
apart.

**Logs.** Structured JSON only. Every log line carries `trace_id`, `service`,
`source_id` where applicable, and `job_id` where applicable. Never log a full job
description, a resume, or a secret. Log levels: `ERROR` needs human action,
`WARN` is degraded but self-healing, `INFO` is a state change, `DEBUG` is off in
production.

**Metrics.** The set that matters, chosen for detecting silent degradation:

| Metric | Type | Labels | Why |
| --- | --- | --- | --- |
| `scout_source_fetch_total` | counter | source_kind, status | Fetch health |
| `scout_source_yield_ratio` | gauge | source_id | **The silent-degradation detector** |
| `scout_source_circuit_state` | gauge | source_id | Which sources are dark |
| `scout_pipeline_stage_duration` | histogram | stage | Where time goes |
| `scout_queue_depth` | gauge | queue | Backpressure |
| `scout_dedup_merge_total` | counter | stage, certainty_bucket | Dedup behavior drift |
| `scout_llm_cost_usd_total` | counter | tier, task | Budget tracking |
| `scout_llm_escalation_ratio` | gauge | from_tier | Cascade calibration |
| `scout_notification_total` | counter | trigger, channel, status | Delivery health |
| `scout_notification_latency` | histogram | trigger | **The primary SLO** |
| `scout_jobs_discovered_total` | counter | role_family, location_tier | Coverage |
| `scout_api_request_duration` | histogram | route, status | API SLO |

### Alerts

Alerts are scarce on purpose. An alert that fires without requiring action trains
the operator to ignore alerts, which is worse than having no alerts.

**Page immediately (Telegram, breaks quiet hours):**

| Condition | Meaning |
| --- | --- |
| Uptime probe fails 3× consecutively | System down |
| Postgres unreachable > 60s | Total outage |
| Disk above 90% | Imminent write failure |
| Notification queue depth > 100 for 10 min | Notifications are not being delivered |
| Zero jobs discovered in 4 hours during a weekday | Pipeline is silently dead |

**Notify (Telegram, respects quiet hours):**

| Condition | Meaning |
| --- | --- |
| Source success rate below 85% over 1h | Widespread fetch problems |
| **Aggregate source yield down >40% week over week** | **Silent adapter breakage** |
| LLM spend above 80% of monthly cap | Budget pressure |
| Tier 2 escalation above 25% | Cascade miscalibrated |
| Any job_group above 15 members | Probable over-merging |
| p95 notification latency above 30 min | SLO breach |
| Memory above 85% | Capacity pressure |
| Certificate expiring within 14 days | Caddy usually handles this; alert if not |

**Dashboard only:** everything else.

The yield alert deserves emphasis. It is the one alert designed specifically to
catch the failure that no other signal reveals — the collector reports 200 OK, the
parser reports success, the pipeline reports healthy, and the source has quietly
returned an empty list for a week because its HTML changed. Nothing is broken;
everything is wrong.

### Dashboards

1. **Overview** — SLO status, discovery rate, notification latency, cost burn.
2. **Ingestion health** — per-source success and yield, circuit breaker states,
   a leaderboard of the worst-performing sources.
3. **Pipeline** — stage durations, queue depths, error rates by stage.
4. **AI** — tier distribution, escalation rates, cost by task, cache hit rate.
5. **Notifications** — delivery by channel, latency distribution, trigger mix.
6. **Infrastructure** — CPU, memory, disk, network, Postgres internals.

## Consequences

**Positive.** Full metrics, logs, and traces for ₹0. Cross-service correlation via
trace propagation through the queue. Vendor-neutral via OTel. The yield metric
catches the failure mode that would otherwise be invisible. External probing
survives host failure.

**Negative.** ~900MB of the 8GB budget, which is ~11% of the host spent on
watching itself — justified, but real. Four more containers to upgrade. Retention
is bounded by local disk (30/14/7 days). Loki's log search is weaker than
full-text. Losing the host loses local telemetry history, which is why the
external probe exists.

**Neutral.** Grafana dashboards are defined as JSON in the repo and provisioned
automatically, so they are code-reviewed and version-controlled like everything
else.

## Reversal conditions

- Observability memory pressure forcing application constraints → move to Grafana
  Cloud free tier, which is a collector endpoint change.
- Retention needs exceeding local disk → same.
- Metric cardinality above ~10k series → prune labels, particularly `source_id`,
  which is the one that could explode; capped by recording only the top 200
  sources individually and aggregating the rest.

## Migration path

Because everything is OpenTelemetry, moving backends means editing the collector
exporter configuration. No application code changes. Estimated 2 hours.
