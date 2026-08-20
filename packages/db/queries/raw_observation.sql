-- raw_observation queries. See docs/06-ingestion-pipeline.md section 9 and
-- infra/migrations/000005_raw_observation.up.sql.
--
-- docs/06 section 9's pseudocode enqueues a River job in the same
-- transaction as the observation insert; this pass has no queue (the P1
-- scope decision in the implementation plan — normalize/classify/dedup/score
-- run synchronously in Go instead), so job_id is set directly here, from
-- apps/collector/internal/dedup.Result, rather than left null for an async
-- consumer to fill in later.

-- name: InsertObservation :one
-- No ON CONFLICT here: the partition's own (source_id, content_hash)
-- unique index (docs/03 section 10) is created directly on each partition,
-- not as a partitioned index on the parent — deliberately, per that
-- migration's own comment, because a unique constraint at the parent level
-- would be forced to include the partition key (observed_at), which
-- guarantees nothing. The consequence: Postgres has no arbiter index it can
-- validate an ON CONFLICT clause against when the INSERT targets the
-- parent table, and adding one anyway fails every insert, not just
-- duplicates ("no unique or exclusion constraint matching the ON CONFLICT
-- specification") — caught in P1 live verification before it shipped.
-- A genuine duplicate (source_id, content_hash) — observed live against a
-- real Greenhouse board — is instead handled by the caller
-- (apps/collector/internal/scheduler.runIngestion) running each posting in
-- its own savepoint, so a real constraint-violation error here rolls back
-- only that one posting rather than poisoning the whole poll's
-- transaction.
insert into raw_observation (
    source_id, external_id, url, canonical_url, canonical_url_hash,
    content_hash, payload, http_status, fetch_duration_ms, process_error, job_id
) values (
    sqlc.arg(source_id)::uuid, sqlc.narg(external_id)::text,
    sqlc.arg(url)::text, sqlc.arg(canonical_url)::text, sqlc.arg(canonical_url_hash)::bytea,
    sqlc.arg(content_hash)::bytea, sqlc.arg(payload)::jsonb,
    sqlc.narg(http_status)::smallint, sqlc.narg(fetch_duration_ms)::int,
    sqlc.narg(process_error)::text, sqlc.narg(job_id)::uuid
)
returning id, observed_at;

-- name: InsertParseErrorObservation :exec
-- docs/06 section 10's "parse error: store raw, alert, do not retry" —
-- there is no adapter-parsed posting to key this on, so it carries no
-- external_id/canonical_url/content_hash the way a normal observation does;
-- url is the source's own url so the row is still traceable to what failed.
insert into raw_observation (
    source_id, url, canonical_url, canonical_url_hash,
    content_hash, payload, http_status, process_error
) values (
    sqlc.arg(source_id)::uuid, sqlc.arg(url)::text, sqlc.arg(url)::text,
    digest(sqlc.arg(url)::text, 'sha256'), digest(sqlc.arg(payload)::text, 'sha256'),
    sqlc.arg(payload)::jsonb, sqlc.narg(http_status)::smallint, sqlc.arg(process_error)::text
);
