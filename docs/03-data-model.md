# Data Model — Scout

**Status:** Draft · **Owner:** Data · **Last updated:** 2026-08-06

PostgreSQL 16 with `pgvector`, `pg_trgm`, `citext`, and `pgcrypto`.

---

## 1. Design principles

**Observations are immutable; jobs are derived.** Raw sightings are append-only
and never updated. Everything downstream is a projection that can be rebuilt.
This means a bug in normalization is fixable by reprocessing rather than by data
loss, and it gives us a complete audit trail of how a posting changed over time.

**Identity is explicit, not implicit.** `company`, `job`, and `job_group` are
separate concepts with separate resolution logic. Conflating them is the root of
most duplicate bugs in systems like this.

**`user_id` everywhere from day one.** Single-tenant now, multi-tenant later,
zero migration. Row-Level Security policies are enabled from the first migration
so they are exercised rather than theoretical.

**Time is always `timestamptz`.** No naive timestamps anywhere. The user is in
IST, sources are worldwide, and the system stores UTC. Every display-time
conversion happens at the edge.

**Soft delete only where recovery matters.** Jobs and companies use
`deleted_at`. Observations are never deleted, only partition-dropped. Sessions
and cache entries hard-delete.

---

## 2. Entity relationship overview

```
                        ┌──────────────┐
                        │   company    │
                        │  identity    │
                        └──┬────────┬──┘
                           │        │
              ┌────────────┘        └────────────┐
              │                                  │
     ┌────────▼────────┐                ┌────────▼────────┐
     │     source      │                │      job        │
     │  what we poll   │                │   canonical     │
     └────────┬────────┘                └───┬─────────┬───┘
              │                             │         │
     ┌────────▼────────────┐                │    ┌────▼──────────┐
     │  raw_observation    │────────────────┘    │   job_group   │
     │  append-only,       │  derives            │  dedup unit   │
     │  monthly partitions │                     └────┬──────────┘
     └─────────────────────┘                          │
                                                      │
     ┌──────────────┐    ┌──────────────┐   ┌─────────▼─────────┐
     │     user     │───▶│  job_score   │◀──│                   │
     │              │    │ 13 subscores │   │                   │
     └──┬───┬───┬───┘    └──────────────┘   │                   │
        │   │   │                            │                   │
        │   │   └──────────▶┌─────────────────▼──────┐           │
        │   │               │  user_job_state        │           │
        │   │               │  saved/applied/...     │           │
        │   │               └────────────────────────┘           │
        │   │                                                     │
        │   └──────────────▶┌────────────────────────┐           │
        │                   │     notification       │◀──────────┘
        │                   │  unique(user,group,    │
        │                   │         trigger)       │
        │                   └────────────────────────┘
        │
        ├──▶ user_profile · resume · watchlist · notification_channel
        └──▶ application · interview · user_feedback
```

---

## 3. Core schema

### 3.1 Company

Identity resolution is the foundation — dedup filters by `company_id`, so getting
this wrong breaks everything downstream.

