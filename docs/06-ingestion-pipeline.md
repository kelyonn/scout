# Ingestion Pipeline — Scout

**Status:** Draft · **Owner:** Discovery · **Last updated:** 2026-08-06

How Scout decides what to fetch, fetches it politely, detects what changed, and
turns bytes into observations.

---

## 1. Objectives

`SCOUT-ING-001` Discover new postings within one poll interval of publication.
`SCOUT-ING-002` Never violate a source's stated access policy.
`SCOUT-ING-003` Minimize bandwidth — most polls should transfer no body at all.
`SCOUT-ING-004` Fail per-source, never globally.
`SCOUT-ING-005` Detect silent failure, where a source returns success and nothing.

The fifth is the hardest and the most important. A source that returns HTTP 200
with an empty job list because its HTML changed looks perfectly healthy to every
conventional metric. This is addressed in section 7.

---

## 2. Pipeline stages

```
┌────────────┐  ┌──────────┐  ┌───────────┐  ┌─────────┐  ┌────────────┐
│ SCHEDULER  │─▶│POLITENESS│─▶│  FETCHER  │─▶│ CHANGE  │─▶│  ADAPTER   │
│  what/when │  │   gate   │  │conditional│  │ DETECT  │  │   parse    │
└────────────┘  └──────────┘  └───────────┘  └─────────┘  └─────┬──────┘
                                                                 │
                                       ┌─────────────────────────▼──────┐
                                       │  OBSERVATION WRITER            │
                                       │  append-only + snapshot to R2  │
                                       │  + transactional enqueue       │
                                       └────────────────────────────────┘
```

Every stage can reject and short-circuit. The politeness gate can refuse to
fetch. The change detector can stop before parsing. The adapter can produce zero
observations. Each rejection is recorded with a reason, because the distribution
of rejection reasons is itself a health signal.

---

## 3. Scheduler

### 3.1 The adaptive interval

A fixed poll interval is wrong in both directions: too slow for a company that
posts daily, too fast for one that posts twice a year. The scheduler adapts per
source based on measured behavior.

```
interval = clamp(
    base_interval
      × yield_factor
      × recency_factor
      × season_factor
      × failure_factor,
    min_interval,
    max_interval
)
```

| Factor | Range | Meaning |
| --- | --- | --- |
| `yield_factor` | 0.3 – 4.0 | High yield → poll more. `0.3 + 3.7 × e^(−2 × yield_ratio)` |
| `recency_factor` | 0.5 – 2.0 | Posted in the last 7 days → 0.5. Nothing in 90 days → 2.0 |
| `season_factor` | 0.6 – 1.5 | Aug–Oct and Jan–Mar are peak internship season → 0.6 |
| `failure_factor` | 1.0 – 8.0 | Exponential backoff on consecutive failures |

**Worked example.** A Greenhouse board with `base = 900s` that posted a job
yesterday, during peak season, with a good yield ratio:

```
900 × 0.45 (high yield) × 0.5 (recent) × 0.6 (peak season) × 1.0 = 121s
→ clamped to min_interval (300s) = 5 minutes
```

**Cyclical sources get a lower ceiling.** Adaptive backoff assumes a source that has
been quiet is likely to stay quiet. That assumption is wrong for IT services firms,
consultancies, and campus platforms, which post nothing for weeks and then open a
drive that fills within days. Left alone, `yield_factor` would push them to a
24-hour interval and Scout would find the drive on day two.

Sources with `hiring_pattern = 'cyclical'` therefore use `max_interval = 4h`
regardless of recent yield:

```
effective_max = (hiring_pattern = 'cyclical') ? 4h : max_interval_s
```

The cost is small — a few hundred extra fetches a day against sources that are
usually unchanged, and conditional requests make most of those free. The failure it
prevents is not small: missing the opening of a campus drive is missing the whole
opportunity, since these close on volume rather than on a deadline.

The same board in June with no posting in 90 days:

```
900 × 3.8 (no yield) × 2.0 (dormant) × 1.2 (off-season) × 1.0 = 8,208s
→ ~2.3 hours
```

