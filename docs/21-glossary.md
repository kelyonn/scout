# Glossary

**Status:** Draft

Domain terms used consistently across the Scout documentation set. Where a term
has a common industry meaning that differs from ours, the difference is noted.

---

## Core entities

**Source** — A single monitored endpoint that can yield job postings. A source is
not a company: `greenhouse.io/stripe` and `stripe.com/jobs` are two sources for
one company. Every source has an access method, a poll schedule, a politeness
policy, and a health record.

**Adapter** — Code that knows how to talk to one *class* of source. The Greenhouse
adapter serves all 3,000+ Greenhouse boards. Adapters are plugins; adding a source
type means adding an adapter, never modifying the pipeline.

**Observation** — One sighting of one posting from one source at one point in
time. Observations are append-only and immutable. Multiple observations of the
same posting over time form its history, which is how we detect edits, closures,
and reposts.

**Job** — The canonical, normalized record derived from observations. One job may
be supported by many observations from many sources.

**Job group** — A set of jobs judged to be the same real-world opportunity. The
unit of notification: you are notified once per job group, never once per job.
The distinction exists because the same Cloudflare internship appears on
Greenhouse, the Cloudflare careers page, and Hacker News as three jobs and one
job group.

**Company** — A resolved organizational identity. Resolution is nontrivial:
"Meta", "Facebook", "Meta Platforms Inc.", and `meta.com` are one company.

**Posting** — Informal synonym for a job as it exists on the source side. Avoided
in code; use *observation* or *job*.

---

## Discovery and ingestion

**Access method** — How a source is read, in descending order of preference:
official API, RSS/Atom feed, JSON feed, webhook, sitemap, structured HTML,
rendered HTML. Preference order is a hard rule, see [06](06-ingestion-pipeline.md).

**Change detection** — Determining whether a source has new content without
downloading and parsing everything. Layered: HTTP `ETag`/`Last-Modified`, then
content hash, then structural diff. Each layer avoids work the next would do.

**Conditional fetch** — An HTTP request carrying `If-None-Match` or
`If-Modified-Since`, allowing the server to answer `304 Not Modified` with no
body. The cheapest possible poll. Roughly 85% of our polls should end this way.

**Politeness policy** — Per-source constraints on request behavior: rate limit,
concurrency cap, `Crawl-delay` compliance, backoff curve, and User-Agent. Scout
identifies itself honestly in every request.

**Poll schedule** — Per-source cadence, adaptive. High-yield sources during
hiring season are polled every 5 minutes; dormant sources drop to daily. Governed
by the yield-based scheduler in [06](06-ingestion-pipeline.md).

**Yield** — New unique jobs discovered per 100 polls of a source. The primary
input to scheduling. A source with zero yield over 30 days is demoted, then
quarantined.

**Inbox ingestion** — Parsing job alert emails delivered to a Scout-owned address
that the user has subscribed with. Legitimate access to boards that prohibit
scraping. See [ADR-007](adr/ADR-007-no-tos-violating-scraping.md).

**Quarantine** — State for a source that is failing, hostile, or yielding
nothing. Quarantined sources are not polled but are retained for audit and
periodic re-evaluation.

---

## Normalization

**Canonical schema** — The single job shape every adapter must produce. Defined
once in `packages/schema` and shared by Go, Python, and TypeScript via generated
types. See [07](07-normalization-taxonomy.md).

**Role taxonomy** — A controlled hierarchy of job families (`swe.backend`,
`swe.ml`, `swe.infra.sre`, …) that titles are mapped into. Exists because titles
are unreliable: "Member of Technical Staff", "SDE-1", and "Graduate Engineer" can
all be the same role.

**Location tier** — Scout's ranking geography, distinct from the raw location
string. Tier 1 Bengaluru, Tier 2 rest of India, Tier 3 remote, Tier 4
international. See [09](09-ranking-scoring.md).

**Compensation parsing** — Extracting a normalized stipend or salary from free
text like "₹80,000/month", "competitive", or "$8,500 monthly". Produces a value,
a currency, a period, and a confidence.

**Paid signal** — The determination that a role is compensated. Three states:
`paid` (explicit compensation or strong inference), `unpaid` (explicitly stated),
`unknown` (no signal). Only `paid` and flagged prestige exceptions notify.

---

## Deduplication

**Exact match** — Stage 1. Identical canonical URL or identical ATS-native job ID.
Certain, cheap, catches roughly 60% of duplicates.

**Structural match** — Stage 2. Same company, normalized title, and location, with
a SimHash similarity above threshold on the description. Catches cross-posting.

**Semantic match** — Stage 3. Embedding cosine similarity above threshold within
a company-and-role-family candidate set. Catches rewrites and translations.

**SimHash** — A locality-sensitive hash where similar documents produce hashes
with small Hamming distance. Used for cheap near-duplicate detection before the
expensive embedding step.

**Canonical URL** — A URL stripped of tracking parameters, session IDs, and
redirect wrappers, then normalized for case and trailing slash. The stable
identity of a posting on the web.

**Merge** — Joining two job groups after determining they are the same
opportunity. Merges are recorded and reversible; an incorrect merge means a
missed opportunity, so merges are logged for audit.

