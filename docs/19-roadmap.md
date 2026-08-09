# Roadmap and Milestones — Scout

**Status:** Draft · **Owner:** Product · **Last updated:** 2026-08-08

---

## Sequencing principle

**Get to a working notification as fast as possible, then widen, then deepen.**

The riskiest assumption in this project is not technical. It is whether an
automated feed of ranked internships actually changes the user's behavior. That
question is answerable in a few weeks with a narrow slice — three ATS adapters,
crude ranking, one notification channel — and unanswerable for months if the
dashboard, the analytics, and the AI features come first.

So P1 ships an ugly, narrow, working system. Everything after widens coverage or
deepens intelligence, in the order that maximizes discovered opportunities per
hour of work.

**This document previously violated that principle.** It planned 15,000 lines of
specification before the first HTTP request, and the repository contained a
runbook for LLM budget exhaustion before it contained an LLM client. The specs
are good and they stay; the sequencing is corrected here.

---

## How these estimates were made, and why they changed

**The previous plan claimed MVP in 8 weeks at 15–20 hrs/week — about 140 hours.**
That was wrong by a wide margin. M0 alone budgeted one week for a three-language
monorepo with codegen, full DDL, three Compose stacks, a provisioned host, CI/CD,
the complete OpenTelemetry/Prometheus/Loki/Tempo/Grafana stack, `pgbackrest` with
a verified restore drill, and passkey authentication end to end. Any one of the
last three is a week on its own for someone doing it the first time.

Estimates below are built bottom-up from tasks, then multiplied by **1.5** — not
as padding, but because the historical ratio of actual to estimated on
unfamiliar work is worse than that, and a plan that is honest about being
uncertain is more useful than one that is precise and wrong.

**The total did not grow as much as the correction implies, because the scope
genuinely shrank.** Removed since the last version:

| Removed | Was | Why |
| --- | --- | --- |
| Passkeys + magic link | ~1 week | [ADR-015](adr/ADR-015-single-user-auth.md) — network gate replaces it |
| `pgbackrest` + WAL archiving + PITR | ~4 days | [ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) — `pg_dump` on a cron |
| Public ingress: domain, DNS, Cloudflare, WAF, ACME | ~3 days | [ADR-014](adr/ADR-014-zero-cost-hosting.md) — Tailscale |
| Inbound email webhook + HMAC | ~2 days | IMAP poll instead |
| Telegram webhook + secret verification | ~1 day | Long-poll instead |
| Staging environment | ~3 days | Local is staging |
| RLS, per-user fanout, scaling stages | ~1 week | [02](02-architecture.md) section 6 |
| WCAG AA pass + axe + Lighthouse CI | ~4 days | Not a gate for one user |
| Cover letters, interview Qs, resume prose, offer comparison | ~2 weeks | [ADR-016](adr/ADR-016-free-tier-llm-cascade.md) — no free frontier tier |
| Meilisearch | ~3 days | Postgres FTS is adequate; was already on the cut list |

**Net: roughly six weeks of work removed.** The honest re-estimate is therefore
~13 weeks to a daily driver rather than 8, not the ~30 the original scope would
actually have taken.

Milestones are renamed `P0`–`P5` (**P** for personal) to make clear these are not
the old `M0`–`M8`. Assume **15–20 hrs/week**, realistic for a final-year student.

---

## Milestone overview

| # | Milestone | Est. hours | Calendar | Ships |
| --- | --- | --- | --- | --- |
| **P0** | It runs | ~35 | 2 weeks | Host, Compose, schema, CI, backups, Tailscale |
| **P1** | **First real notification** | ~55 | 3 weeks | 3 adapters, pipeline, Telegram |
| **P2** | Worth reading | ~55 | 3 weeks | LLM cascade, embeddings, dedup, scoring, evals |
| **P3** | **Daily driver** | ~70 | 4 weeks | Dashboard, email ingestion, Workday/GCC, digest, observability |
| **P4** | On the phone | ~25 | 1.5 weeks | Capacitor shell, FCM, actions |
| **P5** | Coverage and learning | ongoing | — | Company discovery, more adapters, analytics |

