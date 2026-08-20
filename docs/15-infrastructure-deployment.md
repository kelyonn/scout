# Infrastructure and Deployment — Scout

**Status:** Draft · **Owner:** Infrastructure · **Last updated:** 2026-08-19

Topology rationale is in [ADR-014](adr/ADR-014-zero-cost-hosting.md), which
supersedes [ADR-006](adr/ADR-006-deployment-topology.md), and
[ADR-018](adr/ADR-018-laptop-only-hosting.md), which partially supersedes 014.
Backup rationale is in
[ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md). This is the
operational specification.

**Budget: ₹0.** Every choice below is constrained by that and by the fact that
one person operates this in the evenings.

> **Current default: laptop-only ([ADR-018](adr/ADR-018-laptop-only-hosting.md)).**
> Oracle Cloud requires a card for identity verification that isn't available
> right now, so there is no remote host and no Tailscale today. Sections 2, 3,
> 6, 7, and 8 below describe the Oracle+Tailscale design from ADR-014 — it is
> not dead, it is the documented migration target for the day a host becomes
> available (a card, or spare always-on hardware), and everything in it
> (Compose topology, ARM64, hardening, `production.yml`, `Caddyfile`) is
> unchanged and ready to use as-is. Until then, section 1's **live**
> environment below is what actually runs.

---

## 1. Environments

| Environment | Where | Data | Purpose |
| --- | --- | --- | --- |
| **local** | MacBook, Docker | Seeded fixtures, no live fetching | Development |
| **live** | MacBook, Docker, on demand | Real, live fetching | The real thing, today ([ADR-018](adr/ADR-018-laptop-only-hosting.md)) |
| **production** | Oracle A1 (ARM64), *not currently provisioned* | Live | The real thing, once a host exists ([ADR-014](adr/ADR-014-zero-cost-hosting.md)) |

**There is no staging, deliberately.** The earlier design ran staging as a second
Compose project on the production host. For a single-user personal system that is
a second stack to maintain, a second database to migrate, and a second set of
containers competing for the same host — to catch a class of bug that the local
stack, the eval gate, and the migration job in CI already cover. Local *is*
staging. What replaces it is making production rollback fast, which section 4
specifies and which is more useful anyway.

**Local development never fetches live sources.** `SCOUT_FIXTURES_ONLY=true` is
the default in `.env.example` and the collector refuses live egress when it is
set. Local development is therefore deterministic, offline-capable, and
incapable of accidentally hammering a real company's careers page during a
debugging session.

**`live` is the same Compose stack as `local`, with `SCOUT_FIXTURES_ONLY=false`
in `.env`.** There is no separate compose file for it — it is `local.yml` run
with real credentials and real egress, started when the user chooses to have
Scout check for jobs (typically once a day) and stopped or simply left until the
laptop sleeps. This is the entire "deploy" step under ADR-018: there is nothing
to push to, because the machine running it is the machine sitting in front of
the user.

```bash
make dev          # full stack, seeded, fixtures (environment: local)
make dev-db       # database only, for running one service natively
make migrate      # apply pending migrations locally
make test         # all tests, all languages
make lint         # golangci-lint + sqlfluff
make evals        # quality eval harness
make fixtures     # re-record adapter fixtures (requires network + approval)
make deploy       # push to production over Tailscale SSH (section 3) — only
                   # meaningful once a host exists per ADR-014; not used today
make backup-now   # force an immediate irreplaceable-data backup
make restore-drill # restore latest nightly into a throwaway local container
```

`make evals` and `make fixtures` exit non-zero with a pointer to the milestone
that builds them. A target that is documented but missing fails a runbook at step
one; a target that prints "not yet" and exits 0 is worse, because a script
calling it believes it worked.

---

## 2. Container topology

Everything runs in one Compose stack on one host.

```yaml
# infra/compose/production.yml (abridged)
services:
  caddy:        # reverse proxy on the tailnet interface ONLY — no public port
  web:          # Next.js
  api:          # Go
  collector:    # Go
  brain:        # Python
  notifier:     # Go
  postgres:     # 16 + pgvector
  redis:        # 7
  otel:         # OpenTelemetry Collector
  prometheus:
  loki:
  tempo:
  grafana:
  ollama:       # local LLM fallback (ADR-016)
```

