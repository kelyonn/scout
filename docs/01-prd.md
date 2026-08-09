# Product Requirements — Scout

**Status:** Draft · **Owner:** Product · **Last updated:** 2026-08-08

---

## 1. The problem

Internship hiring is a race decided in the first hours. For high-demand software
engineering internships, the applicant-to-slot ratio at large companies commonly
exceeds 1000:1, and a meaningful share of screening capacity is consumed within
the first 24–48 hours of a posting going live. Applying on day 1 versus day 5 is
frequently the difference between a recruiter screen and an auto-reject.

The current process for a fourth-year CS student is:

- Check 6–10 job boards manually, each with a different filter model.
- Follow perhaps 30 company career pages, most of which have no feed.
- Skim Hacker News "Who is Hiring" once a month.
- Rely on a LinkedIn feed optimized for engagement, not for relevance.
- Miss anything posted while asleep, in class, or during exams.

Three failures compound:

**Latency.** Manual checking has a discovery latency measured in days. The value
of a posting decays fastest in the first 48 hours.

**Coverage.** No student monitors the long tail. Seed-stage startups, research
labs, and mid-size product companies post roles that never appear on aggregators
and that often have a 50:1 applicant ratio instead of 1000:1. These are the
highest-expected-value applications and they are the ones nobody sees.

**Signal.** Aggregators optimize for volume. The result is hundreds of
irrelevant, unpaid, expired, or ghost postings for every real opportunity, which
trains the student to stop reading them.

Scout attacks all three: continuous monitoring for latency, aggressive source
breadth for coverage, and per-candidate semantic ranking for signal.

---

## 2. Users

### 2.1 Primary user (design target)

A fourth-year Computer Science student in India, targeting a 2026–2027 internship
or new-grad software engineering role.

| Attribute | Value |
| --- | --- |
| Location preference | Bengaluru first, then anywhere in India, then remote, then international |
| Role interest | SWE, backend, full stack, AI/ML, systems, infra, cloud, DevOps, platform, research, **developer advocacy and solutions engineering** |
| Employer interest | **All types** — product, startups, IT services, consulting, GCCs, core-industry enterprises, research, public sector. No product-company bias and no FAANG bias. |
| Hard requirement | Paid only |
| Technical comfort | High — can self-host, read logs, run Docker |
| Time budget | Wants to spend zero minutes searching and all available minutes applying and preparing |
| Devices | MacBook primary (browser: Dia), Android phone for notifications |
| Peak availability | Evenings and weekends IST; asleep 00:00–08:00 IST |

The system is built for this person specifically — not for a segment they belong
to. Every default is tuned for them, not for a hypothetical average, and where a
choice is arbitrary it resolves in their favour.

### 2.2 Secondary user — deliberately not a constraint

Other students, possibly, someday. **This user constrains nothing.**

The earlier version of this document said the opposite: that no schema, ranking
model, or notification path could assume a single user, and that everything
per-person would be keyed by `user_id` from day one because it "costs almost
nothing now and avoids a rewrite later."

The first half was true and the second half was not. Keeping the door open cost
Row-Level Security on every table, a distributed rate limiter for one collector,
per-user score fanout designed for thousands, and four documented scaling stages
— about a week of work and a permanent tax on every query, spent on a user who
does not exist and may never.

**What is kept is only what is already free:** `user_id` columns stay in the
schema, because they are written and cost nothing when there is one value. RLS,
the scaling stages, and the fanout design are gone. The migration path if a second
user ever appears is one paragraph in
[02-architecture.md](02-architecture.md) section 6, and it is about three weeks —
which is the correct amount to pay *then*, not now.

Scout is built for one person. That is the whole design target and it is a
feature.

### 2.3 Operator

The same person, wearing a different hat, when something breaks at 2am. This user
needs runbooks, clear alerts, and a system that fails partially rather than
totally. See [`16-observability.md`](16-observability.md) and
[`runbooks/`](runbooks/).

---

## 3. Product principles

These resolve disputes when requirements conflict.

**1. A missed opportunity is worse than a false positive — but only just.**
Coverage is the primary goal. Precision is the constraint. We tolerate some noise
to avoid misses, but the moment notifications become ignorable, the product has
failed completely. Measured as **Scout-first rate above 90%** and notification
precision above 90% — see section 7.1 for why it is measured that way and not as
a recall percentage.

