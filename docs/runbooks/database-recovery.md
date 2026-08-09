# Runbook: Database recovery

**Severity:** SEV1 · **Alert:** Postgres unreachable, corruption detected, or
host loss

Postgres is the single point of failure, accepted deliberately in
[ADR-002](../adr/ADR-002-postgres-as-the-primary-store.md). This is the procedure
that makes that acceptable.

**RPO:** ≤5 minutes. **RTO:** ≤2 hours.

---

## Decision tree

```
Is Postgres running?
├─ No  → Step 1: restart
├─ Yes, but rejecting connections → Step 2: connection exhaustion
├─ Yes, but returning errors → Step 3: corruption assessment
└─ Host is gone entirely → Step 5: full restore
```

---

## Step 1 — Postgres not running

```bash
docker compose ps postgres
docker compose logs --tail 200 postgres
```

**Common causes and fixes:**

| Log signature | Cause | Fix |
| --- | --- | --- |
| `could not write to file ... No space left` | Disk full | [disk-pressure](disk-pressure.md) first, then restart |
| `database system was not properly shut down` | Unclean stop | It will auto-recover; wait and watch |
| `invalid page in block` | Corruption | Step 3 |
| OOM killed | Memory pressure | Reduce `shared_buffers`, restart |
| `FATAL: could not create shared memory segment` | Host memory | Free memory, restart |

```bash
docker compose up -d postgres
docker compose exec postgres pg_isready -U scout    # expect "accepting connections"
```

---

## Step 2 — Running but rejecting connections

```bash
docker compose exec postgres psql -U scout -c "
  SELECT count(*), state FROM pg_stat_activity GROUP BY 2;"
docker compose exec postgres psql -U scout -c "SHOW max_connections;"
```

**Connection exhaustion.** Kill idle transactions:

```sql
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'idle in transaction'
  AND state_change < now() - interval '5 minutes';
```

Then find the leak — a service failing to release connections. Check pool
configuration in each service and restart the offender.

---

## Step 3 — Corruption assessment

**Stop writes immediately.** Continuing to write to a corrupt database makes
recovery harder.

```bash
docker compose stop collector brain notifier api web
```

```bash
# Check integrity
docker compose exec postgres psql -U scout -c "
  SELECT datname, pg_database_size(datname) FROM pg_database;"

docker compose exec postgres pg_dump -U scout --schema-only scout > /tmp/schema-check.sql
echo "exit code: $?"     # nonzero means structural damage
```

If `amcheck` is available:

```sql
CREATE EXTENSION IF NOT EXISTS amcheck;
SELECT bt_index_check(index => c.oid)
FROM pg_index i JOIN pg_class c ON i.indexrelid = c.oid
WHERE c.relkind = 'i' AND c.relnamespace = 'public'::regnamespace;
```

**Index-only corruption** is the good case — rebuild without data loss:

```sql
REINDEX DATABASE scout;   -- or REINDEX INDEX <name> CONCURRENTLY for one
```

**Heap corruption** means restore. Go to Step 4.

---

## Step 4 — Restore from the last dump

**There is no point-in-time recovery.**
[ADR-017](../adr/ADR-017-tiered-backup-without-object-storage.md) replaced
`pgbackrest` with WAL archiving by tiered `pg_dump`, because ~95% of this
database is re-derivable by re-polling the internet and the few kilobytes that
are not are backed up hourly. The granularity here is therefore the **last hourly
dump** (irreplaceable tables) or the **last nightly dump** (everything).

Read that sentence before you start, because it changes what "restore" means:
you are not rewinding to a second, you are reconstructing from two dumps of
different ages.