The observability stack is unchanged from
[ADR-011](adr/ADR-011-observability-stack.md). It was sized against an 8 GB
budget and had to argue for its 512 MB; at 24 GB that argument evaporates, so
nothing was cut. The one component that moves is the *external* probe — see
section 7.

**Nothing publishes a host port.** There is no port 80, no port 443, and no ACME
HTTP-01 challenge, because `tailscale cert` issues the certificate for
`scout.<tailnet>.ts.net`. This is not hardening added on top — it is the absence
of a public surface, which is why
[13-security-privacy.md](13-security-privacy.md) is substantially shorter than it
used to be.

The chain, concretely:

```
phone / laptop
  → tailnet (WireGuard)
    → tailscaled ON THE HOST — `tailscale serve --bg --https=443
      http://172.28.0.10:8080`, terminating TLS with the real certificate
      → caddy, at a fixed address on the Docker bridge network
        → api
```

The host routes to a bridge-network container address directly, so the middle hop
needs no `ports:` entry. Caddy's address is pinned in
`infra/compose/production.yml` rather than DHCP-assigned, because a container IP
that changes on recreate would break the ingress silently on the next deploy.
Caddy does not terminate TLS: nothing but tailscaled can reach that listener, so
a certificate there would be encrypting a hop that never leaves the host.

That property is load-bearing for authentication too: because the ingress cannot
be bypassed, the Tailscale identity headers Caddy forwards are trustworthy
([ADR-015](adr/ADR-015-single-user-auth.md)). The deploy health gate asserts that
no container publishes a host port, so a stray `ports:` entry fails the deploy
rather than silently opening the system.

Every service declares an explicit `mem_limit`, a `healthcheck`,
`restart: unless-stopped`, `read_only: true` with a `tmpfs` for `/tmp`,
`cap_drop: [ALL]`, `no-new-privileges`, and a non-root user.

**Two services need capabilities added back, and both are the same class of
problem — a capability the image requires to *start*, not one the service uses.**

- **postgres** — the official entrypoint starts as root, fixes ownership on the
  data directory, and drops to the `postgres` user. That needs `CHOWN`,
  `DAC_OVERRIDE`, `FOWNER`, `SETGID`, `SETUID`. Everything else stays dropped.
- **caddy** — the image runs `setcap cap_net_bind_service=+ep` on the binary so
  it can bind 80 and 443. A file carrying capabilities in its xattrs cannot be
  `exec`'d at all when they are outside the container's bounding set: the kernel
  returns `EPERM` before Caddy's first line runs, and the container
  restart-loops with `operation not permitted`. Scout's Caddy listens on 8080 and
  binds nothing privileged, so `NET_BIND_SERVICE` is there for the exec, not for
  the listener.

**The Go service images are `distroless/static`**, which has no shell and no
`curl` — so the usual `CMD-SHELL curl -f localhost/health` healthcheck is not
available. Each binary takes a `healthcheck` subcommand instead: the API probes
its own `/health` over loopback, which exercises the same path a real request
takes; the collector checks that its heartbeat loop is still ticking. The absence
of a shell is worth the small amount of extra code — it is a container an
attacker with code execution cannot pivot from with the usual toolkit.

### Resource allocation (24 GB, ARM64)

| Service | Memory | Note |
| --- | --- | --- |
| postgres | 4 GB | `shared_buffers=1GB`; generous because there is room |
| brain | 3 GB | Embedding model resident |
| ollama | 8 GB | Only tier that wants real memory; idle most of the time |
| collector | 1 GB | Thousands of idle sockets are cheap in Go |
| api, notifier, web | 512 MB each | |
| otel, prometheus, loki, tempo, grafana | 1.5 GB total | Unchanged from [ADR-011](adr/ADR-011-observability-stack.md) |
| redis | 320 MB | Holds nothing durable (AGENTS.md rule 8) |
| **Total** | **~19.5 GB** | ~4.5 GB headroom |

ADR-006 budgeted 8 GB and had to argue about it. 24 GB removes that argument
entirely, which is the one place where the free tier is *more* generous than the
paid design it replaced.

**ARM64 only.** Images build `linux/arm64` exclusively — the Oracle host and the
MacBook are both ARM64, so local and production are the same architecture. See
[ADR-014](adr/ADR-014-zero-cost-hosting.md).

### Health checks

| Service | Check | Interval | Meaning of failure |
| --- | --- | --- | --- |
| api | `GET /health` | 10s | Cannot serve |
| web | `GET /api/health` | 10s | Cannot render |
| collector | Heartbeat row age < 60s | 30s | Not polling |
| collector *(today)* | Liveness file age < 7m | 30s | Not ticking |
| brain | Queue consumer heartbeat < 120s | 30s | Not processing |
| notifier | Heartbeat < 60s | 30s | Not delivering |
| postgres | `pg_isready` | 10s | Total outage |
| redis | `redis-cli ping` | 10s | Degraded |
| ollama | `GET /api/tags` | 60s | Tier 2 fallback unavailable |

`/health` is a shallow liveness check — the process is up and can serve a
request. `/health/deep` verifies Postgres, Redis, and LLM provider reachability
and is used by monitoring rather than by the container runtime, because a deep
check failing should alert, not restart the container.

The collector's row is listed twice on purpose. The heartbeat-row check is the
real one and it needs a database client, so it arrives with polling; until then
the collector touches a file on its tmpfs each cycle and the healthcheck reads
its age. The difference matters and the weaker version should not be mistaken for
the stronger one: the file proves the loop is *ticking*, the row proves it is
*polling*, and a collector that ticks while fetching nothing is precisely the
silent failure the dead-man's switch exists to catch.

---

## 3. CI/CD

### CI (GitHub Actions, `.github/workflows/ci.yml`)

**The repository is public**, which makes Actions minutes unlimited and is why
this is a ₹0 line. It also means the user's profile, resume, notes, and
application history must never enter the repository — see
[13-security-privacy.md](13-security-privacy.md) section 3.

```
Pull request / push to main
├─ compliance    banned-dependency scan (whatsapp-web.js, ...)      ~5s
├─ go            build · vet · test                                ~40s
├─ golangci      golangci-lint                                     ~50s
├─ migrations    apply forward from empty against pgvector:pg16    ~40s
│                + assert re-apply fails
│                + assert the DEFAULT partition is empty
├─ sql-lint      sqlfluff over infra/migrations                    ~20s
└─ shell         shellcheck over infra/scripts                     ~20s
                 + both compose files parse
                 + assert production publishes no host ports
