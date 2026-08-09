-- Raw observations: append-only, monthly range partitions.
-- See docs/03-data-model.md section 3.3.
--
-- Observations are immutable and jobs are derived from them. A normalization bug
-- is therefore fixable by reprocessing rather than by data loss, and there is a
-- complete audit trail of how a posting changed over time.
--
-- Partitioning is not premature: at ~40k observations/day this reaches ~15M
-- rows/year. Monthly partitions keep index sizes manageable, make retention a
-- DROP TABLE rather than a mass DELETE (which would be a vacuum disaster), and
-- let the planner prune to recent partitions.

CREATE TABLE raw_observation (
    id                 UUID NOT NULL DEFAULT gen_random_uuid(),
    source_id          UUID NOT NULL REFERENCES source (id) ON DELETE CASCADE,
    observed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    external_id        TEXT,          -- ATS-native job id when available
    url                TEXT NOT NULL,
    canonical_url      TEXT NOT NULL,
    canonical_url_hash BYTEA NOT NULL,

    content_hash       BYTEA NOT NULL,   -- sha256 of normalized payload
    payload            JSONB NOT NULL,   -- adapter's structured output
    snapshot_key       TEXT,             -- local snapshot path; see docs/15 s.5
    http_status        SMALLINT,
    fetch_duration_ms  INTEGER,

    processed_at       TIMESTAMPTZ,
    process_error      TEXT,

    -- Deliberately NOT a foreign key to job(id). Observations are written by the
    -- collector before the brain has created the job row, so a FK would either
    -- force a null-then-update dance across two services or make the collector
    -- wait on normalization. The reference is resolved by the brain and verified
    -- by a nightly consistency check rather than by the database.
    --
    -- The tradeoff is real: a deleted job leaves dangling job_id values here.
    -- That is acceptable because observations are an audit log, and an audit log
    -- pointing at something since deleted is still true about the past.
    job_id             UUID,

    -- The partition key must be part of the *parent* primary key, which is why
    -- observed_at is in it rather than id alone.
    PRIMARY KEY (id, observed_at)
) PARTITION BY RANGE (observed_at);

-- Monthly partitions. A nightly job maintains a rolling window three months
-- ahead and drops partitions older than six months (docs/03 section 10).
--
-- The initial set is generated from a FIXED start date rather than now() so the
-- migration produces an identical schema whenever it runs — a migration whose
-- output depends on the wall clock is not reproducible, and CI would diverge
-- from a developer machine.
DO $$
DECLARE
    start_month DATE := DATE '2026-08-01';
    i           INTEGER;
    part_name   TEXT;
    lower_bound DATE;
    upper_bound DATE;
BEGIN
    FOR i IN 0..17 LOOP
        lower_bound := start_month + (i * INTERVAL '1 month');
        upper_bound := start_month + ((i + 1) * INTERVAL '1 month');
        part_name   := 'raw_observation_' || to_char(lower_bound, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE %I PARTITION OF raw_observation '
            'FOR VALUES FROM (%L) TO (%L)',
            part_name, lower_bound, upper_bound
        );

        -- IDEMPOTENCY. This is the real mechanism behind the "observation write
        -- is idempotent on (source_id, content_hash)" guarantee in
        -- docs/02-architecture.md section 4.3.
        --
        -- It cannot live on the parent table: Postgres requires the partition
        -- key in any unique constraint on a partitioned table, and
        -- (source_id, content_hash, observed_at) with observed_at DEFAULT now()
        -- guarantees nothing at all — re-fetching identical content a minute
        -- later inserts a second row. That index looks like a safeguard and is
        -- decorative, which is worse than having none.
        --
        -- A unique index created directly on a *partition* has no such
        -- restriction. The guarantee it buys is therefore scoped honestly:
        --   one row per (source, content) per month.
        -- Re-polling unchanged content within the month is an
        -- ON CONFLICT DO NOTHING no-op. Across a month boundary the same
        -- content inserts once more, which is correct rather than a leak: it
        -- records that the posting still existed in the new month, which is the
        -- signal the staleness detector in docs/06 reads.
        EXECUTE format(
            'CREATE UNIQUE INDEX %I ON %I (source_id, content_hash)',
            part_name || '_dedup_idx', part_name
        );
    END LOOP;
END
$$;

-- Safety net for partition exhaustion. Without it, the first insert past
-- 2028-01-31 fails outright and ingestion stops with a constraint error.
--
-- This partition must ALWAYS be empty. A non-empty default means the nightly
-- maintenance job has stopped extending the window, and it is alerted on
-- (docs/16-observability.md) and asserted in CI. Rows landing here are not lost,
-- but they do make future CREATE TABLE ... PARTITION OF for the covered range
-- fail until they are moved, so the alert is the point — the partition exists to
-- convert a hard outage into a warning, not to be used.
CREATE TABLE raw_observation_default PARTITION OF raw_observation DEFAULT;

CREATE UNIQUE INDEX raw_observation_default_dedup_idx
ON raw_observation_default (source_id, content_hash);

CREATE INDEX raw_obs_unprocessed_idx ON raw_observation (observed_at)
WHERE processed_at IS NULL;
CREATE INDEX raw_obs_canonical_idx ON raw_observation (canonical_url_hash);