**2. Interrupt rarely, and mean it.** Every push notification spends trust. A
notification must clear a bar that a dashboard row does not. Most opportunities
belong in the dashboard or in the daily digest, not on the lock screen.

**3. Explain every ranking.** A score with no explanation is unfalsifiable and
therefore untrustworthy. Every job shows why it scored what it scored, in
sentences, not just numbers.

**4. Degrade, never collapse.** If the LLM provider is down, ranking falls back
to deterministic heuristics. If one source breaks, the other 400 keep running.
If embeddings fail, dedup falls back to structural matching. No single dependency
takes the system offline.

**5. Legal and ethical constraints are requirements, not preferences.** We
respect robots.txt, honor rate limits, identify ourselves honestly, and do not
build against sources whose terms prohibit it. This is not risk aversion; it is
the only way a system that runs unattended for years stays running.

**6. The system must cost ₹0 to run.** Not "cheap" — zero. A ₹1,200/month
subscription is a decision that gets re-made every month, and it competes with
things that are more obviously worth ₹1,200; the failure mode is not that it
becomes unaffordable but that someone switches it off during exam week. **A tool
that costs nothing has no month in which it is reconsidered.** Every design
choice is evaluated against that wall. See
[18-cost-model.md](18-cost-model.md) and
[ADR-014](adr/ADR-014-zero-cost-hosting.md).

---

## 4. Scope

### 4.1 In scope for the daily-driver release (P0–P3)

- Source registry with 400+ configured sources across ATS, feeds, and community.
- Polite, change-detecting ingestion with per-source scheduling.
- Canonical job normalization with role, location, and compensation extraction.
- Three-stage deduplication across sources.
- Scoring with hand-tuned weights and written explanations.
- Instant notifications via Telegram and native push; **daily digest via
  Telegram**, not email — email sending needs a verified domain, which costs
  money ([ADR-014](adr/ADR-014-zero-cost-hosting.md)).
- Dashboard with feed, search, filters, saved/applied tracking, and dark mode.
- Native Android app ([ADR-012](adr/ADR-012-native-app-shell.md)) at P4.
- Observability, tiered backups, and documented recovery.

### 4.2 In scope post-MVP

Resume matching and skill-gap analysis (both local — embedding cosine and
ontology diff) · interview tracker and calendar · company watchlists with
hiring-signal alerts · learned ranking from user feedback · Meilisearch-backed
faceted search · Discord adapter · recruiter contact extraction · email parsing
of application replies.

**Cut, not deferred:** AI cover letter generation, interview question
prediction, resume feedback prose, and offer comparison. All four needed a
frontier model, which has no free tier worth the name, and all four were already
on the cut list. See [ADR-016](adr/ADR-016-free-tier-llm-cascade.md) — the
honest version of that decision is that a student can write three good cover
letters a week by hand better than a small model can, and the three that matter
are the only ones worth writing.

### 4.3 Explicit non-goals

| Non-goal | Reasoning |
| --- | --- |
| **Any recurring cost, of any size** | Not a budget, a wall. See principle 6 and [ADR-014](adr/ADR-014-zero-cost-hosting.md). |
| **Multi-tenancy, or work done in advance for it** | One user. `user_id` stays in the schema because it is already free; RLS, per-user fanout, and scaling stages do not. [02](02-architecture.md) section 6. |
| **A public internet presence** | Tailscale only. No domain, no DNS, no WAF, no public TLS. Enabling Tailscale Funnel requires implementing [ADR-010](adr/ADR-010-authentication.md) first. |
| **Generative AI features** (cover letters, interview questions, resume prose) | Need a frontier model; no free tier provides one ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). Cut rather than deferred. |
| **WCAG 2.2 AA conformance as a gate** | Cheap a11y habits are kept; a certified conformance pass on a single-user dashboard is days of work against requirements written for public services. |
| Automatic application submission | Most ATS terms prohibit automated submission. Detection means a permanent ban across every company using that ATS. Catastrophic downside, marginal upside. |
| Scraping LinkedIn, Indeed, Glassdoor, Handshake | Prohibited, actively enforced, and in Handshake's case tied to university identity. See [ADR-007](adr/ADR-007-no-tos-violating-scraping.md). |
| A separate native UI codebase | The app ships as a Capacitor shell around the same web UI, which gives real FCM notifications without a second frontend to maintain. See [ADR-012](adr/ADR-012-native-app-shell.md). |
| WhatsApp notifications | The official Cloud API needs a dedicated phone number that cannot be the user's own, plus Meta verification and template approval — disproportionate setup for a third copy of a notification. Unofficial libraries are prohibited outright. See [ADR-013](adr/ADR-013-whatsapp-channel.md). |
| Publishing to the App Store or Play Store | Personal app, sideloaded. No review, no fee on Android. Revisit only if Scout becomes multi-tenant. |
| Being a public job board | Different product, different legal posture, different economics. Scout is a personal agent. |
| Non-software internships | Out of scope by definition. Filtered at classification. |
| Developer marketing, community management, technical recruiting | Adjacent to the advocacy family and deliberately excluded. Advocacy roles must show concrete technical evidence to be admitted — see [07](07-normalization-taxonomy.md). |
| Non-technical consulting roles | `advocacy.solutions` covers technical pre-sales and delivery engineering. A sales role with a quota and no technical requirement is not in scope. |
| Salary negotiation advice | High liability, low confidence, better sources exist. |
| CAPTCHA solving or bot-detection evasion | A bright line. If a site does not want automated access, we do not take it. |

