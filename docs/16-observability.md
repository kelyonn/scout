# Observability — Scout

**Status:** Draft · **Owner:** Infrastructure · **Last updated:** 2026-08-06

Stack selection is in [ADR-011](adr/ADR-011-observability-stack.md). This
document covers SLOs, what we measure, and how alerts are handled.

**One amendment to ADR-011 under the ₹0 budget.** The self-hosted stack —
OpenTelemetry, Prometheus, Loki, Tempo, Grafana — is unchanged; it was sized
against 8 GB and the free-tier host has 24 GB
([ADR-014](adr/ADR-014-zero-cost-hosting.md)), so nothing needed cutting. What
changes is the **external** probe. ADR-011 specified Uptime Kuma on a separate
free-tier host, correctly reasoning that monitoring on the monitored host cannot
report that host's death. That reasoning stands; the implementation does not,
because a second host is a second thing to patch, migrate, and forget. It is
replaced by a hosted dead-man's switch — see section 2.2 — which needs no host at
all.

---

## 1. The question observability must answer

Not "is the server up" — that is easy and mostly uninteresting. The question is:

> **Is Scout currently finding the jobs it should be finding?**

A Scout that is up, responsive, healthy on every conventional metric, and quietly
returning zero results from 40% of its sources has failed completely while
looking perfect. Every design choice in this document points at detecting that.

---

## 2. Service level objectives

| SLO | Target | SLI | Window | Error budget |
| --- | --- | --- | --- | --- |
| **Scout-first rate** | ≥90% | Of roles the user applied to, the fraction Scout surfaced *before* the user saw them elsewhere | 28 days | 10% |
| **Notification latency (Tier 1)** | p50 ≤10min, p95 ≤30min | `posted_at → sent_at`, excluding estimated dates | 28 days | 5% |
| **Notification precision** | ≥90% | User-marked relevance | 28 days | 10% |
| **Duplicate notifications** | 0 | Count of `late_merge_duplicate` | 28 days | **0** |
| **Ingestion success** | ≥95% | successful fetches / attempted, excluding intentional refusals | 24h | 5% |
| **Source coverage** | ≥98% of active sources polled on schedule | polls executed / polls due | 24h | 2% |
| **Per-adapter yield stability** | No adapter below 40% of its 28-day median | Rolling comparison per `source_kind` | 7 days | — |
| **API latency** | p95 ≤300ms | Server-side histogram | 7 days | 5% |
| **Search latency** | p95 ≤200ms | Server-side histogram | 7 days | 5% |

**Availability is not an SLO.** The earlier version targeted 99.0% dashboard
availability measured by an external probe. On a free-tier host with no SLA
([ADR-014](adr/ADR-014-zero-cost-hosting.md)) that is a number we cannot honour
and would not act on: the dashboard being down for an hour costs nothing, because
notifications are an outbound path and keep working. What replaced it is the
dead-man's switch in section 2.2, which answers the only availability question
that matters — *is it still running at all?* — rather than assigning it a
percentage.

**Error budget policy.** When a budget is exhausted, feature work on that area
stops until reliability work restores it. With one developer this is a
self-discipline mechanism rather than a team process, but it is written down so
that "I'll fix the source failures later" has a defined stopping point.

**The duplicate budget is zero and that is intentional.** It is the one promise
made unconditionally in the PRD, and the database constraint makes it structurally
achievable rather than aspirational.

### 2.1 Discovery recall is a diagnostic, not an SLO

This is a correction to the earlier specification, and it matters because recall
was the headline number of the whole product.

The old SLI was: *weekly audit of 20 manually-found roles, target ≥95%, error
budget 5%.* **That measurement cannot support that target.** At n=20 the
resolution is 5 percentage points per role — one miss is exactly 95% and two is
90% — so it cannot distinguish "meeting the target" from "missing it by half."
It was also self-audited by the person who wants it to pass, on roles found by
the same search habits the system is supposed to replace, which biases the sample
toward what Scout already covers.

Gating a milestone on a number that noisy produces false confidence in a good
week and pointless alarm in a bad one.

**What replaces it, in three parts:**

| | Measure | Why it works |
| --- | --- | --- |
| **Primary** | **Scout-first rate** (in the SLO table) | Every application is a sample, so n accumulates naturally with no manual audit. It measures the thing actually cared about — did Scout get there first — rather than a proxy for it. One tap on "I found this elsewhere first" is the whole instrument. |
| **Automatic** | Per-adapter yield stability | Catches the failure that recall audits are really trying to catch (a source silently returning zero) continuously, rather than once a week at n=20. This is the workhorse. |
| **Diagnostic** | Monthly recall audit, n≥60, accumulated over the month, bucketed by `company_type` | Big enough to say something, reported **with a confidence interval**, and explicitly not a gate. Its job is to find *categories* that are missing, which is a question about coverage breadth that the Scout-first rate cannot answer. |

