# Cost Model — Scout

**Status:** Draft · **Owner:** Infrastructure · **Last updated:** 2026-08-08

**Budget: ₹0/month. Not "low" — zero. No recurring charge of any size.**

Rationale for the topology this implies is in
[ADR-014](adr/ADR-014-zero-cost-hosting.md),
[ADR-016](adr/ADR-016-free-tier-llm-cascade.md), and
[ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md).

---

## 1. Why zero and not "cheap"

The earlier version of this document targeted ₹2,000/month and landed at ₹1,193.
That was a good number and it was still the wrong target.

Scout is built by a student, for that student, with no revenue and no
institutional backing. A ₹1,200/month subscription is a decision that gets
re-made every month, and it competes with things that are more obviously worth
₹1,200. The failure mode is not that it becomes unaffordable — it is that during
exam week, or in the month after placements finish, someone looks at a bank
statement and switches it off. **A tool that costs nothing has no month in which
it is reconsidered.**

There is a second reason, specific to this project. ₹0 forces every component to
justify itself against a hard wall rather than a soft one. Three of the four
decisions it forced ([ADR-014](adr/ADR-014-zero-cost-hosting.md) through
[017](adr/ADR-017-tiered-backup-without-object-storage.md)) produced a *simpler*
system than the paid design, not a poorer one. The paid budget had been quietly
permitting complexity that nothing needed.

---

## 2. The bill

| Item | Provider | Monthly | Basis |
| --- | --- | --- | --- |
| Compute (4 ARM cores, 24 GB RAM, 200 GB) | Oracle Cloud Always Free | **₹0** | Always Free tier ([ADR-014](adr/ADR-014-zero-cost-hosting.md)) |
| Egress (~200 GB/mo used) | Oracle | **₹0** | 10 TB/mo included |
| Network access, TLS, DNS | Tailscale free personal | **₹0** | 100 devices; `*.ts.net` certs |
| Domain | — | **₹0** | **None needed.** MagicDNS replaces it. |
| Object storage | — | **₹0** | **None.** Snapshots on local disk, 30-day expiry |
| Backups | MacBook + Google Drive | **₹0** | 15 GB free, already owned ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)) |
| LLM | Free tiers, rotated + local Ollama | **₹0** | ([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)) |
| Embeddings | `bge-small-en-v1.5`, local ONNX | **₹0** | Was already local |
| Push notifications | FCM | **₹0** | Free at any volume |
| Chat channel | Telegram Bot API | **₹0** | Free |
| Inbound email (job alerts) | Gmail IMAP poll | **₹0** | Existing account |
| Outbound email | — | **₹0** | **Cut.** Digest goes to Telegram |
| CI | GitHub Actions | **₹0** | Unlimited on a **public** repo |
| Error tracking | Sentry free | **₹0** | 5k events/month; we generate far fewer |
| Dead-man's switch | healthchecks.io free | **₹0** | 20 checks |
| Metrics, logs, dashboards | Prometheus + Loki + Grafana, self-hosted | **₹0** | Runs on the same host |
| Android distribution | Direct APK | **₹0** | No store, no review, no developer fee |
| **Total** | | **₹0** | |

**The repository must stay public for the CI line to hold.** Private repos get
2,000 Actions minutes/month, which this workflow would consume. That has a
consequence with teeth: the user's profile, resume, notes, and application
history must never enter the repository. See
[13-security-privacy.md](13-security-privacy.md) section 3 — it is enforced by
`.gitignore`, by a pre-commit secret scan, and by the fact that all of it lives
in Postgres rather than in files.

---

## 3. What ₹0 actually costs

Nothing is free. These bills are paid in currencies other than rupees, and
pretending otherwise is how a zero-cost design turns into an abandoned one.

| Paid in | What | Roughly |
| --- | --- | --- |
| **Setup time** | Oracle signup, A1 capacity retries, Tailscale, `rclone`, `age`, healthchecks | 4–6 hours, once |
| **Recurring time** | Quarterly host-migration drill; monthly restore drill; watching three providers' free-tier terms | ~3 hours/quarter |
| **Risk** | No SLA, no support, no recourse. An instance can be reclaimed on a Tuesday. | Bounded to ≤1 hour by the migration drill |
| **Capability** | No PITR ([017](adr/ADR-017-tiered-backup-without-object-storage.md)); no frontier model, so no cover letters or interview prep ([016](adr/ADR-016-free-tier-llm-cascade.md)) | Both deliberate |
| **Convenience** | Tailscale must be running on any device that opens the dashboard | Minor; it is always-on anyway |
| **Privacy posture** | A card sits on the Oracle account if PAYG is used to dodge idle reclamation | Optional; MacBook-only avoids it |

The honest summary: **₹0 costs about five hours up front and three hours a
quarter, and buys back the risk that the system gets switched off.** For a tool
whose entire value proposition is running unattended for a year, that is a good
trade.

---

## 4. Headroom against the free tiers

Cost discipline at ₹0 is not about spend, it is about **staying inside limits**.
These are the numbers that matter.

| Resource | Free limit | Projected use | Headroom |
| --- | --- | --- | --- |
| Oracle A1 RAM | 24 GB | ~6 GB (Postgres 2 GB, services 2 GB, observability 1 GB, embeddings 1 GB) | **4×** |
| Oracle block storage | 200 GB | ~25 GB at Year 1 (DB ~15 GB, snapshots ~8 GB) | **8×** |
| Oracle egress | 10 TB/mo | ~200 GB | **50×** |
| Tailscale devices | 100 | 3 | **33×** |
| Google Drive | 15 GB | ~2 GB of rolling encrypted dumps | **7×** |
| Sentry events | 5,000/mo | <200 expected | **25×** |
| healthchecks.io | 20 checks | 4 | **5×** |
| GitHub Actions | Unlimited (public) | ~15 min/day | ∞ |
| LLM Tier 2 requests | Provider-dependent, ≥1,000/day across two providers | ~340/day at Year 1 | **≥3×** |
| Telegram Bot API | No practical limit | ~20 messages/day | ∞ |
| FCM | Unlimited | ~20/day | ∞ |