A 27x difference in polling cost between the two states, discovered
automatically. This is what makes 2,500 sources affordable on one small VPS.

### 3.2 Selection query

The scheduler's hot loop, running every 10 seconds:

```sql
SELECT id, kind, url, adapter_config, max_rps, last_etag, last_modified
FROM source
WHERE status = 'active'
  AND legal_posture IN ('permitted', 'api_only')
  AND next_poll_at <= now()
  AND (circuit_open_until IS NULL OR circuit_open_until <= now())
ORDER BY priority_tier ASC, next_poll_at ASC
LIMIT 200
FOR UPDATE SKIP LOCKED;
```

`FOR UPDATE SKIP LOCKED` means multiple scheduler instances can run without
coordination — each grabs a disjoint batch. `LIMIT 200` bounds the batch so one
scheduler tick cannot flood the fetcher.

The partial index on `source_due_idx` keeps this query at sub-millisecond even
with 10,000 sources, because it only indexes rows that are actually pollable.

### 3.3 Jitter

Every computed `next_poll_at` gets ±10% random jitter. Without it, sources
registered in the same batch synchronize forever and produce periodic thundering
herds against shared infrastructure — 400 Greenhouse boards hitting
`boards-api.greenhouse.io` in the same second is both rude and slow.

---

## 4. Politeness gate

`SCOUT-LEGAL-001` through `SCOUT-LEGAL-006`. No request leaves the collector
without passing all six checks.

```
1. legal_posture == 'permitted' or 'api_only'   → else REFUSE (hard)
2. robots.txt allows this path for our UA        → else REFUSE
3. per-host rate budget available                → else DEFER
4. per-host concurrency below cap                → else DEFER
5. Crawl-delay elapsed since last request        → else DEFER
6. circuit breaker closed                        → else SKIP
```

**REFUSE** is terminal and logged as a compliance event. **DEFER** reschedules.
**SKIP** waits for the breaker.

### robots.txt

Fetched once per host, cached 24 hours in Redis with a Postgres fallback so a
Redis flush does not cause a stampede of robots.txt requests. Parsed per RFC 9309.

- We match the `Scout` user-agent group, falling back to `*`.
- `Crawl-delay` is honored even though it is non-standard. If a site asks us to
  slow down, we slow down.
- A 4xx on robots.txt means "no restrictions" per the RFC. A 5xx means "unknown"
  and we treat it as disallowed until it resolves — conservative on purpose.
- Fetch failures do not fail open.

### Rate limiting

Token bucket per registered domain (not per URL), in Redis so it is shared across
collector instances:

```
key:    ratelimit:host:{registered_domain}
rate:   source.max_rps, default 0.5/s
burst:  2
```

Per registered domain matters: `boards.greenhouse.io` serves thousands of our
sources, and rate limiting per URL would let us hammer their single origin with
thousands of concurrent requests. Shared ATS hosts get an explicit, higher, but
still bounded budget (`greenhouse.io: 5 rps`) negotiated against their published
guidance.

### Identification

Every request carries:

```
User-Agent: Scout/1.0 (+https://<domain>/bot; personal job discovery agent)
From: <operator-email>
Accept-Encoding: gzip, br
```

We identify honestly and provide contact information. If an operator wants us to
stop, they can tell us, and we will. This is not only ethical, it is practical: a
site operator who can reach you sends an email instead of a block.

### Circuit breaker

Per source, three states:

| State | Behavior | Transition |
| --- | --- | --- |
| Closed | Normal | 5 consecutive failures → Open |
| Open | No requests | After `backoff`, → Half-open |
| Half-open | One probe request | Success → Closed. Failure → Open, backoff doubles |

Backoff: `min(60s × 2^(failures−5), 6 hours)`. A source down for a day is probed
every 6 hours rather than every 5 minutes, which respects both their
infrastructure and ours.

**429 and 503 with `Retry-After` are honored exactly.** If a server tells us when
to come back, we come back then, not before.