Per-category recall stays a diagnostic for the same reason and is reported as
"which buckets had zero audited hits", not as a percentage per bucket — with 60
roles across ~16 `company_type` values, a per-bucket percentage is noise.

**The bar that actually decides whether this works** is unchanged and is stated
in the PRD: the user stops opening LinkedIn. Every number above is instrumentation
for diagnosing why that is or is not happening.

### 2.2 Watching the host from outside itself

**Self-hosted monitoring cannot detect its own host being down**, and at ₹0 there
is no second host to watch the first. This is the one genuine monitoring problem
the budget creates, and it is the failure mode
[20-risks.md](20-risks.md) rates most dangerous: Scout silently stops, everything
looks fine because nothing is running to say otherwise, and the user notices two
weeks later.

Three layers, all free, none on the host being watched:

| Layer | Mechanism | Detects |
| --- | --- | --- |
| **Dead-man's switch** | Collector pings a healthchecks.io check every 5 min; alerts Telegram if pings stop for 15 min | Host reclaimed, disk full, network gone, process dead |
| **Backup switch** | Backup job pings a second check hourly | Silent backup failure — the real risk in [ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) |
| **Backup for the backup** | Scheduled GitHub Actions workflow probes the same endpoints daily | healthchecks.io itself vanishing, since it is also a free tier |

The dead-man's switch is inverted from a normal health check, and that inversion
is the point: a probe that must *fire* to report a problem cannot report that it
stopped being able to fire. A heartbeat that must keep arriving reports its own
absence.

---

## 3. The four signals that matter most

Out of everything instrumented, these four would catch the majority of real
failures on their own.

### 3.1 Aggregate source yield

```promql
sum(rate(scout_jobs_discovered_total[7d]))
  /
sum(rate(scout_jobs_discovered_total[7d] offset 7d))
```

Below 0.6 means we are finding 40% fewer jobs than last week. That is either
seasonal (verifiable) or broken (actionable). This single metric catches adapter
regressions, source-wide blocking, and pipeline stalls that no per-service health
check would notice.

### 3.2 Per-source-kind yield

The same ratio, grouped by `source_kind`. Isolates a single broken adapter from a
general slowdown. If `ats_greenhouse` drops 90% while everything else is steady,
the Greenhouse adapter broke — probably in the last deploy.

### 3.3 Notification latency distribution

```promql
histogram_quantile(0.95,
  sum by (le) (rate(scout_notification_latency_seconds_bucket{trigger!="digest"}[1h])))
```

The primary SLO. Rising p95 with a stable p50 means a subset of sources has
slowed; both rising means the pipeline is backing up.

### 3.4 Zero-discovery interval

```promql
time() - scout_last_job_discovered_timestamp > 14400   # 4 hours
```

During a weekday, four hours with no new job anywhere is not plausible. This
fires as a page. It is the crudest metric here and the one most likely to catch a
catastrophic silent failure that every clever metric missed.

---

## 4. Metrics catalog

### Ingestion

| Metric | Type | Labels |
| --- | --- | --- |
| `scout_source_fetch_total` | counter | `source_kind`, `status`, `http_status` |
| `scout_source_fetch_duration_seconds` | histogram | `source_kind` |
| `scout_source_bytes_total` | counter | `source_kind` |
| `scout_source_304_ratio` | gauge | `source_kind` |
| `scout_source_circuit_state` | gauge | `source_id` (top 200 only) |
| `scout_source_yield_ratio` | gauge | `source_id` (top 200), `source_kind` |
| `scout_source_item_count_anomaly_total` | counter | `source_kind` |
| `scout_compliance_refusal_total` | counter | `reason` |
| `scout_robots_disallowed_total` | counter | `host_class` |

**Cardinality control.** `source_id` at 2,500 sources × several metrics would
generate excessive series. Only the top 200 sources by yield are labelled
individually; the rest aggregate into `source_kind`. This is a deliberate
trade — the long tail is monitored in aggregate and inspected via SQL when needed.

### Pipeline

| Metric | Type | Labels |
| --- | --- | --- |
| `scout_pipeline_stage_duration_seconds` | histogram | `stage` |
| `scout_pipeline_stage_errors_total` | counter | `stage`, `error_class` |
| `scout_queue_depth` | gauge | `queue` |
| `scout_queue_oldest_job_age_seconds` | gauge | `queue` |
| `scout_jobs_discovered_total` | counter | `role_family`, `location_tier`, `source_kind` |
| `scout_dedup_merge_total` | counter | `stage`, `certainty_bucket` |
| `scout_job_group_size` | histogram | — |
| `scout_late_merge_duplicate_total` | counter | — |

