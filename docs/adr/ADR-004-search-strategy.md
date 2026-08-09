# ADR-004: Postgres full-text search and pgvector, not Elasticsearch

**Status:** Accepted
**Date:** 2026-08-06
**Note:** This decision departs from the original brief, which listed Elasticsearch.

## Context

Scout has three distinct search needs, and they are often conflated:

1. **Internal similarity search** — during deduplication, find jobs similar to a
   candidate job. Machine-driven, latency-sensitive (it is in the ingest hot
   path), must be exhaustive within a candidate set.
2. **User search** — the user types "backend intern bangalore" and expects
   ranked results with typo tolerance, facets, and instant feedback.
3. **Semantic search** — "jobs like this one" or natural language queries where
   keywords do not match but meaning does.

These have different requirements. Building one engine for all three is how you
end up with an engine that does none of them well.

Scale: 30k documents at MVP, 250k at Year 1. This is small. Elasticsearch is
designed for a world where "small" means tens of millions.

## Options considered

### Option A — Elasticsearch

**For:** the most capable text search engine available. Faceting, aggregations,
highlighting, fuzzy matching, custom analyzers, kNN vector search, and a mature
ecosystem.

**Against:**
- *Memory.* A single ES node wants 4GB heap minimum to be stable, and the JVM
  will use it whether you have 30k documents or 30M. That is half our 8GB VPS for
  a workload Postgres handles in a few hundred MB.
- *Operational cost.* Index mappings, analyzer configuration, shard planning,
  cluster health, JVM tuning, and version upgrades that occasionally require
  reindexing. Elastic Cloud starts around $95/month.
- *Dual-write.* Every job must be written to Postgres and indexed into ES. They
  drift. Reconciliation jobs become a permanent chore, and search results that
  disagree with the database are a confusing class of bug.
- *Overkill.* At 250k documents, Postgres GIN full-text search returns in
  10–30ms. Elasticsearch would return in 5ms. The user cannot perceive that
  difference; the network round-trip dominates both.

### Option B — Postgres full-text search only

**For:** zero new infrastructure. `tsvector` with GIN indexing, `ts_rank_cd` for
relevance, transactional consistency with the data — search results can never
disagree with the database because there is only one database.
**Against:** no typo tolerance (searching "bangalor" returns nothing), weak
faceting (counts require separate aggregate queries), no built-in
search-as-you-type, English-centric stemming.

### Option C — Meilisearch

**For:** purpose-built for exactly this use case. ~100MB RAM at our size. Typo
tolerance and faceting work out of the box with no configuration. Sub-50ms
search-as-you-type. Trivial to self-host — one binary, one volume.
**Against:** still a second system with its own sync and backup. Weaker at
complex aggregations than ES. Vector search exists but is less mature than
pgvector.

### Option D — Typesense

Very similar profile to Meilisearch. Slightly better vector support, slightly
smaller community. No decisive difference; Meilisearch chosen on ecosystem
maturity and simpler self-hosting.

### Option E — Layered: Postgres now, Meilisearch when the UX needs it

Recognize that the three needs have different answers and different timelines.

## Decision

**Option E.** A layered strategy where each need is served by the appropriate
mechanism, introduced when it is actually needed.

| Need | MVP (M0–M3) | From M4 | Rationale |
| --- | --- | --- | --- |
| Dedup similarity | pgvector HNSW | pgvector HNSW | Stays in Postgres — it is in a transaction with the write, and moving it would break that |
| User keyword search | Postgres FTS + `pg_trgm` | **Meilisearch** | Typo tolerance and faceting are UX features; add them when there is a UX to serve |
| Semantic search | pgvector cosine | pgvector cosine | Vectors live with the data |
| Filters and facets | SQL `WHERE` + `COUNT` | Meilisearch facets | SQL is fine for a handful of filters; facet counts get expensive past ~10 dimensions |

**Elasticsearch is not adopted at any stage.** If we ever outgrow Meilisearch, the
next step is a Meilisearch cluster or Typesense — not a JVM search cluster for a
job dataset that will never exceed a few million documents.

**MVP implementation:**

```sql
-- Weighted tsvector: title matters most, then company, then description
ALTER TABLE job ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(company_name, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(location_raw, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(description_text, '')), 'C')
  ) STORED;

CREATE INDEX job_search_idx ON job USING GIN (search_vector);
CREATE INDEX job_title_trgm_idx ON job USING GIN (title gin_trgm_ops);
CREATE INDEX job_embedding_idx ON job USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);
```

`pg_trgm` on title gives partial typo tolerance in the meantime: a trigram
similarity fallback runs when full-text search returns fewer than 5 results,
which covers most misspellings without a second system.

**Hybrid ranking** combines keyword and semantic results with Reciprocal Rank
Fusion, which needs no score normalization between the two very different scales:

```
RRF(d) = Σ over rankers r of  1 / (k + rank_r(d)),  k = 60
```

## Consequences

**Positive.** Zero additional infrastructure at MVP. Search results are always
consistent with the database. Roughly $95/month saved versus Elastic Cloud, and
4GB of RAM freed on the host. One less thing to back up. Meilisearch arrives when
the user-facing search experience genuinely needs typo tolerance, not before.

**Negative.** No typo tolerance at MVP beyond the trigram fallback. Facet counts
require extra queries and get slow past roughly 10 concurrent facets. Postgres
stemming is English-only, which is acceptable since job postings we care about
are overwhelmingly English. HNSW index rebuilds are memory-intensive and must be
scheduled off-peak.

**Neutral.** Search code sits behind a `SearchProvider` interface from day one, so
the Meilisearch swap touches one file.

## Reversal conditions

Adopt Meilisearch when any of:
- FTS p95 latency exceeds 200ms.
- The user reports missed results due to typos more than twice.
- More than 8 facet dimensions are needed simultaneously.
- Document count exceeds 500k.

Reconsider Elasticsearch only if document count exceeds 10M *and* we need
aggregation capabilities Meilisearch lacks. Neither is plausible for this product.

## Migration path

Meilisearch sync is a River job on job insert or update, plus a nightly full
reconciliation that compares document counts and checksums. The `SearchProvider`
interface means the API layer does not change. Estimated 2 days.
