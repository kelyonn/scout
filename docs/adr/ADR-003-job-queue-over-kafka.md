# ADR-003: Postgres-backed job queue instead of Kafka

**Status:** Accepted
**Date:** 2026-08-06
**Note:** This decision departs from the original brief, which specified Kafka.

## Context

The brief listed Kafka and RabbitMQ under "Queues". This ADR explains why neither
is appropriate at Scout's scale, and what would change that.

Scout's queueing needs:

- Fan work from the collector to brain stages (normalize → classify → dedup →
  embed → score → notify).
- Retry with backoff on failure.
- Enqueue **transactionally with the data write**, so a committed observation
  always has a pending job and an aborted one never does.
- Survive process restart without losing work.
- Support replay for backfills after pipeline changes.

Let us be precise about volume, because this is where the decision is actually
made.

| Metric | MVP | Year 1 | Multi-tenant |
| --- | --- | --- | --- |
| Observations/day | 20,000 | 150,000 | 1,000,000 |
| Queue messages/day (6 stages) | 120,000 | 900,000 | 6,000,000 |
| **Sustained messages/second** | **1.4** | **10.4** | **69** |
| Peak (10x burst) | 14 | 104 | 690 |

Peak load at multi-tenant scale is about 690 messages per second. Kafka's design
target is millions per second. We would be using roughly 0.05% of its capability.

## Options considered

### Option A — Apache Kafka

**For:** durable ordered log, replay by offset, multiple independent consumer
groups, effectively unlimited throughput, the industry standard for event
streaming.

**Against, specifically:**

*Operational weight.* Kafka in production means brokers plus either ZooKeeper or
KRaft controllers, plus partition and replication-factor planning, plus retention
and compaction tuning, plus consumer group rebalancing behavior to understand
when it goes wrong. A minimally sane self-hosted cluster is 3 brokers. On our
single 8GB node, one broker alone wants 4–6GB of heap to behave well — more than
Postgres, Redis, and all four services combined.

*Cost.* Managed Kafka (Confluent, MSK, Redpanda Cloud) starts near $100/month for
the smallest useful tier. Our entire infrastructure budget is ₹0 ([ADR-014](ADR-014-zero-cost-hosting.md)); it was under ₹2,000
(~$24)/month. Kafka alone would be 4x the whole budget.

*The dual-write problem.* This is the technical objection, not the cost one. With
Kafka, writing an observation to Postgres and publishing its job to Kafka are two
separate systems and cannot be atomic. Either the write lands and the publish
fails (work silently lost) or the publish lands and the write rolls back (a
consumer processes a record that does not exist). The standard fix is the
transactional outbox pattern — write to an outbox table in the same transaction,
then relay to Kafka with a separate process. Which means you have built a
Postgres-backed queue anyway, *and then also run Kafka.*

*Unused capabilities.* Kafka's real value is ordered replayable logs consumed by
many independent groups. We have one consumer group per stage, no cross-partition
ordering requirement, and replay needs measured in thousands of records, which a
SQL query satisfies.

### Option B — RabbitMQ

**For:** far lighter than Kafka (~200MB), mature, good routing primitives,
excellent retry and dead-letter support.
**Against:** still a separate system with its own backup, clustering, and failure
semantics. Still has the dual-write problem. Its strengths — complex topic
routing, RPC patterns, priority queues — are ones we do not need. Buys us a
second stateful system in exchange for nothing we lack.

### Option C — Redis-based (Asynq, BullMQ)

**For:** Redis is already present. Very fast. Simple.
**Against:** durability depends on Redis persistence configuration, and Redis
persistence is a trade-off between fsync latency and data loss window rather than
a guarantee. Still not transactional with Postgres. And it violates the rule set
in [ADR-002](ADR-002-postgres-as-the-primary-store.md): if losing Redis loses
data, the data was in the wrong place. Queued work is data.

### Option D — River (Postgres-backed queue for Go)

**For:**
- **Transactional enqueue.** `INSERT observation` and `INSERT job` in one
  transaction. The dual-write problem does not exist. This is the decisive point.
