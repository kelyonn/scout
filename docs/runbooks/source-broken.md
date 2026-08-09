# Runbook: Source or adapter broken

**Severity:** SEV2 (adapter-wide) / SEV3 (single source)
**Alert:** Per-source item count anomaly, or per-adapter yield drop >60%

---

## Distinguish first: one source, or a whole adapter?

```sql
SELECT kind,
       count(*) AS sources,
       sum(total_new_jobs) FILTER (WHERE last_polled_at > now() - interval '7 days')  AS new_7d,
       sum(total_new_jobs) FILTER (WHERE last_polled_at BETWEEN now() - interval '14 days'
                                                            AND now() - interval '7 days') AS new_prev_7d
FROM source WHERE status = 'active'
GROUP BY 1 ORDER BY 3;
```

A whole `kind` at zero while others are normal means the **adapter** broke —
almost always a recent deploy or a platform-wide format change. That is SEV2.
A single source at zero is SEV3.

---

## Adapter-wide breakage

**1. Check what changed.**

```bash
git log --oneline -20 -- adapters/ats/greenhouse/
docker compose images    # what version is running
```

**2. Reproduce against a live source.**

```bash
docker compose exec collector /app/scout-cli fetch \
  --source-id <id> --dry-run --verbose
```

This prints the raw response and the parse result without writing anything.

**3. Diff against the fixture.**

```bash
cd adapters/ats/greenhouse
make fixtures-diff SOURCE=<id>
```

If the live response differs structurally from the recorded fixture, the platform
changed its format. If they match but the parse now returns zero, the code broke.

**4a. If the code broke — revert.**

```bash
# Fastest path: roll back the collector to the previous image
docker compose stop collector
docker compose up -d collector --no-deps   # with the previous tag pinned in .env
```

**4b. If the platform changed format — fix the adapter.**

```
1. Record the new response as a fixture.
2. Update the parser.
3. Update expected output. Keep the OLD fixture and its expectation too —
   some tenants may still serve the old format.
4. Verify both fixtures pass.
5. Deploy.
```

**5. Replay the missed window.**

```bash
curl -X POST localhost:8080/api/v1/admin/replay \
  -d '{"source_kind":"ats_greenhouse",
       "from":"<when it broke>",
       "to":"now",
       "stages":["normalize","classify","dedup","score"],
       "suppress_notifications": false}'
```

**Notification decision.** This is the one case where `suppress_notifications`
should be `false`: these are genuinely new jobs the user has not seen. But check
the volume first —

```sql
SELECT count(*) FROM raw_observation
WHERE source_id IN (SELECT id FROM source WHERE kind = 'ats_greenhouse')
  AND observed_at > '<when it broke>' AND job_id IS NULL;
```

If that is more than ~50, replay with suppression on and let the daily digest
carry them instead. A 300-notification burst is worse than a delayed digest.

---

## Single source broken

```sql
SELECT id, url, kind, status, consecutive_failures, circuit_open_until,
       total_polls, total_successes, yield_ratio, last_polled_at, notes
FROM source WHERE id = '<id>';
```

| Signature | Cause | Action |
| --- | --- | --- |
| 404 repeatedly | Board deleted | `status = 'retired'` |
| 403 repeatedly | We are blocked | Quarantine, review [14](../14-legal-compliance.md), do not retry aggressively |
| 401 | Auth now required | `legal_posture = 'api_only'`, quarantine |
| 429 | Rate limited | Halve `max_rps`, increase `min_interval_s` |
| 200 with 0 items | Format change or soft-404 | Inspect manually |
| Timeouts | Slow or down | Increase timeout once; if persistent, quarantine |

**Manual inspection:**

```bash
curl -sS -A "Scout/1.0 (+https://<domain>/bot)" '<url>' | head -c 2000
```

Compare against what the adapter expects. A soft-404 (a "not found" page served
with status 200) is common and is why the item-count anomaly check exists.

**Fixes:**

```sql
-- Retire a dead board
UPDATE source SET status = 'retired', notes = '404 since 2026-08-06' WHERE id = '<id>';

-- Back off a rate-limited source
UPDATE source SET max_rps = max_rps / 2, min_interval_s = min_interval_s * 2,
                  consecutive_failures = 0, circuit_open_until = NULL
WHERE id = '<id>';

-- Reset after a fix
UPDATE source SET status = 'active', consecutive_failures = 0,
                  circuit_open_until = NULL, next_poll_at = now()
WHERE id = '<id>';
```

---

## Follow-up

1. **Add a fixture** for whatever format broke, so the same change cannot break
   silently again.
2. **If a single source broke in a way the anomaly detector missed,** the
   detection threshold needs tuning — that is the real bug.
3. **If the source is a canary,** the nightly canary check should have caught this
   before the yield alert. Find out why it did not.
4. **Estimate what was missed** during the broken window and note it in the weekly
   recall audit.
