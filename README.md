# Scout

**An autonomous internship and new-grad discovery platform, built for one person
and running on ₹0.**

Scout continuously watches the parts of the internet where software engineering
internships and graduate roles are announced, normalizes what it finds into a
single canonical job record, ranks each opportunity against one candidate
profile, and pushes a notification within minutes of discovery.

The design goal is a single sentence:

> Never miss a relevant internship or new-grad software engineering role.

Scout is not a job board. A job board waits for you to search it. Scout runs a
discovery loop on a schedule, decides what matters, and interrupts you only when
something is worth the interruption.

---

## Status

**Foundation built, not yet deployed (P0).** The specification set is complete,
and so is everything in P0 that lives in this repository:

- Database schema — five migrations, applied by `golang-migrate`
- Local and production Compose stacks; hardened, distroless, ARM64 service images
- `make dev` brings the whole local stack up, migrated, from a clean clone
- Bearer-token auth behind the Tailscale network gate ([ADR-015](docs/adr/ADR-015-single-user-auth.md))
- The dead-man's switch — the collector reports in every 5 minutes
- Deploy over Tailscale SSH, with a health gate that rolls back on its own
- Tiered backup and a restore drill, both `age`-encrypted ([ADR-017](docs/adr/ADR-017-tiered-backup-without-object-storage.md))
- CI: compliance, Go build/vet/test, golangci-lint, migrations, sqlfluff, shellcheck

What is left in P0 needs a host, an account, or physical media rather than code:
provisioning the Oracle A1 instance, arming the billing alert, running the first
real deploy, and writing the `age` key and Android keystore to offline media.
[`docs/19-roadmap.md`](docs/19-roadmap.md) lists exactly which exit criteria
those are and why they cannot be closed from here.

Everything past P0 is specified and not yet built — no adapters, no pipeline, no
ranking, no notifications, no dashboard. The roadmap says what lands when, with
estimates that are honest rather than optimistic.

---

## Two constraints shape everything

**1. It is for one person.** Not one *tenant* — one person. There is no
multi-user path being kept warm, no Row-Level Security, no per-user score fanout,
no scaling stages. `user_id` stays in the schema because it is already there and
free; nothing else is spent on a second user who may never exist. If one ever
does, the migration is one paragraph in
[`docs/02-architecture.md`](docs/02-architecture.md) section 6 and about three
weeks — which is the right time to pay for it.

**2. It costs ₹0.** Not "cheap" — zero, with no recurring line item of any size.
A ₹1,200/month tool is a decision that gets re-made every month and eventually
gets switched off during exam week. A ₹0 tool has no month in which it is
reconsidered.

The second constraint changed more of the design than the first. It also made
three of the four things it touched **simpler**, which is worth noticing rather
than treating as luck — the paid budget had been quietly permitting complexity
that nothing needed.

---

## The shape of the system

```
   DISCOVERY            INGESTION           INTELLIGENCE          DELIVERY
   ─────────            ─────────           ────────────          ────────

 source registry  →   fetch + change   →   normalize        →   rank
 company crawler      detection            classify             notify
 ATS enumeration      parse                deduplicate          dashboard
 email alerts         extract              embed                digest
 feeds / APIs         validate             score
```

Five stages, each independently deployable, each with its own failure domain.
A source outage degrades coverage for that source and nothing else.

---

## Start here

| If you want to... | Read |
| --- | --- |
| Understand what we are building and why | [`docs/01-prd.md`](docs/01-prd.md) |
| Understand how it is built | [`docs/02-architecture.md`](docs/02-architecture.md) |
| Understand how it costs nothing | [`docs/18-cost-model.md`](docs/18-cost-model.md) |
| See the full document map | [`docs/00-index.md`](docs/00-index.md) |
| See what ships when | [`docs/19-roadmap.md`](docs/19-roadmap.md) |
| See what I need you to decide | [`docs/22-open-questions.md`](docs/22-open-questions.md) |
| Write code here | [`AGENTS.md`](AGENTS.md) / [`CONTRIBUTING.md`](CONTRIBUTING.md) |

