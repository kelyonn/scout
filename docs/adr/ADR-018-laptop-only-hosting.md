# ADR-018: Laptop-only hosting — no remote host, no Tailscale

**Status:** Accepted
**Date:** 2026-08-19
**Supersedes:** [ADR-014](ADR-014-zero-cost-hosting.md)'s choice of Oracle A1 as
primary host (ADR-014's portability argument and ARM64 reasoning are unaffected
and still apply)

## Context

ADR-014 chose Oracle Cloud's Always Free Ampere A1 as the primary host, with the
user's MacBook as a documented fallback for exactly the case where Oracle is
unavailable. That fallback is now the situation: Oracle Cloud requires a card for
identity verification at signup, and the user does not have one.

This is not an Oracle-specific problem. AWS, GCP, and Azure's free tiers all
require a card for the same anti-abuse reason, so switching providers does not
route around the actual constraint. The realistic alternatives are: get access to
a card (someone else's, for the one-time verification hold; a virtual debit card
from a fintech app), acquire spare always-on hardware to self-host at home, or
stop requiring an always-on host at all.

The last option turns out to remove the constraint entirely rather than work
around it, which is why it is the decision below rather than "wait for a card."

## Options considered

### Option A — Keep looking for a way to satisfy Oracle's card requirement

**For:** no architecture change; ADR-014's design ships as originally specified,
including the overnight (00:00-08:00 IST) coverage that is the project's stated
reason to exist.
**Against:** open-ended blocker with no committed timeline. A borrowed card or a
virtual debit card might work; neither is guaranteed, and Oracle's card
acceptance policies are reported to change without notice.

### Option B — Self-host on spare hardware at home (Raspberry Pi, old laptop)

**For:** real Linux kernel, real Docker, no card, no cloud signup, keeps the
"always on independent of where the user is" property ADR-014 was built around.
**Against:** no such hardware is currently available. Revisit if that changes —
see Reversal triggers.

### Option C — Self-host on a spare Android phone

**For:** no card, no signup, uses hardware already owned.
**Against:** Docker needs kernel cgroup features Android restricts without root;
running it means fighting the platform (Termux/proot), not using it as designed.
More decisively, the stack's own documented resource budget
([15-infrastructure-deployment.md](../15-infrastructure-deployment.md) section 2)
is ~19.5 GB — Ollama alone wants 8 GB — against a phone's 4-8 GB shared with the
OS and, per the user, an already-in-use daily phone. Rejected as impractical for
the full stack as specified, not as a smaller redesign.

### Option D — Laptop-only: no remote host, run on demand or roughly daily

The collector, brain, notifier, api, and web all run as the existing local
Compose stack, on the user's MacBook, whenever the user chooses to have it run —
in practice, roughly once a day, for however long it takes to poll due sources,
dedupe, score, and notify. Nothing runs when the laptop is closed or asleep.

**For:** removes the blocker entirely rather than working around it — there is
no host to card-verify, provision, or keep reachable. Tailscale becomes
unnecessary too: nothing needs remote reachability, since the one machine that
ever runs Scout is the one the user is sitting at. This is a strict operational
simplification, not just a cheaper version of ADR-014 — the entire host-migration
runbook, the external dead-man's switch (section 7), and the "instance reclaimed"
disaster-recovery rows all stop applying, because there is no separate host to
lose.

**Against:** this is the real cost, named plainly. [01-prd.md](../01-prd.md)'s
stated reason the system exists in its current shape is catching postings within
minutes, particularly overnight IST when US companies post and the user is
asleep — exactly the window ADR-014 rejected the MacBook-as-primary option over.
Laptop-only means the user finds out whenever they next open the laptop, which
could be many hours after a posting goes up. For most postings this still beats
manually checking job boards by a wide margin; for the highest-competition
postings, the gap that mattered most is the one this gives up.

