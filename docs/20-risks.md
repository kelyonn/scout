# Risk Register — Scout

**Status:** Draft · **Owner:** Product · **Last updated:** 2026-08-08

Scored as Likelihood (1–5) × Impact (1–5). Anything at 12 or above needs an
active mitigation with an owner, not just a note.

---

## The four that could actually kill the product

### R1 — Notification fatigue · L4 × I5 = **20**

The subtlest and most likely failure. Notifications become slightly too frequent
or slightly too irrelevant. The user starts glancing and dismissing. Within two
weeks they have muted the channel. The system keeps running perfectly and
delivers zero value, and nothing in the logs indicates a problem.

**Why it is the top risk:** it is gradual, it looks like success from the inside,
and by the time it is obvious the trust is gone.

**Mitigations**
- Strict trigger thresholds; most jobs never notify.
- Notification budgets with demotion rather than dropping.
- Open-rate monitoring as a first-class counter-metric.
- Automatic threshold escalation if open rate falls below 50%.
- Per-notification "fewer like this" feedback.
- The dashboard-sessions counter-metric: if the user starts checking manually
  more often, notifications are failing regardless of what open rate says.

**Early warning:** open rate declining three weeks running. **Response:** raise
all thresholds by 5, review the last 50 notifications by hand, and find out what
made them feel like noise.

**Parallel channels make this risk worse, and the design accounts for it.** Native
push and Telegram both firing for the same job means one opportunity produces two
buzzes. That is deliberate redundancy against transport failure, and it is also
double the intrusion.

The notification *budget* is counted per notification, not per delivery, so two
channels do not double the volume — the user still gets at most 8 an hour and 25 a
day. Native push is excluded from digests, so full overlap happens only on instant
and deadline notifications.

This was a stronger concern when WhatsApp was in the design and one job could buzz
three times; dropping it ([ADR-013](adr/ADR-013-whatsapp-channel.md)) reduced the
intrusion surface as a side effect.

Open rate is tracked **per channel** (`scout_notification_open_ratio{channel}`). If
one channel's open rate collapses while the other holds, the user has silently
started ignoring that channel, and the correct response is to demote it rather than
to raise thresholds globally.

---

### R2 — Silent ingestion failure · L4 × I5 = **20**

A source, an adapter, or a whole platform stops returning results. HTTP 200,
parse succeeds, zero items. Every conventional health check is green. The user
misses opportunities and neither of us knows.

**Mitigations**
- Per-source item count anomaly detection (20% of rolling median).
- Per-adapter aggregate yield monitoring week over week.
- Zero-discovery alert at 4 hours.
- Canary sources with recorded fixtures, checked nightly for shape changes.
- Monthly recall diagnostic (n≥60, bucketed by `company_type`) — the only way to
  measure what we never saw. Deliberately a diagnostic and not a gate; see
  [16](16-observability.md) §2.1 for why the old weekly n=20 audit could not
  support the target it was attached to.

**Early warning:** yield decline in one `source_kind`. **Response:**
[source-broken](runbooks/source-broken.md).

---

### R3 — Over-merge in deduplication · L3 × I5 = **15**

The dedup thresholds are slightly too aggressive. Two distinct internships merge
into one group. The user is notified about one and never learns the other
existed. Completely silent.

**Mitigations**
- Precision gated at 0.99 in CI, harder than recall, deliberately.
- Boilerplate stripping so semantic similarity is meaningful.
- Never merge across company, role family, or seniority automatically.
- Group size cap with alerting above 15 members.
- Every merge audited and reversible.
- Merge-rate anomaly detection.

**Early warning:** merge rate above 3σ, or any large group.
**Response:** [quality-regression](runbooks/quality-regression.md).

---

### R4 — Legal action from a source operator · L2 × I5 = **10**

A platform sends a cease-and-desist or bans the user's account.

**Mitigations**
- [ADR-007](adr/ADR-007-no-tos-violating-scraping.md): we do not build against
  hostile sources at all.
