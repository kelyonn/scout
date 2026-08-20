-- User schema: single row for now (ADR-015), user_id kept explicit
-- everywhere per docs/03 section 1 so multi-tenant is a data change later,
-- not a migration. See docs/03-data-model.md section 4.
--
-- `resume` is deliberately not created here — nothing in P1 reads or writes
-- it (resume_match is P3), and it depends on file storage that doesn't exist
-- yet either. Add it with the feature that needs it.

CREATE TABLE app_user (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           CITEXT NOT NULL UNIQUE,
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    display_name    TEXT,
    timezone        TEXT NOT NULL DEFAULT 'Asia/Kolkata',
    locale          TEXT NOT NULL DEFAULT 'en-IN',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at  TIMESTAMPTZ,
    deleted_at      TIMESTAMPTZ
);

-- Preferences drive every ranking decision (SCOUT-RANK-*).
CREATE TABLE user_profile (
    user_id                  UUID PRIMARY KEY REFERENCES app_user (id) ON DELETE CASCADE,

    graduation_year          SMALLINT,
    degree                   TEXT,
    university               TEXT,

    target_roles             role_family [] NOT NULL DEFAULT '{}',
    target_seniority         seniority [] NOT NULL DEFAULT '{internship,new_grad}',
    skills                   TEXT [] NOT NULL DEFAULT '{}',
    skill_levels             JSONB NOT NULL DEFAULT '{}',   -- {"go": 4, "python": 5}

    -- Location preference as explicit tiers with multipliers. Kept here
    -- (rather than only in weight_version) because it is a per-user
    -- preference, not a global scoring weight.
    location_tiers           JSONB NOT NULL DEFAULT '{
        "1": {"cities": ["Bengaluru"], "multiplier": 1.20},
        "2": {"countries": ["IN"], "multiplier": 1.05},
        "3": {"remote": true, "multiplier": 1.12},
        "4": {"countries": ["US", "CA", "SG", "GB", "AU", "JP", "DE", "NL"], "multiplier": 0.90}
    }',

    require_paid             BOOLEAN NOT NULL DEFAULT TRUE,
    allow_prestige_unpaid    BOOLEAN NOT NULL DEFAULT TRUE,
    min_comp_inr_month       NUMERIC(12, 2),

    excluded_companies       UUID [] NOT NULL DEFAULT '{}',
    excluded_keywords        TEXT [] NOT NULL DEFAULT '{}',

    quiet_hours_start        TIME NOT NULL DEFAULT '00:00',
    quiet_hours_end          TIME NOT NULL DEFAULT '07:30',
    digest_time               TIME NOT NULL DEFAULT '08:00',

    notify_threshold         SMALLINT NOT NULL DEFAULT 85,
    max_notifications_hour   SMALLINT NOT NULL DEFAULT 8,
    max_notifications_day    SMALLINT NOT NULL DEFAULT 25,

    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
