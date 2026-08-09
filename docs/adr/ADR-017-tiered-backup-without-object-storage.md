# ADR-017: Tiered backup by recoverability, without object storage

**Status:** Accepted
**Date:** 2026-08-08
**Amends:** [ADR-006](ADR-006-deployment-topology.md) / [15-infrastructure-deployment.md](../15-infrastructure-deployment.md) section 5

## Context

The specified backup design was `pgbackrest` with continuous WAL archiving to
Cloudflare R2, nightly fulls, six-hourly differentials, RPO ≤5 minutes, RTO ≤2
hours, and a monthly restore drill on a throwaway Hetzner instance.

Under [ADR-014](ADR-014-zero-cost-hosting.md) two of its assumptions are gone:
R2 costs money (~₹65/month, small but not ₹0), and there is no paid provider on
which to spin up a throwaway restore target.

The reflex is to find a free object store and keep the design. That would be a
mistake, because the design was over-built for this data in a way that was easy
to miss when it was only costing ₹65.

**Not all of Scout's data is equally recoverable, and the specified design
treated it as if it were.**

| Data | Volume | If lost |
| --- | --- | --- |
| `raw_observation`, `job`, `job_group`, embeddings | ~95% of the database | **Re-derivable.** The sources still have the postings. Re-polling and reprocessing restores it, at the cost of some hours of compute and a gap in historical change-tracking. |
| `company`, `source`, seed lists, taxonomies | Small | **Re-derivable from Git.** These are hand-maintained data files in the repository, loaded by migration. |
| Saved/applied state, interview notes, deadlines, feedback labels, profile | **Tiny** — kilobytes, tens of rows/day | **Unrecoverable.** Nothing in the world has a second copy of "I applied here on the 3rd and the recruiter said X." |

Continuous WAL archiving protects all three to a 5-minute RPO. The third
category is the only one that needs it, and it is the smallest by orders of
magnitude. Paying — in money, in complexity, and in a `pgbackrest` configuration
nobody enjoys debugging at 2am — to give a 5-minute RPO to data that can be
re-fetched from the internet is the wrong shape.

## Decision

**Back up by recoverability class, not uniformly. Destinations are free and
already owned.**

| Class | What | Frequency | Method | Destination | RPO |
| --- | --- | --- | --- | --- | --- |
| **Irreplaceable** | `application`, `application_event`, `interview`, `note`, `user_profile`, `feedback_label`, `notification` | **Hourly**, plus immediately after any state transition | `pg_dump` of those tables, zstd, `age`-encrypted | MacBook over Tailscale **and** Google Drive via `rclone` | ≤1 hour |
| **Bulk** | Whole database | Nightly 03:00 IST | `pg_dump -Fc`, zstd, `age`-encrypted | Same two destinations | ≤24 hours |
| **Config** | Compose, migrations, taxonomies, dashboards, SOPS-encrypted secrets | On change | Git | GitHub | 0 |
| **Keystore** | Android signing key | On change | `age`-encrypted, offline copy | Offline media + Drive | 0 |
| **Snapshots** | Raw HTML | Not backed up | — | Local disk, 30-day expiry | n/a |

The irreplaceable set is small enough that an hourly full dump of it is a few
hundred kilobytes and takes under a second. There is no incremental machinery to
build, no WAL to archive, no `pgbackrest` to configure, and no restore-ordering
puzzle — it is one `pg_dump --table` invocation on a cron.

**Raw HTML snapshots are deliberately not backed up.** They exist for adapter
debugging and expire in 30 days regardless. Backing up data with a 30-day life to
protect against a once-a-decade event is storage spent on nothing.

### Destinations

Both are free and already the user's:

- **The MacBook**, pulled over Tailscale. It is a genuinely independent failure
  domain from the host, in a different country from the Oracle region, and it is
  a machine the user physically controls.
- **Google Drive** (15 GB free on the existing account) via `rclone`. Off-site,
  survives losing both the host and the laptop.

Everything is `age`-encrypted before it leaves the host, with the private key
held offline. Neither Google nor anyone reading the Drive account sees the
application history — which [13-security-privacy.md](../13-security-privacy.md)
identifies as the most sensitive asset in the system.

### RPO and RTO, honestly

| | Specified before | Now |
| --- | --- | --- |
| RPO, irreplaceable data | 5 min | **≤1 hour** |
| RPO, bulk data | 5 min | ≤24 hours, **and re-derivable** |
| RTO | 2 hours | **≤1 hour** (restore is one `pg_restore` into a fresh compose stack) |

RTO improves because the restore path got dramatically simpler. RPO on the data
that matters degrades from 5 minutes to 1 hour, and the realistic worst case is
losing one status change made in the hour before a total host loss — recoverable
from memory in seconds, because the user made the change.

### The drill

Monthly, first Sunday, unchanged in spirit and cheaper in practice: restore the
latest nightly into a throwaway local Docker container on the MacBook, verify row
counts and a checksum of the `job` table, run the smoke tests, destroy it. No
instance to provision, no provider involved, roughly ten minutes.

The quarterly host-migration drill from
[ADR-014](ADR-014-zero-cost-hosting.md) is the heavier exercise and it subsumes a
full restore, so between them both paths are rehearsed.

## Consequences

**Good:**

- ₹0, using storage the user already has.
- The restore path is `pg_restore` and `docker compose up`. It can be executed
  correctly by a tired person at 2am, which was the stated design goal for
  everything in `runbooks/`, and which `pgbackrest` with WAL replay was never
  going to meet.
- Two independent off-host destinations instead of one.
- Encryption before egress is now mandatory rather than incidental.

**Bad, and accepted:**

- **No point-in-time recovery.** Restoring to "just before the bad migration"
  is no longer possible; the granularity is the last hourly or nightly dump.
  [15-infrastructure-deployment.md](../15-infrastructure-deployment.md) section 4
  is updated accordingly, and the two-phase destructive-migration rule matters
  more now that PITR is not the safety net behind it.
- A bulk-data loss costs hours of re-ingestion and a permanent gap in
  observation history for that window. Accepted: nothing in the product reads
  observation history older than the staleness window.
- `rclone` and `age` are two more moving parts, though both are single static
  binaries with no daemon.

## Reversal triggers

- Scout becomes multi-user, at which point other people's data is involved,
  "re-derivable" stops being a sufficient answer, and PITR becomes mandatory.
- The irreplaceable set grows past what an hourly full dump handles comfortably —
  practically, past a few tens of megabytes.
- A restore drill fails, or the hourly job is found to have been silently broken.
  Silent backup failure is the actual risk in this design, which is why the
  backup job pings the same dead-man's switch as the collector
  ([ADR-014](ADR-014-zero-cost-hosting.md)) and a missed backup alerts to
  Telegram.