- Compliance gate enforced in code with tests.
- Honest identification with working contact details.
- Email-alert ingestion instead of scraping for prohibited boards.
- Handshake in particular is never touched automatically.

**Response:** stop immediately, mark the source prohibited, reply within 24
hours. The contact address exists so a site operator's first move is an email
rather than a lawyer, and that only works if we actually answer.

---

## Technical risks

| ID | Risk | L×I | Mitigation |
| --- | --- | --- | --- |
| R5 | Postgres is a single point of failure | 2×5=10 | Hourly dumps of irreplaceable tables, nightly full, two off-host destinations, monthly tested restore, 1h RTO ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)). **No PITR** — the two-phase destructive-migration rule is the primary protection instead. |
| R6 | ATS platforms close public endpoints | 3×4=12 | Adapters are isolated plugins; losing one degrades coverage, not the system. Email ingestion and career-page fallback cover the gap. |
| R7 | Model provider outage | 3×3=9 | Multi-provider fallback, then deterministic degradation. Ranking never stops. |
| R8 | LLM request runaway from a bug | 2×3=6 | No spend is possible — free tiers only. A runaway loop exhausts a daily allowance and degrades to rules, which is visible and self-limiting ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). |
| R9 | Embedding version drift corrupting dedup | 2×4=8 | Version column, comparisons scoped to one version, staged migration with eval verification |
| R10 | Postgres queue contention at scale | 2×3=6 | River benchmarks 14x our peak. NATS migration documented behind an interface. |
| R11 | Memory exhaustion on a single node | 3×3=9 | Per-container limits, 85% alerting, vertical upgrade path is a reboot |
| R12 | Disk fills, writes fail | 3×4=12 | Alert at **70%**, auto-prune at 85%, partition drops. Thresholds lowered because **no volume resize is available on a free tier** — the paid escape hatch is gone, so the warning has to come earlier. |
| R13 | Slow memory leak over weeks | 3×3=9 | 24h soak test in CI, container memory limits so one leak restarts one service |
| R14 | Migration locks a large table in production | 2×4=8 | Every migration tested against a prod-sized snapshot with a 30s ceiling; `CONCURRENTLY` above 100k rows |
| R15 | SSRF via attacker-controlled URLs | 2×5=10 | Resolve-then-check-then-pin, private range blocking at every redirect hop, dedicated test corpus |
| R16 | Stored XSS from a job description | 2×4=8 | Sanitize on write and on read, strict nonce-based CSP, no `dangerouslySetInnerHTML` on untrusted content |
| R17 | Prompt injection via a job description | 3×2=6 | Delimited untrusted content, schema-validated output, and structurally: model output never sets a score directly |
| R17a | **Push token goes stale and notifications silently stop** | 3×5=15 | `scout_push_token_invalid_total` alerts on any occurrence; a device registered but not delivering is detected by comparing `token_refreshed_at` to `last_success_at`; two other primary channels cover the gap meanwhile |
| R17b | Native and Web Push both fire for one device | 3×3=9 | Delivery-level suppression preferring native, with a dedicated test — the notification-level unique index does **not** catch this |
| R17c | **Android signing keystore lost** | 2×4=8 | Never-rotate secret held in SOPS **and** backed up offline. There is no recovery path — losing it forces an uninstall and reinstall on every device, because Android refuses to upgrade an app signed by a different key. |
| R17e | **Only two primary channels, one of which is a single vendor** | 2×4=8 | Native push and Telegram fail independently, and either alone satisfies the SLO. Accepted with the note that dropping WhatsApp removed a third layer — if Telegram open rate ever collapses, native push is carrying the product alone. |

---

## Product risks

