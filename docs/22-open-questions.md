# Open Questions

**Status:** Draft · **Last updated:** 2026-08-08

The only document that asks you for decisions. Everything else states a position;
this lists where I need your input.

**Section 1 records the four departures from your brief** and your ruling on
each. Three were accepted; the fourth was overruled and the docs were reworked.

**Section 2 records what the two new constraints — ₹0 and single-user — settled
on their own.** Four questions that used to need your answer no longer do,
because a hard constraint answered them.

**Section 3 is what is still open.** It is shorter than it was. Nothing in it
costs money, because nothing can.

---

## 1. Departures from the brief — settled

### Q1 — Kafka replaced with a Postgres-backed queue · **ACCEPTED**

River, a Postgres-backed job queue, with NATS JetStream as the documented
successor. Our peak at the most optimistic scale projection is ~690
messages/second; Kafka is designed for millions, and managed Kafka costs about 4x
our entire infrastructure budget. Postgres also lets us enqueue work in the same
transaction as the data write, eliminating the dual-write problem — with Kafka we
would need a transactional outbox, which *is* a Postgres-backed queue, plus Kafka.

Reversal cost is low: queue access is behind an interface, migration is 3–5 days.
[ADR-003](adr/ADR-003-job-queue-over-kafka.md)

### Q2 — Elasticsearch replaced with Postgres FTS, then Meilisearch · **ACCEPTED**

Postgres full-text search plus pgvector at MVP, Meilisearch later if ever, Elasticsearch
at no stage. A single ES node wants 4GB of JVM heap to be stable — half our server
— for a corpus of 250k documents that Postgres GIN searches in 10–30ms.

Reversal cost is low: search is behind a provider interface.
[ADR-004](adr/ADR-004-search-strategy.md)

### Q3 — No scraping of LinkedIn, Indeed, Glassdoor, or Handshake · **ACCEPTED**

Email alert ingestion instead. You subscribe to their job alerts using a
Scout-owned address; Scout parses your own inbox. No automated access to their
systems at any point.

All four prohibit scraping, all four enforce it, and **Handshake is tied to your
university account.** Estimated cost: 5–10% coverage loss, concentrated in small
companies that post only to LinkedIn, partially recovered by the company discovery
pipeline. Plus ~20 minutes of one-time setup configuring alerts.

[ADR-007](adr/ADR-007-no-tos-violating-scraping.md)

### Q4 — No native mobile app · **OVERRULED — and you were right**

You asked for notifications that behave like a normal app's, plus WhatsApp. Two of
my three original arguments did not survive review:

- **App-store review latency does not apply.** For a personal app you sideload —
  direct APK on Android, Xcode or TestFlight on iOS. There is no review queue. I
  was applying a generic-SaaS argument to a personal tool.
- **The "second codebase" cost assumed a React Native UI.** A WebView shell around
  the existing Next.js app is one thin pipeline reusing 100% of the UI.

I also undersold the technical case: iOS Web Push requires the user to install the
PWA, is throttled if the app is rarely opened, and is unreliable after days closed.
That made the *most important path in the product* run on its *least reliable
transport*.

**What now ships instead:** a Capacitor shell in `apps/mobile` wrapping the same
web app, with FCM and APNs as the primary transport — correct app icon, lock-screen
actions, badge counts, biometric unlock, and no install-to-home-screen precondition.
Roughly 5–7 days of work rather than the 3–4 weeks a React Native rewrite would
take. UI changes still ship without rebuilding the binary.

[ADR-012](adr/ADR-012-native-app-shell.md)

### Q4a — WhatsApp · **SPECIFIED, THEN DROPPED**

WhatsApp was added as a third channel, then dropped once its setup cost was
concrete. The blocker: the Cloud API requires a phone number registered to a
WhatsApp Business Account, and **that number cannot also be an ordinary WhatsApp
account** — so your personal number is ineligible and a second one has to be
acquired and verified. Add Meta business verification and template approval, and it
was a disproportionate amount of real-world setup for a third copy of a notification
already arriving on two transports.

**Channels are now native push and Telegram**, with Web Push as the desktop
fallback and email for digests. Every one of them is free.