```bash
# 1. Fetch the most recent dumps. Two destinations; use whichever is newer.
ls -la ~/scout-backups/                       # MacBook copy, over Tailscale
rclone ls scout-drive:backups | tail -20      # Google Drive copy

NIGHTLY=scout-full-2026-08-07T03-00.dump.zst.age
HOURLY=scout-irreplaceable-2026-08-07T14-00.sql.zst.age

# 2. Decrypt (the age key lives on offline media, NOT on the host)
age -d -i ~/keys/scout-backup.age "$NIGHTLY" | zstd -d > /tmp/full.dump
age -d -i ~/keys/scout-backup.age "$HOURLY"  | zstd -d > /tmp/irreplaceable.sql

# 3. Stop everything
docker compose down

# 4. Move the current data directory aside — do NOT delete it.
#    It is the only copy of anything written since the last dump.
mv /opt/scout/data/postgres /opt/scout/data/postgres.broken.$(date +%s)

# 5. Start a clean Postgres and restore the nightly
docker compose up -d postgres
docker compose exec -T postgres psql -U scout -c 'CREATE DATABASE scout;'
docker compose exec -T postgres pg_restore -U scout -d scout --clean --if-exists \
  < /tmp/full.dump

# 6. Replay the newer irreplaceable-tables dump ON TOP of it.
#    This is the step that recovers the hours between the nightly and now.
docker compose exec -T postgres psql -U scout -d scout -v ON_ERROR_STOP=1 \
  < /tmp/irreplaceable.sql
```

**Step 6 is the one people skip and regret.** The nightly is up to 24 hours old;
the hourly is at most 60 minutes old and contains the only data that cannot be
re-fetched — applications, interview notes, saved state, feedback labels.
Restoring only the nightly silently discards up to a day of the user's own work
while appearing to succeed.

**Verify before starting anything else:**

```sql
SELECT count(*) FROM job;
SELECT count(*) FROM company;
SELECT max(first_seen_at) FROM job;
SELECT count(*) FROM notification;
-- The one that actually matters — user data is the irreplaceable set:
SELECT count(*) FROM user_job_state WHERE state != 'new';
SELECT max(created_at) FROM application_event;   -- should be within the hour
```

Compare against the last known values from the Overview dashboard. If
`application_event` is more than an hour stale, the hourly dump did not apply —
go back to step 6.

```bash
# 7. Bring up services WITHOUT the notifier first.
#    Anything reprocessed could otherwise fire notifications.
docker compose up -d api web brain collector

# 8. Watch for 10 minutes. Confirm the pipeline is healthy and
#    no unexpected notification rows are being created.
docker compose exec postgres psql -U scout -c "
  SELECT count(*) FROM notification WHERE created_at > now() - interval '10 min';"

# 9. Only then start the notifier.
docker compose up -d notifier
```

**Step 8 is not optional.** After a restore, the pipeline may reprocess
observations that were already notified before the restore point. Starting the
notifier first can send a burst of duplicate notifications — recoverable data,
unrecoverable trust.

**Re-ingestion is expected and is fine.** Bulk job data lost between the nightly
dump and the failure will refill on its own as the collector re-polls, because
the sources still have the postings. Do not try to hand-recover it.

---

## Step 5 — Total host loss

Host loss is not a database procedure under
[ADR-014](../adr/ADR-014-zero-cost-hosting.md) — it is the standard, rehearsed
migration, because the host is deliberately disposable on a free tier.

**Follow [`host-migration.md`](host-migration.md).** Target is under an hour, and
it is drilled quarterly precisely so this is not the first time you read it.

---

## Verification checklist

- [ ] `pg_isready` returns accepting connections
- [ ] Row counts match expectations within the RPO window
- [ ] User data intact: saved jobs, applications, interviews
- [ ] No unexpected notifications created post-restore
- [ ] Ingestion resumed: new observations within 15 minutes
- [ ] Dashboard loads and shows correct data
- [ ] A test notification delivers on every channel
- [ ] Backups resumed: the next hourly dump appears at both destinations
- [ ] The backup dead-man's switch has received a fresh ping

---

## Follow-up

1. **Preserve the broken data directory** for at least a week. It may contain
   data written after the last dump that could be manually recovered — and with
   no PITR, this directory is the only route to it.
2. **Measure actual RTO** and compare against the 1-hour target. Update this
   runbook if any step took materially longer than documented.
3. **Determine what was lost** in the RPO window and whether any of it mattered.
4. **Postmortem** in `docs/postmortems/`, always, for any SEV1.
5. **Verify the next scheduled restore drill happens.** A restore under pressure
   is not a substitute for a practiced one.
