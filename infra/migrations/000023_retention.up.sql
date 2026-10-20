-- Bounded data retention (docs/03-data-model.md section 11, ADR-019).
--
-- The hosted database lives on Neon's free tier (~0.5 GB). One live day of
-- collection measured ~1.1 GB locally, of which ~720 MB was job's TOAST
-- (description_html / description_text / description_stripped / raw_payload)
-- and ~160 MB was raw_observation payloads. Retention is applied by
-- apps/collector/internal/scheduler/retention.go; this migration only adds
-- what that code needs to run cheaply. It changes no data and no columns —
-- compaction nulls existing nullable columns, it never drops identity.

-- Compaction finds rows that still carry bulky columns. A partial index that
-- covers only those rows is self-shrinking: a compacted job drops out of it,
-- so the index stays proportional to the *hot* set rather than the table.
CREATE INDEX job_compaction_idx ON job (first_seen_at)
WHERE description_html IS NOT NULL
OR description_text IS NOT NULL
OR description_stripped IS NOT NULL
OR requirements_text IS NOT NULL
OR embedding IS NOT NULL
OR raw_payload <> '{}'::jsonb;

-- Payload scrubbing finds observations whose payload is still full-size. Same
-- self-shrinking idea. Created on the parent, so every existing and future
-- monthly partition gets one automatically. This is a plain (non-unique)
-- index, so the partition-key restriction in migration 000005 does not apply.
CREATE INDEX raw_obs_unscrubbed_idx ON raw_observation (observed_at)
WHERE payload <> '{}'::jsonb;

-- Stale queue rows are found by age within a state.
CREATE INDEX river_job_stale_idx ON river_job (state, created_at)
WHERE state IN ('available', 'retryable', 'scheduled');

-- Retention turns these tables into steady update/delete churn (docs/03
-- section 11 prescribes the same tuning for the queue). Vacuum sooner so freed
-- space — including TOAST — is reusable before the next cycle writes more.
ALTER TABLE job SET (
    autovacuum_vacuum_scale_factor = 0.02,
    toast.autovacuum_vacuum_scale_factor = 0.02
);
ALTER TABLE job_score SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE river_job SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.01
);
