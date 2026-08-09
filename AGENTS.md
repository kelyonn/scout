# AGENTS.md

Instructions for AI agents working in this repository. Humans should read
[`CONTRIBUTING.md`](CONTRIBUTING.md), which covers the same ground with more
context.

---

## Read before writing code

This repository is **specification-first**. The documentation in `docs/` is not
descriptive, it is prescriptive — it defines behavior that code must implement.

Before implementing anything, read the relevant spec:

| Working on | Read first |
| --- | --- |
| Anything at all | [`docs/02-architecture.md`](docs/02-architecture.md) |
| Database, queries, migrations | [`docs/03-data-model.md`](docs/03-data-model.md) |
| API endpoints | [`docs/04-api-design.md`](docs/04-api-design.md) |
| Adapters, fetching, scheduling | [`docs/06-ingestion-pipeline.md`](docs/06-ingestion-pipeline.md) |
| Parsing, classification, taxonomy | [`docs/07-normalization-taxonomy.md`](docs/07-normalization-taxonomy.md) |
| Deduplication | [`docs/08-dedup-identity.md`](docs/08-dedup-identity.md) |
| Scoring, ranking | [`docs/09-ranking-scoring.md`](docs/09-ranking-scoring.md) |
| Anything using a model | [`docs/10-ai-features.md`](docs/10-ai-features.md) |
| Notifications | [`docs/11-notifications.md`](docs/11-notifications.md) |
| Frontend | [`docs/12-frontend-ux.md`](docs/12-frontend-ux.md) |

**If the code and the spec disagree, that is a bug in one of them.** Fix both in
the same pull request, or open an issue explaining why the spec is wrong.

---

## Hard rules

These are not style preferences. Violating any of them is a bug that reaches the
user.

### 1. Never bypass the compliance gate

Every outbound HTTP request goes through `PolitenessGate.Allow()`. No exceptions,
no direct `http.Get`, no "just for testing". A source marked `prohibited` must
generate zero requests through every code path.
See [`docs/14-legal-compliance.md`](docs/14-legal-compliance.md).

### 1a. Never use an unofficial WhatsApp library

`whatsapp-web.js`, `baileys`, `venom-bot`, and anything else that automates
WhatsApp Web or reimplements its protocol are **prohibited**. They violate
WhatsApp's terms and Meta enforces by banning the phone number — the user's real
account, with every recruiter conversation in it.

WhatsApp is not a Scout channel at all
([ADR-013](docs/adr/ADR-013-whatsapp-channel.md)), so this rule exists for the case
where someone decides to add it. If that ever happens it is the official Cloud API
or nothing. CI fails the build if a banned package appears in a dependency
manifest — this is the same rule as rule 1, applied to outbound messaging.

### 2. Never weaken notification deduplication

The unique index on `(user_id, job_group_id, trigger)` is the guarantee behind
"never notify twice". Do not add an `ON CONFLICT DO UPDATE` that circumvents it,
do not add a code path that inserts around it.

**There is a second, separate guarantee.** Delivery-level suppression keeps one
notification from producing two buzzes on a device reachable via both native push
and Web Push. It is recorded as `status = 'skipped'`, not as a failure. Do not
"fix" a skipped delivery by removing the suppression.

### 3. Backfills never notify

Any reprocessing path must set `suppress_notifications`. The guard lives at the
notifier, not the caller. Do not move it.

### 4. Bias dedup toward under-merging

A false merge is a silently missed opportunity. A missed merge is a visible
duplicate. When adjusting thresholds, favor precision. CI gates precision at
0.99 and recall at 0.92 for exactly this reason.

### 5. Model output never sets a score directly

LLMs classify and explain. Deterministic code scores. This is the structural
defense against prompt injection — see
[`docs/10-ai-features.md`](docs/10-ai-features.md) section 10.

### 6. Every AI call has a degraded mode

If the provider is down, the feature degrades. It does not error and it does not
block the pipeline.

### 7. Never log PII

No resume content, no email addresses, no phone numbers, no full job
descriptions, no tokens. Reference IDs instead.

### 8. Redis holds nothing durable

If losing Redis loses data, the data was in the wrong place. There is a chaos
test that flushes Redis and asserts zero data loss.

### 9. The user's own data never reaches a hosted model

Job descriptions are public documents and sending them to an LLM is fine. **The
resume, application history, interview notes, and rejection records are not, and
must never leave the host.**

Free LLM tiers commonly reserve the right to train on submitted data, and this
system runs entirely on free tiers ([ADR-016](docs/adr/ADR-016-free-tier-llm-cascade.md)).
The rule is enforced at the LLM client boundary, which refuses to construct a
request whose payload draws text from those tables — not by convention, because a
convention gets violated by whoever is adding a feature at midnight.

This costs almost nothing: resume matching is embedding cosine plus keyword
overlap and was always local, and the features that genuinely needed to send the
resume were cut with the frontier tier.

### 10. Nothing costs money

The budget is ₹0 and it is a wall, not a target
([ADR-014](docs/adr/ADR-014-zero-cost-hosting.md)). Do not add a dependency on a
paid service, a paid tier of a free service, or a resource outside a provider's
always-free limits. If a task appears to require one, that is a design question
to raise, not a ₹40/month line item to quietly add.

Corollary: **the repository is public**, which is what makes CI free. Never
commit the resume, application history, notes, personal seed lists, or anything
from `.env`. See [`docs/13-security-privacy.md`](docs/13-security-privacy.md)
section 3.