```sql
CREATE TABLE company (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                CITEXT NOT NULL UNIQUE,

    -- Identity signals, in confidence order
    canonical_name      TEXT NOT NULL,
    legal_name          TEXT,
    registered_domain   CITEXT UNIQUE,          -- strongest signal
    normalized_name     CITEXT NOT NULL,        -- lowercased, suffixes stripped

    -- Enrichment
    description         TEXT,
    logo_url            TEXT,
    website_url         TEXT,
    linkedin_url        TEXT,
    github_org          TEXT,
    crunchbase_slug     TEXT,

    -- Classification. NOTE: none of these may feed company_quality — see
    -- 09-ranking-scoring.md. They drive adapter choice, scheduling, competition
    -- estimation, and per-category coverage auditing only.
    company_type        TEXT NOT NULL DEFAULT 'unknown' CHECK (company_type IN
                          ('product',
                           'services_it','services_consulting','services_engineering',
                           'gcc',
                           'core_bfsi','core_manufacturing','core_energy',
                           'core_telecom','core_retail_cpg','core_healthcare',
                           'core_aerospace_def','core_logistics',
                           'research','public_sector','nonprofit',
                           'unknown')),
    company_type_source TEXT CHECK (company_type_source IN
                          ('registry','heuristic','llm')),

    -- GCCs and subsidiaries: "Target in India" → "Target Corporation".
    -- Deliberately NOT a merge; see 08-dedup-identity.md.
    parent_company_id   UUID REFERENCES company(id) ON DELETE SET NULL,

    size_bucket         TEXT CHECK (size_bucket IN
                          ('1-10','11-50','51-200','201-500','501-1000',
                           '1001-5000','5001-10000','10000+')),
    founded_year        SMALLINT,
    stage               TEXT CHECK (stage IN
                          ('pre_seed','seed','series_a','series_b','series_c',
                           'series_d_plus','public','private','bootstrapped',
                           'nonprofit','government','academic')),
    total_funding_usd   BIGINT,
    last_funding_at     DATE,
    industries          TEXT[] NOT NULL DEFAULT '{}',
    tech_stack          TEXT[] NOT NULL DEFAULT '{}',
    hq_country          CHAR(2),
    hq_city             TEXT,

    -- Scoring inputs (0-100), computed nightly
    quality_score       SMALLINT CHECK (quality_score BETWEEN 0 AND 100),
    engineering_score   SMALLINT CHECK (engineering_score BETWEEN 0 AND 100),
    quality_computed_at TIMESTAMPTZ,

    -- Discovery provenance
    discovered_via      TEXT NOT NULL,   -- 'seed'|'ats_enumeration'|'funding'|...
    discovered_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX company_normalized_name_trgm ON company
    USING GIN (normalized_name gin_trgm_ops);
CREATE INDEX company_quality_idx ON company (quality_score DESC NULLS LAST)
    WHERE deleted_at IS NULL;
CREATE INDEX company_industries_idx ON company USING GIN (industries);
CREATE INDEX company_type_idx ON company (company_type) WHERE deleted_at IS NULL;
CREATE INDEX company_parent_idx ON company (parent_company_id)
    WHERE parent_company_id IS NOT NULL;

-- Aliases: "Meta" = "Facebook" = "Meta Platforms Inc."
CREATE TABLE company_alias (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id    UUID NOT NULL REFERENCES company(id) ON DELETE CASCADE,
    alias         CITEXT NOT NULL,
    alias_kind    TEXT NOT NULL CHECK (alias_kind IN
                    ('former_name','abbreviation','ats_token','domain',
                     'legal_entity','misspelling','subsidiary')),
    confidence    REAL NOT NULL DEFAULT 1.0 CHECK (confidence BETWEEN 0 AND 1),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (alias, alias_kind)
);
CREATE INDEX company_alias_lookup ON company_alias (alias);
```

### 3.2 Source

```sql
CREATE TYPE source_kind AS ENUM (
    'ats_greenhouse','ats_lever','ats_ashby','ats_workday','ats_smartrecruiters',
    'ats_bamboohr','ats_rippling','ats_icims','ats_jobvite','ats_successfactors',
    'ats_workable','ats_recruitee','ats_teamtailor',
    'rss','atom','json_feed','sitemap','html_page',
    'hackernews','github_repo','github_discussions','reddit','x_account',
    'discord','telegram_channel',
    'email_alert','api_partner','hackathon','vc_portfolio','research_lab'
);

CREATE TYPE legal_posture AS ENUM (
    'permitted',   -- robots.txt and ToS allow automated access
    'email_only',  -- prohibited to fetch; ingest via user's alert email
    'api_only',    -- only via authorized API
    'prohibited'   -- do not touch (collector refuses)
);

CREATE TYPE source_status AS ENUM (
    'active','paused','quarantined','failed','retired','pending_review'
);

CREATE TABLE source (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id            UUID REFERENCES company(id) ON DELETE SET NULL,
    kind                  source_kind NOT NULL,
    status                source_status NOT NULL DEFAULT 'pending_review',
    legal_posture         legal_posture NOT NULL DEFAULT 'pending_review'::TEXT::legal_posture,

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
    -- 'cyclical' caps max_interval_s at 4h regardless of recent yield.
    hiring_pattern        TEXT NOT NULL DEFAULT 'continuous'
                            CHECK (hiring_pattern IN ('continuous','cyclical')),

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

    priority_tier         SMALLINT NOT NULL DEFAULT 3 CHECK (priority_tier BETWEEN 1 AND 5),
    notes                 TEXT,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The scheduler's hot query. Partial index keeps it tiny.
CREATE INDEX source_due_idx ON source (next_poll_at)
    WHERE status = 'active' AND legal_posture IN ('permitted','api_only');
CREATE INDEX source_company_idx ON source (company_id);
CREATE INDEX source_yield_idx ON source (yield_ratio DESC);
```

