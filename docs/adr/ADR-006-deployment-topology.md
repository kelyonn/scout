# ADR-006: Single VPS with Docker Compose

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout must run continuously and unattended, operated by one person who would
rather spend their time on the product than on infrastructure. It must cost under
₹2,000 (~$24) per month at MVP. It must be recoverable from total host loss.

Notable constraint: the collector makes outbound requests to thousands of hosts.
Serverless platforms bill for wall-clock time on I/O-bound work, which makes them
a poor fit for a crawler that spends most of its life waiting on sockets.

## Options considered

### Option A — Kubernetes (managed: GKE, EKS, DOKS)

**For:** the industry default. Self-healing, rolling deploys, horizontal
autoscaling, declarative everything.
**Against:** managed control planes cost $70–100/month before a single node.
Nodes on top of that. And the real cost is cognitive: Deployments, Services,
Ingresses, ConfigMaps, Secrets, PVCs, HPAs, network policies, and a Helm or
Kustomize layer over all of it. Kubernetes solves multi-team, multi-service,
multi-node orchestration. We have one team member, four services, one node. Every
capability it offers is a solution to a problem we do not have, and each one still
costs its maintenance.

### Option B — Serverless (Lambda, Cloud Run, Vercel Functions)

**For:** no servers, scale to zero, pay per use.
**Against:** the collector is long-running and I/O-bound — exactly the workload
serverless bills worst. A 15-minute crawl cycle billed per-100ms of wall clock is
expensive for work that is 95% socket wait. Cold starts hurt the notification
path. Persistent connection pools to Postgres are awkward and need a pooler.
Local embedding models do not fit comfortably in a function runtime. The job
queue would need to become an external service, reintroducing the dual-write
problem from [ADR-003](ADR-003-job-queue-over-kafka.md).

### Option C — PaaS (Railway, Render, Fly.io)

**For:** genuinely pleasant. Git push to deploy, managed Postgres, managed TLS,
good logs.
**Against:** cost scales unfavorably. Our workload — 4 services, 8GB RAM,
Postgres, Redis, persistent volumes — lands around $60–120/month on any of them.
Less control over the crawler's outbound networking. Fly.io is the closest fit
and is the recommended fallback if VPS management becomes a burden.

### Option D — Single VPS with Docker Compose

**For:** a Hetzner CX32 is 4 vCPU, 8GB RAM, 80GB NVMe for €7.05/month. Docker
Compose is a file most engineers can read without documentation. Complete control.
Deploys are `docker compose pull && docker compose up -d`. Debugging is `ssh` and
`docker logs`.
**Against:** single point of failure. Manual OS patching. No autoscaling. Requires
basic Linux operations competence.

### Option E — Hybrid: VPS for backend, Vercel for frontend

**For:** Vercel's edge network and Next.js integration are genuinely excellent,
and the free tier covers a single-user dashboard.
**Against:** splits the deployment across two systems, requires CORS
configuration, and adds cross-origin latency on every API call. The benefit is
marginal for one user in one geography.

## Decision

**Option D.** Single Hetzner CX32 running Docker Compose, with Cloudflare in
front and Cloudflare R2 for object storage.

### Topology

```
                    Internet
                        │
              ┌─────────▼─────────┐
              │  Cloudflare       │  DNS · TLS · WAF · caching
              │  (free tier)      │  DDoS protection
              └─────────┬─────────┘
                        │
        ┌───────────────▼───────────────────────────────┐
        │  Hetzner CX32 · Falkenstein · €7.05/mo        │
        │  4 vCPU · 8GB RAM · 80GB NVMe · Ubuntu 24.04  │
        │                                               │
        │  ┌─────────────────────────────────────────┐  │
        │  │ Caddy — :80 :443, automatic TLS         │  │
        │  └───┬─────────────────────┬───────────────┘  │
        │      │                     │                  │
        │  ┌───▼────┐  ┌──────────┐  ┌───▼──────────┐   │
        │  │  web   │  │   api    │  │  (internal)  │   │
        │  │ :3000  │  │  :8080   │  │              │   │
        │  └────────┘  └──────────┘  │  collector   │   │
        │                            │  brain       │   │
        │                            │  notifier    │   │
        │                            └──────────────┘   │
        │  ┌──────────────┐  ┌──────────┐               │
        │  │ PostgreSQL16 │  │ Redis 7  │  Docker net   │
        │  │ 2GB shared   │  │  256MB   │  no host port │
        │  └──────┬───────┘  └──────────┘               │
        │         │                                     │
        │  ┌──────▼──────────────────────────────────┐  │
        │  │ Grafana · Prometheus · Loki · Tempo     │  │
        │  └─────────────────────────────────────────┘  │
        └───────────────────┬───────────────────────────┘
                            │  WAL archive · snapshots · backups
                  ┌─────────▼─────────┐
                  │  Cloudflare R2    │  no egress fees
                  └───────────────────┘
```

### Resource allocation on 8GB

