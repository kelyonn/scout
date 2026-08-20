-- Scoring. See docs/03-data-model.md section 5 and docs/09-ranking-scoring.md.
--
-- All thirteen subscores are NOT NULL per the spec, but P1 can only honestly
-- compute a handful from data it has: skill_match (SCOUT-RANK-002),
-- overall_match (SCOUT-RANK-001, itself built from skill_match plus the
-- role_family/seniority fit that Tier 0 classification already produces),
-- deadline_urgency (SCOUT-RANK-012, which has an explicit "unknown -> 40"
-- default), and priority (SCOUT-RANK-013). The remaining eight
-- (company_quality, compensation, learning_opportunity, engineering_culture,
-- growth_potential, interview_probability, competition_estimate,
-- ease_of_applying) need data this pass doesn't have yet -- a company
-- registry, comparables, public engineering signals -- and are written as a
-- neutral 50 with score_inputs flagging {"placeholder": true}. P2 replaces
-- them with real values under a new weight_version; no schema change needed.

CREATE TABLE weight_version (
    version      TEXT PRIMARY KEY,
    weights      JSONB NOT NULL,
    source       TEXT NOT NULL CHECK (source IN ('hand_tuned', 'learned')),
    trained_on   INTEGER,          -- number of labelled examples
    metrics      JSONB,            -- offline eval results
    active       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX weight_version_active_idx ON weight_version (active) WHERE active;

-- One row per (job, user, weight_version). Recomputed on weight change.
CREATE TABLE job_score (
    job_id                  UUID NOT NULL REFERENCES job (id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    weight_version          TEXT NOT NULL REFERENCES weight_version (version),

    -- The thirteen (SCOUT-RANK-001..013), all 0-100.
    overall_match            SMALLINT NOT NULL,
    skill_match               SMALLINT NOT NULL,
    resume_match              SMALLINT NOT NULL,
    company_quality           SMALLINT NOT NULL,
    compensation              SMALLINT NOT NULL,
    learning_opportunity      SMALLINT NOT NULL,
    engineering_culture       SMALLINT NOT NULL,
    growth_potential          SMALLINT NOT NULL,
    interview_probability     SMALLINT NOT NULL,
    competition_estimate      SMALLINT NOT NULL,   -- higher = LESS competition
    ease_of_applying          SMALLINT NOT NULL,
    deadline_urgency          SMALLINT NOT NULL,
    priority                  SMALLINT NOT NULL,    -- the ordering value

    -- Multipliers applied, retained for explainability.
    location_multiplier      REAL NOT NULL DEFAULT 1.0,
    freshness_multiplier     REAL NOT NULL DEFAULT 1.0,

    explanation               TEXT,
    explanation_model         TEXT,
    score_inputs               JSONB NOT NULL DEFAULT '{}',   -- full audit of inputs

    computed_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, user_id, weight_version)
);
CREATE INDEX job_score_priority_idx ON job_score (user_id, priority DESC, computed_at DESC);
