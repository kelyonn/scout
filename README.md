# Scout

Scout is an autonomous discovery platform for software engineering internships
and new-grad roles. It continuously monitors the sources where these positions
are announced, normalizes each posting into a canonical job record, ranks it
against a candidate profile, and delivers a notification within minutes of
discovery.

> Never miss a relevant internship or new-grad software engineering role.

Scout is not a job board — it does not wait to be searched. It runs a
discovery loop on a schedule, applies a scoring model to what it finds, and
surfaces only what clears the bar for a notification.

---

## How it works

```
   DISCOVERY            INGESTION           INTELLIGENCE          DELIVERY
   ─────────            ─────────           ────────────          ────────

 source registry  →   fetch + change   →   normalize        →   rank
 company crawler      detection            classify             notify
 ATS enumeration      parse                deduplicate          dashboard
 email alerts         extract              embed                digest
 feeds / APIs         validate             score
```

Four stages, each independently deployable with its own failure domain. A
source going down degrades coverage for that source only — it does not affect
the rest of the pipeline.

- **Discovery** — a registry of sources (ATS boards, company career pages,
  RSS/JSON feeds, email alerts) is polled on an adaptive schedule that speeds
  up for high-yield sources and slows down for dormant ones.
- **Ingestion** — every fetch passes through a compliance gate (robots.txt,
  rate limits, legal posture, a circuit breaker) before it happens. Conditional
  requests and layered change detection keep bandwidth and parse cost low.
- **Intelligence** — normalization, classification, deduplication, embedding,
  and scoring turn a raw posting into a ranked, canonical job record.
- **Delivery** — ranked jobs reach the user through push notifications, a
  dashboard, and a digest, gated by per-channel budgets and quiet hours.

## Architecture

Scout is a monorepo split by language along task boundaries:

| Service | Language | Responsibility |
| --- | --- | --- |
| `apps/collector` | Go | Scheduling, fetching, politeness, change detection |
| `apps/brain` | Python | Normalization, classification, embeddings, scoring |
| `apps/notifier` | Go | Notification triggers, budgets, channel delivery |
| `apps/api` | Go | HTTP API, authentication, serving |
| `apps/web` | TypeScript (Next.js) | Dashboard |
| `apps/mobile` | Capacitor | Android shell around the web app, push notifications |

PostgreSQL (with `pgvector`) is the single data store for everything —
relational data, full-text search, and vector similarity — backed by a
Postgres-native job queue rather than a separate message broker. Redis is used
only for ephemeral state (rate limits, caches); nothing durable lives there.
Access is restricted to a private network (Tailscale) with no public ingress,
and the system is designed for a single operator rather than multi-tenancy.

Each major design decision — language split, data store, queueing, search,
auth, hosting, backups — is recorded as an Architecture Decision Record under
[`docs/adr/`](docs/adr/), with the alternatives considered and the conditions
that would revisit the decision.

---

## Status

**P1 (first real notification) and P2 (worth reading) are code-complete. P3
(daily driver) has started.** See [`HANDOFF.md`](HANDOFF.md) for the current
state in detail and [`docs/19-roadmap.md`](docs/19-roadmap.md) for what each
milestone means and its annotated exit criteria.

**Infrastructure**
- Database schema, applied via `golang-migrate`
- Local and production Docker Compose stacks; hardened, distroless, ARM64
  service images
- Bearer-token authentication behind the network gate
- An external dead-man's switch that alerts if the collector stops reporting
- Deployment over SSH with an automatic health-gate rollback
- Tiered, encrypted backups with a scripted restore drill

**Ingestion pipeline** (`apps/collector`), per
[`docs/06-ingestion-pipeline.md`](docs/06-ingestion-pipeline.md):
- A politeness gate enforcing legal posture, robots.txt (RFC 9309), per-domain
  rate limiting, concurrency limits, crawl-delay, and a circuit breaker
- An SSRF-safe fetcher with conditional GET, tiered timeouts, and redirect
  re-validation on every hop
- Content-hash change detection that strips volatile page elements (render
  timestamps, CSRF tokens) so unrelated noise doesn't register as a change
- An adaptive scheduler that varies poll frequency by source yield, recency,
  and season, claiming work in short transactions so a large batch never holds
  a lock across a live network fetch
- Greenhouse, Lever, and Ashby adapters, ~2,267 seeded sources

**Intelligence** (`apps/collector/internal/{normalize,classify,dedup,scoring}`,
`apps/brain`):
- Normalization, taxonomy (roles including `advocacy`, locations, skills,
  companies), three-stage dedup, and most of the scoring model