Kept as a `Rejected` ADR rather than deleted, because it is an obvious thing to
propose again and the phone-number constraint is not obvious until you go looking.
[ADR-013](adr/ADR-013-whatsapp-channel.md)

**One part survives:** `whatsapp-web.js`, Baileys, and similar libraries remain
prohibited, enforced by a CI dependency check. They are the shortcut that skips
every constraint above, and they get your phone number banned — the same reasoning
you accepted for LinkedIn and Handshake in Q3.

---

## 2. Settled by the ₹0 and single-user constraints

Four questions that used to need your answer no longer do. Recorded rather than
deleted, because "why don't we just…" comes back otherwise.

### Q5 — X / Twitter monitoring · **CLOSED: no**

API v2 Basic is $200/month. Under [ADR-014](adr/ADR-014-zero-cost-hosting.md) the
budget is ₹0, so there is nothing to weigh — this stopped being a judgement call
and became arithmetic. The signal was also largely duplicative: a recruiter who
tweets a role has almost always already posted it to their ATS, which we poll
faster.

### Q6 — Where does this run · **CLOSED: free-tier ARM64, portable**

Was: Hetzner CX32 at ₹640/month, with Singapore and Bangalore upgrades on the
table. All of them cost money.

Now: Oracle Cloud Always Free (4 ARM cores, 24 GB, Mumbai/Hyderabad), with the
MacBook as a first-class fallback and **host portability as the actual decision**
— under an hour, rehearsed quarterly. The host became disposable, which is what
makes a free tier survivable. [ADR-014](adr/ADR-014-zero-cost-hosting.md).

**One thing to know before you sign up:** Oracle requires a card for identity
verification, and avoiding idle-instance reclamation means upgrading to
Pay-As-You-Go — still ₹0 within Always Free limits, but a live card on the
account. If you would rather not, say so: the MacBook path is fully supported and
the only thing lost is overnight coverage.

### Q12 — Multi-tenant ambitions · **CLOSED: no, and nothing is spent on it**

Was: "the architecture keeps this open at near-zero cost." It did not — it cost
RLS on every table, per-user score fanout, a distributed rate limiter for one
collector, and four scaling stages. About a week, spent on a user who does not
exist.

Now: one user. `user_id` columns stay because they are already written and free;
everything else is removed. If a second user ever appears, the path is one
paragraph in [02](02-architecture.md) section 6 and about three weeks — the right
price to pay *then*.

### Q11 — Prestige exception list · **CLOSED: keep it, ~40 entries**

Unpaid roles stay excluded unless flagged. The list is major AI labs, IITs and
IISc, ISRO/DRDO/C-DAC, GSoC, Outreachy, MITACS and similar. It costs nothing to
maintain and the downside of omitting it is missing a genuinely prestigious
research placement. **Say so if you are not interested in unpaid research at all**
and I will drop the whole mechanism — that is the only version of this question
still worth your time.

---

## 3. Still open


### Q4b — Platform and devices · **RESOLVED**

**Android phone, MacBook, Dia browser.** Consequences, all already applied:

**Android only, no iOS.** No Apple Developer Program, no APNs, no `.p8` key, no
macOS build lane. This removes ₹725/month and one of the two never-rotate signing
secrets. Under the ₹0 budget this is no longer a saving, it is a **precondition**:
the Apple fee alone would make a zero-cost design impossible, so the Android-only
choice is what keeps [ADR-014](adr/ADR-014-zero-cost-hosting.md) achievable.

The iOS path stays reachable without a rewrite: Capacitor targets both from the same
shell, and the MacBook means no new hardware would be needed. Roughly 2–3 days if
you ever switch. `device_platform` stays an enum rather than a boolean for exactly
this reason.

**Dia is the primary desktop browser.** It is Chromium-based, so the capability
surface is Chrome's — Web Push, service workers, Background Sync, PWA install.
One addition under [ADR-014](adr/ADR-014-zero-cost-hosting.md): the dashboard
lives on a `*.ts.net` address, so Tailscale must be running on the MacBook for
any of it to load.
Playwright cannot drive Dia, so Chromium is the automated stand-in and Dia is
verified by hand each release. There is no Dia-specific code and no user-agent
sniffing; anything broken there is fixed as a Chromium issue or documented as a
known limitation.