`scout_queue_oldest_job_age_seconds` is better than depth alone: a queue with
10,000 jobs all enqueued 5 seconds ago is healthy, and a queue with 3 jobs
enqueued 40 minutes ago is stuck.

### AI

| Metric | Type | Labels |
| --- | --- | --- |
| `scout_llm_calls_total` | counter | `tier`, `task`, `provider`, `cached` |
| `scout_llm_cost_usd_total` | counter | `tier`, `task` |
| `scout_llm_latency_seconds` | histogram | `tier`, `provider` |
| `scout_llm_errors_total` | counter | `provider`, `error_class` |
| `scout_llm_escalation_ratio` | gauge | `from_tier` |
| `scout_llm_budget_used_ratio` | gauge | — |
| `scout_embedding_duration_seconds` | histogram | — |

### Notifications

| Metric | Type | Labels |
| --- | --- | --- |
| `scout_notification_total` | counter | `trigger`, `urgency`, `outcome` |
| `scout_notification_latency_seconds` | histogram | `trigger`, `channel` |
| `scout_notification_delivery_total` | counter | `channel`, `status` |
| `scout_notification_suppressed_total` | counter | `reason` |
| `scout_notification_open_ratio` | gauge | `trigger`, `channel` |
| `scout_push_token_invalid_total` | counter | `platform` |
| `scout_push_devices_registered` | gauge | `platform` |

**`scout_push_token_invalid_total` is the silent-failure detector for native push.**
FCM returns `UNREGISTERED` or `NOT_FOUND` for a stale token, and a naive
implementation treats that as a delivered-with-error and moves on. It is not: it
means the user has *stopped receiving notifications on that device* and nothing
visible has broken. Any occurrence retires the token and, if it was the user's only
registered device, notifies them on Telegram to reopen the app.

`scout_notification_suppressed_total{reason="backfill"}` should be large during a
backfill and zero otherwise. A nonzero value outside a backfill window means
something is incorrectly flagged, and a *zero* value during a backfill means the
suppression is not working — which is the failure mode that floods the user.

---

## 5. Tracing

One trace per job, spanning every service and every stage.

```
Trace: job-discovery [4.2s]
├─ collector.fetch                    [0.4s]  source_kind=ats_greenhouse
│  ├─ politeness.check                [0.01s]
│  ├─ http.request                    [0.35s] http.status=200
│  └─ change.detect                   [0.03s] layer=content_hash result=changed
├─ collector.parse                    [0.1s]  items=47 new=1
├─ collector.write                    [0.08s]
│  └─ db.transaction                  [0.07s]
├─ brain.normalize                    [0.3s]
├─ brain.classify                     [0.2s]  tier=0 family=swe.backend
├─ brain.dedup                        [0.9s]
│  ├─ stage.exact                     [0.001s] result=miss
│  ├─ stage.structural                [0.02s]  result=miss
│  └─ stage.semantic                  [0.87s]  cosine=0.61 result=new_group
├─ brain.embed                        [0.02s]
├─ brain.score                        [0.9s]
│  └─ llm.explain                     [0.85s]  tier=2 tokens=340
└─ notifier.evaluate                  [1.3s]
   ├─ trigger.match                   [0.01s]  trigger=high_score
   ├─ dedup.insert                    [0.02s]
   ├─ channel.native_push             [0.6s]   platform=android status=delivered
   ├─ channel.telegram                [1.1s]   status=delivered
   └─ channel.web_push                [0.0s]   status=skipped reason=native_preferred
```

The `skipped` span is deliberately kept rather than omitted. A missing span is
indistinguishable from a bug; a span that records *why* it did nothing is
evidence.

**Cross-process propagation.** The `trace_id` travels in the River job payload,
so a trace continues across a queue boundary and across a language boundary. This
is the piece that makes "why did this specific job take 40 seconds" answerable at
all — without it, four services produce four disconnected fragments.

**Sampling:** 100% of errors, 100% of notifications, 10% of normal pipeline runs.
Full sampling on notifications because they are the SLO and the volume is low.

---

## 6. Logging

Structured JSON, one line per event, always including:

```json
{
  "ts": "2026-08-06T11:04:32.881Z",
  "level": "info",
  "service": "collector",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "msg": "source fetched",
  "source_id": "0192b7c4-...",
  "source_kind": "ats_greenhouse",
  "http_status": 200,
  "items": 47,
  "new_items": 1,
  "duration_ms": 352
}
```

| Level | Use | Retention |
| --- | --- | --- |
| ERROR | Requires human action | 14 days |
| WARN | Degraded, self-healing | 14 days |
| INFO | State change worth recording | 14 days |
| DEBUG | Off in production | — |

