-- Bounded retention — docs/03-data-model.md section 11, ADR-019.
-- apps/collector/internal/scheduler/retention.go owns the orchestration.
--
-- Hard invariants, enforced in SQL rather than by caller discipline:
--   * No job_group, job, user_job_state, notification or note row is ever
--     deleted here. Jobs are COMPACTED (bulky nullable columns cleared); the
--     row, its canonical_url_hash, simhash, group membership and merge
--     history stay, so a re-observed posting still lands on the same job and
--     the same job_group, and notification dedup (rule 2) is untouched.
--   * A job whose group has ANY user_job_state / user_job_state_event /
--     notification / google_calendar_event row is protected: saved, applied,
--     noted, rated, notified. Protection is by group, so every member of a
--     protected group is kept whole.
--   * Compaction never touches the notifier's inputs (status, priority,
--     notified_at, is_software, paid, ...), so it cannot create or suppress a
--     notification (rule 3).
--   * Only dedup-input columns are cleared, and clearing them can only make
--     Stage 2/3 find fewer matches, i.e. under-merge (rule 4).

-- name: CompactColdJobs :execrows
-- A job is cold when nothing will read its bulky columns again:
--   * closed/expired/filled for longer than closed_grace_secs;
--   * not a software role (never shown in the feed — feed.sql hard-filters
--     is_software) and older than irrelevant_grace_secs, which leaves recent
--     descriptions available for dedup and boilerplate learning;
--   * still 'open' but first posted more than stale_open_age_secs ago —
--     beyond the 45-day dedup window and with a near-zero freshness
--     multiplier;
--   * not seen on its source for unseen_age_secs.
-- Batched by the caller (batch_size) so one statement never holds a long
-- transaction against the hosted database; order by first_seen_at makes
-- successive batches deterministic.
update job
set description_html = NULL,
    description_text = NULL,
    description_stripped = NULL,
    requirements_text = NULL,
    embedding = NULL,
    embedding_version = NULL,
    raw_payload = '{}'::jsonb
where id in (
    select j.id
    from job as j
    where (
        j.description_html is not NULL
        or j.description_text is not NULL
        or j.description_stripped is not NULL
        or j.requirements_text is not NULL
        or j.embedding is not NULL
        or j.raw_payload <> '{}'::jsonb
    )
    and (
        (j.status <> 'open'
            and coalesce(j.closed_at, j.last_seen_at)
            < now() - make_interval(secs => sqlc.arg(closed_grace_secs)::float8))
        or (j.is_software = FALSE
            and j.first_seen_at
            < now() - make_interval(secs => sqlc.arg(irrelevant_grace_secs)::float8))
        or (j.status = 'open'
            and coalesce(j.posted_at, j.first_seen_at)
            < now() - make_interval(secs => sqlc.arg(stale_open_age_secs)::float8))
        or j.last_seen_at
        < now() - make_interval(secs => sqlc.arg(unseen_age_secs)::float8)
    )
    and not exists (
        select 1 from user_job_state as s
where s.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from user_job_state_event as e
where e.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from notification as n
where n.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from google_calendar_event as g
where g.job_group_id = j.job_group_id
    )
    order by j.first_seen_at
    limit sqlc.arg(batch_size)::int
);

-- name: ClearOldJobRawPayload :execrows
-- job.raw_payload is write-only: the ingest path stores the adapter's output on
-- insert and nothing reads it back (it duplicates the description columns and
-- the raw_observation payload, ~4.5 KB per job). It is cleared from every
-- unprotected job after max_age_secs, hot ones included, so the hot set carries
-- only what the feed and detail page render.
update job
set raw_payload = '{}'::jsonb
where id in (
    select j.id
    from job as j
    where j.raw_payload <> '{}'::jsonb
        and j.first_seen_at < now() - make_interval(secs => sqlc.arg(max_age_secs)::float8)
        and not exists (
            select 1 from user_job_state as s
where s.job_group_id = j.job_group_id
        )
        and not exists (
            select 1 from user_job_state_event as e
where e.job_group_id = j.job_group_id
        )
        and not exists (
            select 1 from notification as n
where n.job_group_id = j.job_group_id
        )
        and not exists (
            select 1 from google_calendar_event as g
where g.job_group_id = j.job_group_id
        )
    order by j.first_seen_at
    limit sqlc.arg(batch_size)::int
);