A secondary, smaller cost: the adaptive scheduler
([06-ingestion-pipeline.md](../06-ingestion-pipeline.md)) assigns each source a
poll interval based on observed yield, tuned for continuous operation. Under a
bounded daily session, a source with a short interval simply gets polled less
often than its interval target — the scheduler's own yield-ratio adjustment
(HANDOFF.md's `source.yield_ratio` fix) already handles a source being polled
less than ideal without misbehaving, so this degrades gracefully rather than
breaking, but coverage on a given day is bounded by how long the laptop is open,
not by the scheduler's own pacing.

## Decision

**Laptop-only. No remote host, no Tailscale, no Oracle.** The user runs the full
Compose stack locally, on demand — typically once a day — for as long as it takes
to work through due sources. `SCOUT_FIXTURES_ONLY` is turned off for these runs
(unlike the deterministic, offline `local` environment ADR-014/
[15-infrastructure-deployment.md](../15-infrastructure-deployment.md) section 1
describes for development), so this is simultaneously "local" and "production" —
there is no longer a separate production environment to deploy to.

Telegram notifications are unaffected: delivery is outbound (long-poll on the
sending side, push from Telegram's servers to the user's phone), which needs
nothing inbound-reachable regardless of where the collector runs. The web
dashboard is reached at `localhost` while the stack is running, which is simpler
than Tailscale MagicDNS, not a reduced version of it — there is no remote leg to
secure.

**ADR-015's static bearer token remains the auth mechanism unchanged.** Its outer
gate is now "this process only ever binds to localhost" rather than "Tailscale
network membership," which is at least as strong a boundary for a single machine
that is never remotely reachable. The Tailscale-identity-header defense-in-depth
bullet in ADR-015 simply has nothing to attach to and is inert, not wrong.

## Consequences

**Positive:**

- Unblocks today. No signup, no card, no waiting.
- Removes an entire category of operational surface: no host-migration runbook,
  no external dead-man's switch subscription, no "instance reclaimed" disaster
  row, no Tailscale device management across three devices.
- [ADR-017](ADR-017-tiered-backup-without-object-storage.md)'s backup design
  simplifies to one machine: the "MacBook copy" leg of the irreplaceable-data
  backup was already redundant with the host under ADR-014's two-destination
  design, and is now simply the primary copy. Google Drive via `rclone`,
  `age`-encrypted, remains the offsite leg against laptop loss/disk failure.
- ARM64-only, distroless Go images, `read_only` containers, and every other
  ADR-014 hardening choice are unaffected — they were never Oracle-specific.

**Negative, accepted:**

- Loses the overnight coverage window that is the project's own stated reason
  for existing in its current form. This is the real trade being made, not a
  minor one.
- Coverage on any given day is bounded by how long the laptop runs Scout, not by
  the adaptive scheduler's per-source interval targets.
- The whole system is a single point of failure with no automated recovery
  beyond restoring a dump onto whatever machine runs it next — acceptable for a
  single-user personal tool, not for anything with an uptime expectation.

**Neutral:**

- `infra/compose/production.yml` and `infra/caddy/Caddyfile` are not deleted —
  they are the migration path back (see below) and remain valid if a host
  becomes available later. They are simply not exercised by the laptop-only path.
- Every catastrophe test, dedup guarantee, and notification-dedup guarantee in
  AGENTS.md is host-independent and continues to apply exactly as written.

## Reversal triggers

- A card (borrowed, virtual, or the user's own) successfully completes Oracle
  verification, **and** the overnight coverage gap is judged, after some weeks of
  actual laptop-only use, to be costing real missed opportunities rather than a
  theoretical one.
- Spare always-on hardware (a Raspberry Pi, an old laptop) becomes available —
  cheaper to revisit than Oracle specifically, since it needs no card at all.
- The user's daily-open-laptop pattern changes such that "once a day" stops
  being a realistic cadence (e.g., laptop unavailable for days at a stretch).

## Migration path

Unchanged from [15-infrastructure-deployment.md](../15-infrastructure-deployment.md)
section 6, because nothing about the application was ever host-specific — that
portability property is the actual point of ADR-014 and survives this ADR
untouched:

```
1. Provision the new host (Oracle / a Pi / anything with Docker).
2. Install docker + tailscale; join the tailnet; approve the node.
3. git clone the repository to /opt/scout.
4. Restore the latest dump (from the laptop or Google Drive).
5. docker compose -f infra/compose/production.yml up -d.
6. Re-point Tailscale MagicDNS at the new node.
7. Verify: health gate, one forced poll, one test notification.
```

Moving *to* laptop-only from ADR-014's design is even simpler and needs no
runbook: stop deploying to the host, run `docker compose up` locally instead. No
data migration is required if the laptop was already the backup destination.