**The tightest line is LLM requests at ~3×**, and it is the one that moves with
scale. It is also the one with a designed response: rotate providers, then fall
back to local, then degrade
([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). Every other resource has an
order of magnitude of room.

**Storage is the one to actually watch**, because it grows monotonically while
everything else oscillates. The 30-day snapshot expiry and the six-month
partition drop are what keep it flat; if either job stops, disk fills silently.
Both are alerted on ([16-observability.md](16-observability.md)) and both have a
runbook ([disk-pressure](runbooks/disk-pressure.md)).

---

## 5. What was avoided, and what it would have cost

Kept from the earlier version because the reasoning is the useful part, updated
for a ₹0 baseline.

| Alternative | Would cost/month | Decision |
| --- | --- | --- |
| Frontier LLM on every job | ~₹2,992,000 | [ADR-005](adr/ADR-005-llm-cascade.md) — the cascade |
| Managed Kafka (smallest) | ~₹8,800 | [ADR-003](adr/ADR-003-job-queue-over-kafka.md) |
| Elastic Cloud (smallest) | ~₹8,360 | [ADR-004](adr/ADR-004-search-strategy.md) |
| Residential proxies for scraping | ~₹8,800 | [ADR-007](adr/ADR-007-no-tos-violating-scraping.md) |
| Managed Kubernetes (control plane only) | ~₹6,600 | [ADR-014](adr/ADR-014-zero-cost-hosting.md) |
| Datadog (1 host, our cardinality) | ~₹5,000 | [ADR-011](adr/ADR-011-observability-stack.md) |
| Managed Postgres | ~₹2,200 | [ADR-002](adr/ADR-002-postgres-as-the-primary-store.md) |
| Apple Developer Program (iOS build) | ₹725 | Not needed; the phone is Android ([ADR-012](adr/ADR-012-native-app-shell.md)) |
| Hetzner CX32 + backups + R2 + domain + LLM | ₹1,193 | [ADR-014](adr/ADR-014-zero-cost-hosting.md) |
| WhatsApp Cloud API | ₹70–150 + a dedicated number | [ADR-013](adr/ADR-013-whatsapp-channel.md) |

The frontier-LLM line still dominates and is still not a typo. It is what "send
every job to a frontier model" costs at 150k observations/day, and it is why the
cascade is the most valuable decision in the project — worth more than every
other cost decision combined, by three orders of magnitude.

---

## 6. Cost controls

At ₹0 these are limit controls, not spend controls.

| Control | Mechanism | Enforcement |
| --- | --- | --- |
| LLM request budget | Per-provider RPM/RPD/TPM; wait, rotate, then degrade | In the client ([016](adr/ADR-016-free-tier-llm-cascade.md)) |
| Response caching | 30-day TTL on prompt hash | Automatic |
| Input truncation | Per-task character limits | Automatic |
| Rendered-HTML cap | Max 20 sources | Enforced at source registration |
| Snapshot retention | 30 days | Nightly job, alerted if it stops |
| Partition retention | 6 months, DROP not DELETE | Nightly job, alerted if it stops |
| Disk | Alert at 70%, auto-prune at 85% | Lowered from 80/90 — no volume resize is available on a free tier |
| Bandwidth | 85% 304-response target | Monitored |
| **Oracle billing** | **Alert at $0.01** | Console budget alert; an exit criterion in [19](19-roadmap.md) |

**The $0.01 billing alert is the important one.** On a PAYG-upgraded account
(which is how idle reclamation is avoided), provisioning a resource outside the
Always Free limits is a normal-looking action that starts charging silently. A
one-cent threshold turns "I accidentally picked the wrong shape" into a
notification within hours instead of a surprise at month end. This is the single
control standing between ₹0 and an unexpected bill, and it costs nothing to set.

**The rendered-HTML cap is worth keeping.** Headless Chromium costs roughly 200×
a plain fetch in CPU and memory. Twenty sources is affordable on 4 ARM cores; two
hundred would need a host that does not exist for free.

---

## 7. Cost per outcome

| Metric | MVP | Year 1 |
| --- | --- | --- |
| Monthly cost | **₹0** | **₹0** |
| Jobs discovered/month | ~1,500 | ~24,000 |
| Cost per job discovered | ₹0 | ₹0 |
| Cost per notification | ₹0 | ₹0 |

The interesting denominator is no longer money, it is **the user's time**. The
manual alternative costs roughly 6 hours/week. Scout costs about 5 hours of
setup and 1 hour/month of operation. It breaks even in the first week and every
week after that is profit, measured in the only currency the project actually
spends.

---

## 8. Review

| Activity | Frequency | Why |
| --- | --- | --- |
| Free-tier terms for all providers | Quarterly | They change without notice; this is the main ₹0 risk |
| Storage growth projection | Monthly | The only monotonically growing resource |
| LLM request volume vs. provider limits | Monthly | The tightest headroom |
| Oracle billing page | Monthly | Confirm ₹0, not just assume it |
| Host-migration drill | Quarterly | [ADR-014](adr/ADR-014-zero-cost-hosting.md) |
| Restore drill | Monthly | [ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) |

**Alert thresholds:** any Oracle charge above $0.01 · storage above 70% · storage
growth projecting full within 60 days · LLM daily requests above 60% of the
combined provider allowance · any backup job missing its dead-man's-switch ping.