---

## Ranking

**Subscore** — One of thirteen independently computed 0–100 dimensions:
skill match, resume match, company quality, compensation, learning opportunity,
engineering culture, growth potential, interview probability, competition
estimate, ease of applying, deadline urgency, overall match, priority.

**Priority score** — The final 0–100 ranking value. A weighted combination of
subscores, multiplied by location tier and freshness factors. The only number
used for ordering and notification thresholds.

**Weight vector** — The coefficients combining subscores. Hand-tuned at MVP,
learned from user feedback at P5, if enough labels accumulate.

**Calibration** — Ensuring a score of 80 means the same thing in January and in
June. Without it, score inflation makes fixed notification thresholds useless.

**Explanation** — Human-readable prose stating why a job scored as it did.
Required for every job; a score without one is considered incomplete.

**Learning loop** — The feedback cycle: user saves, applies, dismisses, or
ignores → labelled examples → periodic model retraining → updated weights.

---

**Company type** — What kind of employer this is: `product`, `services_*`, `gcc`,
`core_*`, `research`, `public_sector`, `nonprofit`. Drives adapter choice,
scheduling, competition estimation, and coverage auditing. **Never a ranking
input.**

**GCC (Global Capability Centre)** — A foreign company's owned engineering centre
in India. Modelled as its own `company_type` with a `parent_company_id`, because
the parent's industry describes the business and not the engineering organisation.
Concentrated in Bengaluru and largely absent from startup-oriented ATS platforms.

**Advocacy family** — `advocacy.devrel`, `advocacy.devex`, `advocacy.solutions`.
Developer-facing engineering roles. Admitted only with concrete technical evidence
in the description, which is what keeps developer marketing and sales roles out.

**Technical evidence gate** — The requirement that an `advocacy.*` posting name a
language, SDK, API, or coding responsibility before it is accepted as engineering.

---

## Notifications

**Trigger** — A named rule that can cause a notification: `bengaluru_match`,
`high_score`, `watchlist_hiring`, `deadline_approaching`, `prestige_opening`,
`remote_high_quality`, `newgrad_match`. Each has its own threshold and channel
routing.

**Channel** — A delivery mechanism: native push, Telegram, Web Push, email,
Discord, in-app. The first two are *primary* — a notification is expected on both,
and delivery succeeds if either one lands.

**Native push** — FCM delivery to the Capacitor app shell on Android. APNs is the
iOS equivalent, supported by the design but not built.
Distinguished from Web Push, which is browser-based and used as a desktop and
not-installed fallback.

**Delivery-level dedup** — Suppressing a *second delivery of one notification* to a
device reachable on two transports, distinct from the notification-level uniqueness
guarantee. Recorded as `status = 'skipped'`, not as a failure.

**Urgency class** — `INSTANT` (deliver now, breaks quiet hours only for Tier 1
Bengaluru), `BATCHED` (hourly rollup), `DIGEST` (daily 08:00 IST).

**Notification budget** — A cap on notifications per window (default 8/hour,
25/day) preventing a source backfill or bug from flooding the user.

**Quiet hours** — 00:00–07:30 IST by default. Notifications are queued and
delivered at the boundary, never dropped.

---

## AI

**Cascade** — The tiered decision pipeline: deterministic rules → embeddings →
small LLM → large LLM. Each tier handles what it can and escalates only what it
cannot. Roughly 92% of decisions terminate before reaching any LLM.

**Embedding** — A dense vector representing text meaning. Used for semantic
dedup, semantic search, role classification, and resume matching.

**RAG** — Retrieval-augmented generation. Grounding LLM output in retrieved job
descriptions, company data, and the user's resume rather than model memory.

**Golden set** — A hand-labelled dataset of jobs with known-correct
classifications, used to measure quality and catch regressions in CI.

**Eval harness** — The automated system that runs the golden set against the
current pipeline and reports precision, recall, and F1 per stage. A CI gate.

**Confidence** — A model's self-reported certainty, used to decide escalation.
Low confidence at a cheap tier routes to an expensive tier.

---

## Operations

**Failure domain** — The blast radius of a component failing. Scout's are
deliberately small: one adapter, one source, one worker.

**Backfill** — Reprocessing historical observations after a pipeline change.
Must never produce notifications; backfilled records bypass the notifier.

**Ghost posting** — A posting that remains live but is not actually being hired
for. Detected via age, repost frequency, and description staleness. Downranked,
not hidden.

**Freshness decay** — The exponential reduction of priority score with posting
age, reflecting that application value decays fast.

**Watchlist** — A user-defined set of companies whose *first* posting of any
relevant role triggers a notification regardless of score.

---

## Abbreviations

| Term | Meaning |
| --- | --- |
| ATS | Applicant Tracking System |
| FCP / TTI | First Contentful Paint / Time to Interactive |
| IST | India Standard Time (UTC+5:30) |
| JD | Job description |
| LSH | Locality-sensitive hashing |
| PWA | Progressive Web App |
| RSC | React Server Component |
| SLO / SLI | Service Level Objective / Indicator |
| VAPID | Voluntary Application Server Identification (Web Push auth) |
| WCAG | Web Content Accessibility Guidelines |
