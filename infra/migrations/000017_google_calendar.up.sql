-- Google Calendar integration: both directions, per the user's own
-- explicit choice — Scout pushes tracked deadlines out as real events,
-- and pulls the user's existing events in for the Calendar page to show
-- alongside them.
--
-- One credential row per user (unique on user_id), matching every other
-- single-user assumption already in this schema (ADR-015) — no
-- multi-account support, no reason to build it for one person.
--
-- Tokens are stored in plaintext columns, not application-layer
-- encrypted, the same posture this schema already takes with the resume
-- and notes columns (AGENTS.md rule 9 is about never sending this data
-- to a hosted LLM, not about not storing it locally at all) — Postgres
-- itself is the trust boundary here, same as everywhere else in this
-- database.
CREATE TABLE google_calendar_credential (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    access_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    token_expiry  TIMESTAMPTZ NOT NULL,
    -- 'primary' is the user's default calendar. Exposed as a column (not
    -- hardcoded in the Go layer) so a future settings UI could let them
    -- pick a different calendar without a schema change.
    calendar_id   TEXT NOT NULL DEFAULT 'primary',
    connected_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

-- Tracks the push direction's own idempotency: which job_group already has
-- a Google event, and what its ID is, so a re-sync updates the existing
-- event instead of creating a duplicate every time. One row per tracked
-- deadline that has ever been pushed.
CREATE TABLE google_calendar_event (
    job_group_id     UUID PRIMARY KEY REFERENCES job_group (id) ON DELETE CASCADE,
    user_id          UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    google_event_id  TEXT NOT NULL,
    pushed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX google_calendar_event_user_idx ON google_calendar_event (user_id);
