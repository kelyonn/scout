# Architecture Decision Records

Each ADR records one significant decision: the context that forced it, the
options considered, what was chosen, and what would make us change our mind.

An ADR is immutable once accepted. If a decision changes, write a new ADR that
supersedes the old one and leave the original in place. The history of *why* a
decision was reversed is often more valuable than the decision itself.

Use [`ADR-000-template.md`](ADR-000-template.md) for new records.

| # | Decision | Status |
| --- | --- | --- |
| [001](ADR-001-monorepo-and-language-split.md) | Monorepo with a three-language split | Accepted |
| [002](ADR-002-postgres-as-the-primary-store.md) | PostgreSQL as the single primary datastore | Accepted |
| [003](ADR-003-job-queue-over-kafka.md) | Postgres-backed job queue instead of Kafka | Accepted |
| [004](ADR-004-search-strategy.md) | Postgres FTS + pgvector instead of Elasticsearch | Accepted |
| [005](ADR-005-llm-cascade.md) | Tiered model cascade with provider abstraction | Accepted |
| [006](ADR-006-deployment-topology.md) | Single VPS with Docker Compose | **Superseded by 014** |
| [007](ADR-007-no-tos-violating-scraping.md) | No scraping of sources that prohibit it | Accepted |
| [008](ADR-008-three-stage-deduplication.md) | Three-stage deduplication | Accepted |
| [009](ADR-009-frontend-stack.md) | Next.js PWA, no native mobile app | **Partially superseded by 012** |
| [010](ADR-010-authentication.md) | Passkeys with magic-link fallback | **Superseded by 015** |
| [011](ADR-011-observability-stack.md) | OpenTelemetry into a self-hosted Grafana stack | Accepted |
| [012](ADR-012-native-app-shell.md) | Native app shell via Capacitor for real OS notifications | Accepted |
| [013](ADR-013-whatsapp-channel.md) | WhatsApp as a notification channel | **Rejected** |
| [014](ADR-014-zero-cost-hosting.md) | Free-tier host, Tailscale-only access, portability as the guarantee | **Partially superseded by 018** |
| [015](ADR-015-single-user-auth.md) | Network-gated bearer token instead of passkeys | Accepted |
| [016](ADR-016-free-tier-llm-cascade.md) | Free-tier and local models; budgets in requests, not rupees | Accepted |
| [017](ADR-017-tiered-backup-without-object-storage.md) | Back up by recoverability class, to storage already owned | Accepted |
| [018](ADR-018-laptop-only-hosting.md) | Laptop-only hosting — no remote host, no Tailscale | Accepted |

## The ₹0 group: 014–017

Four records, one cause. The budget went from ₹2,000/month to **zero**, and that
is not the same design with a smaller number in it.

At ₹2,000 the binding constraint is cost, and the mitigation is right-sizing. At
₹0 the binding constraint is **revocation** — a free tier can be withdrawn,
capacity-denied, or reclaimed, and you have no SLA, no support queue, and no
recourse, because you are not a customer. Money cannot fix a free-tier outage.

Each of the four is that shift applied to one layer:

- **[014](ADR-014-zero-cost-hosting.md)** — the host is disposable, so the real
  decision is a rehearsed one-hour migration, not which provider.
- **[015](ADR-015-single-user-auth.md)** — nothing is on the public internet
  anymore, so a week of WebAuthn defends against an attacker who cannot reach the
  endpoint.
- **[016](ADR-016-free-tier-llm-cascade.md)** — free tiers rate-limit rather than
  bill, so the budget mechanism changes from a currency cap to a request
  scheduler, and the correct response to a limit becomes *wait* rather than
  *degrade*.
- **[017](ADR-017-tiered-backup-without-object-storage.md)** — 95% of the data is
  re-derivable from the internet, so uniform 5-minute-RPO backup was over-built
  even when it was cheap.

Three of the four make the system **simpler** than the paid design, not poorer.
That is worth noticing rather than treating as luck: a hard constraint deleted
work that a soft one had been quietly permitting.

## On the two that did not stick

Both concern notifications, which is not a coincidence — it is the part of the
product where the requirements were least settled.

**[ADR-009](ADR-009-frontend-stack.md), partially reversed by
[ADR-012](ADR-012-native-app-shell.md).** ADR-009 argued against a native app. It
was not wrong about the facts; it was wrong about which facts applied. It cited
app-store review latency, which does not exist for a sideloaded personal app, and a
second-codebase cost that assumed a React Native rewrite rather than a WebView
shell. The original stays in place because a record showing only the current answer
would hide the reasoning error, and the error is the transferable lesson: **check
that an argument's premises apply to your deployment model before accepting its
conclusion.**

**[ADR-013](ADR-013-whatsapp-channel.md), accepted and withdrawn the same day.**
WhatsApp as a channel, dropped once its setup cost was concrete — a dedicated phone
number that cannot be the user's own, plus Meta verification and template approval,
for a third copy of a notification already arriving on two transports. Kept as a
`Rejected` record rather than deleted, because it is an obvious thing to propose
again and the constraints are not obvious until you go looking. Its prohibition on
unofficial WhatsApp libraries remains in force.

**Neither reversal cost anything, and that is the point.** Both were caught in
review, before code. An ADR that is cheap to reverse on paper and expensive to
reverse in production is doing its job when it gets reversed on paper.

## The fifth ₹0 record: 018

**[018](ADR-018-laptop-only-hosting.md)** is the same shift as 014–017 taken one
step further: Oracle's free tier still requires a card, and the user does not
have one. Rather than block on acquiring one, 018 removes the always-on host
requirement entirely — Scout runs on the user's laptop, on demand, with no
remote host and no Tailscale. This trades away the overnight coverage window
that 014 built the whole design around catching, named plainly as the real cost
in that record rather than glossed over. Everything else 014 decided —
portability, ARM64, the Compose topology, `production.yml`, `Caddyfile` — stays
valid as the migration path back, which is why 014 is only *partially*
superseded.
