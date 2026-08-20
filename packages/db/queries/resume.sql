-- Resume queries — docs/09-ranking-scoring.md section 3.2's resume_match.
-- Single-user system (ADR-015): one resume row, upserted on
-- user_id, never inserted a second time.

-- name: UpsertResume :one
-- embedding/embedding_version reset to NULL on every update — this is what
-- apps/brain/scout_brain/resume_embed.py's staleness check
-- (`embedding IS NULL OR embedding_version IS DISTINCT FROM ...`) looks
-- for, whether that recompute happens at brain startup or via an
-- embed_resume queue job (apps/api's resume upload handler enqueues one in
-- the same transaction as this write).
insert into resume (user_id, raw_text)
values (sqlc.arg(user_id)::uuid, sqlc.arg(raw_text)::text)
on conflict (user_id) do update set
    raw_text = excluded.raw_text,
    embedding = NULL,
    embedding_version = NULL,
    updated_at = now()
returning id, user_id, raw_text, updated_at;

-- name: GetResume :one
-- Explicit ::boolean cast: sqlc can't infer a scalar type for an
-- expression over `embedding` (pgvector's custom vector type, opaque to
-- sqlc's analyzer) without one, and falls back to `interface{}` — the same
-- reason source.sql's own boolean expressions over non-builtin-typed
-- columns get an explicit cast elsewhere in this project.
select id, user_id, raw_text, (embedding is not null)::boolean as has_embedding, embedding_version, updated_at
from resume
where user_id = sqlc.arg(user_id)::uuid;

-- name: UpdateUserProfileSkills :exec
-- Replaces the whole skills/skill_levels pair rather than merging —
-- resume_match's keyword term reads user_profile.skills directly, so a
-- stale skill from a previous, different resume left behind after a swap
-- would silently keep contributing to that score forever. A full
-- replacement on every resume upload is what actually satisfies "remove
-- the old one, put a new one there."
update user_profile
set skills = sqlc.arg(skills)::text[],
    skill_levels = sqlc.arg(skill_levels)::jsonb,
    updated_at = now()
where user_id = sqlc.arg(user_id)::uuid;