### 3.3 Raw observation (partitioned)

```sql
CREATE TABLE raw_observation (
    id               UUID NOT NULL DEFAULT gen_random_uuid(),
    source_id        UUID NOT NULL REFERENCES source(id) ON DELETE CASCADE,
    observed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    external_id      TEXT,          -- ATS-native job id when available
    url              TEXT NOT NULL,
    canonical_url    TEXT NOT NULL,
    canonical_url_hash BYTEA NOT NULL,

    content_hash     BYTEA NOT NULL,          -- sha256 of normalized payload
    payload          JSONB NOT NULL,          -- adapter's structured output
    snapshot_key     TEXT,                    -- local snapshot path (docs/15 s.5)
    http_status      SMALLINT,
    fetch_duration_ms INTEGER,

    processed_at     TIMESTAMPTZ,
    process_error    TEXT,
    job_id           UUID,                    -- set after normalization

    PRIMARY KEY (id, observed_at)
) PARTITION BY RANGE (observed_at);

-- Monthly partitions, created 3 months ahead by a cron job,
-- dropped after 6 months (raw bytes live on local disk for 30 days separately).
CREATE TABLE raw_observation_2026_08 PARTITION OF raw_observation
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE UNIQUE INDEX raw_obs_dedup_idx ON raw_observation (source_id, content_hash, observed_at);
CREATE INDEX raw_obs_unprocessed_idx ON raw_observation (observed_at)
    WHERE processed_at IS NULL;
CREATE INDEX raw_obs_canonical_idx ON raw_observation (canonical_url_hash);
```

**Why partition.** At 150k observations/day, the table reaches ~55M rows/year.
Monthly range partitions keep index sizes manageable, make retention a
`DROP TABLE` instead of a mass `DELETE` (which would be a vacuum disaster), and
let the planner prune to recent partitions for the queries that matter.

### 3.4 Job — the canonical record