---

## 5. Fetcher

### Conditional requests — the bandwidth story

```http
GET /v1/boards/cloudflare/jobs?content=true HTTP/1.1
Host: boards-api.greenhouse.io
If-None-Match: "a3f8b2c9e1"
If-Modified-Since: Wed, 06 Aug 2026 09:15:00 GMT
Accept-Encoding: gzip, br
```

A `304 Not Modified` costs ~500 bytes and ~0.1ms. A `200` with a 300KB body costs
600x that. **Target: 85% of polls return 304.**

Measured impact at 2,500 sources polling on average every 20 minutes:

| | Without conditional requests | With, at 85% 304 rate |
| --- | --- | --- |
| Daily requests | 180,000 | 180,000 |
| Daily transfer | ~36 GB | ~5.5 GB |
| Daily parse CPU | ~90 min | ~14 min |

Not every source supports ETags. For those, the content-hash layer below does the
same job one step later — it saves the parse but not the transfer.

### Timeouts

Tiered, because a slow source must not block others:

| Phase | Timeout |
| --- | --- |
| DNS | 3s |
| TCP connect | 5s |
| TLS handshake | 5s |
| First byte | 10s |
| Total (standard) | 30s |
| Total (rendered HTML) | 60s |

Response bodies are capped at 10 MB. Anything larger is truncated and flagged —
a career page returning 50 MB is either misconfigured or a trap.

### SSRF protection

The collector fetches attacker-influenceable URLs (from email alerts, from HTML
links), so this is a real attack surface, not a theoretical one:

- Resolve DNS first, then check the resolved IP, then connect to that IP with the
  hostname pinned for TLS. Checking the hostname alone is vulnerable to
  DNS rebinding.
- Reject private ranges: `10/8`, `172.16/12`, `192.168/16`, `127/8`, `169.254/16`,
  `::1`, `fc00::/7`, `fe80::/10`.
- Reject any scheme other than `http` and `https`.
- Follow at most 3 redirects, re-checking the IP at every hop.
- Reject responses that redirect to a different scheme.

---

## 6. Change detection

Three layers, each avoiding the work of the next.

```
Layer 1 — HTTP validators (ETag / Last-Modified)
  304 → done, zero bytes, zero parse.       ~85% of polls
      ↓ 200
Layer 2 — Content hash
  sha256 of the body after normalizing whitespace,
  removing timestamps, session tokens, CSRF tokens, and
  known-volatile fragments (view counters, "posted N hours ago").
  Match → done, no parse.                   ~9% of polls
      ↓ differs
Layer 3 — Structural diff
  Parse, extract the job list, hash each posting's identity
  (external_id or canonical_url) plus content.
  Emit observations only for new or changed postings.  ~6% of polls
```

**Layer 2's normalization matters more than it sounds.** Many career pages embed
a render timestamp, a CSRF token, or a "12 jobs found · updated 3 minutes ago"
string. Without stripping those, every single poll produces a different hash and
Layer 2 never fires. The normalizer strips:

- Any ISO-8601 or common date format in the body
- `csrf`, `nonce`, `_token`, and session identifiers in attributes
- Relative time strings ("2 hours ago", "yesterday")
- Comment nodes
- Whitespace runs

The strip rules are per-source-kind and extensible via `adapter_config`.

**Layer 3 is where per-posting identity matters.** A board with 47 postings where
one changed should produce one observation, not 47. Identity is `external_id`
where the source provides it, else the canonical URL hash.

---

## 7. Detecting silent failure

`SCOUT-ING-005`, the requirement that everything else in this document exists to
serve.

A source can fail in a way that looks like success:

| Silent failure | Detection |
| --- | --- |
| HTML changed; selectors match nothing | Parse yields 0 items where history says 20+ |
| API returns an empty list due to auth change | Same — item count anomaly |
| Site returns a soft-404 page with 200 | Content hash stable but item count drops to 0 |
| Rate limited into an error page with 200 | Response size anomaly |
| Adapter regression after a deploy | Aggregate yield drop across all sources of one kind |

