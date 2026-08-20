-- The canonical job record and the notification unit above it.
-- See docs/03-data-model.md sections 3.4 and 3.5.
--
-- job_group must exist before job (job.job_group_id references it), but
-- job_group.representative_job_id points at job(id) — the same circular
-- reference docs/03 calls out ("FK added after job exists"). Resolved here by
-- creating job_group with a bare UUID column, then job, then adding the
-- foreign key once both tables exist.

CREATE TABLE job_group (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id             UUID NOT NULL REFERENCES company (id),
    representative_job_id  UUID,       -- best record; fk added below
    member_count           INTEGER NOT NULL DEFAULT 1,
    first_seen_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    notified_at            TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_group_company_idx ON job_group (company_id);
-- Alert trigger: over-merging detection (docs/08).
CREATE INDEX job_group_large_idx ON job_group (member_count) WHERE member_count > 10;

CREATE TABLE job (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_group_id          UUID NOT NULL REFERENCES job_group (id),
    company_id            UUID NOT NULL REFERENCES company (id),
    primary_source_id     UUID NOT NULL REFERENCES source (id),

    -- Identity
    canonical_url         TEXT NOT NULL,
    canonical_url_hash    BYTEA NOT NULL,
    ats_platform          TEXT,
    ats_job_id            TEXT,
    content_hash          BYTEA NOT NULL,

    -- Content
    title                 TEXT NOT NULL,
    normalized_title      TEXT NOT NULL,
    description_html      TEXT,
    description_text      TEXT,
    description_stripped  TEXT,   -- boilerplate removed; dedup/embed input
    requirements_text     TEXT,
    apply_url             TEXT NOT NULL,

    -- Classification (SCOUT-NORM-*). Tier 0 only populates role_family,
    -- role_confidence, seniority, is_software, skills, tech_stack — Tier 1/2
    -- (embeddings, LLM) and the advocacy.* gate are P2 (docs/19 roadmap).
    role_family           role_family NOT NULL DEFAULT 'swe.other',
    role_confidence       REAL NOT NULL DEFAULT 0,
    seniority             seniority NOT NULL DEFAULT 'unknown',
    is_software           BOOLEAN NOT NULL DEFAULT FALSE,
    skills                TEXT [] NOT NULL DEFAULT '{}',
    tech_stack            TEXT [] NOT NULL DEFAULT '{}',

    -- Location (SCOUT-RANK-LOC)
    location_raw          TEXT,
    location_city         TEXT,
    location_region       TEXT,
    location_country      CHAR(2),
    location_tier         SMALLINT CHECK (location_tier BETWEEN 1 AND 4),
    work_mode             work_mode NOT NULL DEFAULT 'unknown',
    visa_sponsorship      BOOLEAN,

    -- Compensation (SCOUT-NORM-COMP)
    paid                  paid_signal NOT NULL DEFAULT 'unknown',
    comp_min              NUMERIC(12, 2),
    comp_max              NUMERIC(12, 2),
    comp_currency         CHAR(3),
    comp_period           TEXT CHECK (comp_period IN ('hour', 'month', 'year', 'stipend_total')),
    comp_normalized_inr_month  NUMERIC(12, 2),   -- single comparable number
    comp_confidence       REAL,
    prestige_exception    BOOLEAN NOT NULL DEFAULT FALSE,  -- unpaid but notable

    -- Lifecycle
    status                job_status NOT NULL DEFAULT 'open',
    posted_at             TIMESTAMPTZ,
    posted_at_estimated   BOOLEAN NOT NULL DEFAULT FALSE,
    deadline_at           TIMESTAMPTZ,
    first_seen_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at             TIMESTAMPTZ,

    -- Quality flags
    is_ghost              BOOLEAN NOT NULL DEFAULT FALSE,
    ghost_reason          TEXT,
    observation_count     INTEGER NOT NULL DEFAULT 1,

    -- Search and similarity. embedding/simhash are P2+ (Tier 1 classification,
    -- dedup Stage 2/3) and stay NULL until that code exists.
    embedding             VECTOR(384),
    embedding_version     TEXT,
    simhash               BIGINT,
    search_vector         TSVECTOR GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A')
        || setweight(to_tsvector('english', coalesce(location_raw, '')), 'B')
        || setweight(to_tsvector('english', coalesce(description_text, '')), 'C')
    ) STORED,

    raw_payload           JSONB NOT NULL DEFAULT '{}',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ
);

CREATE UNIQUE INDEX job_canonical_url_idx ON job (canonical_url_hash)
WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX job_ats_idx ON job (ats_platform, ats_job_id)
WHERE ats_platform IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX job_group_idx ON job (job_group_id);
CREATE INDEX job_company_idx ON job (company_id, posted_at DESC);
CREATE INDEX job_search_idx ON job USING gin (search_vector);
CREATE INDEX job_skills_idx ON job USING gin (skills);
CREATE INDEX job_embedding_idx ON job USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);
CREATE INDEX job_simhash_idx ON job (simhash);

-- The feed query: open, relevant, recent.
CREATE INDEX job_feed_idx ON job (posted_at DESC)
WHERE status = 'open' AND is_software = TRUE AND paid = 'paid' AND deleted_at IS NULL;

-- Dedup candidate lookup.
CREATE INDEX job_dedup_candidates_idx ON job (company_id, role_family, posted_at DESC)
WHERE deleted_at IS NULL;

ALTER TABLE job_group
ADD CONSTRAINT job_group_representative_job_fk
FOREIGN KEY (representative_job_id) REFERENCES job (id);

-- Full audit trail of every merge decision (SCOUT-DEDUP-AUDIT). Stage 1
-- (exact match) writes 'exact' rows; Stages 2/3 are P2 and unused for now.
CREATE TABLE job_merge_event (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID NOT NULL REFERENCES job (id) ON DELETE CASCADE,
    matched_job_id  UUID NOT NULL REFERENCES job (id) ON DELETE CASCADE,
    from_group_id   UUID NOT NULL,
    into_group_id   UUID NOT NULL,
    stage           TEXT NOT NULL
    CHECK (stage IN ('exact', 'structural', 'semantic', 'llm', 'manual')),
    certainty       REAL NOT NULL,
    signal          JSONB NOT NULL,    -- {simhash_distance: 2, cosine: 0.96, ...}
    reverted_at     TIMESTAMPTZ,
    reverted_by     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_merge_job_idx ON job_merge_event (job_id);