```sql
CREATE TYPE role_family AS ENUM (
    'swe.general','swe.backend','swe.frontend','swe.fullstack','swe.mobile',
    'swe.ml','swe.ml.research','swe.data','swe.infra','swe.infra.sre',
    'swe.infra.devops','swe.infra.platform','swe.infra.cloud','swe.systems',
    'swe.security','swe.embedded','swe.qa','swe.research','swe.other'
);

CREATE TYPE seniority AS ENUM (
    'internship','apprenticeship','new_grad','entry','mid','senior','staff','unknown'
);

CREATE TYPE paid_signal AS ENUM ('paid','unpaid','unknown');

CREATE TYPE work_mode AS ENUM ('onsite','hybrid','remote','unknown');

CREATE TYPE job_status AS ENUM ('open','closed','expired','filled','unknown');

CREATE TABLE job (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_group_id         UUID NOT NULL REFERENCES job_group(id),
    company_id           UUID NOT NULL REFERENCES company(id),
    primary_source_id    UUID NOT NULL REFERENCES source(id),

    -- Identity
    canonical_url        TEXT NOT NULL,
    canonical_url_hash   BYTEA NOT NULL,
    ats_platform         TEXT,
    ats_job_id           TEXT,
    content_hash         BYTEA NOT NULL,

    -- Content
    title                TEXT NOT NULL,
    normalized_title     TEXT NOT NULL,
    description_html     TEXT,
    description_text     TEXT,
    description_stripped TEXT,     -- boilerplate removed, used for dedup/embed
    requirements_text    TEXT,
    apply_url            TEXT NOT NULL,

    -- Classification (SCOUT-NORM-*)
    role_family          role_family NOT NULL DEFAULT 'swe.other',
    role_confidence      REAL NOT NULL DEFAULT 0,
    seniority            seniority NOT NULL DEFAULT 'unknown',
    is_software          BOOLEAN NOT NULL DEFAULT false,
    skills               TEXT[] NOT NULL DEFAULT '{}',
    tech_stack           TEXT[] NOT NULL DEFAULT '{}',

    -- Location (SCOUT-RANK-LOC)
    location_raw         TEXT,
    location_city        TEXT,
    location_region      TEXT,
    location_country     CHAR(2),
    location_tier        SMALLINT CHECK (location_tier BETWEEN 1 AND 4),
    work_mode            work_mode NOT NULL DEFAULT 'unknown',
    visa_sponsorship     BOOLEAN,

    -- Compensation (SCOUT-NORM-COMP)
    paid                 paid_signal NOT NULL DEFAULT 'unknown',
    comp_min             NUMERIC(12,2),
    comp_max             NUMERIC(12,2),
    comp_currency        CHAR(3),
    comp_period          TEXT CHECK (comp_period IN ('hour','month','year','stipend_total')),
    comp_normalized_inr_month NUMERIC(12,2),   -- single comparable number
    comp_confidence      REAL,
    prestige_exception   BOOLEAN NOT NULL DEFAULT false,  -- unpaid but notable

    -- Lifecycle
    status               job_status NOT NULL DEFAULT 'open',
    posted_at            TIMESTAMPTZ,
    posted_at_estimated  BOOLEAN NOT NULL DEFAULT false,
    deadline_at          TIMESTAMPTZ,
    first_seen_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at            TIMESTAMPTZ,

    -- Quality flags
    is_ghost             BOOLEAN NOT NULL DEFAULT false,
    ghost_reason         TEXT,
    observation_count    INTEGER NOT NULL DEFAULT 1,

    -- Search and similarity
    embedding            vector(384),
    embedding_version    TEXT,
    simhash              BIGINT,
    search_vector        tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title,'')), 'A') ||
        setweight(to_tsvector('english', coalesce(location_raw,'')), 'B') ||
        setweight(to_tsvector('english', coalesce(description_text,'')), 'C')
    ) STORED,

    raw_payload          JSONB NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ
);

CREATE UNIQUE INDEX job_canonical_url_idx ON job (canonical_url_hash)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX job_ats_idx ON job (ats_platform, ats_job_id)
    WHERE ats_platform IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX job_group_idx        ON job (job_group_id);
CREATE INDEX job_company_idx      ON job (company_id, posted_at DESC);
CREATE INDEX job_search_idx       ON job USING GIN (search_vector);
CREATE INDEX job_skills_idx       ON job USING GIN (skills);
CREATE INDEX job_embedding_idx    ON job USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
CREATE INDEX job_simhash_idx      ON job (simhash);

-- The feed query: open, relevant, recent
CREATE INDEX job_feed_idx ON job (posted_at DESC)
    WHERE status = 'open' AND is_software = true
      AND paid = 'paid' AND deleted_at IS NULL;

-- Dedup candidate lookup
CREATE INDEX job_dedup_candidates_idx ON job (company_id, role_family, posted_at DESC)
    WHERE deleted_at IS NULL;
```

### 3.5 Job group — the notification unit

```sql
CREATE TABLE job_group (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id        UUID NOT NULL REFERENCES company(id),
    representative_job_id UUID,       -- best record; FK added after job exists
    member_count      INTEGER NOT NULL DEFAULT 1,
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    notified_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_group_company_idx ON job_group (company_id);
-- Alert trigger: over-merging detection
CREATE INDEX job_group_large_idx ON job_group (member_count) WHERE member_count > 10;

-- Full audit trail of every merge decision (SCOUT-DEDUP-AUDIT)
CREATE TABLE job_merge_event (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID NOT NULL REFERENCES job(id) ON DELETE CASCADE,
    matched_job_id UUID NOT NULL REFERENCES job(id) ON DELETE CASCADE,
    from_group_id  UUID NOT NULL,
    into_group_id  UUID NOT NULL,
    stage          TEXT NOT NULL CHECK (stage IN ('exact','structural','semantic','llm','manual')),
    certainty      REAL NOT NULL,
    signal         JSONB NOT NULL,    -- {simhash_distance: 2, cosine: 0.96, ...}
    reverted_at    TIMESTAMPTZ,
    reverted_by    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_merge_job_idx ON job_merge_event (job_id);
```

