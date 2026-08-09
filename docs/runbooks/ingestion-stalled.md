# Runbook: Ingestion stalled

**Severity:** SEV1 · **Alert:** Zero jobs discovered in 4 hours on a weekday

This is the alert that catches the failure Scout exists to prevent. The system
looks healthy and is finding nothing.

---

## Symptoms

- No new jobs in the feed for hours.
- `scout_jobs_discovered_total` flat.
- Everything else appears normal — no errors, all containers up.

---

## Immediate triage (2 minutes)

```bash
# Is anything actually being polled?
docker compose exec postgres psql -U scout -c "
  SELECT count(*) FILTER (WHERE last_polled_at > now() - interval '10 min') AS recent,
         count(*) FILTER (WHERE status = 'active')                          AS active,
         count(*) FILTER (WHERE next_poll_at < now() - interval '30 min')   AS overdue
  FROM source;"
```

| Result | Meaning | Go to |
| --- | --- | --- |
| `recent = 0`, `overdue` high | Scheduler or collector is not running | Step 1 |
| `recent` high, no new jobs | Fetching works, parsing or writing does not | Step 2 |
| `active = 0` | Every source got quarantined | Step 3 |

---

## Step 1 — Collector not polling

```bash
docker compose ps collector
docker compose logs --tail 200 collector
```

**Container not running:**

```bash
docker compose up -d collector
docker compose logs -f collector      # watch it come up
```

If it crash-loops, the log will show why. Most common: a database connection
failure (check Postgres), a config parse error (check a recent config change), or
an OOM kill (check `docker stats` and `mem_limit`).

**Container running but idle:** the scheduler query is returning nothing.

```sql
-- Is anything actually due and eligible?
SELECT count(*) FROM source
WHERE status = 'active'
  AND legal_posture IN ('permitted','api_only')
  AND next_poll_at <= now()
  AND (circuit_open_until IS NULL OR circuit_open_until <= now());
```

If zero, check whether every circuit breaker is open:

```sql
SELECT count(*) FROM source WHERE circuit_open_until > now();
```

Widespread open breakers means a network-level problem. Test outbound
connectivity:

```bash
docker compose exec collector wget -qO- https://boards-api.greenhouse.io/v1/boards/cloudflare/jobs | head -c 200
```

Failure here means DNS, egress firewall, or the host has lost network. Check
`ufw status` and `docker network inspect scout_default`.

---

## Step 2 — Fetching works, jobs are not appearing

Fetches are succeeding but producing nothing. This is the silent-failure case.

```sql
-- Are observations being written?
SELECT count(*) FROM raw_observation WHERE observed_at > now() - interval '1 hour';

-- Are they being processed?
SELECT count(*) FILTER (WHERE processed_at IS NULL) AS unprocessed,
       count(*) FILTER (WHERE process_error IS NOT NULL) AS errored
FROM raw_observation WHERE observed_at > now() - interval '1 hour';
```

**Observations = 0:** every parse is returning zero items. Almost certainly an
adapter regression from a recent deploy. Check what deployed recently, and go to
[source-broken](source-broken.md).

**Observations exist, unprocessed is growing:** the brain is not consuming.

```bash
docker compose ps brain
docker compose logs --tail 200 brain
```

```sql
SELECT queue, count(*), min(created_at) AS oldest
FROM river_job WHERE state = 'available' GROUP BY 1 ORDER BY 2 DESC;
```

A deep queue with an old `oldest` means the consumer is dead or stuck. Restart:

```bash
docker compose restart brain
```

If it OOMs on restart, the embedding model plus a large batch has exceeded the
memory limit. Reduce `brain.embedding_batch_size` to 16 in config and restart.

**`errored` is high:** read the errors.

```sql
SELECT process_error, count(*) FROM raw_observation
WHERE process_error IS NOT NULL AND observed_at > now() - interval '1 hour'
GROUP BY 1 ORDER BY 2 DESC LIMIT 10;
```

---

## Step 3 — Everything quarantined

```sql
SELECT status, count(*) FROM source GROUP BY 1;
SELECT id, url, consecutive_failures, notes FROM source
WHERE status = 'quarantined' ORDER BY updated_at DESC LIMIT 20;
```

Mass quarantine means a systemic cause: the host IP got blocked, outbound network
failed for a period, or a bad deploy broke the fetcher. Fix the cause first, then
release:

```sql
UPDATE source SET status = 'active', consecutive_failures = 0,
                  circuit_open_until = NULL, next_poll_at = now()
WHERE status = 'quarantined' AND updated_at > now() - interval '6 hours';
```

**Stagger the restart.** Releasing 2,000 sources with `next_poll_at = now()`
produces a thundering herd. Spread them:

```sql
UPDATE source SET next_poll_at = now() + (random() * interval '30 minutes')
WHERE status = 'active';
```

---

## Verification

```sql
-- Should be climbing within 15 minutes
SELECT count(*) FROM raw_observation WHERE observed_at > now() - interval '15 min';
SELECT count(*) FROM job WHERE first_seen_at > now() - interval '15 min';
```

Confirm the Overview dashboard shows discovery rate recovering, and that the
alert clears.

---

## Follow-up

1. **How long was the stall?** Anything over 4 hours means opportunities were
   likely missed. Verify recent postings from the top 20 sources manually.
2. **Why did detection take this long?** If the 4-hour alert was the first signal,
   consider whether a per-adapter yield alert would have caught it sooner.
3. **Write a postmortem** in `docs/postmortems/` if the stall exceeded 2 hours.
4. **Add a test** reproducing the failure mode if it was a code bug.