**Three detection mechanisms:**

**1. Per-source item count anomaly.** Each source keeps a rolling 30-day median
item count. A poll returning fewer than 20% of the median, when the median is
above 5, flags the source as `suspected_broken` and alerts. This catches
individual source breakage within one poll.

**2. Aggregate yield monitoring.** `scout_source_yield_ratio` per source kind,
compared week over week. A drop above 40% for an entire `source_kind` means the
adapter broke, not the sources — this is the deploy-regression detector. It
alerts even when every individual source looks borderline-plausible.

**3. Canary sources.** Five sources per adapter kind are designated canaries and
have a known-good recorded fixture in the repo. A nightly job fetches them live
and diffs the parse result against the fixture's *shape* (field presence, types,
count within tolerance). A shape change alerts immediately, before yield metrics
would show anything. This is the earliest possible warning.

---

## 8. Adapters

### The interface

```go
type Adapter interface {
    Kind() SourceKind
    // Fetch performs the network request(s) and returns raw bytes plus validators.
    Fetch(ctx context.Context, src Source, hints FetchHints) (*RawResponse, error)
    // Parse converts raw bytes into postings. Must be pure and deterministic.
    Parse(ctx context.Context, src Source, raw *RawResponse) ([]Posting, error)
    // Validate reports whether a parse result is plausible for this source.
    Validate(ctx context.Context, src Source, postings []Posting) error
}
```

`Parse` being pure and deterministic is what makes fixture-based testing possible:
record a real response once, replay it in CI forever, and a parse regression fails
the build rather than production.

`Validate` is the per-adapter expression of section 7 — a Greenhouse adapter
knows that a board returning zero jobs when it returned 40 yesterday is
suspicious, and can say so.

**Who actually calls `Fetch`.** For every adapter through P2 (Greenhouse,
Lever, Ashby, Workable, SmartRecruiters, Recruitee, Teamtailor), the
scheduler never calls it at all: `Fetch` is one conditional GET to
`Source.URL`, which the scheduler already performs generically for every
source before it even knows which adapter is registered, so the same
response is simply handed to `Parse`. That single shared fetch is also
what makes each adapter's own integration test fakeable — the scheduler's
fetch is an injectable interface, while `fetch.Fetcher` itself is a
concrete, SSRF-guarded type no adapter-level test can point at a local
fixture server.

Workday broke that assumption (P3): its CXS endpoint requires a POST with
a JSON search body, which no GET can produce, and paginates besides. An
adapter that genuinely cannot be represented as one conditional GET
implements `padapter.OwnFetcher` (`RequiresOwnFetch() bool`), and the
scheduler calls that adapter's own `Fetch` directly instead — see
`apps/collector/internal/scheduler.fetchResult` and that interface's own
comment for the full reasoning, including why this is a narrow, opt-in
exception rather than how every adapter is driven.

### Adapter build order

| Milestone | Adapters | Cumulative company coverage |
| --- | --- | --- |
| P1 | Greenhouse, Lever, Ashby, HN, RSS/Atom | ~250 |
| P2 | JSON-LD, sitemap, Workable, SmartRecruiters, GitHub, YC | ~600 |
| P3 | Email alerts, **Workday**, Rippling, BambooHR, Recruitee, GCC seed list | ~900 |
| P5 | **SuccessFactors, Darwinbox, Keka, Zoho Recruit**, iCIMS, Jobvite, Indian services portals, campus platforms, Reddit, VC portfolios, funding feeds | ~1,500 |
| P5+ | Taleo, Telegram, Discord, hackathons | ~2,000+ |

Ordered by coverage-per-unit-effort, **with one deliberate exception**. Three
adapters in P1 reach 250 companies; the same effort spent on career-page scrapers
would reach maybe 30.