**Daily driver at P3, roughly 13 weeks (~215 hours) in.** P4 is a fortnight after
that. P5 has no end date because it is maintenance, and pretending otherwise is
how a roadmap starts lying.

**If only one milestone gets built, build P1.** It answers the question the whole
project exists to answer, and it does so in three weeks.

---

## P0 — It runs (~35h, 2 weeks)

**Objective:** a deployable skeleton that does nothing useful, correctly.

**Deliverables**

- Oracle A1 provisioned (expect capacity retries — see
  [ADR-014](adr/ADR-014-zero-cost-hosting.md)), or MacBook fallback configured.
- Tailscale on host, laptop, phone. MagicDNS name, `tailscale cert`.
- Docker Compose: Postgres + Redis. Migrations applied. `make dev` from a clean
  clone.
- Go workspace, one working `/health`. **No codegen, no `packages/schema`, no
  Python, no pnpm yet** — those arrive with the first thing that needs them.
- CI green: compliance, go build/vet/test, golangci-lint, migrations, sqlfluff.
- Backups: hourly + nightly `pg_dump`, `age`-encrypted, to MacBook and Drive.
  One successful restore.
- `SCOUT_AUTH_TOKEN` auth ([ADR-015](adr/ADR-015-single-user-auth.md)).
- healthchecks.io dead-man's switch wired to Telegram.
- **Oracle billing alert at $0.01.**
- `age` key and Android signing keystore generated and backed up offline.

**Exit criteria**

- [x] `make dev` starts the stack from a clean clone
- [ ] `make deploy` reaches production over Tailscale in under 5 minutes
- [x] A restore drill completes successfully
- [ ] Dashboard reachable at `scout.<tailnet>.ts.net` from phone and laptop
- [ ] Killing the host triggers a Telegram alert within 15 minutes
- [ ] **Oracle billing page shows ₹0 and the $0.01 alert is armed**
- [ ] `age` key and keystore exist on offline media

**The repository side of P0 is done; the host side is not, and the split is not
arbitrary.** Everything that can be written, tested, and reviewed exists:
Compose stacks for local and production, the migration runner, the hardened
service images, Caddy, the deploy and health-gate scripts, the tiered backup and
restore drill, `SCOUT_AUTH_TOKEN` auth, the dead-man's switch, and CI covering
all of it. The full production stack has been brought up and verified locally,
and the restore drill has been run end to end against a real dump.

What remains needs a host, an account, or physical media, and cannot be finished
by writing code:

| Criterion | Blocked on |
| --- | --- |
| `make deploy` under 5 minutes | An Oracle A1 instance (or the MacBook in fallback mode) on the tailnet. The script is written and its preflight and health gate are tested; it has never run against a real host. |
| Dashboard reachable at `scout.<tailnet>.ts.net` | The host, plus one `tailscale serve` on it (`make tailscale-serve` prints the command). There is also no dashboard until P3 — what will answer today is `/health` through Caddy. |
| Telegram alert within 15 minutes | A healthchecks.io check, its Telegram integration, and `SCOUT_HEALTHCHECK_URL` in the host environment file. The collector pings on a 5-minute interval already. |
| Oracle billing at ₹0, $0.01 alert armed | An Oracle account. |
| `age` key and keystore on offline media | Generating both and physically writing them to media held offline. |

The last two are the ones with no recovery path, which is why they are exit
criteria despite nothing using them until P4. Neither has a code representation
that could be reviewed, so neither can be quietly half-done — they are either on
the media or they are not.

**Deliberately not in P0:** observability stack (P3), Python (P2), frontend (P3).
Building the Grafana stack before there is a single metric worth looking at is
the mistake the previous plan made.

**Risks:** A1 capacity denial — retry across Indian regions, MacBook meanwhile.
The two one-shot secrets have no recovery path, which is why they are exit
criteria despite nothing using them until P4.

---

## P1 — First real notification (~55h, 3 weeks)

**Objective:** a real internship posting reaches the phone within 15 minutes of
being published.

