-- The application-tracking pipeline — docs/03-data-model.md section 6 and
-- docs/04-api-design.md section 4.1's POST /v1/jobs/{group_id}/state. The
-- application_state enum already exists (000002_enums.up.sql); this
-- migration adds the tables that actually track a user's state per job.
--
-- found_elsewhere_first is not in docs/03's literal table definition — it
-- is the one addition this migration makes to the spec. docs/16-
-- observability.md section 2.1 calls the Scout-first rate the primary
-- SLO and "one tap on 'I found this elsewhere first' ... the whole
-- instrument," but no document gives that tap a schema. Placing it here,
-- captured once (defaulting false — Scout found it first, unless the user
-- says otherwise), is the natural home: it is a fact about this specific
-- user/job pair, exactly like `state` itself, and the Scout-first rate is
-- computable directly as a fraction over user_job_state rows in an
-- applied-or-later state.

CREATE TABLE user_job_state (
    user_id       UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    job_group_id  UUID NOT NULL REFERENCES job_group (id) ON DELETE CASCADE,
    state         application_state NOT NULL DEFAULT 'new',
    state_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    found_elsewhere_first BOOLEAN NOT NULL DEFAULT FALSE,
    notes         TEXT,
    rating        SMALLINT CHECK (rating BETWEEN 1 AND 5),
    applied_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, job_group_id)
);
CREATE INDEX ujs_state_idx ON user_job_state (user_id, state, state_changed_at DESC);

-- Append-only history; feeds the Pipeline view's "days in current state"
-- and the eventual learning loop (docs/09 section 5), and is what makes
-- the Scout-first rate auditable rather than a single mutable flag.
CREATE TABLE user_job_state_event (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL,
    job_group_id UUID NOT NULL,
    from_state   application_state,
    to_state     application_state NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ujs_event_lookup_idx ON user_job_state_event (user_id, job_group_id, occurred_at DESC);
