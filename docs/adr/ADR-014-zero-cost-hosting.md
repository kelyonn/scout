# ADR-014: Zero-cost hosting on a free tier, with portability as the guarantee

**Status:** Accepted
**Date:** 2026-08-08
**Supersedes:** [ADR-006](ADR-006-deployment-topology.md) (single Hetzner VPS)

## Context

ADR-006 chose a Hetzner CX32 at €7.05/month and argued the budget was ₹2,000.
The budget is now **₹0**. Not "low" — zero. No recurring charge of any size.

That is a smaller change than it sounds in one way and a much larger one in
another.

**Smaller:** the workload has not changed. Docker Compose, one node, Postgres,
five services. Everything ADR-006 argued against — Kubernetes, serverless, PaaS —
it argued against on grounds that get *stronger* at ₹0, not weaker.

**Larger:** the dominant risk changes completely. On a paid VPS the risk is cost
overrun, and the mitigation is right-sizing. On a free tier the risk is
**revocation** — the provider withdraws the tier, denies capacity, reclaims the
instance, or closes the account, and there is no SLA, no support queue, and no
recourse, because you are not a customer. Money cannot fix a free-tier outage.

So this ADR is not really about which host. It is about making the choice of host
cheap to reverse. **The decision is portability; the host is an implementation
detail of it.**

## Options considered

### Option A — Oracle Cloud Infrastructure, Always Free tier

**For:** the only free tier large enough to run the whole system. The Ampere A1
allocation is 4 OCPU and 24 GB RAM (divisible across up to 4 VMs), 200 GB block
storage, and 10 TB/month egress. There are regions in India (Mumbai, Hyderabad),
so latency to Indian sources and to the user is good. It is "always free", not a
12-month trial.

**Against:** three real problems, none of them fatal but all of them worth
knowing before you depend on it.

1. **A1 capacity is frequently unavailable.** "Out of host capacity" on instance
   creation is common in popular regions and can persist for days. Plan on
   retrying, possibly across regions.
2. **Idle instances are reclaimed.** Always Free compute that stays under roughly
   15% CPU is subject to reclamation. Scout polling continuously sits near that
   line. The documented escape is upgrading the account to Pay-As-You-Go, which
   exempts it from idle reclamation and still bills ₹0 as long as usage stays
   inside Always Free limits — but it does mean a live card on an account that
   *could* be billed if a resource is provisioned outside those limits.
3. **A card is required for identity verification** at signup.

Free-tier terms change. Verify all three at signup rather than trusting this
document, which was written on 2026-08-08.

### Option B — Google Cloud `e2-micro` Always Free

**For:** genuinely free, no capacity problems, reputable.
**Against:** 1 shared vCPU and 1 GB RAM, in US regions only. Postgres with
pgvector, five services, and a local embedding model do not fit in 1 GB, and
would not fit if they did — the ONNX model alone wants more. Useful as an
external watchdog (see Consequences), useless as the host.

### Option C — The user's MacBook

**For:** ₹0, no signup, no card, no provider to revoke anything, full control,
and already ARM64 — the same architecture as Option A, so images are identical.
**Against:** it sleeps when the lid closes, changes networks, and reboots for
updates. Coverage gaps land exactly in the 00:00–08:00 IST window that
[01-prd.md](../01-prd.md) identifies as the reason the system exists — that is
when US companies post and when the user is asleep. `caffeinate -s` and "never
sleep on power" mitigate but do not solve it, because the lid still closes and
the machine still travels.

### Option D — Free PaaS tiers (Fly.io, Railway, Render, Koyeb)

**For:** pleasant, managed TLS, git-push deploys.
**Against:** none of them still offer a free tier that fits this footprint.
Render's free web services sleep on idle; Railway's free allowance was replaced
by a trial credit; Fly.io's free allowances were withdrawn. What remains free is
sized for a demo, not for Postgres plus a crawler. Re-checking these annually is
cheap, so this option is kept on the reversal list rather than dismissed.

### Option E — GitHub Actions as the compute, free Postgres elsewhere

**For:** unlimited Actions minutes on a public repository, no host to manage.
**Against:** scheduled workflows are best-effort — they are delayed under load
and skipped outright on busy runners, which is disqualifying for a system whose
entire product claim is latency. Jobs cap at 6 hours, so nothing long-running
survives. Free Postgres tiers (Neon, Supabase) auto-suspend on idle, adding a
cold start to the notification path. And using Actions as general-purpose compute
rather than as CI is at best a grey area in GitHub's terms, which is not a
position this project takes anywhere else
([ADR-007](ADR-007-no-tos-violating-scraping.md)).

Rejected as the host. Kept for two narrower jobs where it is genuinely the right
tool: CI, and a scheduled external watchdog.

## Decision

**Oracle Cloud Always Free (Ampere A1, ARM64, Mumbai or Hyderabad) as the primary
host. The user's MacBook as a first-class documented fallback, not an emergency
improvisation. Host portability is a hard requirement with a rehearsed drill and
a stated time target.**