---

## Headline technical decisions

Each links to a full Architecture Decision Record with the alternatives that were
rejected and the conditions that would reverse it.

| Decision | Choice | Why |
| --- | --- | --- |
| [ADR-001](docs/adr/ADR-001-monorepo-and-language-split.md) | Monorepo, Go + Python + TypeScript | Each language does what it is best at |
| [ADR-002](docs/adr/ADR-002-postgres-as-the-primary-store.md) | PostgreSQL for everything | One store, one backup, one mental model |
| [ADR-003](docs/adr/ADR-003-job-queue-over-kafka.md) | **Postgres-backed queue, not Kafka** | Our peak is ~1.4 msg/s. Kafka is built for millions. |
| [ADR-004](docs/adr/ADR-004-search-strategy.md) | **Postgres FTS + pgvector, not Elasticsearch** | A single ES node wants more RAM than the whole app |
| [ADR-005](docs/adr/ADR-005-llm-cascade.md) | Tiered model cascade | 92% of decisions never reach an LLM |
| [ADR-007](docs/adr/ADR-007-no-tos-violating-scraping.md) | **No LinkedIn/Indeed/Glassdoor scraping** | Legally hostile; email-alert ingestion covers it |
| [ADR-008](docs/adr/ADR-008-three-stage-deduplication.md) | Three-stage dedup | Exact, structural, then semantic |
| [ADR-012](docs/adr/ADR-012-native-app-shell.md) | **Native Android app (Capacitor)** | Real FCM notifications, one UI codebase, ₹0 |
| [ADR-013](docs/adr/ADR-013-whatsapp-channel.md) | WhatsApp — *rejected* | Needs a dedicated number that can't be yours |
| [**ADR-014**](docs/adr/ADR-014-zero-cost-hosting.md) | **Free-tier host, Tailscale-only, portable in ≤1h** | At ₹0 the risk is revocation, not cost — so the decision is portability |
| [**ADR-015**](docs/adr/ADR-015-single-user-auth.md) | **Network gate + bearer token, not passkeys** | A week of WebAuthn defends an endpoint strangers cannot reach |
| [**ADR-016**](docs/adr/ADR-016-free-tier-llm-cascade.md) | **Free tiers + local Ollama; budgets in requests** | Free tiers rate-limit, they don't bill — so *wait*, don't degrade |
| [**ADR-017**](docs/adr/ADR-017-tiered-backup-without-object-storage.md) | **Back up by recoverability class** | 95% of the data can be re-fetched from the internet |

ADR-014 supersedes [006](docs/adr/ADR-006-deployment-topology.md); ADR-015
supersedes [010](docs/adr/ADR-010-authentication.md). Both originals stay in
place — a record showing only the current answer hides the reasoning.

---

## What ₹0 actually changed

| | Was | Now |
| --- | --- | --- |
| Host | Hetzner CX32, €7.05/mo, 8 GB | Free-tier ARM64, **24 GB** |
| Access | Public domain + Cloudflare + WAF | Tailscale only — **no public ingress at all** |
| Auth | Passkeys + magic link (~1 week) | Bearer token behind the network gate (~50 lines) |
| Backup | `pgbackrest` + WAL → R2, PITR | Tiered `pg_dump` → MacBook + Drive, `age`-encrypted |
| LLM | 4-tier cascade, $20/mo cap | 3 tiers + local; **no frontier tier**, budgets in requests |
| Email in | Cloudflare Routing → webhook | IMAP poll |
| Telegram | Webhook + secret verification | Long-poll |
| Digest out | Resend (needs a domain) | Telegram |
| Monthly cost | ₹1,193 | **₹0** |

**It is not all upside, and the docs say so.** No SLA, no support, no
point-in-time recovery, no generated cover letters, capacity that may not exist
at signup, and about three hours a quarter on migration and restore drills. The
full accounting is [`docs/18-cost-model.md`](docs/18-cost-model.md) section 3 —
the bills are paid in time, risk, and capability rather than rupees, and
pretending otherwise is how a zero-cost design becomes an abandoned one.

---

