# System Architecture — Scout

**Status:** Draft · **Owner:** Architecture · **Last updated:** 2026-08-08

---

## 1. Architectural goals

In priority order, because these conflict and the order resolves the conflicts:

1. **Small failure domains.** A broken source, adapter, or model provider must
   degrade one capability, never the system.
2. **Low operational cost.** One person operates this. Every component must
   justify its ongoing maintenance burden, not just its build cost.
3. **Freshness.** The architecture must support sub-10-minute detection-to-push.
4. **Clean scaling path.** Every MVP choice has a documented, incremental
   upgrade — never a rewrite.
5. **Adapter extensibility.** Adding a source type must not require touching
   pipeline code.

---

## 2. System overview

```
┌────────────────────────────────────────────────────────────────────────┐
│                            EXTERNAL WORLD                              │
│  ATS APIs · RSS/Atom · Sitemaps · Career pages · HN · GitHub · Reddit   │
│  Inbound job-alert email · Funding announcements · Hackathon sponsors   │
└───────────────────────────────┬────────────────────────────────────────┘
                                │
┌───────────────────────────────▼────────────────────────────────────────┐
│  COLLECTOR (Go)                                                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │ Scheduler│─▶│  Fetcher │─▶│  Change  │─▶│  Adapter │─▶│Observation│ │
│  │  yield-  │  │ conditio-│  │ detector │  │ registry │  │  writer   │ │
│  │  driven  │  │ nal HTTP │  │  3-layer │  │ 12 kinds │  │ append-only│ │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └─────┬─────┘ │
│  Rate limiter · robots.txt cache · circuit breakers · politeness       │
└─────────────────────────────────────────────────────────────────┬──────┘
                                                                  │
                                                    raw_observation (Postgres)
                                                                  │
┌─────────────────────────────────────────────────────────────────▼──────┐
│  BRAIN (Python)                                                        │
│  ┌───────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐  ┌───────────┐  │
│  │ Normalize │─▶│ Classify │─▶│Deduplicate│─▶│ Embed │─▶│   Score   │  │
│  │ canonical │  │ role/paid│  │  3-stage  │  │pgvector│  │ 13 factors│  │
│  │  schema   │  │ /location│  │  grouping │  │        │  │ + explain │  │
│  └───────────┘  └──────────┘  └──────────┘  └────────┘  └─────┬─────┘  │
│  Model cascade: rules → embeddings → small LLM → large LLM             │
└─────────────────────────────────────────────────────────────────┬──────┘
                                                                  │
                                                        job / job_group
                                                                  │
        ┌─────────────────────────────────────────────────────────┤
        │                                                         │
┌───────▼────────────────────────┐            ┌───────────────────▼──────┐
│  NOTIFIER (Go)                 │            │  API (Go)                │
│  trigger eval · budgets ·      │            │  REST + SSE · auth ·     │
│  quiet hours · channel fanout  │            │  search · state machine  │
│  FCM│Telegram│WebPush│         │            │  rate limiting           │
│  Email│Discord                 │            │                          │
└───────┬────────────────────────┘            └───────────────────┬──────┘
        │                                                         │
        ▼                                                         ▼
   your devices                                       ┌──────────────────────┐
                                                      │  WEB (Next.js PWA)   │
                                                      │  dashboard · install │
                                                      └──────────────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│  DATA PLANE                                                            │
│  PostgreSQL 16 (+pgvector, +pg_trgm, +partitioning)  ·  Redis          │
│  Job queue: River (Postgres-backed)  ·  Raw snapshots: local disk      │
└────────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│  OBSERVABILITY: OpenTelemetry → Prometheus · Loki · Tempo · Grafana    │
│                 Sentry (errors) · healthchecks.io (dead-man's switch)  │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Components

### 3.1 Collector (Go)

**Responsibility:** get bytes from the internet, politely, and turn them into
observations. It does no interpretation beyond structural parsing.

**Why Go.** The collector is thousands of concurrent, mostly-idle HTTP requests.
Goroutines make this natural and cheap: 5,000 in-flight fetches consume roughly
80MB of RAM. Python asyncio can do this but with 5–8x the memory and a harder
deployment story. Node is competitive but weaker for the CPU-bound hashing and
HTML parsing that happens on every fetch.

**Subcomponents:**

| Subcomponent | Responsibility |
| --- | --- |
| Scheduler | Decides what to poll and when, from source yield history and hiring-season signals |
| Fetcher | Conditional HTTP with `ETag`/`If-Modified-Since`, connection pooling, timeout tiers |
| Politeness gate | robots.txt cache, per-host rate limits, `Crawl-delay`, concurrency caps |
| Change detector | Three layers: HTTP 304 → content hash → structural diff |
| Adapter registry | Dispatches to the right parser by source kind |
| Circuit breaker | Trips a source after consecutive failures; exponential re-probe |
| Observation writer | Append-only writes, batched, idempotent by content hash |

**Failure behavior.** Per-source circuit breakers. A hard-down source trips after
5 consecutive failures and re-probes on an exponential schedule capped at 6
hours. Collector crash loses at most one in-flight batch; the scheduler is
durable in Postgres, so restart resumes exactly where it stopped.

### 3.2 Brain (Python)

**Responsibility:** turn observations into ranked, deduplicated, explained jobs.

**Why Python.** This is where every AI library lives. `sentence-transformers`,
`tokenizers`, `scikit-learn`, `pydantic` for structured LLM output, and the
provider SDKs are all Python-first. Rewriting or reimplementing them in Go would
be a large cost for no benefit — the brain is not latency-critical in the way the
collector is, and it processes batches, not connections.

**Stages, each independently restartable and idempotent:**

| Stage | Input | Output | Cost profile |
| --- | --- | --- | --- |
| Normalize | raw_observation | canonical job draft | Pure CPU, ~2ms |
| Classify | job draft | role family, paid signal, location tier, seniority | Rules 90%, small LLM 10% |
| Deduplicate | classified job | job_group assignment | Hash 60%, SimHash 30%, embeddings 10% |
| Embed | job text | 384-dim vector | Local model, ~15ms batched |
| Score | job + user profile | 13 subscores + explanation | Deterministic + one LLM call for explanation |

Each stage is a queue consumer. Backpressure is natural: if scoring is slow, the
scoring queue grows and everything upstream keeps working.

### 3.3 API (Go)

**Responsibility:** serve the dashboard and any future clients.

Stateless. Horizontally scalable the moment we need it, though we will not for a
long time. REST with an OpenAPI 3.1 contract generated from code, plus
Server-Sent Events for live feed updates. Full surface in [04](04-api-design.md).

**Why not GraphQL.** One client, one developer, well-known access patterns.
GraphQL's benefit is client-driven query flexibility across many teams; its cost
is N+1 risk, caching complexity, and query-depth security. Wrong trade for us.

**Why SSE and not WebSockets.** Updates flow one direction — server to client.
SSE is plain HTTP, survives proxies, reconnects automatically with `Last-Event-ID`,
and needs no extra infrastructure. WebSockets would buy bidirectionality we do
not use.

### 3.4 Notifier (Go)

**Responsibility:** decide whether to interrupt the user, and do it once.

Kept separate from the brain because notification is a *decision about the user*,
not a property of the job, and because it must be independently rate-limited and
independently disable-able. Killing the notifier during a backfill must not stop
ingestion.

Fanout to native push (FCM), Telegram, Web Push, email, and Discord runs in
parallel with per-channel retry. Delivery is idempotent on
`(job_group_id, user_id, trigger)` — the uniqueness guarantee behind "never notify
twice" is a database constraint, not application logic.

**Two levels of deduplication, for two different problems.** The unique index
guarantees one *notification* per opportunity. A second, delivery-level suppression
guarantees one *buzz* per device — a phone registered for both native push and Web
Push must not receive two. The first is a correctness guarantee; the second is a
courtesy, and conflating them is how "we only sent one notification" ends up
technically true and practically wrong.

### 3.5 Web (Next.js)

Next.js 15 App Router. Server Components for the data-heavy feed, Client
Components only where interaction requires. Serwist for the service worker, Web
Push as the desktop notification fallback, offline shell for the saved-jobs view.
Full spec in [12](12-frontend-ux.md).

### 3.6 Mobile (Capacitor shell)

An Android binary whose main view is the same Next.js application —
**not a second UI codebase**. It exists to provide what the web cannot: FCM and
FCM delivery, lock-screen notification actions, badge counts, biometric unlock,
and the native share sheet. UI changes ship without rebuilding the binary. See
[ADR-012](adr/ADR-012-native-app-shell.md).

---

## 4. Data flow

### 4.1 The happy path, end to end

```
 T+0s     Scheduler selects source (Greenhouse board: stripe)
 T+0.1s   Politeness gate: robots.txt cached OK, rate budget available
 T+0.2s   Fetcher: GET with If-None-Match → 200 (new content)
 T+0.4s   Change detector: content hash differs from last → proceed
 T+0.5s   Greenhouse adapter parses 47 postings; 46 hashes known, 1 new
 T+0.6s   Observation written (append-only, raw snapshot → object store)
 T+0.7s   Enqueue: normalize
 T+1.0s   Normalize → canonical job draft
 T+1.2s   Classify → role=swe.backend, paid=true, tier=3 (remote), seniority=intern
 T+1.4s   Dedup stage 1: URL unseen. Stage 2: no structural match.
          Stage 3: embedding search finds nothing above 0.94 → new job_group
 T+1.6s   Embed → 384-dim vector stored
 T+2.1s   Score → priority 91; explanation generated by small LLM
 T+2.2s   Enqueue: notify
 T+2.4s   Notifier: trigger `high_score` fires (91 ≥ 85 threshold)
          Budget check OK · quiet hours OK · not previously sent
 T+2.6s   FCM delivered → banner on the phone with Apply/Save/Dismiss
 T+2.7s   Telegram delivered
 T+2.7s   Web Push skipped — same device already reached via FCM
 T+3.2s   SSE pushes the row into an open dashboard