**Never logged:** resume content, full job descriptions, email addresses, phone
numbers, session tokens, API keys, notification channel credentials. A scrubbing
middleware enforces this, and there is a test that seeds known PII patterns
through the logger and asserts they never reach the sink.

---

## 7. Alerting

Alerts are scarce by design. An alert that does not require action trains the
operator to ignore alerts, and an ignored alerting system is worse than none.

### Page — breaks quiet hours, Telegram

| Alert | Condition | Runbook |
| --- | --- | --- |
| Service down | External probe fails 3× consecutively | [service-down](runbooks/service-down.md) |
| Database unreachable | >60s | [database-recovery](runbooks/database-recovery.md) |
| Disk above 90% | — | [disk-pressure](runbooks/disk-pressure.md) |
| **Zero discovery 4h** | Weekday, no new job anywhere | [ingestion-stalled](runbooks/ingestion-stalled.md) |
| Notification queue stuck | Depth >100 for 10 min | [notifications-stuck](runbooks/notifications-stuck.md) |
| **Both primary channels failing** | Native push and Telegram both failing 15 min | [notifications-stuck](runbooks/notifications-stuck.md) |
| Security event | Recovery code used, or a request to a prohibited source | [security-incident](runbooks/security-incident.md) |

### Notify — respects quiet hours

| Alert | Condition |
| --- | --- |
| Source success rate low | <85% over 1 hour |
| **Aggregate yield drop** | >40% week over week |
| **Per-adapter yield drop** | >60% week over week for one `source_kind` |
| LLM budget | >80% of monthly cap |
| LLM escalation high | Tier 2 rate >25% |
| Over-merge suspected | Any `job_group` above 15 members |
| Late-merge duplicate | Any occurrence |
| SLO breach | p95 notification latency >30 min |
| Push token invalidated | Any occurrence — the device has silently stopped receiving |
| No registered push devices | Sustained 1h while the app was previously registered |
| Memory pressure | >85% for 15 min |
| Certificate expiry | <14 days (Caddy should have renewed) |
| Eval regression | Any golden-set metric down >3% |

### Dashboard only

Everything else. Individual source failures, single request errors, cache miss
rates, and the rest are visible when investigating but never interrupt.

---

## 8. Dashboards

Six, provisioned as JSON from the repo.

**1. Overview** — the one to check first. SLO compliance for all nine objectives,
discovery rate over 7 days, notification latency percentiles, LLM budget burn,
and a source health summary.

**2. Ingestion** — per-source-kind success rate and yield, circuit breaker states,
304 ratio, bandwidth, and a table of the 20 worst-performing sources sorted by
yield decline. This is where investigation of a yield alert starts.

**3. Pipeline** — stage durations as a stacked view, queue depths and oldest-job
ages, error rates by stage, dedup merge distribution by stage.

**4. AI** — tier distribution over time, escalation rates, cost by task, cache hit
rate, provider latency and error rates.

**5. Notifications** — deliveries by trigger and channel, latency histogram,
suppression reasons, open rate trend, and the fatigue counter-metrics.

**6. Infrastructure** — CPU, memory, disk, network, container restarts, Postgres
internals (connections, cache hit ratio, replication lag, table bloat, autovacuum
activity on the queue tables).

---

## 9. Operational review cadence

| Activity | Frequency | Output |
| --- | --- | --- |
| Check the Overview dashboard | Daily, 2 minutes | Nothing, usually |
| Review quarantined sources | Weekly | Fix, retire, or accept |
| Recall audit — 20 manual searches vs. Scout | Weekly | Recall SLI, and golden-set additions for anything missed |
| **Recall audit broken out by `company_type`** | Weekly | Detects a whole category going missing while aggregate recall looks healthy |
| **Feed composition by `company_type` and `role_family`** | Weekly | An all-product-company feed is a bug, not a preference |
| Review notification precision | Weekly | Threshold adjustments |
| Review LLM cost by task | Weekly | Optimization targets |
| Full eval harness review | Monthly | Quality trend |
| Restore drill | Monthly | Verified RTO |
| SLO review and error budget | Monthly | Priority for the next cycle |
| Dependency and security review | Monthly | Upgrades |
| Source registry audit | Quarterly | Prune, expand, re-tier |

**The weekly recall audit is the most valuable item on this list and the easiest
to skip.** Manually search for internships the way the user would have before
Scout existed, then check whether Scout already had each one. Anything Scout
missed becomes both a recall data point and a concrete task: find out which
source would have carried it, and add that source.

Without this audit, recall is unmeasurable — you cannot count what you never
saw — and recall is the metric the entire product is judged on.