---

## 5. User stories

Each story has an identifier, an acceptance criterion, and a target milestone.

### Discovery

| ID | Story | Acceptance criterion | Milestone |
| --- | --- | --- | --- |
| `US-DISC-01` | As a candidate, I want new postings found without my involvement | ≥400 sources polled on schedule; ≥95% success rate over 24h | P1 |
| `US-DISC-02` | I want new companies discovered automatically | Registry grows ≥50 companies/week without manual entry | P5 |
| `US-DISC-03` | I want coverage of small startups, not just big names | ≥40% of surfaced roles from companies under 500 employees | P3 |
| `US-DISC-04` | I want roles found even when the company has no feed | Sitemap and HTML change detection fallback operational | P2 |
| `US-DISC-05` | I want board coverage without ToS violations | Email-alert ingestion live for ≥4 boards | P3 |
| `US-DISC-06` | I want Bengaluru GCC roles, not just startups | ≥100 GCC entities polled; GCC roles present in the feed weekly | P3 |
| `US-DISC-07` | I want services and consulting firms covered | IT services, consulting, and engineering-services employers all represented | P5 |
| `US-DISC-08` | I want core-industry employers covered | BFSI, manufacturing, energy, telecom, retail represented | P5 |
| `US-DISC-09` | I want no category silently missing | Weekly recall audit reported **per `company_type`**, not just in aggregate | P3 |

### Relevance

| ID | Story | Acceptance criterion | Milestone |
| --- | --- | --- | --- |
| `US-REL-01` | I want only software engineering roles | Classification precision ≥97% on the golden set | P2 |
| `US-REL-02` | I want unpaid roles excluded | Zero unpaid in notifications, except flagged prestige exceptions | P2 |
| `US-REL-03` | I want Bengaluru ranked first | Bengaluru role outranks identical role elsewhere, always | P2 |
| `US-REL-04` | I want remote roles boosted | Remote applies a documented positive multiplier | P2 |
| `US-REL-05` | I want to know *why* something ranked high | Every job has a generated explanation ≤50 words | P3 |
| `US-REL-06` | I want ranking to learn from me | Model retrains on feedback after ≥200 labelled interactions | P5, optional |
| `US-REL-07` | I want developer advocacy roles too | `advocacy.*` classified at ≥95% precision; marketing and sales roles excluded | P3 |
| `US-REL-08` | I want advocacy roles scored on their own terms | Advocacy skill match weights breadth and communication, not stack depth | P3 |
| `US-REL-09` | I want no company-type penalty | Mean `company_quality` varies ≤10 points across `company_type` buckets, enforced in CI | P2 |

### Notification

| ID | Story | Acceptance criterion | Milestone |
| --- | --- | --- | --- |
| `US-NOTIF-01` | I want to know within minutes | p50 ≤10 min, p95 ≤30 min, detection to delivery | P3 |
| `US-NOTIF-02` | I never want the same role twice | Zero duplicate notifications per job group, ever | P3 |
| `US-NOTIF-03` | I want no alerts while asleep | Quiet hours 00:00–07:30 IST; queued, not dropped | P3 |
| `US-NOTIF-04` | I want a morning summary | Digest delivered 08:00 IST daily | P3 |
| `US-NOTIF-05` | I want deadline warnings | Alert at T-72h and T-24h on saved jobs | P5 |
| `US-NOTIF-06` | I want multiple channels | Native push + Telegram + email operational, failing independently | P3 |
| `US-NOTIF-07` | I want normal app notifications, not just a bot | Installed Android app delivers via FCM with correct icon, actions, badge count, and tunable OS notification channels | P3 |
| `US-NOTIF-08` | I want to triage without opening the app | Save/Dismiss work from the lock screen and from Telegram inline buttons | P3 |