```

Under four seconds from fetch to phone. The 10-minute p50 SLO is dominated by
*poll interval*, not by processing — which is why adaptive scheduling matters
far more than pipeline micro-optimization.

### 4.2 Backpressure and ordering

Queues are Postgres-backed via River. Each stage has its own queue with its own
concurrency limit. There is no global ordering requirement — jobs are independent
— which removes the hardest constraint a streaming system usually carries.

The one ordering rule: **dedup must be serialized per company.** Two workers
processing two postings from the same company concurrently could create two job
groups for one opportunity. Enforced with a Postgres advisory lock keyed on
`company_id` for the duration of the dedup stage.

### 4.3 Idempotency

Every stage is idempotent, because every stage will be re-run — after a crash,
after a backfill, after a bug fix.

| Stage | Idempotency key | Enforced by |
| --- | --- | --- |
| Observation write | `(source_id, content_hash)`, **scoped per monthly partition** | Unique index on each partition |
| Normalize | `observation_id` — overwrite the draft | Application |
| Dedup | Advisory lock on `company_id` + unique group membership | Postgres advisory lock + unique index |
| Score | `(job_id, user_id, weight_version)` — overwrite | Unique index |
| Notify | `(job_group_id, user_id, trigger)` unique — **the guarantee** | Unique index |

**The observation-write row needs its caveat stated, because the obvious reading
of it is wrong.** `raw_observation` is range-partitioned by `observed_at`, and
Postgres requires the partition key in any unique constraint declared on a
partitioned table. A unique index on `(source_id, content_hash, observed_at)`
would therefore be legal and would guarantee **nothing**: `observed_at` defaults
to `now()`, so re-fetching identical content a minute later inserts a second row.
An index that looks like a safeguard and is decorative is worse than no index,
because it stops anyone from looking.

The working mechanism is a unique index created directly on each *partition*,
where the restriction does not apply. The guarantee it buys is therefore scoped
honestly: **one row per (source, content) per month.** Re-polling unchanged
content within the month is an `ON CONFLICT DO NOTHING` no-op. Across a month
boundary the same content inserts once more — which is correct rather than a
leak, because it records that the posting still existed in the new month, and
that is exactly the signal the staleness detector in
[06](06-ingestion-pipeline.md) reads.

See `infra/migrations/000005_raw_observation.up.sql`, where this is implemented
and the reasoning is repeated at the point of use.

---

## 5. Technology decisions

Summary table; each row links to the ADR with rejected alternatives and the
conditions that would reverse the decision.

| Layer | Choice | Rejected | ADR |
| --- | --- | --- | --- |
| Repo | Monorepo, Turborepo + Go workspaces + uv | Polyrepo | [001](adr/ADR-001-monorepo-and-language-split.md) |
| Collector, API, Notifier | Go 1.23, chi, sqlc | Python, Node, Rust | [001](adr/ADR-001-monorepo-and-language-split.md) |
| Brain | Python 3.12, FastAPI, pydantic | Go with ONNX | [001](adr/ADR-001-monorepo-and-language-split.md) |
| Web | Next.js 15, TypeScript, Tailwind | Remix, SvelteKit, Vite SPA | [009](adr/ADR-009-frontend-stack.md) |
| Mobile | Capacitor shell over the web app | React Native rewrite, PWA-only | [012](adr/ADR-012-native-app-shell.md) |
| Push | FCM, Web Push as fallback | Web Push only | [012](adr/ADR-012-native-app-shell.md) |
| Chat channel | Telegram Bot API | WhatsApp (rejected — needs a dedicated number) | [013](adr/ADR-013-whatsapp-channel.md) |
| Primary store | PostgreSQL 16 + pgvector + pg_trgm | Mongo, Dynamo, split stores | [002](adr/ADR-002-postgres-as-the-primary-store.md) |
| Queue | River (Postgres) → NATS JetStream | **Kafka**, RabbitMQ, SQS | [003](adr/ADR-003-job-queue-over-kafka.md) |
| Cache | Redis 7 | Memcached, in-process | [002](adr/ADR-002-postgres-as-the-primary-store.md) |
| Search | Postgres FTS + pgvector → Meilisearch | **Elasticsearch**, Typesense | [004](adr/ADR-004-search-strategy.md) |
| Embeddings | `bge-small-en-v1.5` local, hosted fallback | OpenAI-only, `bge-m3` | [005](adr/ADR-005-llm-cascade.md) |
| LLM | Cascade over **rotated free tiers + local Ollama**; no frontier tier | Single frontier model, paid Tier 3 | [005](adr/ADR-005-llm-cascade.md), [016](adr/ADR-016-free-tier-llm-cascade.md) |
| Object store | **None — local disk** | R2, S3 | [014](adr/ADR-014-zero-cost-hosting.md) |
| Host | **Free-tier ARM64, portable in ≤1h** | Hetzner VPS, K8s, PaaS, serverless | [014](adr/ADR-014-zero-cost-hosting.md) *(supersedes 006)* |
| Network | **Tailscale only — no public ingress** | Cloudflare + public TLS | [014](adr/ADR-014-zero-cost-hosting.md) |
| CI/CD | GitHub Actions (public repo) → human-run deploy over Tailscale SSH | Automated deploy with a tailnet key in CI | [014](adr/ADR-014-zero-cost-hosting.md) |
| Telemetry | OpenTelemetry → Grafana stack + external dead-man's switch | Datadog, New Relic, Uptime Kuma on a second host | [011](adr/ADR-011-observability-stack.md) |
| Auth | **Network gate + bearer token** | Passkeys, passwords, OAuth, Auth0 | [015](adr/ADR-015-single-user-auth.md) *(supersedes 010)* |
| Backup | **Tiered by recoverability, to disk already owned** | pgbackrest + WAL to object storage | [017](adr/ADR-017-tiered-backup-without-object-storage.md) |

---

## 6. Scale, and the absence of a scaling path

**Scout is a single-user personal system and this document no longer plans for it
being anything else.** That is a change from the earlier version, which specified
four scaling stages up to 5,000 users, and the change is deliberate.

### The actual load

| | MVP | Year 1 |
| --- | --- | --- |
| Sources | 400 | 2,500 |
| Observations/day | 20,000 | 150,000 |
| Users | 1 | 1 |
| Peak queue throughput | ~1.4 msg/s | ~10.4 msg/s |
| Database size | ~4 GB | ~40 GB |

The host is 4 ARM cores and 24 GB RAM ([ADR-014](adr/ADR-014-zero-cost-hosting.md)).
400 sources at an average 15-minute cadence is ~38k polls/day, ~85% of which
return `304`, with peak concurrency under 50. **The node sits at roughly 5%
utilization at MVP and under 20% at Year 1 load.**

There is no scaling path because there is nothing to scale toward. Year 1 load on
this host is comfortable, and the next user does not exist.

### Why the four stages were removed

The earlier Stages 2–4 specified a second collector node, managed Postgres with a
read replica, NATS JetStream, Meilisearch as primary search, a CDN, Citus
sharding, and the conditions under which Kubernetes and Kafka become justified.

None of it was wrong. All of it was **work being planned for a system that does
not exist**, and AGENTS.md prohibits exactly that under "do not add speculative
abstraction." The plan was applying that rule to other people's ideas and
exempting its own.

Concretely, keeping those stages cost real decisions: user-side/job-side score
splitting designed for per-user fanout, Row-Level Security on every table,
`user_id` threaded through paths that will only ever see one, and a Redis-backed
distributed rate limiter for collector instances that will number one. Each was
individually cheap. Together they are a week of work and a permanent tax on every
query, spent on a hypothetical.

### What is kept, because it is free

Three things survive the cut, and the test for each is that it costs nothing
today:

- **`user_id` columns stay in the schema.** They are already written, they cost
  no query complexity when there is one value, and removing them would be work.
  RLS does **not** stay — see [13](13-security-privacy.md) section 4.
- **The job-side / user-side score distinction stays as documentation**, in
  [09](09-ranking-scoring.md). It is a useful way to think about what a score
  *means* regardless of tenancy, and writing it down costs nothing.
- **Every ADR keeps its reversal triggers**, including the ones that read "if this
  becomes multi-user." The analysis stays available; the implementation does not
  happen in advance.

### If it ever does go multi-user

One paragraph, deliberately, replacing three stages:

Enable RLS, add a real auth system ([ADR-010](adr/ADR-010-authentication.md) is
already written and is the specification to implement), move to a paid host
because free tiers do not permit serving other people, revisit the copyright
posture in [14](14-legal-compliance.md) section 7 because mass display of
copyrighted job descriptions is materially different from personal use, and
precompute job-side subscores once rather than per user. Roughly three weeks,
none of it blocked by anything in the current design.

**That paragraph is the entire multi-tenant plan, and it is enough.** Writing it
as a paragraph rather than as three milestone specifications is the point: it
records that the path exists and is not expensive, without spending anything on
it now. Building Stage 4 infrastructure for Stage 1 load remains the single most
common way projects like this die — the operator spends all their time on
infrastructure and none on the product, and then stops.

---

## 7. Failure domains and degradation

The core design property. Each row is a real failure and the intended behavior.

| Failure | Blast radius | Degraded behavior | Recovery |
| --- | --- | --- | --- |
| One source 500s | That source | Circuit breaker trips, others unaffected | Exponential re-probe to 6h |
| Adapter throws | That source kind | Observations queue up unparsed | Fix, redeploy, replay from queue |
| LLM provider down | Explanations, cover letters | Deterministic scoring only; explanation shows "pending" | Auto-resume on provider recovery |
| Embedding model OOM | Semantic dedup, semantic search | Falls back to structural dedup; search falls back to FTS | Restart with smaller batch |
| Redis down | Rate limiting, caching | Rate limits fall back to conservative in-process; cache misses | Restart; no data loss (cache only) |
| Postgres down | **Everything** | Hard outage | Restore from PITR; see [runbook](runbooks/database-recovery.md) |
| Notifier down | Notifications | Ingestion and ranking continue; notifications queue durably | Restart drains the queue |
| Web down | Dashboard | Notifications still deliver — the critical path survives | Restart |
| Disk full | Everything | Writes fail | Alert at 80%; auto-prune snapshots at 90% |
| Telegram API down | Telegram channel | Native push still delivers | Retry with backoff |
| FCM down | Native push | Telegram still delivers | Retry with backoff; FCM queues on its side |
| Push token stale | **That device, silently** | Telegram delivers; the device receives nothing | Alert on `push_token_invalid`; user reopens the app to re-register |
| **Free tier reclaims the host** | **Everything** | Hard outage until migrated | Rehearsed migration, ≤1h ([ADR-014](adr/ADR-014-zero-cost-hosting.md)) |
| **Tailscale down** | Dashboard only | Notifications still deliver — FCM and Telegram are outbound | Wait; no action available or needed |
| **All LLM free tiers rate-limited** | Explanations, ambiguous classification | Local Ollama serves Tier 2; then degrade to rules | Automatic ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)) |
| **Scout stops entirely and silently** | **Everything** | Nothing runs, nothing alerts, nobody notices | Dead-man's switch fires from off-host ([15](15-infrastructure-deployment.md) §7) |

**Three of those rows are new and all three are the cost of ₹0.** Provider
reclamation and free-tier exhaustion do not exist on a paid host. Each is bounded
rather than open-ended: one rehearsed hour, one automatic fallback, one external
heartbeat.

**The Tailscale row is worth reading twice.** Losing the only network component
in the design costs the dashboard and nothing else, because discovery, ranking,
and notification are all outbound paths. The product's core value survives an
outage of its own network layer — which is a property worth having noticed on
purpose.

**Postgres is the single point of failure and we accept it.** Removing it means
multi-node consensus, which for a one-user system costs vastly more than the
occasional hour of downtime it prevents. Mitigation is depth of backup, not
redundancy — but the *shape* of that backup changed with the budget
([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)): hourly dumps of
the few kilobytes that are genuinely irreplaceable, nightly dumps of the bulk
that is re-derivable by re-polling the internet, two independent off-host
destinations, and a *tested* restore. There is no point-in-time recovery
anymore, which makes the two-phase rule for destructive migrations the primary
protection rather than a convenience on top of PITR. Untested backups are not
backups; restore drills are a monthly calendar item.

---

## 8. Security architecture

Detail in [13](13-security-privacy.md). Structurally:

- **Network.** **Nothing is exposed to the internet.** Access is over Tailscale;
  Caddy binds to the tailnet interface and no container publishes a host port,
  asserted by the deploy health gate. Postgres, Redis, and Ollama are reachable
  only from inside the Docker network.
- **Egress.** The collector is the only component making arbitrary outbound
  requests, and it enforces an SSRF guard: no private IP ranges, no
  link-local, no redirect chains longer than 3, no non-HTTP(S) schemes.
- **Secrets.** SOPS-encrypted with age, decrypted at container start. Never in
  the image, never in the repo, never in an environment variable printed by a
  crash log.
- **Auth.** Network membership is the outer gate; a bearer token is the inner one
  ([ADR-015](adr/ADR-015-single-user-auth.md)). No password exists to leak.
- **Untrusted input.** Every byte from the internet is hostile. HTML sanitized
  before storage and again before render. LLM output is parsed as structured data
  against a schema, never executed, never rendered as HTML.

---

## 9. Key architectural risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Postgres as queue hits contention | Ingestion stalls | River is proven past 10k jobs/s; our peak is ~5/s. Migration path to NATS documented and small. |
| Single node loses disk | Total data loss | WAL archived off-node continuously; monthly restore drills |
| Adapter drift as ATS platforms change HTML | Silent coverage loss | Contract tests against recorded fixtures; yield-drop alerting catches silent breakage |
| Embedding model drift on version bump | Dedup breaks silently | Embedding version is a column; re-embedding is a batch job; dedup compares only within a version |
| LLM cost spike from a bug | Budget blown overnight | Hard per-day spend cap enforced at the client layer, plus a kill switch |
| Notification storm from backfill | User loses trust in one night | Backfills set a `suppress_notifications` flag; enforced at the notifier, tested |

---

## 10. What we are deliberately not building

Recorded so future-us does not relitigate:

**A microservice per concern.** Five services, not twelve. Service boundaries cost
network hops, deploy complexity, and distributed debugging. Our boundaries are
drawn where failure isolation genuinely matters and nowhere else.

**A second UI for mobile.** The app is a shell around the same web application.
Every screen built once. See [ADR-012](adr/ADR-012-native-app-shell.md).

**An event-sourced core.** Observations are already append-only, which gives us
most of the audit benefit. Full event sourcing would add projection complexity to
a domain with simple state transitions.

**A plugin marketplace / DSL for adapters.** Adapters are Go code implementing an
interface. A configuration DSL sounds flexible until the first source needs
pagination with a cursor in a header.

**Multi-region.** For one user in one timezone, this is pure cost.

**Multi-tenancy, in any form that costs work today.** Not RLS, not per-user score
precomputation, not a distributed rate limiter, not the four scaling stages this
document used to specify. `user_id` stays in the schema because it is already
there and free; everything else waits until a second user exists. See section 6.

**A separate analytics store.** Postgres with proper indexes answers every
analytics question we have at this data size. Revisit at 500GB.
