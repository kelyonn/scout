# ADR-002: PostgreSQL as the single primary datastore

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout needs relational data (companies, jobs, users, applications), full-text
search, vector similarity search, time-series observations, a job queue, and
JSON blobs of varying shape from 12 kinds of adapter.

The default modern instinct is a purpose-built store per need: Postgres for
relations, Elasticsearch for text, Pinecone or Qdrant for vectors, ClickHouse for
time series, Kafka for the queue, Redis for cache. Five to six systems.

Each additional datastore costs, roughly: its own backup and restore procedure,
its own failure mode and runbook, its own upgrade path, its own client library
and connection pool, its own monitoring, its own memory reservation on the host,
and — the expensive one — consistency reasoning at every boundary between it and
the others.

We have one operator. Data volume at Year 1 is roughly 40GB and 150k
writes/day, which is small by any of these systems' standards.

## Options considered

### Option A — Best-of-breed per workload

Postgres + Elasticsearch + Qdrant + ClickHouse + Kafka + Redis.

**For:** each workload gets an optimal engine. Scales essentially without limit.
**Against:** six backup procedures, six upgrade paths, six failure modes. Minimum
viable RAM across these is roughly 12–16GB, versus 8GB for the whole current
design. Dual-write consistency problems everywhere: a job written to Postgres and
indexed to Elasticsearch can diverge, and reconciling that is ongoing work.
Roughly ₹8,000–15,000/month in hosting for a workload that fits in ₹700.

### Option B — Postgres for everything, no exceptions

**For:** one store, one backup, one connection pool, transactional consistency
across every concern including the queue.
**Against:** Postgres is a mediocre cache. Session lookups and rate-limit
counters at high frequency generate write amplification and vacuum pressure for
data that is, by definition, disposable.

### Option C — Postgres primary + Redis for ephemeral state

**For:** everything durable is transactional and in one place. Ephemeral,
high-churn, disposable state (rate limit counters, distributed locks, hot cache,
SSE fanout) lives in Redis where it belongs and where losing it on restart costs
nothing.
**Against:** two systems instead of one. Redis adds ~100MB and one more thing to
monitor.

## Decision

**Option C.** PostgreSQL 16 as the single source of truth, Redis strictly for
ephemeral state.

**Postgres carries:**

| Need | Mechanism | Adequate until |
| --- | --- | --- |
| Relational core | Standard tables, FKs | Indefinitely |
| Full-text search | `tsvector` + GIN | ~1M documents |
| Vector similarity | `pgvector` + HNSW | ~1M vectors |
| Fuzzy matching | `pg_trgm` | Indefinitely |
| Time-series observations | Declarative range partitioning by month | ~100M rows |
| Job queue | River (transactional, Postgres-native) | ~10k jobs/s |
| Semi-structured payloads | `JSONB` + GIN | Indefinitely |
| Audit trail | Append-only tables | Indefinitely |

Every one of these limits is at least an order of magnitude beyond our Year 1
projection.

**Redis carries only:** rate limit counters, distributed locks, hot cache
(company metadata, robots.txt), SSE connection registry, and short-lived
idempotency keys. **Rule: if losing Redis entirely loses data, it was in the
wrong place.** This is testable, and there is a chaos test that flushes Redis in
staging and asserts the system recovers with no data loss.

**Critically: the job queue is transactional with the data.** Writing an
observation and enqueuing its normalization happen in one transaction. With Kafka
or SQS this is the dual-write problem and needs an outbox pattern with a relay
process. With River it is `BEGIN; INSERT; INSERT; COMMIT;`. That single property
removes an entire category of bug — the "job enqueued but data not committed"
race — and is worth more than any throughput advantage an external broker offers
at our scale.

## Consequences

**Positive.** One backup covers everything, and one PITR restore returns the
entire system to a consistent point in time. Transactional guarantees across
what would otherwise be service boundaries. Joins between jobs, scores, and
vectors in a single query. ~₹700/month instead of ₹10,000. One system to learn
deeply rather than six shallowly.

**Negative.** Postgres becomes the single point of failure — accepted and
mitigated by backup depth rather than redundancy, per [02](../02-architecture.md).
Vacuum tuning matters more when the queue lives in the database; the queue tables
need aggressive autovacuum settings. pgvector HNSW index builds are memory-hungry
and must be scheduled off-peak. Full-text search lacks typo tolerance and
faceting, which is why Meilisearch arrives later — see
[ADR-004](ADR-004-search-strategy.md).

**Neutral.** We will become good at Postgres. Given how much of the industry runs
on it, this is a reasonable place to concentrate expertise.

## Reversal conditions

- Vector count above ~2M with p95 similarity search over 100ms → dedicated vector
  store (Qdrant).
- Observation table above ~200M rows with analytics queries over 5s → ClickHouse
  for analytics only, Postgres stays authoritative.
- Queue throughput sustained above 5k jobs/s → NATS JetStream.
- Any single Postgres instance above 500GB → Citus sharding or workload split.

## Migration path

Each concern exits independently and incrementally:
search → Meilisearch behind the existing search interface;
vectors → Qdrant behind the existing similarity interface;
queue → NATS behind the existing enqueue interface.
Every one of these is behind an interface *from day one* specifically so the
migration is a swap, not a rewrite.