Deliberately narrow. Three adapters, exact-URL dedup only, three scoring factors,
one channel, no dashboard. The point is to close the loop and find out what is
actually hard.

**Deliverables**

- Collector: scheduler, politeness gate, conditional fetch, circuit breakers.

  **All four are built and wired end-to-end**, verified against the real
  Docker stack and a real Postgres, not only `go test`.

  - **Politeness gate** (`internal/politeness`) — all six checks from
    [06](06-ingestion-pipeline.md) section 4, in the spec's exact order,
    backed by a real RFC 9309 robots.txt parser (`internal/robots`) and a
    Redis-backed per-registered-domain token bucket (`internal/ratelimit`),
    reused for crawl-delay enforcement rather than duplicated.
    `TestProhibitedSourceMakesZeroRequests` is the unit test
    [14](14-legal-compliance.md) section 1 requires by name.
  - **Fetcher** (`internal/fetch`) — conditional GET (If-None-Match /
    If-Modified-Since), the full tiered timeout table, a 10MB body cap with
    truncate-and-flag, and SSRF protection: resolve-then-pin-the-IP, reject
    private/loopback/link-local ranges, re-validate on every redirect hop, cap
    redirects at 3, refuse a scheme-changing redirect. The SSRF dialer
    (`internal/ssrf`) is shared with the robots checker — both fetch
    attacker-influenceable hosts, and an earlier version of this checklist
    left robots.txt fetches on the default transport by mistake; that gap is
    closed.
  - **Change detection** — Layer 1 (HTTP validators) is the fetcher's own
    304 short-circuit; Layer 2 (`internal/changedetect`) hashes the body after
    stripping dates, CSRF/session tokens, and relative-time strings, exactly
    the "12 jobs found · updated 3 minutes ago" case docs/06 section 6 names.
    Layer 3 (structural diff via an adapter's `Parse`) needs an adapter and is
    not built.
  - **Scheduler** (`internal/scheduler`) — the adaptive interval formula
    (`internal/interval`) implementing docs/06 section 3.1's `yield_factor`
    formula exactly and reproducing its two worked examples; `±10%` jitter;
    the cyclical-source 4h cap; the circuit breaker's `min(60s×2^(n−5), 6h)`
    backoff; the `FOR UPDATE SKIP LOCKED` batch query claimed in a short
    transaction (`packages/db/queries/source.sql`, via sqlc) so a 200-source
    batch never holds a lock across a 30-second fetch.

  **What's still out of scope, by design:** Layer 3 and observation writing
  (docs/06 sections 8–9) need an adapter; the full per-status-code error
  table in section 10 (quarantine on 401/403, retire after three 404s, honor
  `Retry-After`) is deliberately coarsened to "2xx/304 is success, everything
  else counts toward the circuit breaker" until an adapter makes the
  distinction matter — see the package comment on `internal/scheduler`.
- Adapters: Greenhouse, Lever, Ashby. **~60 seeded boards, not 250.** Sixty is
  enough to prove the loop and is two hours of curation instead of two days.
- Normalization: URL canonicalization, title normalization, location parsing with
  the gazetteer, compensation parsing **including Indian numbering** (`1,00,000`
  is one lakh; `8 LPA` is 800,000/year — getting this wrong breaks the primary
  market).
- Classification: Tier 0 rules only. Is it software? Is it an internship?
- Dedup: Stage 1, exact URL.
- Scoring: crude priority from skill match, location tier, freshness.
- Notifier: `bengaluru_match` and `high_score`, Telegram long-poll, quiet hours,
  **the unique index**.

**Exit criteria**

- [ ] 60+ sources polling on schedule, ≥95% success over 24 hours
- [ ] A genuinely new posting produces a Telegram message within 15 minutes
- [ ] Zero duplicate notifications over one week
- [ ] The four catastrophe tests from [17](17-testing-qa.md) pass
- [ ] 85%+ of polls return `304`
- [ ] **The user finds at least one opportunity they would have missed**

The last criterion is the real one. Everything else is instrumentation. **If it
fails, stop and reconsider the product before building P2** — that is what
building P1 first is for.

**Risks:** ATS endpoints differ from documentation (fixture-record early, budget
2 days) · Telegram setup friction (test on a real device day one).

---

## P2 — Worth reading (~55h, 3 weeks)

**Objective:** the notifications become worth reading.

**Deliverables**

- Python service scaffolding — first module that needs it.
- LLM client: provider rotation, **request-shaped budgets**, local Ollama
  fallback, response cache ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)).