Access is over **Tailscale** (free personal tier), not the public internet.

### Portability is the actual decision

The whole system is: a git repository, a Docker Compose file, and one Postgres
dump. Moving hosts is:

```
provision host → install docker + tailscale → git clone → restore dump → compose up
```

**Target: under 60 minutes, rehearsed quarterly**, recorded in
[`runbooks/host-migration.md`](../runbooks/host-migration.md). A drill that has
never been run is a hypothesis, exactly as with backups.

This is what converts free-tier revocation from an unbounded risk into a bounded
one. It is also the reason the milestone plan in [19](../19-roadmap.md) never
spends time on host-specific tooling: no Terraform, no Ansible, no cloud-init
that only Oracle understands. Anything that makes the host special makes the
escape slower.

### ARM64 everywhere, deliberately

Oracle A1 is ARM64. The MacBook is ARM64. Building `linux/arm64` only — rather
than multi-arch — makes local and production the same architecture, halves build
time, and removes an entire class of "works on my machine" bug. `pgvector/pgvector`
publishes arm64, Go cross-compiles trivially, and ONNX Runtime ships aarch64
wheels, so nothing in the stack objects.

The cost: a future x86 host needs a rebuild, not a re-architecture. That is a
`docker buildx --platform` flag, and it is on the migration runbook.

### Nothing is exposed to the internet

Tailscale's free personal tier covers 100 devices, which is 97 more than this
needs. MagicDNS plus `tailscale cert` provides real certificates on
`scout.<tailnet>.ts.net` with no domain and no ACME HTTP-01 challenge, therefore
no open port 80.

This deletes work rather than adding it — see
[13-security-privacy.md](../13-security-privacy.md), which loses most of its
public-surface sections as a direct result. Two design consequences follow and
both are simplifications:

| Was | Becomes | Why it is better |
| --- | --- | --- |
| Inbound email webhook (Cloudflare Email Routing → HTTPS endpoint) | **IMAP poll** of a dedicated Gmail account | No public endpoint, no HMAC verification, no domain, no MX records. Polling a mailbox every 2 minutes meets the same latency target. |
| Telegram webhook | **Telegram long-poll** (`getUpdates`) | No public endpoint and no secret-token verification path to get wrong. |

Tailscale Funnel remains available, free, as an escape hatch if a genuine public
endpoint is ever needed. It is not needed today.

The Android app must have Tailscale installed to reach the dashboard. Push
notifications are unaffected: FCM delivery is Google→phone, and Scout's side of
it is an outbound call, so no inbound reachability is required
([ADR-012](ADR-012-native-app-shell.md)).

## Consequences

**Good:**

- Recurring cost is ₹0, verifiably, with no line item that can drift.
- 24 GB of RAM is 3× what ADR-006 budgeted, which removes the memory pressure
  that shaped several earlier decisions. The embedding model, Postgres, and the
  observability stack now fit without argument.
- The public attack surface is gone, not reduced.
- Host migration is a rehearsed hour, so provider risk is capped.

**Bad, and accepted:**

- **No SLA and no support.** If Oracle reclaims the instance on a Tuesday, the
  recourse is the migration runbook, not a ticket.
- **A card sits on the account** if PAYG is used to avoid idle reclamation.
  Billing alerts at $0.01 are mandatory, not optional, and are an exit criterion
  in [19](../19-roadmap.md). If the user prefers no card at all, the MacBook path
  is fully supported and the only thing lost is overnight coverage.
- **Capacity may not exist at signup.** The fallback is real and must be ready
  before it is needed, which is why the MacBook path is specified rather than
  improvised.
- **Quarterly drills cost real hours** that a paid host would not.

**A free tier cannot page you when it dies, and self-hosted monitoring cannot
detect its own host being down.** This is the one monitoring problem the ₹0
constraint genuinely creates. The answer is an external dead-man's switch: Scout
pings a free healthchecks.io check every 5 minutes, and if the pings stop —
because the host was reclaimed, the disk filled, or the network went — the
service alerts Telegram from *outside* the failure domain. A scheduled GitHub
Actions workflow is the backup for the same job. Both are free, and between them
the "silently switched off" failure mode is covered.

## Reversal triggers

Revisit this decision if any of the following becomes true:

- Oracle withdraws or materially shrinks the Always Free tier, or A1 capacity is
  unobtainable in every Indian region for more than two weeks.
- The instance is reclaimed more than once despite PAYG.
- Scout becomes multi-user, at which point free-tier terms almost certainly no
  longer permit it and a paid host is both required and affordable from revenue.
- A budget appears. A Hetzner CX32 remains the right paid answer and
  [ADR-006](ADR-006-deployment-topology.md) remains the right reasoning for it —
  this ADR supersedes its *conclusion*, not its analysis.

The migration runbook makes acting on any of these an afternoon.
