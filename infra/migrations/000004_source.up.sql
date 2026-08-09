-- Sources: what we poll. See docs/03-data-model.md section 3.2 and
-- docs/06-ingestion-pipeline.md.

CREATE TABLE source (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id            UUID REFERENCES company (id) ON DELETE SET NULL,
    kind                  source_kind NOT NULL,
    status                source_status NOT NULL DEFAULT 'pending_review',

    -- SPEC DEVIATION, deliberate. docs/03 had
    --   DEFAULT 'pending_review'::TEXT::legal_posture
    -- but 'pending_review' is not a legal_posture value (it belongs to
    -- source_status), so that default fails at migration time.
    --
    -- Defaulting to 'prohibited' is the correct fix rather than adding a
    -- 'pending_review' value: a source whose legality nobody has established
    -- yet must generate zero requests. AGENTS.md rule 1 and the "refuse over
    -- fetch" preference both point the same way. Promotion to 'permitted' is an
    -- explicit act with a recorded basis in `notes`.
    legal_posture         legal_posture NOT NULL DEFAULT 'prohibited',

    url                   TEXT NOT NULL,
    url_hash              BYTEA NOT NULL UNIQUE,   -- sha256 of canonical url
    adapter_config        JSONB NOT NULL DEFAULT '{}',

    -- Politeness (SCOUT-LEGAL-*)
    robots_allowed        BOOLEAN,
    robots_checked_at     TIMESTAMPTZ,
    robots_crawl_delay_s  REAL,
    max_rps               REAL NOT NULL DEFAULT 0.5,
    max_concurrency       SMALLINT NOT NULL DEFAULT 2,

    -- Scheduling (SCOUT-ING-*)
    base_interval_s       INTEGER NOT NULL DEFAULT 900,
    current_interval_s    INTEGER NOT NULL DEFAULT 900,
    min_interval_s        INTEGER NOT NULL DEFAULT 300,
    max_interval_s        INTEGER NOT NULL DEFAULT 86400,
    next_poll_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_polled_at        TIMESTAMPTZ,

    -- Services firms and campus platforms post in bursts tied to hiring season,
    -- so yield-based backoff would sleep through the opening day of a drive.
    -- 'cyclical' caps the effective max interval at 4h regardless of yield.
    hiring_pattern        TEXT NOT NULL DEFAULT 'continuous'
    CHECK (hiring_pattern IN ('continuous', 'cyclical')),

    -- Change detection
    last_etag             TEXT,
    last_modified         TEXT,
    last_content_hash     BYTEA,
    last_changed_at       TIMESTAMPTZ,

    -- Health and yield (drives the scheduler and the silent-failure alert)
    consecutive_failures  SMALLINT NOT NULL DEFAULT 0,
    circuit_open_until    TIMESTAMPTZ,
    total_polls           BIGINT NOT NULL DEFAULT 0,
    total_successes       BIGINT NOT NULL DEFAULT 0,
    total_jobs_found      BIGINT NOT NULL DEFAULT 0,
    total_new_jobs        BIGINT NOT NULL DEFAULT 0,
    yield_ratio           REAL NOT NULL DEFAULT 0,   -- new jobs / 100 polls
    yield_computed_at     TIMESTAMPTZ,

    priority_tier         SMALLINT NOT NULL DEFAULT 3
    CHECK (priority_tier BETWEEN 1 AND 5),
    notes                 TEXT,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The scheduler's hot query. The partial predicate keeps the index tiny and is
-- a second enforcement point for the compliance gate: a prohibited or
-- email_only source is not merely skipped, it is not even visible to the
-- "what is due" query.
CREATE INDEX source_due_idx ON source (next_poll_at)
WHERE status = 'active' AND legal_posture IN ('permitted', 'api_only');
CREATE INDEX source_company_idx ON source (company_id);
CREATE INDEX source_yield_idx ON source (yield_ratio DESC);