---

## 4. User schema

```sql
CREATE TABLE app_user (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          CITEXT NOT NULL UNIQUE,
    email_verified BOOLEAN NOT NULL DEFAULT false,
    display_name   TEXT,
    timezone       TEXT NOT NULL DEFAULT 'Asia/Kolkata',
    locale         TEXT NOT NULL DEFAULT 'en-IN',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ,
    deleted_at     TIMESTAMPTZ
);

-- Preferences drive every ranking decision (SCOUT-RANK-*)
CREATE TABLE user_profile (
    user_id              UUID PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,

    graduation_year      SMALLINT,
    degree               TEXT,
    university           TEXT,

    target_roles         role_family[] NOT NULL DEFAULT '{}',
    target_seniority     seniority[] NOT NULL DEFAULT '{internship,new_grad}',
    skills               TEXT[] NOT NULL DEFAULT '{}',
    skill_levels         JSONB NOT NULL DEFAULT '{}',   -- {"go": 4, "python": 5}

    -- Location preference as explicit tiers with multipliers
    location_tiers       JSONB NOT NULL DEFAULT
      '{"1":{"cities":["Bengaluru"],"multiplier":1.20},
        "2":{"countries":["IN"],"multiplier":1.05},
        "3":{"remote":true,"multiplier":1.12},
        "4":{"countries":["US","CA","SG","GB","AU","JP","DE","NL"],"multiplier":0.90}}',

    require_paid         BOOLEAN NOT NULL DEFAULT true,
    allow_prestige_unpaid BOOLEAN NOT NULL DEFAULT true,
    min_comp_inr_month   NUMERIC(12,2),

    excluded_companies   UUID[] NOT NULL DEFAULT '{}',
    excluded_keywords    TEXT[] NOT NULL DEFAULT '{}',

    quiet_hours_start    TIME NOT NULL DEFAULT '00:00',
    quiet_hours_end      TIME NOT NULL DEFAULT '07:30',
    digest_time          TIME NOT NULL DEFAULT '08:00',

    notify_threshold     SMALLINT NOT NULL DEFAULT 85,
    max_notifications_hour SMALLINT NOT NULL DEFAULT 8,
    max_notifications_day  SMALLINT NOT NULL DEFAULT 25,

    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE resume (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    label          TEXT NOT NULL,
    file_key       TEXT NOT NULL,          -- host path on the encrypted volume
    content_text   TEXT,
    extracted      JSONB NOT NULL DEFAULT '{}',   -- skills, projects, education
    embedding      vector(384),
    embedding_version TEXT,
    is_default     BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX resume_default_idx ON resume (user_id) WHERE is_default;
```

---

## 5. Scoring

```sql
-- One row per (job, user, weight_version). Recomputed on weight change.
CREATE TABLE job_score (
    job_id              UUID NOT NULL REFERENCES job(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    weight_version      TEXT NOT NULL,

    -- The thirteen (SCOUT-RANK-001..013), all 0-100
    overall_match       SMALLINT NOT NULL,
    skill_match         SMALLINT NOT NULL,
    resume_match        SMALLINT NOT NULL,
    company_quality     SMALLINT NOT NULL,
    compensation        SMALLINT NOT NULL,
    learning_opportunity SMALLINT NOT NULL,
    engineering_culture SMALLINT NOT NULL,
    growth_potential    SMALLINT NOT NULL,
    interview_probability SMALLINT NOT NULL,
    competition_estimate SMALLINT NOT NULL,   -- higher = LESS competition
    ease_of_applying    SMALLINT NOT NULL,
    deadline_urgency    SMALLINT NOT NULL,
    priority            SMALLINT NOT NULL,    -- the ordering value

    -- Multipliers applied, retained for explainability
    location_multiplier REAL NOT NULL DEFAULT 1.0,
    freshness_multiplier REAL NOT NULL DEFAULT 1.0,

    explanation         TEXT,
    explanation_model   TEXT,
    score_inputs        JSONB NOT NULL DEFAULT '{}',   -- full audit of inputs

    computed_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, user_id, weight_version)
);
CREATE INDEX job_score_priority_idx ON job_score (user_id, priority DESC, computed_at DESC);

CREATE TABLE weight_version (
    version      TEXT PRIMARY KEY,
    weights      JSONB NOT NULL,
    source       TEXT NOT NULL CHECK (source IN ('hand_tuned','learned')),
    trained_on   INTEGER,          -- number of labelled examples
    metrics      JSONB,            -- offline eval results
    active       BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX weight_version_active_idx ON weight_version (active) WHERE active;
```