---

## Conventions

### Layout

```
apps/api  apps/collector  apps/notifier   Go
apps/brain                                Python
apps/web                                  TypeScript — Next.js
apps/mobile                               Capacitor shell — wraps apps/web,
                                          NOT a second UI. Never fork a screen
                                          into it. See ADR-012.
packages/schema                           Canonical schemas — source of truth
packages/taxonomy                         Role, skill, location vocabularies
packages/prompts                          Versioned prompt files
adapters/                                 One module per source type
infra/                                    Compose, migrations, CI, config
evals/                                    Golden sets and harnesses
docs/                                     Specifications
```

### Go

`golangci-lint` config is authoritative. Errors wrapped with `%w` and context.
`context.Context` first parameter on anything doing I/O. No global state except
`init`-time configuration. Queries via `sqlc` — **never** string-concatenated
SQL. Table-driven tests.

### Python

`ruff` and `mypy --strict`. Full type annotations. `pydantic` for every external
data boundary including LLM output. `async` for I/O. No bare `except`.

### TypeScript

`strict: true`. No `any` — use `unknown` and narrow. Server Components by
default; `"use client"` only where interaction requires it. Zod at every
boundary. Import the generated API client, never hand-write a fetch.

### SQL

Lowercase keywords in application queries, uppercase in migrations. Every
migration numbered and forward-only. `CREATE INDEX CONCURRENTLY` on any table
above 100k rows. Explicit column lists, never `SELECT *`.

---

## Testing expectations

A pull request without tests will not pass CI. Specifically:

| Change | Required test |
| --- | --- |
| New adapter | Fixtures for standard, empty, malformed, and unicode cases |
| Scoring change | Unit tests plus the property tests still passing |
| Dedup change | Eval suite above threshold; precision ≥0.99 |
| Notification change | The concurrency and suppression tests |
| API change | Contract test; OpenAPI regenerated |
| Migration | Applies to a prod-sized snapshot in under 30s |
| Prompt change | Eval diff attached to the PR |
| Any bug fix | A test reproducing the bug, failing before the fix |

**Every production quality bug becomes a golden-set entry before it is fixed.**

---

## Common mistakes in this codebase

Things that look right and are not:

**Comparing embeddings across versions.** Always filter on
`embedding_version`. Vectors from different models are not comparable. Embeddings
are **384-dimensional** (`bge-small-en-v1.5`), not 768 — an earlier draft of
[`docs/02-architecture.md`](docs/02-architecture.md) said 768 and was wrong.

**Assuming a unique index on a partitioned table does what it looks like it
does.** `raw_observation` is partitioned by `observed_at`, and Postgres requires
the partition key in any unique constraint declared on the parent. A unique index
on `(source_id, content_hash, observed_at)` is legal and guarantees **nothing**,
because `observed_at` defaults to `now()`. The real guarantee comes from unique
indexes created on each *partition*, and it is scoped to one row per
`(source, content)` **per month**. See
`infra/migrations/000005_raw_observation.up.sql`.

**Forgetting the advisory lock in dedup.** Concurrent dedup for the same company
creates duplicate groups. Lock on `company_id`.

**Using offset pagination.** The feed receives new rows continuously; offset
pagination causes genuinely missed jobs. Cursor only.

**Scoring without the multipliers.** `priority` is
`base × location_multiplier × freshness_multiplier`. Applying location as an
additive bonus breaks the Bengaluru-always-higher guarantee at high base scores.

**Treating `paid = 'unknown'` as `'unpaid'`.** Most postings state no
compensation. Excluding them discards most of the market. See
[`docs/07-normalization-taxonomy.md`](docs/07-normalization-taxonomy.md)
section 7.

**Parsing Indian numbers with Western assumptions.** `1,00,000` is one lakh.
`8 LPA` is 800,000/year. Getting this wrong breaks the primary market.

**Adding a dependency without justification.** The supply chain is the most
likely compromise path. Justify additions in the PR description.

---

## Commits and pull requests

Conventional Commits, scoped to a service or package:

```
feat(collector): add Recruitee adapter
fix(dedup): filter candidates by embedding_version
docs(adr): record the decision to defer NATS
```

Pull requests must state: what changed, why, which spec section it implements or
modifies, and what was tested. If it changes a documented behavior, the doc
change is in the same PR.

---

## When you are unsure

Prefer, in order:

1. The behavior the spec describes.
2. The safer failure mode — under-merge over over-merge, under-notify over
   over-notify, refuse over fetch.
3. The simpler implementation.
4. Asking, rather than guessing on anything touching notifications, dedup
   thresholds, or the compliance gate.

Do not add speculative abstraction. The architecture is deliberately sized for
current load with documented upgrade paths; building for a scale we do not have
is the failure mode called out as R34 in
[`docs/20-risks.md`](docs/20-risks.md).

**This applies to multi-tenancy specifically, and it is the rule the earlier
plan broke.** Scout has exactly one user. Do not add Row-Level Security, per-user
score fanout, distributed rate limiting, or anything else whose justification is
"when there are more users." `user_id` columns stay because they are already
written and cost nothing; everything beyond that waits until a second user
exists. The full migration path is one paragraph in
[`docs/02-architecture.md`](docs/02-architecture.md) section 6 — read it before
proposing to start on any of it early.