-- name: TrimColdJobScores :execrows
-- job_score.score_inputs (~1.7 KB) and explanation are an audit/explainer
-- for rows a user can actually see. Scores of compacted-cold jobs are not
-- shown, and a rescore rebuilds them. The numeric score columns and priority
-- stay, so ordering and the (job, user, weight_version) key are untouched.
-- Same cold + unprotected definition as CompactColdJobs; keep them in sync.
update job_score
set score_inputs = '{}'::jsonb,
    explanation = NULL
where (job_id, user_id, weight_version) in (
    select
js.job_id,
js.user_id,
js.weight_version
    from job_score as js
    inner join job as j on js.job_id = j.id
    where (js.score_inputs <> '{}'::jsonb or js.explanation is not NULL)
    and (
        (j.status <> 'open'
            and coalesce(j.closed_at, j.last_seen_at)
            < now() - make_interval(secs => sqlc.arg(closed_grace_secs)::float8))
        or (j.is_software = FALSE
            and j.first_seen_at
            < now() - make_interval(secs => sqlc.arg(irrelevant_grace_secs)::float8))
        or (j.status = 'open'
            and coalesce(j.posted_at, j.first_seen_at)
            < now() - make_interval(secs => sqlc.arg(stale_open_age_secs)::float8))
        or j.last_seen_at
        < now() - make_interval(secs => sqlc.arg(unseen_age_secs)::float8)
    )
    and not exists (
        select 1 from user_job_state as s
where s.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from user_job_state_event as e
where e.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from notification as n
where n.job_group_id = j.job_group_id
    )
    and not exists (
        select 1 from google_calendar_event as g
where g.job_group_id = j.job_group_id
    )
    order by j.first_seen_at
    limit sqlc.arg(batch_size)::int
);

-- name: ScrubOldObservationPayloads :execrows
-- raw_observation is an audit log; its payload is only needed to reprocess
-- recent data. The ROW is kept: (source_id, content_hash) is the idempotency
-- key (migration 000005), so removing rows would let unchanged content
-- re-insert on the next poll and re-run the whole pipeline for it. Clearing
-- the payload keeps that guarantee and the staleness signal (observed_at,
-- canonical_url_hash, job_id). Rows not yet processed or errored are kept
-- whole: the observation is still the only copy of its data.
update raw_observation
set payload = '{}'::jsonb
where (id, observed_at) in (
    select
o.id,
o.observed_at
    from raw_observation as o
    where o.payload <> '{}'::jsonb
        and o.observed_at < now() - make_interval(secs => sqlc.arg(max_age_secs)::float8)
        and (o.processed_at is not NULL or o.job_id is not NULL or o.process_error is not NULL)
    order by o.observed_at
    limit sqlc.arg(batch_size)::int
);

-- name: DeleteFinalizedQueueJobs :execrows
-- Completed/cancelled/discarded River rows are history; River's own cleaner
-- would do this, but the insert-only client in -once mode never runs one.
delete from river_job
where id in (
    select q.id
    from river_job as q
    where q.state in ('completed', 'cancelled', 'discarded')
        and q.finalized_at < now() - make_interval(secs => sqlc.arg(max_age_secs)::float8)
    order by q.finalized_at
    limit sqlc.arg(batch_size)::int
);

-- name: DeleteStaleQueueJobs :execrows
-- Work that has waited longer than max_age_secs without being claimed is moot:
-- embeddings and Stage 3 adjudication are best-effort enrichment with a
-- degraded mode (rule 6), and the job they target has aged out of the
-- freshness window. Deleting never loses user data.
delete from river_job
where id in (
    select q.id
    from river_job as q
    where q.state in ('available', 'retryable', 'scheduled')
        and q.created_at < now() - make_interval(secs => sqlc.arg(max_age_secs)::float8)
    order by q.created_at
    limit sqlc.arg(batch_size)::int
);

-- name: SelectDatabaseSizeBytes :one
select pg_database_size(current_database())::bigint as size_bytes;