- Local embeddings: `bge-small-en-v1.5` via ONNX, **384-dim**, batched.
- Classification Tiers 1 and 2: role family, seniority, paid inference.
- Role taxonomy including the **`advocacy` family** with its technical-evidence
  gate and family-specific skill matching ([07](07-normalization-taxonomy.md)).
- Company taxonomy: `company_type`, curated ~400-employer registry,
  `parent_company_id` for GCC/subsidiary relationships.
- Dedup Stages 2 and 3: SimHash, boilerplate stripping, semantic matching, LLM
  adjudication, union-find grouping.
- **Scoring: the 7 subscores that can be computed from data we actually have.**
  The remaining 6 (`interview_probability`, `growth_potential`,
  `engineering_culture`, `glassdoor_proxy`, `ease_of_applying`,
  `competition_estimate`) wait for P5, when there is a corpus to calibrate
  against. Specifying thirteen weights before observing a single job was
  designing precision we could not justify; [09](09-ranking-scoring.md) keeps all
  thirteen definitions, and P2 implements the subset whose inputs exist.
- LLM-generated explanations with template fallback.
- Seed expansion to engineering-services firms already on supported ATS
  platforms — Thoughtworks, EPAM, Globant, Nagarro, Publicis Sapient. **Data, not
  code:** the cheapest coverage win available.
- Eval harness with golden sets, wired into CI as a gate.

**Exit criteria**

- [ ] Classification precision ≥97% on the golden set
- [ ] Paid inference precision ≥99%
- [ ] Dedup precision ≥99%, recall ≥92%
- [ ] Every notification carries a specific, non-generic explanation
- [ ] 400+ sources active
- [ ] **LLM spend ₹0** — every call served by a free tier or locally
- [ ] Bengaluru property test passing
- [ ] `advocacy.*` precision ≥95%; zero marketing or sales roles admitted
- [ ] Company-type fairness test passing (≤10 point variance)

**Risks:** boilerplate stripping insufficient → golden set gates precision at
0.99 · embedding inference too slow on ARM → batch tuning, INT8 · advocacy titles
bleeding into marketing → technical-evidence gate, ~25% Tier 2 escalation
budgeted · **free-tier rate limits tighter than expected** → local Ollama absorbs
it, which is why it is built in the same milestone rather than later.

---

## P3 — Daily driver (~70h, 4 weeks)

**Objective:** the user stops opening LinkedIn.

The largest milestone, and the one most at risk of scope creep. The cut order
within it is: Analytics, then Trending, then Search filters. **Feed, detail, and
pipeline are not cuttable.**

**Deliverables**

- Next.js dashboard: Home, Opportunities with filters and sort, job detail with
  all implemented scores, Saved, Applied, Search.
- Design system per [12](12-frontend-ux.md) — tokens, components, dark mode
  (default; the user works evenings).
- State machine: new → applied → interviewing → offer/rejected.
- **The "I found this elsewhere first" control** — one tap, and the entire
  instrument behind the Scout-first SLO ([16](16-observability.md) §2.1).
- SSE live feed.
- **Email alert ingestion via IMAP poll**: parsers for LinkedIn, Indeed,
  Glassdoor, Handshake. This is what unlocks prohibited-source coverage
  ([ADR-007](adr/ADR-007-no-tos-violating-scraping.md)).
- Daily digest at 08:00 IST **to Telegram**.
- Remaining ATS adapters: Workable, SmartRecruiters, Recruitee, Teamtailor,
  **Workday**.