Two things I will check rather than assume, since AI-browser shells sometimes
diverge from upstream Chromium: whether PWA install is offered, and whether Web
Push permission and delivery work. Neither is critical — desktop Web Push is a
fallback, and the Android app plus Telegram are the primary channels.

### Q4c — Advocacy and wider company coverage · **APPLIED, one assumption to confirm**

**Developer advocacy is now a first-class role family**, not a keyword filter:
`advocacy.devrel`, `advocacy.devex`, and `advocacy.solutions`. It carries the
strictest negative-pattern set in the taxonomy plus a technical-evidence gate, so
developer marketing, community management, and quota-carrying sales roles stay out.
Advocacy skill matching weights **breadth and communication over stack depth**,
which is the inverse of the backend weighting — applying the SWE formula would have
systematically under-ranked exactly the roles you asked for.

> **The assumption:** I read "advocacy jobs" as **Developer Advocacy / DevRel**. If
> you meant something else, this is the one thing here worth correcting, since it
> shaped a taxonomy family and a scoring path.

**Company coverage now spans every employer type**, with `company_type` as an axis
separate from size, stage, and industry: product, IT services, consulting,
engineering services, GCCs, core-industry (BFSI, manufacturing, energy, telecom,
retail, healthcare, aerospace, logistics), research, public sector, nonprofit.

Three things worth surfacing from that work:

**GCCs were the biggest gap, and they are Bengaluru-shaped.** Walmart Global Tech,
Target, Goldman Sachs, Bosch, Micron, Rolls-Royce and their peers run structured
paid internship programmes in your Tier 1 city, and they are nearly invisible to the
tools students use because they post to Workday and SuccessFactors rather than
Greenhouse. High value, low competition — that combination now earns a genuine
`competition_estimate` bonus rather than a thumb on the scale.

**The real bias risk was the adapter roadmap, not the ranking formula.** Greenhouse,
Lever, and Ashby cover startups beautifully and cover GCCs not at all, so stopping
at the easy three would have encoded a product-company bias through *coverage* —
much harder to notice than a bad weight. Workday moved firmly into P3 as a result,
and the recall diagnostic is bucketed by `company_type` so a missing category
shows up as a number.

**Two `company_quality` terms were quietly product-biased** and have been fixed:
`funding_health` became `financial_stability` (profitable, public, and
government-backed now score full marks, not zero), and `growth_signal` became
`hiring_momentum` measured against the company's own baseline. Reputation terms are
additive-only, so being invisible on GitHub costs nothing. A CI fairness test
enforces ≤10 point variance across company types, matching the existing size test.

**Cheapest win found along the way:** Thoughtworks, EPAM, Globant, Nagarro, and
Publicis Sapient are high-quality engineering-services firms already on ATS
platforms Scout supports from P1. They were missing from the seed list, not from the
architecture — a data change, not a code change.

**Decision needed:** confirm the advocacy interpretation, and tell me if any
employer category still feels missing.

### Q7 — Which free LLM tiers can you sign up for?

Changed shape. It used to be "which paid providers do you have access to, and
confirm the $20/month cap." There is no cap now because there is no spend
([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)).

What the cascade needs is **at least two independent free tiers** plus the local
Ollama fallback, so that one provider changing its terms is a failover rather
than an outage. Google AI Studio, Groq, and OpenRouter's `:free` variants are the
obvious candidates; any two will do.

Projected usage is roughly 340 requests/day at Year 1 scale, which fits inside a
single provider's daily allowance with about 3× headroom. This is the tightest
resource in the whole ₹0 design, and it is still not tight.

**Decision needed:** sign up for two, tell me which. Signing up is free and takes
about ten minutes each.

**One thing to be aware of:** free tiers commonly reserve the right to train on
what you send them. That is fine for job descriptions, which are public
documents. It is why **your resume, application history, and notes never reach
any of them** — AGENTS.md rule 9, enforced at the client boundary rather than by
good intentions.

### Q8 — Your profile data

Needed before ranking means anything:

- Resume (PDF or text)
- Skills with self-assessed proficiency 1–5
- Graduation month and year, and university
- Any companies to exclude
- Minimum acceptable stipend, if you have one
- Confirm quiet hours 00:00–07:30 IST and digest at 08:00 IST

Can wait until P2, but the sooner it exists the sooner ranking is meaningful rather
than generic.

### Q9 — Seed lists

Now three lists rather than one, since widening the company taxonomy turned this
into the highest-leverage manual work in the project.

| List | Size | When | Who |
| --- | --- | --- | --- |
| Company board tokens (product and startups) | **~60 at P1**, growing to ~250 | P1 → P3 | Me, from public YC, Wellfound, and known-ATS sources |
| **GCC entities** — `(parent, India entity, ATS platform, tenant)` | ~150 | P3 | Me. Known, finite, stable. |
| **Company-type registry** — the top employers hand-classified | ~400 | P2 | Me |

All three are unglamorous hand-maintained data, and all three beat any inference
approach on both accuracy and cost. They are called out here because it is easy to
mistake them for engineering work that can be automated away; they cannot, and they
are worth the hours.

**What I would like from you:** 50–100 companies you actually care about, which get
Tier 1 priority ahead of anything I compile. If you have preferences within the
newer categories — particular GCCs, specific consultancies, an industry you would
rather not hear from — that is worth more than a longer generic list.

### Q10 — Notification aggressiveness

| Setting | Default |
| --- | --- |
| Bengaluru trigger | priority ≥78 |
| General high-score trigger | ≥88 |
| Remote trigger | ≥82 |
| Max per hour | 8 |
| Max per day | 25 |
| Quiet hours | 00:00–07:30 IST |
| Quiet hours exception | Bengaluru at ≥92 only |

Roughly 5–15 notifications on a normal day, more in peak season.

**One thing to be aware of with two channels.** Budgets count *notifications*, not
deliveries, so native push and Telegram together do not double the volume — you
still get at most 8/hour. But each one does buzz twice, on the phone and in
Telegram. Native push is excluded from digests, so the overlap is on instant and
deadline alerts only. If that feels like too much, the fix is turning one channel
down rather than raising thresholds, and open rate is tracked per channel so we
will see which one you actually read.

**Decision needed:** start here, or looser? My recommendation is here — easier to
loosen after a week of "I want more" than to rebuild trust after a week of noise.

## Decisions I made without asking

Judgment calls within the brief's latitude. Not asking for review unless something
stands out.

| Decision | Rationale |
| --- | --- |
| Go + Python + TypeScript rather than one language | [ADR-001](adr/ADR-001-monorepo-and-language-split.md) |
| Network-gated bearer token rather than passkeys | [ADR-015](adr/ADR-015-single-user-auth.md) — supersedes [ADR-010](adr/ADR-010-authentication.md) for the single-user case |
| Cutting cover letters, interview prep, and resume prose | [ADR-016](adr/ADR-016-free-tier-llm-cascade.md) — they needed a frontier model and there is no free one |
| Dropping point-in-time recovery for tiered dumps | [ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) — 95% of the data is re-fetchable |
| Making the repository public | It is what makes CI free; personal data lives in Postgres, never in files |
| Removing the weekly 20-role recall audit | It could not measure what it claimed at n=20; replaced by Scout-first rate ([16](16-observability.md) §2.1) |
| Self-hosted Grafana rather than Datadog | [ADR-011](adr/ADR-011-observability-stack.md) |
| Capacitor rather than React Native for the shell | Same notifications, one UI codebase instead of two |
| Web Push demoted to desktop/fallback | Native push supersedes it on mobile; suppressed to avoid double-buzzing |
| Rejected ADRs kept rather than deleted | A withdrawn decision gets re-proposed; the constraints that killed it are the useful part |
| Local embeddings rather than a hosted API | Zero cost, no network dependency in the dedup hot path |
| Cursor pagination, not offset | Offset pagination on a live feed causes genuinely missed jobs |
| SSE rather than WebSockets | Updates are one-directional |
| REST rather than GraphQL | One client, known access patterns |
| Dark mode as the default | You will be using this in the evenings |
| No automated application submission | [14](14-legal-compliance.md) section 8 — the downside is a cross-ATS ban |