| Service | Memory limit | Notes |
| --- | --- | --- |
| PostgreSQL | 2.5GB | `shared_buffers=2GB`, `work_mem=32MB` |
| Brain (Python) | 1.5GB | Includes the ~600MB embedding model |
| Collector (Go) | 1.0GB | Handles 5k concurrent fetches comfortably |
| API (Go) | 384MB | |
| Notifier (Go) | 256MB | |
| Web (Next.js) | 512MB | |
| Redis | 256MB | `maxmemory-policy allkeys-lru` |
| Observability stack | 1.0GB | Prometheus, Loki, Tempo, Grafana |
| Caddy | 64MB | |
| **Total** | **~7.5GB** | ~500MB headroom; alert at 85% |

Every container has an explicit memory limit. Without limits, one leak takes down
the host; with them, one container restarts and everything else survives. This is
the cheapest possible form of failure isolation.

### Environments

| Environment | Where | Purpose |
| --- | --- | --- |
| Local | Developer machine, Compose | Full stack, seeded fixtures, no external calls |
| Staging | Same VPS, separate Compose project + database | Pre-production verification, real adapters against recorded fixtures |
| Production | The VPS | Live |

Staging shares the host deliberately. A separate staging server doubles cost to
catch a class of bug that our test suite already covers. Staging is isolated by
Docker project, database, and port range, and it is resource-capped so it cannot
starve production.

### CI/CD

```
push to main
    ↓
GitHub Actions
    ├─ lint (golangci-lint, ruff, eslint)
    ├─ typecheck (go vet, mypy, tsc)
    ├─ unit tests (all three languages, parallel)
    ├─ integration tests (Postgres + Redis in services)
    ├─ eval harness (golden set — fails on quality regression)
    ├─ Lighthouse CI (fails on performance regression)
    ├─ security scan (govulncheck, pip-audit, npm audit, trivy)
    └─ build + push multi-arch images to GHCR
    ↓
deploy job (SSH, deploy key scoped to one command)
    ├─ docker compose pull
    ├─ run migrations (forward-only, transactional)
    ├─ docker compose up -d --no-deps <changed services>
    ├─ health check with 60s timeout
    └─ on failure: automatic rollback to previous image tags
```

Deploys are per-service, so a web change does not restart the collector
mid-crawl.

### Backup and recovery

| What | Method | Frequency | Retention | Target |
| --- | --- | --- | --- | --- |
| Postgres WAL | `pgbackrest` continuous archive | Streaming | 7 days | R2 |
| Postgres full | `pgbackrest` full backup | Nightly 03:00 IST | 30 days | R2 |
| Raw snapshots | Written directly | On ingest | 30 days | R2 |
| Secrets | SOPS-encrypted in repo | On change | Full history | Git |
| Compose + config | In repo | On change | Full history | Git |

**Recovery objectives:** RPO ≤5 minutes (WAL archive interval), RTO ≤2 hours
(provision, restore, verify). The full procedure is in
[`runbooks/database-recovery.md`](../runbooks/database-recovery.md).

**A restore drill runs on the first Sunday of every month** into a throwaway
Hetzner instance, and the runbook is updated with whatever the drill revealed. A
backup that has never been restored is a hypothesis, not a backup.

### Cost

| Item | Monthly |
| --- | --- |
| Hetzner CX32 | €7.05 (~₹640) |
| Hetzner backups (20%) | €1.41 (~₹128) |
| Cloudflare (DNS, CDN, WAF) | ₹0 |
| Cloudflare R2 (~50GB, no egress fee) | ~$0.75 (~₹65) |
| Domain (amortized) | ~₹100 |
| LLM APIs | ~$3 (~₹260) |
| **Total** | **~₹1,200** |

Comfortably inside the ₹2,000 budget with headroom for LLM usage growth.

## Consequences

**Positive.** ~₹1,200/month. One `docker compose up` reproduces the entire
production stack locally. Debugging is SSH and `docker logs` rather than a
multi-layer abstraction. Deploys take under two minutes. No cloud vendor lock-in —
the entire stack moves to any provider with Docker.

**Negative.** Single point of failure; host loss means downtime until restore
(≤2h). Manual OS patching, mitigated by unattended-upgrades for security patches.
No autoscaling, acceptable because load is predictable and the node is at ~15%
utilization. Requires Linux operational competence. The 8GB ceiling is real and
we will hit it at Stage 2 — the split is already planned.

**Neutral.** Compose is not Kubernetes, and the eventual migration (if ever) is
real work. Compose files translate to Kubernetes manifests mechanically, so this
is measured in days, not weeks.

## Reversal conditions

- Sustained memory above 85% or CPU above 70% → add a second node first (Stage 2).
- More than ~10 services → Compose file becomes unwieldy → consider Nomad, then
  Kubernetes.
- More than one operator → Kubernetes' declarative model starts earning its cost.
- More than 100 users with real availability expectations → managed Postgres with
  a replica, plus multi-node application tier.

## Migration path

**To two nodes:** run collector on node 2, point it at node 1's Postgres over a
WireGuard tunnel. Roughly half a day.
**To Kubernetes:** Kompose converts the Compose files as a starting point, then
manual refinement of secrets, volumes, and health checks. Roughly one week.
**To another provider:** copy the Compose files, restore the database, repoint
DNS. Roughly two hours.
