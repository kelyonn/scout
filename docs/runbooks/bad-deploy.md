# Runbook: Bad deploy

**Severity:** SEV1 · **Alert:** Health gate failure, or error rate spike after a
deploy

---

## Automatic rollback should already have happened

The deploy script re-pins previous image tags and restarts on health-gate
failure. Verify:

```bash
ssh scout@<host> && cd /opt/scout
cat .env | grep IMAGE_TAG
docker compose ps
docker compose logs --tail 50 deploy 2>/dev/null || tail -50 /var/log/scout-deploy.log
```

If `IMAGE_TAG` is the previous SHA and services are healthy, the rollback worked.
Investigate the failure in CI rather than in production.

---

## Manual rollback

```bash
# Find the last known-good tag
git log --oneline -10
docker images | grep ghcr.io/.../scout

# Pin and restart
export IMAGE_TAG=<previous-sha>
sed -i "s/^IMAGE_TAG=.*/IMAGE_TAG=$IMAGE_TAG/" .env
docker compose pull
docker compose up -d
./scripts/health-gate.sh --timeout 60
```

Roughly 90 seconds.

---

## If a migration ran

This is the case that makes rollback hard, and it is why the two-phase rule
exists.

```sql
SELECT version, dirty, applied_at FROM schema_migrations
ORDER BY version DESC LIMIT 5;
```

**Additive migration** (new table, new nullable column, new index): the old code
ignores it. Roll back the code and leave the schema. No further action.

**Destructive migration** (dropped column, changed type, dropped table): the old
code will fail. Two options:

*Option A — roll forward.* Usually faster if the bug is small. Fix, test, deploy.

*Option B — PITR restore* to just before the migration. See
[database-recovery](database-recovery.md) Step 4. Loses everything written since,
so only worth it if the migration corrupted data.

**`dirty = true`** means a migration failed partway. Do not run more migrations.
Inspect what applied, complete or reverse it manually, then clear the flag:

```sql
UPDATE schema_migrations SET dirty = false WHERE version = <v>;
```

**This is exactly what the two-phase rule prevents.** If a destructive migration
went out in one release, that is the process failure to fix in the postmortem,
not just the bug.

---

## Diagnose the failure

```bash
docker compose logs --tail 300 <failing-service>
```

| Signature | Cause |
| --- | --- |
| Config parse error | Config schema changed without updating the file |
| Connection refused to postgres | Migration still running, or DB down |
| Import/module error | Dependency change not reflected in the image |
| Immediate OOM | New code needs more memory than `mem_limit` |
| Health check timeout | Slow startup — model loading, index warming |
| Panic on first request | A code bug the tests missed |

**Slow startup is a common false alarm.** The brain loads a 600MB embedding model
on boot, which can take 20–30 seconds. If the health gate timeout is too tight,
a working deploy fails. Check whether the service became healthy shortly after
the gate gave up:

```bash
docker compose ps brain
curl localhost:8081/health
```

If so, raise the gate timeout rather than reverting.

---

## Partial deploy

CI deploys only changed services, so a mixed state is possible: new `api`, old
`brain`. This is normally fine — they communicate through Postgres, not RPC —
but a schema change bridging both breaks it.

```bash
docker compose images    # confirm which tag each service is on
```

Get everything onto the same tag, either forward or back:

```bash
docker compose up -d      # all services, current IMAGE_TAG
```

---

## Verification

```bash
./scripts/health-gate.sh --timeout 120
curl -sS https://scout.<domain>/api/v1/health/deep | jq
```

```sql
-- Pipeline moving again
SELECT count(*) FROM raw_observation WHERE observed_at > now() - interval '10 min';
SELECT count(*) FROM job WHERE first_seen_at > now() - interval '10 min';
```

Check the Overview dashboard for error rate returning to baseline, and confirm no
unexpected notifications were created during the incident.

---

## Follow-up

1. **Why did CI pass?** A deploy failure that CI did not catch means a missing
   gate. Add the test.
2. **Was the health gate adequate?** If it passed a broken deploy, it is checking
   the wrong thing. A shallow liveness check will pass a service that starts and
   then fails on real work.
3. **Was anything lost?** Observations queued during the outage should replay
   automatically. Verify:
   ```sql
   SELECT count(*) FROM raw_observation
   WHERE processed_at IS NULL AND observed_at > now() - interval '2 hours';
   ```
   This should be trending to zero.
4. **Postmortem** for any deploy causing more than 15 minutes of degradation.
