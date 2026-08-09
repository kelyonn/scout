# Runbook: Service down

**Severity:** SEV1 · **Alert:** External uptime probe fails 3× consecutively

---

## Triage

```bash
# From your machine — is it the host or the app?
ping -c 3 <host-ip>
curl -sS -o /dev/null -w "%{http_code} %{time_total}s\n" https://scout.<domain>/api/v1/health
```

| Result | Meaning | Go to |
| --- | --- | --- |
| Ping fails | Host down or network | Step 1 |
| Ping OK, HTTP times out | App down or Caddy down | Step 2 |
| HTTP 502/503 | Caddy up, backend down | Step 3 |
| HTTP 200 but slow | Overload | Step 4 |

---

## Step 1 — Host unreachable

Check the Hetzner console. If the VM is running but unreachable, use the console
(not SSH) to look at the host:

```bash
systemctl status docker
journalctl -xe --no-pager | tail -100
df -h /
free -h
```

Common: disk full (Docker cannot start containers), OOM killing the kernel's
network stack, or a failed unattended-upgrade reboot.

If the VM itself is dead and will not recover, go to
[database-recovery](database-recovery.md) Step 5.

---

## Step 2 — Host up, HTTP not responding

```bash
ssh scout@<host> && cd /opt/scout
docker compose ps
```

**Caddy not running:**

```bash
docker compose logs --tail 100 caddy
docker compose up -d caddy
```

Most common Caddy failure is an ACME certificate renewal problem. Check the log
for rate limiting from Let's Encrypt — if rate-limited, Caddy will retry, and the
existing certificate is usually still valid for weeks.

**Caddy running, upstream unreachable:** Step 3.

---

## Step 3 — Backend down

```bash
docker compose ps
docker compose logs --tail 200 api web
```

| Cause | Signature | Fix |
| --- | --- | --- |
| OOM kill | `Killed`, exit 137 | Check `docker stats`; raise `mem_limit` or find the leak |
| Crash loop | Restarting repeatedly | Read the log; usually config or a database connection |
| Database unreachable | Connection errors | [database-recovery](database-recovery.md) |
| Bad deploy | Started after a deploy | [bad-deploy](bad-deploy.md) |

```bash
docker compose up -d api web
./scripts/health-gate.sh --timeout 60
```

---

## Step 4 — Up but slow

```bash
docker stats --no-stream
uptime                       # load average
```

```sql
-- Slow queries in flight
SELECT pid, now() - query_start AS duration, state, left(query, 120)
FROM pg_stat_activity
WHERE state != 'idle' AND now() - query_start > interval '5 seconds'
ORDER BY duration DESC;
```

**A long-running query blocking everything** — usually an unindexed query added
in a recent deploy, or an `ANALYZE`-starved planner choosing a sequential scan:

```sql
SELECT pg_cancel_backend(<pid>);      -- graceful
SELECT pg_terminate_backend(<pid>);   -- if cancel does not work
```

Then find and fix the query. `EXPLAIN (ANALYZE, BUFFERS)` on the offender.

**High load from the collector:** it may be polling too aggressively after a mass
source reset. Throttle:

```bash
docker compose stop collector
# spread next_poll_at, then restart
```

---

## Verification

```bash
curl -sS https://scout.<domain>/api/v1/health/deep | jq
```

Every dependency should report healthy. Confirm the external uptime probe clears
and that the dashboard loads in a browser.

---

## Follow-up

1. **Duration of the outage** and whether notifications were missed. Notifications
   queue durably, so they should deliver on recovery — verify they did.
2. **Why was the external probe the first signal?** If an internal alert should
   have fired earlier, fix the alert.
3. **Postmortem** for any outage over 30 minutes.
