# Runbooks

Step-by-step procedures for when something is wrong. Written to be followed at
2am by someone who is tired and not thinking clearly — which is when they get
used.

**Every runbook follows the same structure:** symptoms, immediate triage,
diagnosis, resolution, verification, and follow-up. Read top to bottom; do not
skip to resolution.

**Every alert links to its runbook.** If an alert has no runbook, either write one
or delete the alert.

| Runbook | Triggered by |
| --- | --- |
| [service-down](service-down.md) | Dead-man's switch fired, container not running |
| [host-migration](host-migration.md) | **Instance reclaimed, free tier withdrawn, or quarterly drill** |
| [database-recovery](database-recovery.md) | Postgres unreachable, corruption, total loss |
| [ingestion-stalled](ingestion-stalled.md) | Zero discovery for 4 hours |
| [source-broken](source-broken.md) | Per-source or per-adapter yield collapse |
| [notifications-stuck](notifications-stuck.md) | Notification queue depth, delivery failures |
| [disk-pressure](disk-pressure.md) | Disk above 70% |
| [llm-budget-exceeded](llm-budget-exceeded.md) | All LLM free tiers rate-limited; degraded mode active |
| [bad-deploy](bad-deploy.md) | Health gate failure, post-deploy errors |
| [security-incident](security-incident.md) | Any SEV1 security event |
| [quality-regression](quality-regression.md) | Eval failure, bad merges, wrong rankings |

## Before anything else

```bash
ssh scout@<host>
cd /opt/scout

docker compose ps                    # what is running
docker compose logs --tail 100 -f    # what is happening
df -h /                              # disk
free -h                              # memory
docker stats --no-stream             # per-container resources
```

## Emergency stop

If Scout is doing something actively harmful — hammering a source, sending a
notification storm, burning LLM budget:

```bash
# Stop notifications only (ingestion continues, nothing is lost)
docker compose stop notifier

# Stop all outbound fetching (queue drains, nothing is lost)
docker compose stop collector

# Stop all paid model calls immediately
curl -X POST localhost:8080/api/v1/admin/llm/kill-switch \
     -H "Cookie: $SESSION"

# Full stop (last resort)
docker compose stop
```

Stopping the notifier or collector is safe and reversible. Queued work persists
in Postgres and resumes on restart. Nothing is lost by stopping; a great deal can
be lost by hesitating.