- **GCC coverage**: curated ~150-entity seed list, Workday tenant enumeration,
  location-facet fetching so a global board costs 3 requests rather than 3,000.
  This is what puts Bengaluru enterprise roles in the feed.
- Observability: OTel, Prometheus, Loki, Tempo, Grafana, the Overview dashboard.
  Arrives here, when there is finally something worth graphing.

**Exit criteria**

- [ ] 900+ sources active
- [ ] ≥100 GCC entities polled, GCC roles appearing weekly
- [ ] p50 notification latency ≤10 minutes
- [ ] Notification precision ≥85% by user assessment
- [ ] Scout-first rate ≥80%
- [ ] **The user manages their entire search in Scout for one full week**
- [ ] Recall diagnostic run once, bucketed by `company_type`, no bucket at zero

**Risks:** email parsing fragile across board formats (fixtures per board, fail
soft) · frontend scope creep (cut list above) · **Workday takes longer than
budgeted — do not defer it.** It is one adapter, but it is the one that unlocks
every GCC and enterprise employer, so deferring it defers an entire company
category.

---

## P4 — On the phone (~25h, 1.5 weeks)

**Objective:** notifications that behave like an app's, not a bot's.

- Capacitor shell wrapping the deployed `apps/web`
  ([ADR-012](adr/ADR-012-native-app-shell.md)). **Never fork a screen into it.**
- FCM registration, four notification channels, actions, badge count, deep links,
  safe-area handling.
- Signed APK from the P0 keystore, sideloaded.
- Tailscale on the phone (already done in P0).

**Exit criteria**

- [ ] Installed on the actual phone, delivering FCM with correct icon, working
      actions, and badge count
- [ ] Save/Dismiss work from the lock screen
- [ ] Web Push does **not** double-buzz a device already reached by FCM
- [ ] WebView feed scrolls without jank on the real device

**Risk:** WebView scroll performance disappoints. **Measure on the real phone in
the first two days, not at the end.** Fallback is a native feed list only.

---

## P5 — Coverage and learning (ongoing)

No end date, because this is maintenance and a roadmap that pretends otherwise is
lying.

**Highest value first:** automatic company discovery (funding feeds, VC
portfolios, YC directory, GitHub trending → registry → ATS token discovery) ·
Indian IT services and consulting with cyclical scheduling (4-hour poll ceiling
so a campus drive opening is never missed) · core-industry employers · Indian
campus platforms via email alerts · the remaining 6 subscores, calibrated against
a real corpus · interview tracker and calendar · skill gap and resume match (both
local) · ghost posting detection · learned ranking, **if** ≥200 labels
accumulate.

---

## Cut lines

If time runs short, cut in this order.

1. **Learned ranking** — hand-tuned weights are genuinely adequate, and it needs
   200 labels that may never arrive.
2. **Meilisearch** — Postgres FTS is adequate at this scale.
3. **Analytics beyond the basic funnel** — nice, not load-bearing.
4. **Discord and Slack adapters** — low yield, high per-source effort.
5. **Ghost posting detection** — interesting, marginal.
6. **The Android app (P4)** — reluctantly. Telegram alone satisfies the
   notification SLO, so P4 is about the experience being right rather than the
   alerts arriving. It is the cheapest large thing on this list to cut.

**Never cut:** notification correctness, dedup precision, the compliance gate,
backups, the dead-man's switch, or the eval harness. These fail silently or
catastrophically, and they are the reason the system can run unattended.

---

## What ships when

| Week | The user can... |
| --- | --- |
| 2 | Nothing (it runs, correctly) |
| 5 | **Receive Telegram alerts for Bengaluru and high-scoring internships** |
| 8 | Trust that alerts are relevant, non-duplicate, and explained |
| 13 | **Run their entire search in Scout: browse, filter, save, apply, track** |
| 15 | Get real app notifications on the phone |
| ongoing | Find opportunities from companies they have never heard of |

**Week 5 is the one that matters.** Everything before it is setup and everything
after is improvement; week 5 is where the project either proves its premise or
disproves it, and it does so after about 90 hours rather than after eight
months.