### Management

| ID | Story | Acceptance criterion | Milestone |
| --- | --- | --- | --- |
| `US-MGMT-01` | I want to save and track jobs | Full state machine: new → saved → applied → interviewing → offer/rejected | P3 |
| `US-MGMT-02` | I want to search everything found | Sub-200ms p95 full-text + semantic search | P5 |
| `US-MGMT-03` | I want to track interviews | Interview tracker with stages and dates | P5 |
| `US-MGMT-04` | I want application analytics | Funnel conversion by company tier, role, source | P5 |
| `US-MGMT-05` | I want it to work on my phone | Native Android app installed and usable; feed scrolls without jank on the actual device | P4 |

### Preparation

| ID | Story | Acceptance criterion | Milestone |
| --- | --- | --- | --- |
| `US-PREP-01` | I want to know what skills I am missing | Gap analysis across my ranked opportunity set — ontology diff, computed locally | P5 |
| `US-PREP-04` | I want resume-to-role match feedback | Per-job resume score with specific missing keywords named — embedding + keyword overlap, computed locally | P5 |

`US-PREP-02` (cover letters) and `US-PREP-03` (interview questions) are **cut**.
Both needed a frontier model and there is no free tier for one
([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). The two that remain survive
because they were never generative — they are a diff and a cosine, and both run
locally at zero cost.

---

## 6. Functional requirements

Abbreviated; each links to its full specification.

| ID | Requirement | Spec |
| --- | --- | --- |
| `SCOUT-SRC-*` | Source registry, per-source access strategy, tiering | [05](05-source-catalog.md) |
| `SCOUT-ING-*` | Scheduling, conditional fetch, change detection, politeness, retry | [06](06-ingestion-pipeline.md) |
| `SCOUT-NORM-*` | Canonical schema, role/location/comp extraction, taxonomies | [07](07-normalization-taxonomy.md) |
| `SCOUT-DEDUP-*` | Company identity resolution, three-stage job dedup, grouping | [08](08-dedup-identity.md) |
| `SCOUT-RANK-*` | Thirteen subscores, weighting, calibration, learning loop | [09](09-ranking-scoring.md) |
| `SCOUT-AI-*` | Model cascade, prompt contracts, RAG, cost ceilings, evals | [10](10-ai-features.md) |
| `SCOUT-NOTIF-*` | Trigger rules, channel routing, budgets, quiet hours, templates | [11](11-notifications.md) |
| `SCOUT-UI-*` | Screens, states, design system, PWA, accessibility | [12](12-frontend-ux.md) |
| `SCOUT-SEC-*` | Threat model, authn/z, secret handling, PII, retention | [13](13-security-privacy.md) |
| `SCOUT-LEGAL-*` | robots.txt, per-source ToS posture, identification | [14](14-legal-compliance.md) |

---

## 7. Non-functional requirements

### 7.1 Service level objectives

| SLO | Target | Measurement | Error budget |
| --- | --- | --- | --- |
| **Scout-first rate** | ≥90% | Of roles the user applied to, the fraction Scout surfaced before they saw it elsewhere | 10% |
| Notification latency (Tier 1) | p50 ≤10 min, p95 ≤30 min | Source `posted_at` → delivery ack | 5% of notifications/month |
| Notification precision | ≥90% | User marks notification relevant/not | 10% noise |
| Duplicate rate | 0 per job group | Unique constraint on `(job_group_id, user_id, trigger)` | Zero tolerance |
| Ingestion success rate | ≥95% per 24h | Successful fetches / attempted | 5% |
| Per-adapter yield stability | No adapter below 40% of its 28-day median | Rolling per-`source_kind` comparison | — |
| API p95 latency | ≤300ms | Server-side histogram | 5% of requests |
| Search p95 latency | ≤200ms | Server-side histogram | 5% of queries |

**Discovery recall is no longer an SLO, and that is a correction rather than a
retreat.** The previous target — ≥95%, measured by a weekly manual audit against
20 hand-searched roles — could not be measured by that instrument. At n=20 the
resolution is 5 percentage points per role: one miss is exactly 95%, two is 90%.
It cannot distinguish meeting the target from missing it by half. It was also
self-audited, on roles found by the same search habits Scout is meant to replace,
which biases the sample toward what Scout already covers.

What replaces it is the **Scout-first rate** at the top of the table. Every
application is a sample, so n accumulates on its own with no audit ritual, and it
measures the thing actually cared about rather than a proxy for it. One tap on "I
found this elsewhere first" is the entire instrument. Recall survives as a
**monthly diagnostic** at n≥60, reported with a confidence interval and bucketed
by `company_type` to find missing *categories* — a question the Scout-first rate
cannot answer. Full reasoning in [16-observability.md](16-observability.md)
section 2.1.

**Availability is not an SLO either.** The host is a free tier with no SLA
([ADR-014](adr/ADR-014-zero-cost-hosting.md)), so a percentage would be a number
we cannot honour and would not act on. It also measures the wrong thing: the
dashboard being down costs nothing, because notifications are an outbound path
and keep working. The question that matters — *is it running at all?* — is
answered by an external dead-man's switch instead.

Notification latency is where the strictness goes, because that is where the
product's value actually lives.

### 7.2 Scale targets

| Dimension | MVP | Year 1 |
| --- | --- | --- |
| Sources monitored | 400 | 2,500 |
| Companies tracked | 1,500 | 15,000 |
| Job observations/day | 20,000 | 150,000 |
| Live job records | 30,000 | 250,000 |
| **Users** | **1** | **1** |
| Notifications/day | 5–20 | 10–40 |
| DB size | ~4 GB | ~40 GB |

**The multi-tenant column is gone.** It projected 5,000 users and 1M
observations/day, and every number in it was shaping decisions today for a system
that does not exist. The host has 4 ARM cores and 24 GB and sits at roughly 5%
utilization at MVP and under 20% at Year 1 — there is nothing to plan toward.
See [02-architecture.md](02-architecture.md) section 6 for what a second user
would actually cost, stated as one paragraph rather than three milestones.

### 7.3 Other constraints

**Performance.** First Contentful Paint ≤1.2s and Time to Interactive ≤2.5s on a
mid-range Android over 4G. Dashboard feed renders 50 items without jank.

**Accessibility.** Keyboard navigation, visible focus, 4.5:1 contrast, and
`prefers-reduced-motion` honored — because they are cheap, they are good habits,
and two of them the user directly benefits from. **WCAG 2.2 AA is no longer a
gate and there is no CI a11y budget.** A full conformance pass plus axe-core in
CI is several days of work to certify a single-user dashboard against
requirements written for public services. If Scout ever has other users, this
becomes a real requirement again.

**Security.** No secret in source control — enforced by `gitleaks`, and load-
bearing because **the repository is public** so that CI is free. Nothing exposed
to the internet; access over Tailscale only. Full detail in
[13](13-security-privacy.md), including the list of what must never be committed.

**Cost. ₹0/month.** Not a target, a constraint. Enforced by free-tier limits
rather than by a spend cap: LLM budgets are denominated in requests per window
and degrade to local inference and then to rules
([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). The single control standing
between ₹0 and a surprise is a billing alert set at $0.01.

**Data retention.** Raw HTML snapshots 30 days. Job records indefinite. User
interactions indefinite. Full policy in [13](13-security-privacy.md).

---

## 8. Success metrics

### 8.1 The one metric that matters

**Time from posting publication to candidate awareness.** Everything else is
instrumental. Baseline (manual): 2–5 days. Target: under 30 minutes.

### 8.2 Supporting metrics

| Metric | Baseline | P3 target | P5 target |
| --- | --- | --- | --- |
| **Scout-first rate** | 0% | 80% | 90% |
| Relevant roles discovered/week | ~10 (manual) | 60 | 120 |
| Median discovery latency | ~48h | ≤30 min | ≤10 min |
| Notification precision | n/a | 85% | 92% |
| Applications submitted/week | ~5 | 15 | 25 |
| Time spent searching/week | ~6h | ≤30 min | ≤10 min |
| Interview conversion rate | unknown | measured | +20% vs. P3 |
| Duplicate notifications | n/a | 0 | 0 |
| **Monthly cost** | ₹0 | **₹0** | **₹0** |

The cost row is the one that changed shape. It used to describe a budget that
grew with scale; it is now a constant that any nonzero value violates. A ₹40 line
item appearing is a defect, investigated like any other.

### 8.3 Counter-metrics

Watched to catch success that is actually failure:

- **Notification ignore rate.** Rising means we are training the user to ignore
  us. Above 30% triggers a mandatory ranking review.
- **Dashboard sessions per day.** If this goes *up* over time, notifications are
  not doing their job and the user is compensating by checking manually.
- **Saved-but-never-applied rate.** Above 60% means ranking surfaces things that
  look good and are not.

### 8.4 The qualitative bar

The project has succeeded when the user stops opening LinkedIn Jobs. Not because
they were told to — because it stopped being useful.

---

## 9. Assumptions

| # | Assumption | If wrong |
| --- | --- | --- |
| A1 | Most ATS platforms expose stable public JSON endpoints | Fall back to sitemap and HTML diffing; ingestion cost rises ~3x |
| A2 | Job alert emails from boards are parseable and reliable | Board coverage drops; lean harder on direct ATS and aggregator APIs |
| A3 | A small LLM can classify roles at ≥97% precision | Escalate more; if free tiers cannot, local Ollama at reduced precision |
| A4 | One free-tier host handles Year 1 load | It is at <20% utilization at Year 1; if wrong, the MacBook fallback or a paid host — both are a one-hour migration |
| A5 | Embeddings resolve duplicates at ≥95% F1 | Add a human confirm step in the UI for uncertain merges |
| A6 | Telegram remains free and reliable | FCM is a parallel channel; no single point of failure |
| A7 | User will provide feedback on ≥200 jobs within 3 months | Learned ranking is dropped; hand-tuned weights persist and are adequate |
| **A8** | **Free tiers used (Oracle, Tailscale, LLM providers, healthchecks.io, Sentry, Drive) remain free and available** | **The largest assumption in the project.** Each has a named fallback in [ADR-014](adr/ADR-014-zero-cost-hosting.md)/[016](adr/ADR-016-free-tier-llm-cascade.md)/[017](adr/ADR-017-tiered-backup-without-object-storage.md); the host is the only one whose loss stops the system, and that is bounded to a rehearsed one-hour migration |
| **A9** | **Oracle A1 capacity is obtainable at signup** | Retry across Indian regions; MacBook fallback meanwhile. Capacity denial is common and expected, not a surprise |

**A8 is the assumption that replaced "we can afford ₹2,000/month", and it is
weaker.** A paid host can be relied upon because money creates an obligation; a
free tier cannot, because there is no contract, no SLA, and no support queue. The
mitigation is not to make A8 more likely — nothing can — but to make its failure
cheap, which is the entire subject of
[ADR-014](adr/ADR-014-zero-cost-hosting.md).

---

## 10. Dependencies and risks

Full register in [`20-risks.md`](20-risks.md). The four that could actually kill
the product:

**Legal action from a source operator.** Mitigated by [ADR-007](adr/ADR-007-no-tos-violating-scraping.md)
and the compliance matrix in [14](14-legal-compliance.md). We do not build
against hostile sources.

**ATS endpoints closing.** Greenhouse, Lever, and Ashby public boards could
require authentication at any time. Mitigated by adapter isolation — each is a
plugin, and losing one degrades coverage rather than breaking ingestion.

**Free-tier withdrawal.** The risk that replaced cost overrun, and the largest
assumption in the project (A8). A free tier can be withdrawn, capacity-denied, or
reclaimed with no notice and no recourse, because there is no contract to appeal
to. Mitigated not by making it less likely — nothing can — but by making failure
cheap: a rehearsed one-hour host migration
([ADR-014](adr/ADR-014-zero-cost-hosting.md)), rotated LLM providers with a local
fallback ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)), and two independent
backup destinations ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)).

**Scout stops silently.** On a free tier with no SLA and self-hosted monitoring,
the system can simply stop — reclaimed instance, full disk, dead process — with
nothing running to report it. Mitigated by an external dead-man's switch that
alerts when heartbeats *stop*, which is the only monitoring shape that can report
its own absence.

**Notification fatigue.** The subtlest and most likely failure. Mitigated by
strict trigger thresholds, notification budgets, and the ignore-rate counter-metric.
