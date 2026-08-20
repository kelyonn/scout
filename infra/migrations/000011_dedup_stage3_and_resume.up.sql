-- Two small additions P2 needs:
--
-- job.possible_duplicate: docs/08-dedup-identity.md section 3.5's Stage 3
-- LLM-adjudication outcome when confidence lands below merge (< 0.85) —
-- "otherwise: distinct, flag possible_duplicate in the UI." No UI exists
-- yet to surface it (P3+), but the column is the durable record of the
-- flag regardless of when a UI reads it.
--
-- resume table: deliberately not created in 000007_user.up.sql ("resume_match
-- is P3... depends on file storage that doesn't exist yet") — that blocker
-- doesn't apply here. This is a single-user system (ADR-015); the resume is
-- plain text already in hand, not a file upload, so no storage dependency
-- exists to wait for. docs/09 section 3.2's resume_match formula needs
-- exactly this: raw text for the lexical `keyword` term, an embedding for
-- the semantic term.

ALTER TABLE job ADD COLUMN possible_duplicate BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX job_possible_duplicate_idx ON job (possible_duplicate) WHERE possible_duplicate = TRUE;

CREATE TABLE resume (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL UNIQUE REFERENCES app_user (id) ON DELETE CASCADE,
    raw_text           TEXT NOT NULL,
    embedding          VECTOR(384),
    embedding_version  TEXT,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