- A Python service running local embeddings and, per
  [ADR-016](docs/adr/ADR-016-free-tier-llm-cascade.md), a provider-rotated
  LLM cascade (free hosted tiers, local Ollama fallback) for classification,
  dedup adjudication, posting summaries, and personalized match explanations
- An eval harness (`evals/`) gating classification, dedup, and explanation
  quality in CI against golden sets

**Delivery**
- Telegram notifier with `bengaluru_match`/`high_score` triggers, quiet
  hours, and exactly-once delivery
- A Next.js dashboard with a feed, job detail (including an AI-generated
  summary and a personalized match explanation), and a resume page

This has been verified end-to-end against a real Postgres and Redis instance,
not only with unit tests.

**Not yet built:** the save/applied/interviewing state machine, the "found
elsewhere first" control, email-alert ingestion, the remaining ATS adapters
(Workday among them), GCC coverage, and observability.
[`HANDOFF.md`](HANDOFF.md) has the full list and
[`docs/19-roadmap.md`](docs/19-roadmap.md) lists what ships in each
milestone.

---

## Getting started

```bash
git clone https://github.com/kelyonn/scout.git && cd scout
make dev      # starts the full local stack, migrated, from a clean clone
make test     # runs the full test suite, including database-backed tests
```

Local development never reaches the live internet — `SCOUT_FIXTURES_ONLY=true`
is the default, and the collector's scheduler will not start without it being
explicitly disabled. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the full
command reference and
[`docs/15-infrastructure-deployment.md`](docs/15-infrastructure-deployment.md)
for how the pieces fit together in production.

---

## Repository layout

```
scout/
├── apps/
│   ├── api/              Go — HTTP API, auth, serving
│   ├── collector/        Go — fetching, change detection, politeness
│   ├── brain/            Python — normalize, classify, embed, rank, LLM
│   ├── notifier/         Go — triggers, budgets, channel fanout
│   ├── web/              TypeScript — Next.js dashboard
│   └── mobile/           Capacitor shell — Android, FCM push
├── packages/
│   ├── db/               sqlc-generated query layer
│   ├── schema/           Shared canonical job schema (source of truth)
│   ├── taxonomy/         Role, skill, location, company taxonomies
│   └── prompts/          Versioned prompt files
├── adapters/             One module per source type
├── infra/
│   ├── compose/          Docker Compose stacks — local and production
│   ├── docker/           Service images
│   ├── caddy/            Ingress routing
│   ├── migrations/       Versioned SQL migrations
│   ├── host/             Host cron for backup jobs
│   └── scripts/          Compliance gate, deploy, health gate, backup, restore
├── evals/                Golden datasets and scoring harnesses
├── .github/workflows/    CI
└── docs/                 Specifications and architecture decision records
```

---

## Documentation

| Topic | Reference |
| --- | --- |
| Product goals and scope | [`docs/01-prd.md`](docs/01-prd.md) |
| System architecture | [`docs/02-architecture.md`](docs/02-architecture.md) |
| Data model | [`docs/03-data-model.md`](docs/03-data-model.md) |
| Ingestion pipeline | [`docs/06-ingestion-pipeline.md`](docs/06-ingestion-pipeline.md) |
| Legal and compliance posture | [`docs/14-legal-compliance.md`](docs/14-legal-compliance.md) |
| Infrastructure and deployment | [`docs/15-infrastructure-deployment.md`](docs/15-infrastructure-deployment.md) |
| Full document index | [`docs/00-index.md`](docs/00-index.md) |
| Roadmap | [`docs/19-roadmap.md`](docs/19-roadmap.md) |
| Contributing | [`AGENTS.md`](AGENTS.md) / [`CONTRIBUTING.md`](CONTRIBUTING.md) |

This repository is specification-first: the documents under `docs/` are
prescriptive, not descriptive. Where code and specification disagree, that is
treated as a bug in one of them.

---

## Testing and CI

CI runs on every push and pull request: a dependency compliance scan, Go
build/vet/test, `golangci-lint`, migration validation against a real
Postgres instance, SQL linting, and `shellcheck` over the operational scripts.
Tests that require Postgres or Redis run against real instances rather than
mocks — the query correctness and Redis wire-format behavior they verify are
exactly what a mock would hide.

---

## Security and compliance

Every outbound request the collector makes passes through a compliance gate
before it happens — legal posture, `robots.txt`, and rate limits are checked
first, with zero exceptions. Sources whose access is legally prohibited never
generate a request at any point in the code path. SSRF protections resolve
and validate the destination IP before connecting, on every request and every
redirect hop. See [`docs/14-legal-compliance.md`](docs/14-legal-compliance.md)
and [`docs/13-security-privacy.md`](docs/13-security-privacy.md) for the full
policy.

---

## License

Private. Not currently licensed for redistribution.