The exception is Workday, which has the worst effort-to-coverage ratio of anything
in P3 — per-tenant configuration, POST-based search, 30 requests where Greenhouse
costs one. It is scheduled early anyway because it is the **only** route to GCCs and
large enterprises. Ranking adapters purely by efficiency would leave an entire
company category uncovered indefinitely, which is a coverage bias arriving through
the build order rather than through the ranking formula — and therefore much harder
to spot. See [07](07-normalization-taxonomy.md) and [05](05-source-catalog.md).

---

## 9. Observation writing

The transactional core, and the reason for
[ADR-003](adr/ADR-003-job-queue-over-kafka.md):

```go
tx, _ := db.Begin(ctx)
defer tx.Rollback(ctx)

for _, p := range newPostings {
    obsID := insertObservation(tx, p)          // append-only
    riverClient.InsertTx(ctx, tx, NormalizeArgs{ObservationID: obsID}, nil)
}
updateSourceHealth(tx, src, result)

tx.Commit(ctx)                                  // atomic: data + work
```

Either the observation and its processing job both exist, or neither does. There
is no window in which an observation is committed with no job to process it, and
no window in which a job references an uncommitted observation.

**Raw snapshots** go to R2 *before* the transaction commits, keyed by
`{source_id}/{date}/{content_hash}`. Object storage writes are idempotent by key,
so a retry after a failed transaction just overwrites identical bytes. Snapshots
are retained 30 days and exist so that a normalization bug can be fixed by
reprocessing rather than by re-fetching every source.

---

## 10. Error handling

| Condition | Classification | Action |
| --- | --- | --- |
| DNS failure | Transient | Retry ×3 with backoff, then count as failure |
| Connection refused | Transient | Same |
| TLS error | Persistent | Fail immediately, alert — usually a real config change |
| 401 / 403 | Persistent | Quarantine the source, alert. Access changed. |
| 404 | Persistent | Mark `retired` after 3 consecutive. Board deleted. |
| 429 | Rate limited | Honor `Retry-After`, halve `max_rps` permanently |
| 5xx | Transient | Circuit breaker path |
| Timeout | Transient | Retry once, then failure |
| Parse error | Bug | Store raw, alert, do not retry — retrying a deterministic parse failure is pointless |
| Validation failure | Suspected breakage | Store raw, alert, keep the previous jobs live |
| Body too large | Anomaly | Truncate, flag, alert |

**Parse errors never lose data.** The raw response is already in R2. When the
adapter is fixed, a replay job reprocesses every failed observation. Nothing that
was fetched is ever unrecoverable.

---

## 11. Backfill and replay

Reprocessing is a first-class operation, not an emergency procedure.

```
POST /api/v1/admin/replay
{
  "source_kind": "ats_greenhouse",
  "from": "2026-08-01T00:00:00Z",
  "to":   "2026-08-06T00:00:00Z",
  "stages": ["normalize", "classify", "score"],
  "suppress_notifications": true,
  "dry_run": false
}
```

**`suppress_notifications` defaults to `true` and this is enforced at the
notifier, not just at the caller.** Replayed observations carry a `backfill=true`
flag through the pipeline, and the notifier drops them. A backfill that pages the
user with 400 notifications for jobs from last week is the kind of mistake that
destroys trust in the whole system permanently, so the guard lives at the last
possible point and has a dedicated test.

Backfills run on the `backfill` queue at low priority with concurrency 2, so they
cannot starve live ingestion.

---

## 12. Capacity

At Year 1 scale (2,500 sources):

| Metric | Value |
| --- | --- |
| Polls/day | ~180,000 |
| Polls/second (mean) | ~2.1 |
| Polls/second (peak) | ~15 |
| 304 responses | ~153,000 (85%) |
| Bodies parsed | ~11,000 (6%) |
| New observations/day | ~150,000 |
| New unique jobs/day | ~800 |
| Bandwidth/day | ~5.5 GB |
| Collector CPU (mean) | ~8% of 4 vCPU |
| Collector memory | ~400 MB steady |

The collector is not the bottleneck. The brain is, and it is bounded by
embedding throughput, which is why embeddings are batched and run locally rather
than per-job over a network.
