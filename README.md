# Scout

Scout is an autonomous discovery platform for internship and new-grad roles
across software engineering, product, consulting, and business. It continuously
monitors the sources where these positions are announced, normalizes each
posting into a canonical job record, ranks it against a candidate profile, and
delivers a notification within minutes of discovery.

> Never miss a relevant internship or new-grad role.

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
- A Next.js dashboard — ranked feed with URL-state filters, job detail (AI
  summary, personalized match explanation, full score breakdown, private
  notes), save/apply/dismiss with undo, a pipeline board, search, calendar,
  companies and watchlist, insights, resume upload, and settings. Responsive
  down to a phone (bottom tab bar), with first-run guidance, handled
  loading/empty/error/offline/not-found states, and toasts for every action

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

**Prerequisites:** Docker Desktop (or Docker + Compose), `make`, Go 1.25+,
Node 22 with `pnpm` (`corepack enable`), and Python 3.12 with `uv`. Go, Node,
and Python are only needed to run tests or a service natively — `make dev`
builds every service inside Docker.

```bash
git clone https://github.com/kelyonn/scout.git && cd scout
make dev      # starts the full local stack, migrated and seeded, from a clean clone
make test     # runs the full test suite, including database-backed tests
```

Then open **http://127.0.0.1:3000** and sign in with the access token —
locally that is the placeholder `SCOUT_AUTH_TOKEN` in `.env.example`, which
`make dev` copied into `.env`. You type it once per device; it stays signed in.

A fresh database has no postings yet. The Home page walks you through the two
setup steps: upload a resume (`/resume`) and let the collector run. Locally,
`make seed-sources` loads the curated source list and `make live-run` runs
the collector against real sources for one bounded window (see
[ADR-018](docs/adr/ADR-018-laptop-only-hosting.md)).

Local development never reaches the live internet unless you ask it to —
`SCOUT_FIXTURES_ONLY=true` is the default, and the collector's scheduler will
not start without it being explicitly disabled. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for the full command reference and
[`docs/15-infrastructure-deployment.md`](docs/15-infrastructure-deployment.md)
for how the pieces fit together in production.

### Configuration

Everything is configured through environment variables, documented inline in
[`.env.example`](.env.example) (the stack) and
[`apps/web/.env.example`](apps/web/.env.example) (only for running the web app
natively with `make web-dev`). Every integration is optional and degrades
cleanly when unset:

| Variable | Purpose |
| --- | --- |
| `SCOUT_AUTH_TOKEN` | The single access token (32+ chars). Rotating it signs every device out. |
| `SCOUT_DATABASE_URL`, `SCOUT_REDIS_URL` | Postgres (with pgvector) and Redis. |
| `SCOUT_FIXTURES_ONLY` | `true` (default) replays fixtures; `false` fetches real sources. |
| `SCOUT_USER_EMAIL` | The sole user's email, seeded by `make seed`. |
| `SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN` | Telegram job notifications. |
| `SCOUT_FCM_PROJECT_ID`, `SCOUT_FCM_SERVICE_ACCOUNT_JSON` | Native Android push. |
| `SCOUT_GOOGLE_CLIENT_ID`, `SCOUT_GOOGLE_CLIENT_SECRET` | Google Calendar sync (Settings page). |
| `SCOUT_BRAIN_*_API_KEY` | Hosted LLM tiers for classification and summaries; local Ollama otherwise. |
| `SCOUT_EMAIL_IMAP_*` | Email-alert ingestion (LinkedIn/Indeed/Glassdoor/Handshake). |
| `SCOUT_HOST_BIND_ADDR` | Bind API/web to a tailnet IP so the Android app can reach them. |

Secrets never go in the repository — the repo is public, which is what makes CI
free. See [`docs/13-security-privacy.md`](docs/13-security-privacy.md).

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
Postgres instance, SQL linting, Python lint/type-check/tests, the web app's
eslint/vitest/tsc/production build, the eval harness, and `shellcheck` over
the operational scripts.
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