```

**The shell job is not a style check.** Two of those scripts are controls rather
than conveniences: `health-gate.sh` is what fails a deploy when a container
acquires a published host port, and `backup.sh` is the job whose silent failure
[ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) names as the real
risk in the backup design. Both are written on macOS and run on Linux, where the
shells and the coreutils differ — and the failure mode of a portability bug in a
gate is that the gate passes. One was found this way: `grep -E '->'`, which BSD
grep parses as a flag and rejects, inside a pipeline whose `|| true` turned the
error into "no ports found".

The no-host-ports assertion is deliberately in two places. CI checks the compose
file, so a stray `ports:` entry fails the pull request that adds it; the health
gate checks the running containers, so one added by hand on the host fails the
next deploy. The file and the reality are different claims.

Jobs are added by the milestone that introduces the thing they test — Python and
TypeScript jobs at P2 and P3, the eval gate at P2, Playwright at P3. **A workflow
full of skipped jobs teaches you to ignore a red X**, so nothing is stubbed in
advance.

The compliance check is a dependency-manifest scan, not a lint rule. It fails the
build if `whatsapp-web.js`, `baileys`, `venom-bot`, or equivalents appear
anywhere in `package.json`, `go.mod`, or `pyproject.toml`. WhatsApp is not a
channel ([ADR-013](adr/ADR-013-whatsapp-channel.md)), so nothing should pull
these in today — the check exists because a prohibition that lives only in a
document gets violated by whoever is in a hurry six months from now. Five seconds
of CI is cheap insurance. See [14-legal-compliance.md](14-legal-compliance.md).

### Deploy is human-initiated, from the MacBook

```bash
make deploy      # runs over Tailscale SSH
```

`infra/scripts/deploy.sh`, abridged:

```bash
set -euo pipefail
ssh scout-host '
  cd /opt/scout &&
  git pull --ff-only &&
  docker compose build ${CHANGED_SERVICES} &&
  docker compose --profile tools run --rm migrate up &&
  docker compose up -d --no-deps ${CHANGED_SERVICES} &&
  ./infra/scripts/health-gate.sh --timeout 90