---

## 6. User interaction and tracking

```sql
CREATE TYPE application_state AS ENUM (
    'new','viewed','saved','dismissed','applied','screening',
    'interviewing','offer','accepted','rejected','withdrawn','expired'
);

CREATE TABLE user_job_state (
    user_id       UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    job_group_id  UUID NOT NULL REFERENCES job_group(id) ON DELETE CASCADE,
    state         application_state NOT NULL DEFAULT 'new',
    state_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    notes         TEXT,
    rating        SMALLINT CHECK (rating BETWEEN 1 AND 5),
    applied_at    TIMESTAMPTZ,
    resume_id     UUID REFERENCES resume(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, job_group_id)
);
CREATE INDEX ujs_state_idx ON user_job_state (user_id, state, state_changed_at DESC);

-- Append-only history; feeds the learning loop and the analytics funnel
CREATE TABLE user_job_state_event (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL,
    job_group_id UUID NOT NULL,
    from_state   application_state,
    to_state     application_state NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Implicit and explicit signals for the learning loop (SCOUT-RANK-LEARN)
CREATE TABLE user_feedback (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    job_group_id UUID NOT NULL REFERENCES job_group(id) ON DELETE CASCADE,
    signal       TEXT NOT NULL CHECK (signal IN
                   ('impression','click','dwell','save','dismiss','apply',
                    'notification_open','notification_ignore','marked_irrelevant')),
    value        REAL,          -- dwell seconds, etc.
    context      JSONB NOT NULL DEFAULT '{}',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_feedback_training_idx ON user_feedback (user_id, occurred_at DESC);

CREATE TABLE interview (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    job_group_id UUID NOT NULL REFERENCES job_group(id) ON DELETE CASCADE,
    round_number SMALLINT NOT NULL DEFAULT 1,
    round_type   TEXT CHECK (round_type IN
                   ('oa','phone_screen','technical','system_design',
                    'behavioral','hiring_manager','onsite','final')),
    scheduled_at TIMESTAMPTZ,
    duration_min SMALLINT,
    interviewer  TEXT,
    outcome      TEXT CHECK (outcome IN ('pending','passed','failed','cancelled','no_show')),
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX interview_upcoming_idx ON interview (user_id, scheduled_at)
    WHERE outcome = 'pending';

CREATE TABLE watchlist (
    user_id      UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    company_id   UUID NOT NULL REFERENCES company(id) ON DELETE CASCADE,
    notify_any_role BOOLEAN NOT NULL DEFAULT true,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, company_id)
);
```

---

## 7. Notifications

