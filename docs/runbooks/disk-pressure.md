# Runbook: Disk pressure

**Severity:** SEV2 at 80%, SEV1 at 90% · **Alert:** Disk usage threshold

A full disk stops Postgres from writing, which stops everything. Act at 80%; do
not wait.

---

## Find what is consuming space

```bash
df -h /
du -sh /opt/scout/* /var/lib/docker/* 2>/dev/null | sort -rh | head -20
```

```sql
-- Largest tables including indexes
SELECT schemaname, relname,
       pg_size_pretty(pg_total_relation_size(relid)) AS total,
       pg_size_pretty(pg_relation_size(relid))       AS heap
FROM pg_catalog.pg_statio_user_tables
ORDER BY pg_total_relation_size(relid) DESC LIMIT 15;
```

Expected offenders, in order: `raw_observation` partitions, Docker images and
build cache, Loki logs, `llm_call`, Prometheus TSDB.

---

## Reclaim, in order of safety

**1. Docker cruft — always safe, usually the biggest single win.**

```bash
docker system df                     # see what is reclaimable
docker image prune -af --filter "until=168h"
docker builder prune -af
docker volume prune -f               # only unused volumes
```

Typically reclaims 3–15GB.

**2. Old observation partitions.**

```sql
SELECT tablename, pg_size_pretty(pg_total_relation_size('public.'||tablename))
FROM pg_tables WHERE tablename LIKE 'raw_observation_%' ORDER BY tablename;
```

Retention is 6 months. Verify the R2 snapshot archive covers the partition before
dropping it, then:

```sql
ALTER TABLE raw_observation DETACH PARTITION raw_observation_2026_01;
DROP TABLE raw_observation_2026_01;
```

`DROP TABLE` on a detached partition is instant and reclaims immediately, unlike
a `DELETE` which would leave dead tuples and require a `VACUUM FULL`.

**3. Old LLM call records.**

```sql
DELETE FROM llm_call WHERE occurred_at < now() - interval '90 days';
```

**4. Observability retention.** Reduce in `prometheus.yml` (30d), `loki.yaml`
(14d), `tempo.yaml` (7d) and restart those containers. They will compact on
startup.

**5. Table bloat.** If tables are large but row counts are not:

```sql
SELECT relname, n_dead_tup, n_live_tup,
       round(n_dead_tup::numeric / NULLIF(n_live_tup, 0), 3) AS dead_ratio,
       last_autovacuum
FROM pg_stat_user_tables WHERE n_dead_tup > 10000
ORDER BY n_dead_tup DESC;
```

High `n_dead_tup` on `river_job` or `job_score` means autovacuum is not keeping
up — the known cost of the queue-in-Postgres decision.

```sql
VACUUM (ANALYZE, VERBOSE) river_job;
-- Only if desperate: VACUUM FULL takes an exclusive lock and needs
-- free space equal to the table size. Do not run this at 95% full.
```

Then tighten autovacuum so it does not recur:

```sql
ALTER TABLE river_job SET (autovacuum_vacuum_scale_factor = 0.01,
                           autovacuum_vacuum_cost_delay = 0);
```

---

## If you cannot reclaim enough

Resize the Hetzner volume. This requires a reboot but is the correct answer when
the data is genuinely needed:

```
Hetzner console → Volumes → Resize → then on the host:
  resize2fs /dev/sdX
```

CX32 → CX42 doubles RAM and disk for about €6/month more.

---

## Prevention

Already in place, verify they are running:

- Alert at 80%, page at 90%.
- Nightly partition maintenance: create 3 months ahead, drop beyond 6 months.
- Hourly River completed-job pruning.
- Weekly `docker image prune`.
- Snapshot upload to R2 with 30-day lifecycle expiry configured on the bucket.

If the disk filled despite these, one of them is not running. Check the cron
container's logs before closing the incident.

---

## Verification

```bash
df -h /                              # below 70%
docker compose ps                    # everything healthy
```

```sql
SELECT pg_is_in_recovery();          -- false
INSERT INTO health_check (ts) VALUES (now());   -- writes work
```

---

## Follow-up

1. **Project forward.** At the current growth rate, when will this recur? If under
   3 months, resize now rather than repeating this.
2. **Did Postgres reject any writes?** Check the log for `No space left`. If so,
   observations may have been lost — replay from R2 snapshots for the affected
   window.