'
```

Before any of that it refuses to run against a dirty working tree or a `HEAD`
that is not `origin/main`. The host deploys from a `git pull`, so uncommitted or
unpushed work is not merely undeployed — the build that runs is not the build
that was tested, which is a worse failure than a refusal.

**CI validates; a human deploys.** This is deliberate and it is not laziness.
An automated deploy from GitHub Actions would require a Tailscale auth key stored
as a GitHub secret, which puts a credential for the user's entire private network
into a third-party system — to save typing eight characters on a project that
deploys a few times a week. Building on the host from a `git pull` also removes
the need for a container registry entirely.

If deploy frequency ever justifies automating it, the upgrade is the official
Tailscale GitHub Action with an ephemeral, ACL-scoped, tagged auth key. Not
before.

**Per-service deploys matter.** A web change should not restart the collector
mid-crawl or the brain mid-batch. `CHANGED_SERVICES` is computed from the diff.

**Automatic rollback** on health-gate failure: previous image IDs are pinned in
`.env.previous`, and the gate script re-applies them and re-runs the health check
before alerting to Telegram.

### The mobile build

Runs on an explicit `mobile-v*` tag, a few times a year
([ADR-012](adr/ADR-012-native-app-shell.md)). The shell wraps the deployed web
app, so a UI change reaches the phone without a new binary. Building an APK on
every merge would add minutes to every deploy to produce an artifact identical to
the last one. Output is a GitHub release artifact, installed by sideload.

### Migrations

Forward-only, transactional, applied before the new code starts.

| Rule | Reason |
| --- | --- |
| Never rewrite a shipped migration | It has already run somewhere |
| Additive changes only in a single deploy | Old and new code run concurrently during rollout |
| Destructive changes over two releases | Stop writing, deploy, then drop |
| `CREATE INDEX CONCURRENTLY` above 100k rows | Avoids a write lock |
| Apply from empty in CI on every PR | Catches an invalid migration before it reaches the only database that exists |
| Backfills are queue jobs, not migrations | Keeps deploys fast and backfills resumable |

**The two-phase rule matters more now than it did.** Under
[ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md) there is no
point-in-time recovery, so "restore to just before the bad migration" is no
longer available. The granularity is the last hourly or nightly dump. A
destructive migration split across two releases is now the *primary* protection
rather than a convenience on top of PITR.

Migration files are named `NNNNNN_name.up.sql` and applied with
**`golang-migrate`**. There are no `.down.sql` files, because migrations are
forward-only by policy; `migrate down` is therefore not a supported operation and
will fail. Rolling back schema means rolling forward with a compensating
migration, or restoring a dump.

It runs from the pinned `migrate/migrate` image — a Compose service under the
`tools` profile, so it never starts with `up` — rather than as a Go dependency.
Nothing in the repository imports it, so vendoring it would add a dependency tree
to three service binaries for the sake of a command that runs at deploy time.
Same binary and same version locally and in production, which is what stops a
migration passing on a laptop and failing on the only database that matters.
`make migrate` and `deploy.sh` both go through it.

CI still applies migrations with `psql` instead. That is not an inconsistency:
CI is checking that the *SQL* is valid and applies in order from empty, and
going through the migration runner there would test the runner rather than the
schema.

---

## 4. Rollback

| Scenario | Procedure | Time |
| --- | --- | --- |
| Bad application deploy | Re-pin previous image IDs, `up -d` | ~90s |
| Bad migration (additive) | Roll back code; the column is harmless | ~90s |
| Bad migration (destructive) | Restore last dump, replay since | ~30 min |
| Bad weight version | Set previous version `active = true`, rescore | ~5 min |
| Bad prompt version | Revert the prompt file, redeploy the brain | ~90s |
| Bad adapter | Quarantine affected sources, revert, replay | ~10 min |
| Data corruption | Restore last good dump | ~30 min |

**Weight and prompt rollbacks are the common ones** and both are deliberately
cheap — a database update and a file revert respectively, neither requiring a
full deploy. Quality regressions are more frequent than crashes in a system like
this, so their rollback path gets the most attention.

Every rollback is logged in `docs/postmortems/` with the trigger and outcome.

---

## 5. Backup and recovery

Full rationale in
[ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md). The principle:
**back up by recoverability class, not uniformly**, because ~95% of the database
can be re-derived by re-polling the internet and a few kilobytes of it cannot be
re-derived at all.

| Class | What | Frequency | Destination | RPO |
| --- | --- | --- | --- | --- |
| **Irreplaceable** | `application`, `application_event`, `interview`, `note`, `user_profile`, `feedback_label`, `notification` | Hourly + on every state transition | MacBook (Tailscale) **and** Google Drive (`rclone`) | ≤1h |
| **Bulk** | Whole database, `pg_dump -Fc` | Nightly 03:00 IST | Same two | ≤24h |
| **Config** | Compose, migrations, taxonomies, dashboards, SOPS secrets | On change | Git | 0 |
| **Keystore** | Android signing key | On change | Offline media + Drive, `age`-encrypted | 0 |
| **Snapshots** | Raw HTML | **Not backed up** | Local disk, 30-day expiry | n/a |

Everything is `age`-encrypted on the host before it leaves, with the private key
held offline. Google never sees the application history.

**Objectives: RPO ≤1 hour on irreplaceable data. RTO ≤1 hour.**

**Monthly restore drill, first Sunday.** `make restore-drill` restores the latest
nightly into a throwaway local container on the MacBook, verifies row counts and
a checksum of the `job` table, runs the smoke tests, and destroys it. Roughly ten
minutes, no provider involved.

A backup that has never been restored is a hypothesis. The drill is what converts
it into a backup — and the silent failure of the backup job is the real risk in
this design, which is why it pings the same dead-man's switch as the collector
(section 7) and a missed backup alerts to Telegram.

---

## 6. Host migration

The distinguishing operational procedure of a ₹0 design. A free tier can be
withdrawn, capacity-denied, or reclaimed with no recourse, so the mitigation is
that moving is cheap and rehearsed rather than that it never happens.

```
1. Provision the new host (Oracle other region / MacBook / anything with Docker).
2. Install docker + tailscale; join the tailnet; approve the node.
3. git clone the repository to /opt/scout.
4. Restore the latest nightly dump.
5. docker compose up -d.
6. Re-point the Tailscale MagicDNS name at the new node.
7. Verify: health gate, one forced poll, one test notification.
```

**Target: under 60 minutes. Rehearsed quarterly**, recorded in
[`runbooks/host-migration.md`](runbooks/host-migration.md).

This is why nothing in the stack is host-specific: no Terraform, no Ansible, no
Oracle-only cloud-init. Anything that makes the host special makes the escape
slower, and the escape is the guarantee.

**Fallback mode (MacBook as production)** is a supported configuration, not an
improvisation. The differences: `caffeinate -s` plus "never sleep on power", and
an accepted coverage gap whenever the lid closes. That gap lands in the
00:00–08:00 IST window that matters most, which is why it is a fallback and not
the plan.

---

## 7. Monitoring the host from outside it

**Not applicable under `live` ([ADR-018](adr/ADR-018-laptop-only-hosting.md)).**
A dead-man's switch exists to notice a host that was supposed to be always-on and
silently isn't. `live` was never supposed to be always-on — it runs when the
user chooses to run it — so there is no "unexpectedly down" state to detect.
This section describes the ADR-014 design, relevant again once a host exists.

**Self-hosted monitoring cannot detect its own host being down.** At ₹0 there is
no second host to watch the first, and this is the one genuine monitoring problem
the constraint creates.

The answer is an external dead-man's switch:

- The collector pings a **healthchecks.io** check (free, 20 checks) every 5
  minutes. If the pings stop — reclaimed instance, full disk, dead network — the
  service alerts Telegram from outside the failure domain.
- The backup job pings a second check hourly.
- A scheduled **GitHub Actions** workflow performs the same probe as a backup for
  the backup, since healthchecks.io is itself a free tier that can vanish.

Between them, "Scout silently stopped and nobody noticed" — which
[20-risks.md](20-risks.md) rates as one of the top failure modes — is covered by
two independent free services and neither runs on the host being watched.

---

## 8. Network and access

There is no domain, no DNS zone, no MX record, and no public IP in the design.

```
scout.<tailnet>.ts.net     MagicDNS → host on the tailnet
                           TLS via `tailscale cert` (real Let's Encrypt certs)
```

Devices on the tailnet: the Oracle host, the MacBook, the Android phone. Three of
a free allowance of 100.

**Inbound paths that used to need a public endpoint, and what replaced them:**

| Was | Now |
| --- | --- |
| Cloudflare Email Routing → HTTPS webhook | **IMAP poll** of a dedicated Gmail account every 2 minutes |
| Telegram webhook | **Telegram long-poll** (`getUpdates`) |

Both are simpler: no HMAC verification, no secret-token comparison, no public
route to get wrong. Tailscale Funnel remains available and free if a genuine
public endpoint is ever needed — but see
[ADR-015](adr/ADR-015-single-user-auth.md), which requires implementing real
authentication *before* Funnel is enabled, not after.

---

## 9. Configuration

**Precedence:** defaults in code → `config.yaml` per environment → environment
variables → SOPS-decrypted secrets.

```yaml
# infra/config/production.yaml
collector:
  max_concurrent_fetches: 200
  default_rps_per_host: 0.5
  robots_cache_ttl: 24h
  rendered_html_source_cap: 20        # hard cap, enforced

brain:
  embedding_model: bge-small-en-v1.5  # 384 dimensions
  embedding_batch_size: 64
  dedup:
    simhash_merge_distance: 3
    semantic_merge_threshold: 0.94
    semantic_adjudicate_floor: 0.88

llm:
  # Budgets are REQUESTS, not currency — free tiers rate-limit, they do not bill.
  # See ADR-016. Provider names live here rather than in the ADR because
  # free-tier terms change faster than documents do.
  providers:
    - { name: <primary>,   rpm: 15, rpd: 1000, tpm: 250000 }
    - { name: <secondary>, rpm: 30, rpd: 14000 }
  local:
    model: <8b-instruct>              # Ollama; always available, never limited
  on_exhaustion: degrade              # only after all providers AND local

notifier:
  max_per_hour: 8
  max_per_day: 25
  quiet_hours: { start: "00:00", end: "07:30", tz: "Asia/Kolkata" }
```

**Every tunable that affects behavior is configuration, not a constant.** Dedup
thresholds, notification budgets, ranking weights, poll intervals. This is what
makes tuning a config change and a restart rather than a code change and a
deploy — which matters because these values need adjustment based on production
behavior, repeatedly.

Config changes are still version-controlled and reviewed. "Configuration" means
"changeable without recompiling", not "changeable without review".

---

## 10. Disaster scenarios

| Scenario | Detection | Response | RTO |
| --- | --- | --- | --- |
| Host unresponsive | Dead-man's switch (external) | Reboot via Oracle console; if unrecoverable, migrate (section 6) | 15 min / 1h |
| **Instance reclaimed by provider** | Dead-man's switch | Migrate to another region or the MacBook | **1h** |
| **Free tier withdrawn entirely** | Provider email, or the above | Migrate to the MacBook, then decide | 1h |
| Disk full | Alert at 70% | Prune snapshots, drop old partitions | 15 min |
| Postgres corruption | Health check + checksum | Restore last nightly | 30 min |
| Tailscale outage | Dashboard unreachable | Notifications still deliver — FCM and Telegram are outbound and unaffected | n/a |
| Google Drive lost | Backup job failure alert | MacBook copy is independent | n/a |
| Total loss of everything | — | Rebuild from Git + the MacBook dump. This is what the quarterly drill rehearses. | 1h |

**Two rows are new and they are the ₹0 rows.** Provider reclamation and free-tier
withdrawal do not exist on a paid host; they are the price of the budget, and
they are bounded to one rehearsed hour rather than left open-ended.

**The Tailscale row is worth noting**: losing network access costs the dashboard
and nothing else. Discovery, ranking, and notification are entirely outbound
paths, so the product's core value survives an outage of the only network
component in the design.

**Everything is in Git.** Compose files, config, migrations, dashboards, and
encrypted secrets. Recovery from total loss is clone, provision, restore,
`compose up`. Nothing about this infrastructure exists only in someone's memory
or only in a provider's console — which is the property that makes a free tier
survivable.