```sql
CREATE TYPE notification_trigger AS ENUM (
    'bengaluru_match','high_score','remote_high_quality','watchlist_hiring',
    'deadline_approaching','prestige_opening','newgrad_match','digest'
);

CREATE TYPE notification_channel_kind AS ENUM (
    'native_push',   -- FCM via the Capacitor shell — ADR-012
    'telegram',
    'web_push',      -- desktop and not-installed fallback
    'email','discord','in_app'
);

-- Enum rather than a boolean: only Android ships, but ADR-012 keeps iOS
-- reachable without a migration.
CREATE TYPE device_platform AS ENUM ('android','ios');

CREATE TABLE notification_channel (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    kind         notification_channel_kind NOT NULL,
    config       JSONB NOT NULL,     -- encrypted: chat_id, push subscription, ...
    enabled      BOOLEAN NOT NULL DEFAULT true,
    verified_at  TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count SMALLINT NOT NULL DEFAULT 0,

    -- native_push only: one row per physical device (ADR-012)
    platform      device_platform,
    device_token  TEXT,             -- FCM registration token
    device_label  TEXT,             -- "Pixel 8", shown in settings
    app_version   TEXT,
    token_refreshed_at TIMESTAMPTZ,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT native_push_needs_device CHECK (
        kind <> 'native_push' OR (platform IS NOT NULL AND device_token IS NOT NULL)
    )
);

-- One channel row per device token; re-registering the same token updates in place.
CREATE UNIQUE INDEX notification_channel_device_idx
    ON notification_channel (user_id, device_token)
    WHERE kind = 'native_push';

-- A user has at most one Telegram, email, or Discord channel, but many devices.
CREATE UNIQUE INDEX notification_channel_single_idx
    ON notification_channel (user_id, kind)
    WHERE kind IN ('telegram','email','discord');

CREATE TABLE notification (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    job_group_id UUID REFERENCES job_group(id) ON DELETE CASCADE,
    trigger      notification_trigger NOT NULL,
    urgency      TEXT NOT NULL CHECK (urgency IN ('instant','batched','digest')),
    payload      JSONB NOT NULL,
    priority_at_send SMALLINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    scheduled_for TIMESTAMPTZ NOT NULL DEFAULT now(),
    suppressed_reason TEXT     -- 'quiet_hours'|'budget'|'backfill'|null
);

-- THE guarantee: never notify twice for the same opportunity. (SCOUT-NOTIF-002)
CREATE UNIQUE INDEX notification_dedup_idx
    ON notification (user_id, job_group_id, trigger)
    WHERE job_group_id IS NOT NULL;

CREATE TABLE notification_delivery (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID NOT NULL REFERENCES notification(id) ON DELETE CASCADE,
    channel_id      UUID NOT NULL REFERENCES notification_channel(id) ON DELETE CASCADE,
    status          TEXT NOT NULL CHECK (status IN
                      ('pending','sent','delivered','failed','skipped')),
    attempts        SMALLINT NOT NULL DEFAULT 0,
    error           TEXT,
    sent_at         TIMESTAMPTZ,
    opened_at       TIMESTAMPTZ,
    latency_ms      INTEGER,        -- posted_at → sent_at, the SLO measurement
    provider_msg_id TEXT,           -- for reconciling delivery receipts
    UNIQUE (notification_id, channel_id)
);
```

**`skipped` is a first-class status, not a failure.** It records a delivery that was
intentionally not attempted — currently only Web Push suppressed because the same
device is reachable via `native_push`. Distinguishing "we chose not to" from "we
tried and failed" is what keeps the channel-health alerts in
[`16-observability.md`](16-observability.md) from firing on correct behavior.

**Every current channel is free at our volume**, so there is no cost column here. If
a metered channel is ever added, `cost_usd NUMERIC(10,6)` belongs on this table so
notification spend gets the same per-item attribution as LLM spend.

---

## 8. Operational tables

```sql
-- Every LLM call, for cost attribution and caching (SCOUT-AI-COST)
CREATE TABLE llm_call (
    id             BIGSERIAL PRIMARY KEY,
    task           TEXT NOT NULL,
    tier           SMALLINT NOT NULL,
    provider       TEXT NOT NULL,
    model          TEXT NOT NULL,
    prompt_hash    BYTEA NOT NULL,
    input_tokens   INTEGER NOT NULL,
    output_tokens  INTEGER NOT NULL,
    cost_usd       NUMERIC(10,6) NOT NULL,
    latency_ms     INTEGER NOT NULL,
    cached         BOOLEAN NOT NULL DEFAULT false,
    error          TEXT,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX llm_call_cost_idx ON llm_call (occurred_at DESC, task);
CREATE INDEX llm_call_cache_idx ON llm_call (prompt_hash, model);

-- Auth
CREATE TABLE webauthn_credential (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    credential_id   BYTEA NOT NULL UNIQUE,
    public_key      BYTEA NOT NULL,
    sign_count      BIGINT NOT NULL DEFAULT 0,
    transports      TEXT[],
    device_label    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ
);

CREATE TABLE session (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash    BYTEA NOT NULL UNIQUE,
    user_agent    TEXT,
    ip_prefix     INET,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ
);
CREATE INDEX session_cleanup_idx ON session (expires_at) WHERE revoked_at IS NULL;
```