## Where I disagreed with the brief

The brief asked for Kafka, Elasticsearch, LinkedIn/Indeed/Glassdoor monitoring,
and mobile push. I proposed something different for all four. Three departures
were accepted; the fourth was overruled, and correctly so.

**Kafka and Elasticsearch are the wrong size.** Peak queue throughput is about
1.4 messages/second at MVP and 10 at Year 1. Kafka is designed for millions and
costs more to operate than every other component combined. Postgres handles this
with room to spare, and the migration path to NATS JetStream is documented and
cheap. Same story for Elasticsearch.

**Scraping LinkedIn, Indeed, Glassdoor, and Handshake is not something I will
build.** All four prohibit it, all four enforce it, and Handshake is tied to your
university identity — losing that account would cost more than Scout could ever
earn you. The legitimate route: subscribe to their job alert emails using a
Scout-owned address, and Scout parses your own inbox. You are entitled to the
mail sent to you. It is a first-class ingestion adapter, not a workaround.

**On the native app, I was wrong and reversed it.** Two of the three original
premises did not hold: app-store review latency does not apply to a personal app
you sideload, and the "second codebase" cost assumed a React Native UI rather
than a WebView shell. Scout ships a Capacitor shell around the same web app —
real FCM notifications, one UI to maintain, Android only, ₹0.

**WhatsApp was specified, then dropped.** The Cloud API needs a number registered
to a Business Account, and that number **cannot also be an ordinary WhatsApp
account** — so a personal number is ineligible. [ADR-013](docs/adr/ADR-013-whatsapp-channel.md)
is kept as a `Rejected` record rather than deleted, because the constraint is not
obvious until you go looking and this gets re-proposed.

One part stands regardless: `whatsapp-web.js` and Baileys are prohibited,
enforced by a CI dependency check. They skip every constraint above, and Meta
enforces its terms by banning the **phone number**.

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
│   ├── schema/           Shared canonical job schema (source of truth)
│   ├── taxonomy/         Role, skill, location, company taxonomies
│   └── prompts/          Versioned prompt files
├── adapters/             One module per source type
├── infra/
│   ├── compose/          Docker Compose stacks — local and production
│   ├── docker/           Service images
│   ├── caddy/            Ingress routing, behind `tailscale serve`
│   ├── migrations/       Versioned SQL migrations
│   ├── host/             Host cron for the backup jobs
│   └── scripts/          Compliance gate, deploy, health gate, backup, restore
├── evals/                Golden datasets and scoring harnesses
├── .github/workflows/    CI
└── docs/                 You are here
```

---

## A note on this repository being public

It is public so that GitHub Actions minutes are unlimited, which is what makes CI
a ₹0 line. That has a consequence with teeth: **the resume, application history,
interview notes, and personal seed lists must never be committed.** All of it
lives in Postgres rather than in files, `gitleaks` runs pre-commit and in CI, and
[`docs/13-security-privacy.md`](docs/13-security-privacy.md) section 3 is the
list.

The *generic* seed lists — company board tokens, GCC entities, the taxonomy — are
public information and are probably the most useful part of this repository to
anyone else. Only the personal layer stays out.

---

## Success criteria

Scout works when all five of these are true:

1. **Scout-first** — of the roles you applied to, 90% were surfaced by Scout
   before you saw them anywhere else.
2. **Latency** — p50 under 10 minutes from posting to notification for Tier 1.
3. **Precision** — fewer than 1 in 10 notifications is one you would call noise.
4. **Zero duplicates** — you are never notified twice for the same role.
5. **Replacement** — you stop opening job boards.

Criterion 1 replaced a "≥95% discovery recall, audited weekly against 20
hand-searched roles" target that could not be measured by that instrument — at
n=20 one miss is exactly 95%. The reasoning is in
[`docs/16-observability.md`](docs/16-observability.md) section 2.1, and it is
worth reading as an example of a metric that looked rigorous and was not.

Criterion 5 is the one that actually decides it.

---

## License

Private. Not yet licensed for redistribution. (Public repository, all rights
reserved — see the note above for why it is public.)