| ID | Risk | L×I | Mitigation |
| --- | --- | --- | --- |
| R18 | Ranking does not match the user's actual preferences | 3×4=12 | Explanations make disagreement visible; feedback loop; 10% exploration; hand-tuned weights retain 30% influence permanently |
| R19 | Coverage gaps in exactly the roles the user wants | 3×4=12 | Weekly recall audit against manual search; every miss becomes a source to add |
| R20 | Duplicate notifications erode trust | 2×4=8 | Database-enforced uniqueness; concurrency-tested |
| R21 | Unpaid role reaches a notification | 2×4=8 | Paid inference gated at 0.99 precision; verbatim-evidence validation |
| R22 | ~~AI-generated cover letter contains a fabrication the user sends~~ | — | **Eliminated.** Generative features are cut with the frontier tier ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). Recorded rather than deleted because the risk returns the moment anyone re-proposes the feature. |
| R23 | Learned ranking narrows over time | 3×3=9 | 10% exploration slot; blend cap at 70%; fairness gates in retraining |
| R24 | Small companies systematically under-ranked | 3×3=9 | `company_quality` contains no size or brand term; CI fairness test asserts ≤10 point variance across size buckets |
| R24a | **Product-company bias via the adapter roadmap** | 4×4=16 | The likeliest bias route, because it arrives through *coverage* rather than ranking. Greenhouse/Lever/Ashby cover startups and miss GCCs entirely. Mitigated by scheduling Workday to P3 and SuccessFactors to P5, a curated GCC seed list, and recall auditing bucketed by `company_type`. |
| R24b | Services and GCC roles under-ranked by `company_quality` | 3×4=12 | `funding_health` → `financial_stability`, `growth_signal` → relative `hiring_momentum`, reputation terms additive-only. CI fairness test across `company_type` buckets. |
| R24c | Advocacy classification admits marketing or sales roles | 3×3=9 | Strictest negative-pattern set in the taxonomy plus a technical-evidence gate; ~25% Tier 2 escalation accepted as the cost of precision |
| R24d | Advocacy roles under-scored by SWE-shaped skill matching | 3×3=9 | Family-specific weighting favouring breadth and communication; `advocacy parity` property test in CI |
| R24e | GCC entity over-merged with its foreign parent | 2×4=8 | `parent_company_id` models the relationship instead of merging. Without it a Minneapolis role and a Bengaluru role collapse into one group and the location tier — the strongest ranking signal — becomes wrong. Dedicated dedup test. |
| R25 | The user stops using it after placement season | 4×2=8 | Not a failure. Costs are low enough to leave running, and new-grad roles extend the useful window. |

---

## Operational risks

| ID | Risk | L×I | Mitigation |
| --- | --- | --- | --- |
| R26 | Backup exists but restore fails | 2×5=10 | Monthly `make restore-drill` into a throwaway local container, with a measured RTO |
| R27 | Backfill floods the user with notifications | 2×5=10 | Suppression enforced at the notifier, not the caller, with a dedicated 10,000-observation test |
| R28 | Bad deploy takes the system down unnoticed | 2×4=8 | Health gate with automatic rollback; external dead-man's switch ([15](15-infrastructure-deployment.md) §7) |
| R29 | Secret leaked to Git | 2×5=10 | `gitleaks` pre-commit and in CI; documented rotation runbook |
| R30 | Single operator unavailable during an incident | 3×3=9 | Runbooks written for a tired reader; system degrades rather than collapses; notifications survive dashboard failure |
| R31 | Documentation drifts from implementation | 4×2=8 | ADRs are immutable; specs updated in the same PR as the code; requirement IDs referenced from tests |
| R32 | Alert fatigue on the operator side | 3×3=9 | Deliberately scarce alerts; every alert has a runbook; alerts without action get deleted |

---

## Zero-cost risks

New with [ADR-014](adr/ADR-014-zero-cost-hosting.md). These did not exist on a
paid host, and they are the price of the ₹0 constraint. Recorded together because
they share a shape: **there is no contract, so there is no recourse, and the only
available mitigation is making failure cheap.**