- Zero new infrastructure — it is tables in the database we already run and back
  up.
- Benchmarked well past 10,000 jobs/second on modest hardware. Our multi-tenant
  peak is 690/s, about 1.4% of that.
- Built-in retries with exponential backoff, scheduled jobs, unique jobs,
  periodic jobs, and a web UI.
- Replay and inspection are SQL. Debugging a stuck queue means `SELECT`, not
  a specialized CLI.
- One backup covers application data and in-flight work together, so a PITR
  restore returns to a consistent state including the queue.

**Against:**
- Ceiling around 10k jobs/s. Real, but 14x our furthest projection.
- Adds write and vacuum load to Postgres. Requires aggressive autovacuum on queue
  tables — a known, documented tuning task, not a research problem.
- Go-only. The Python brain needs a client. Addressed below.

### Option E — NATS JetStream

**For:** lightweight (~50MB), fast, durable streams with replay, good clustering,
far simpler to operate than Kafka.
**Against:** still a separate system, still no transactional enqueue. Genuinely
the right answer at Stage 3 scale, but premature now.

## Decision

**Option D — River**, with **NATS JetStream as the documented Stage 3 successor**
and Kafka reserved for a Stage 4 that will probably never arrive.

**Cross-language access.** River's schema is documented and stable. The Python
brain uses a thin client (`packages/riverpy`) implementing the same table
contract — roughly 200 lines wrapping `SELECT ... FOR UPDATE SKIP LOCKED`. This
is deliberately a small, owned piece of code rather than a dependency, and it is
contract-tested against River's Go implementation in CI so the two cannot drift.

**Queue topology:**

| Queue | Concurrency | Max attempts | Priority |
| --- | --- | --- | --- |
| `fetch` | 50 | 5 | Normal |
| `normalize` | 10 | 3 | High |
| `classify` | 8 | 3 | High |
| `dedup` | 4 (advisory-locked per company) | 3 | High |
| `embed` | 4 | 3 | Normal |
| `score` | 8 | 3 | Normal |
| `notify` | 4 | 5 | **Critical** |
| `backfill` | 2 | 2 | Low |

`notify` runs at critical priority with a dedicated worker pool so that a
backfill flooding `score` can never delay a real-time notification. This is the
concrete reason priorities exist here.

## Consequences

**Positive.** Zero additional infrastructure. Transactional enqueue eliminates a
whole bug class. Queue state is inspectable with SQL. One backup covers
everything. Roughly $100/month saved versus managed Kafka, which at our scale is
the difference between a project that stays running and one that gets switched
off.

**Negative.** Throughput ceiling around 10k/s. Postgres carries extra write load —
budget roughly 15% of database capacity for the queue. Queue tables need tuned
autovacuum or dead tuples accumulate. The Python client is code we own and must
maintain.

**Neutral.** Queue metrics come from SQL queries rather than a broker's built-in
telemetry, which is arguably better for our purposes since it is one fewer
system to instrument.

## Reversal conditions

Move to NATS JetStream when **any** of these holds for a sustained hour:

- Sustained enqueue rate above 3,000/s (30% of River's practical ceiling).
- Queue-induced database write load above 30% of Postgres capacity.
- More than 3 nodes needing to consume the same queue.
- p99 job pickup latency above 5 seconds under normal load.

Move to Kafka only if **all** of these hold:

- Sustained above 50,000 messages/second.
- More than 5 independent consumer groups needing independent replay.
- A genuine requirement to replay more than 30 days of history.
- A dedicated operator, or the budget for a managed service.

## Migration path

All enqueue and consume calls go through `packages/queue`, an interface with
`Enqueue`, `Consume`, and `Schedule`. River is one implementation; a NATS
implementation is another. Migration is: implement the interface, dual-write for
one week, verify parity, cut over, remove River. Estimated 3–5 days.

The transactional-enqueue property is lost in that migration, so at that point we
also introduce the outbox pattern — which is the honest, deferred cost of ever
leaving Postgres.
