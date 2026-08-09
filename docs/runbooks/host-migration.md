# Runbook: Host migration

**Severity:** SEV1 when unplanned · **Target: under 60 minutes**
**Rationale:** [ADR-014](../adr/ADR-014-zero-cost-hosting.md)

---

## When this runs

| Trigger | Planned? |
| --- | --- |
| Quarterly drill | **Yes** — the whole point |
| Instance reclaimed by the provider | No |
| Free tier withdrawn or account closed | No |
| A1 capacity lost in the current region | No |
| Moving to a paid host because a budget appeared | Yes |
| Moving to the MacBook temporarily | Either |

**This is the distinguishing procedure of a ₹0 design.** On a paid host, losing
the machine is rare and you file a ticket. On a free tier there is no ticket, no
SLA, and no recourse — so the mitigation is not preventing the loss, it is making
recovery routine. **The one-hour target is the guarantee that replaces the
support contract.**

If you are reading this during an unplanned outage and it feels unfamiliar, the
quarterly drill was skipped. Note that in the follow-up.

---

## Symptoms (unplanned)

- Dead-man's switch fired to Telegram: no collector heartbeat for 15 minutes
- The Oracle console shows the instance terminated, stopped, or missing
- SSH over Tailscale times out and the node shows offline in the Tailscale admin
- An email from the provider about tier changes or account status

---

## Step 0 — Decide the destination first (2 min)

Do not start until this is answered, because it determines everything after.

| Situation | Go to |
| --- | --- |
| Instance is merely stopped | Restart it from the console. **Stop here** — this runbook is not needed. |
| Reclaimed, capacity exists elsewhere | New A1 in another Indian region |
| Reclaimed, no A1 capacity anywhere | **MacBook**, now. Move to a cloud host later. |
| Free tier withdrawn entirely | MacBook now; decide about paying afterwards |
| Planned drill | A throwaway instance; do **not** repoint DNS at the end |

**When in doubt, choose the MacBook.** It is always available, needs no capacity
grant, and gets notifications flowing again in twenty minutes. Migrating a second
time later is cheap — that is the entire premise of this document. Waiting hours
for A1 capacity while Scout is down is the expensive choice.

---

## Step 1 — Provision (10–20 min)

```bash
# Oracle: create an Ampere A1 instance, Ubuntu 24.04, ARM64.
#   4 OCPU / 24 GB is the full Always Free allocation.
#   "Out of host capacity" is common — retry, or change region.
#
# MacBook fallback: nothing to provision. Ensure Docker is running and
#   `caffeinate -s` is active so the machine does not sleep.
```

On a fresh Linux host:

```bash
sudo apt update && sudo apt install -y docker.io docker-compose-v2 age zstd
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up          # approve the node in the Tailscale admin console
```

**Do not open any inbound port.** No 80, no 443, no SSH on the public interface.
Access is over Tailscale only ([ADR-014](../adr/ADR-014-zero-cost-hosting.md)).
If you find yourself configuring a firewall rule for inbound traffic, stop — you
are rebuilding the public surface the design deliberately removed.

---

## Step 2 — Clone and decrypt (5 min)

```bash
git clone <repo> /opt/scout && cd /opt/scout

# The age key lives on offline media, never on a host.
sops -d infra/secrets/production.enc.yaml > infra/secrets/production.yaml
```

---

## Step 3 — Restore the database (10 min)

```bash
docker compose up -d postgres
# wait for healthy
docker compose exec postgres pg_isready -U scout
```

Then follow **Step 4 of [`database-recovery.md`](database-recovery.md)** —
nightly dump first, then the hourly irreplaceable-tables dump on top. Do not
improvise it here; the two-dump sequence is easy to half-do under pressure and
the half that gets skipped is the user's own data.

---

## Step 4 — Start services, notifier last (5 min)

```bash
docker compose up -d api web brain collector
sleep 600                                  # ten minutes, genuinely
docker compose exec postgres psql -U scout -c "
  SELECT count(*) FROM notification WHERE created_at > now() - interval '10 min';"
docker compose up -d notifier
```

**The notifier goes last, every time.** A restored pipeline reprocesses
observations that were already notified, and starting the notifier first sends a
burst of duplicates. That is recoverable data and unrecoverable trust — the same
reasoning as `suppress_notifications` for backfills (AGENTS.md rule 3).

---

## Step 5 — Repoint and verify (10 min)

```bash
# Point the MagicDNS name at the new node.
sudo tailscale up --hostname=scout
tailscale cert scout.<tailnet>.ts.net       # certificate for the new host

# Verify from the MacBook, not from the host.
curl -I https://scout.<tailnet>.ts.net/health
```

**Skip the repoint entirely during a drill.** Leave the throwaway node on a
different hostname, verify, then destroy it. Repointing during a drill is how a
drill becomes an outage.

---

## Verification checklist

- [ ] `docker compose ps` — every service healthy
- [ ] Dashboard loads from the phone **and** the laptop
- [ ] Row counts match [`database-recovery.md`](database-recovery.md) expectations
- [ ] `application_event` max timestamp is within the last hour
- [ ] Collector polled at least one source: new `raw_observation` within 15 min
- [ ] A test notification delivers on Telegram **and** FCM
- [ ] Dead-man's switch received a fresh ping
- [ ] Backup job ran once successfully to **both** destinations
- [ ] **Oracle billing page shows ₹0** and the $0.01 alert survived the move
- [ ] No container publishes a host port: `docker compose ps --format json | grep -c PublishedPort` is 0

The last two are specific to this design and easy to lose in a rebuild. A
migration that quietly ends with a published Postgres port or a billing alert
that was never re-armed has traded one problem for a worse, quieter one.

---

## Follow-up

1. **Record the elapsed time.** If it exceeded 60 minutes, find the step that
   overran and fix this runbook. The number is the guarantee; an unmeasured
   guarantee is a hope.
2. **Destroy the old host** if it still exists, after a week.
3. **Postmortem** in `docs/postmortems/` for any unplanned migration.
4. **If this was unplanned and the last drill was more than a quarter ago**, note
   that as the root cause of any step that felt unfamiliar. The drill is the
   control; skipping it is the incident.
5. **If the free tier was withdrawn rather than the instance reclaimed**, revisit
   [ADR-014](../adr/ADR-014-zero-cost-hosting.md) reversal triggers. That is the
   documented moment to reconsider paying for a host.