| ID | Risk | L×I | Mitigation |
| --- | --- | --- | --- |
| **R37** | **Free-tier host reclaimed or withdrawn** | **3×5=15** | Rehearsed quarterly host migration, ≤1h ([runbook](runbooks/host-migration.md)). PAYG upgrade exempts from idle reclamation at ₹0. MacBook fallback is a supported configuration. |
| **R38** | **Scout stops silently and nobody notices** | **3×5=15** | External dead-man's switch alerting on *absent* heartbeats, plus a GitHub Actions probe as backup for the backup. Self-hosted monitoring cannot report its own host's death — see [15](15-infrastructure-deployment.md) §7. |
| **R39** | A1 capacity unobtainable at signup | 4×2=8 | Expected, not exceptional. Retry across Indian regions; MacBook meanwhile. |
| **R40** | Every LLM free tier rate-limits or withdraws at once | 2×3=6 | Rotation across ≥2 independent providers, then local Ollama. Built in the same milestone as the cascade so the fallback is never theoretical. |
| **R41** | Accidental charge on a PAYG-upgraded account | 2×3=6 | Billing alert at **$0.01**, armed as a P0 exit criterion. One cent turns a silent surprise into a same-day notification. |
| **R42** | Personal data committed to the public repository | 2×5=10 | Personal data lives in Postgres, never in files; `gitleaks` pre-commit and in CI; the explicit list in [13](13-security-privacy.md) §3. |
| **R43** | `age` backup key lost | 1×5=5 | Every backup at both destinations is encrypted with it, so losing it orphans all history simultaneously. Offline media at generation time, a P0 exit criterion alongside the Android keystore. |

**R37 and R38 are the two that matter**, and they compound: a reclaimed instance
is exactly the failure that self-hosted monitoring cannot report. Either alone is
recoverable in an hour; together, undetected, they are "Scout quietly stopped
three weeks ago." That pairing is why the dead-man's switch is a P0 deliverable
rather than something that arrives with the observability stack at P3.

**R43 is low-likelihood and maximum-consequence**, which is the profile that gets
underweighted. It is on the list so that generating the key and putting it on
offline media happen in the same sitting.

---

## Strategic risks

| ID | Risk | L×I | Mitigation |
| --- | --- | --- | --- |
| R33 | Scope creep delays the daily driver past placement season | 4×4=16 | Documented cut lines in [19](19-roadmap.md); daily driver at P3 with dashboard scope explicitly trimmed. **The estimates in [19](19-roadmap.md) were corrected upward from 8 weeks to ~13 for exactly this risk** — an optimistic plan does not make the work faster, it just removes the warning. |
| R34 | Over-engineering consumes all available time | 3×4=12 | Every ADR justifies the *smallest* sufficient choice; Stage 4 architecture explicitly not built |
| R35 | Multi-tenant launch creates copyright exposure | 2×4=8 | Mass display of copyrighted job descriptions is a different legal posture; would need licensed feeds or snippet-only display. Flagged before any public launch. |
| R36 | Building this replaces time spent actually applying | 3×5=15 | **See below.** |

### R36 deserves elaboration

The failure mode where the user spends the placement season building a tool to
find internships instead of applying to internships. It is the highest-impact
strategic risk and the least technical.

**Mitigations**
- P1 ships a working notification in three weeks. Value starts early.
- MVP at P3, roughly 8 weeks. Everything after is optional iteration.
- The cut lines exist so that "good enough" is a defined state.
- Hard rule: **if Scout is not producing applications by week 8, stop building
  and go back to manual search.** The tool serves the goal; it is not the goal.

---

## Risk review

| Activity | Frequency |
| --- | --- |
| Review the top 4 risks and their early-warning metrics | Weekly |
| Full register review | Monthly |
| Add risks discovered through incidents | As they happen |
| Re-score after each milestone | Per milestone |

**A risk that has been mitigated and verified moves to a resolved section rather
than being deleted.** The history of what was worried about and why is useful
later, particularly when someone proposes reversing a decision that was made to
mitigate something.
