-- ai_summary is a factual TL;DR of the posting itself — what the company
-- does, what the role is, requirements, pay — distinct from
-- job_score.explanation (a personalized "why this matches you" narrative,
-- scaffolded via packages/queue's TaskExplain but not yet implemented).
-- Job-level, not job_score-level: the same posting summarizes the same
-- way regardless of which user is looking at it, so it belongs on `job`
-- the same way description_text does, not duplicated per user_id in
-- job_score.
ALTER TABLE job
    ADD COLUMN ai_summary TEXT,
    ADD COLUMN ai_summary_model TEXT,
    ADD COLUMN ai_summary_generated_at TIMESTAMPTZ;