---

## 9. Row-Level Security — not enabled

The earlier version of this document enabled RLS on every user-scoped table from
the first migration, "with one user, deliberately", so that it would be exercised
rather than theoretical.

**That has been removed.** There is one user and one bearer token
([ADR-015](adr/ADR-015-single-user-auth.md)). RLS protects one row set from
another row set that does not exist, and it costs a `current_setting()` on every
query, a `BYPASSRLS` worker role, a policy per table to keep in sync with every
new table, and a class of debugging where a query silently returns zero rows
because a session variable was not set.

**`user_id` columns stay.** They are already written, they cost nothing when
there is one value, and they mean enabling RLS later is a migration rather than a
schema change:

```sql
-- If a second user ever exists, this is the whole change.
ALTER TABLE user_profile ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_isolation ON user_profile
    USING (user_id = current_setting('scout.user_id', true)::uuid);
-- ... repeated for resume, job_score, user_job_state, user_feedback,
--     interview, watchlist, notification, notification_channel
CREATE ROLE scout_worker BYPASSRLS;
```

That is perhaps an hour of work, done at the point it becomes necessary rather
than fourteen weeks before. `company`, `source`, `job`, and `job_group` would
remain unprotected in any case — they contain no user data.

**What replaces it today:** the API is the only writer, every route requires the
bearer token, and the integration suite asserts that a request with a missing or
wrong token is rejected on every state-changing route
([17-testing-qa.md](17-testing-qa.md)).

See [02-architecture.md](02-architecture.md) section 6 for the full accounting of
what multi-tenant readiness was costing and why it was removed.

---

## 10. Migrations

**Tool:** `golang-migrate`, forward-only, one file per change, applied
transactionally where Postgres permits.

**Rules:**

1. **Never rewrite a shipped migration.** Fix forward with a new one.
2. **Additive-first.** Adding a nullable column or a new table is always safe.
3. **Destructive changes are two-phase.** To drop a column: (a) stop writing to
   it and deploy, (b) drop it in the next release. Never in one step, because
   rollback must remain possible.
4. **Index creation uses `CONCURRENTLY`** on any table above 100k rows, and runs
   outside a transaction accordingly.
5. **Every migration is tested against a production-sized snapshot** in CI. A
   migration that takes 40 seconds on an empty table and 40 minutes on the real
   one is a production incident waiting to happen.
6. **Backfills are River jobs, not migrations.** A migration adds the column; a
   job populates it. This keeps deploys fast and backfills resumable.

**Partition maintenance** runs as a nightly job: create partitions three months
ahead, detach and drop partitions older than six months after verifying the
snapshot archive.

---

## 11. Capacity and retention

| Table | Rows @ Year 1 | Size estimate | Retention |
| --- | --- | --- | --- |
| `raw_observation` | ~55M | ~28 GB | 6 months (partition drop) |
| `job` | ~250k | ~4 GB | Indefinite (soft delete) |
| `job_score` | ~250k | ~200 MB | Current weight version + 1 |
| `user_feedback` | ~50k | ~10 MB | Indefinite (training data) |
| `llm_call` | ~500k | ~120 MB | 90 days |
| `notification` | ~10k | ~5 MB | Indefinite |
| River queue tables | ~50k live | ~50 MB | Completed rows pruned hourly |
| **Total** | | **~33 GB** | Within the 80GB NVMe with room |

**The vacuum consideration.** Queue tables and `job_score` are high-churn. Both
get per-table autovacuum tuning:

```sql
ALTER TABLE river_job SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.01,
    autovacuum_vacuum_cost_delay = 0
);
```

This is the concrete, known cost of the decision in
[ADR-003](adr/ADR-003-job-queue-over-kafka.md) to keep the queue in Postgres.
