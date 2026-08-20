-- docs/05-source-catalog.md section 13 documents a two-step review process
-- for every newly registered source: 48h shadow run (poll and parse, but
-- never notify), then automated promotion to 'active' unless the shadow run
-- looks anomalous (zero jobs, or a suspiciously high count). That promotion
-- step never existed in code — SelectDueSources only ever selected
-- status = 'active', so every source registered as 'pending_review' (1,504
-- of 1,505 at the time this was found) would sit there forever. This
-- migration and the query/scheduler changes alongside it are what actually
-- build the automation the spec already claimed existed.
--
-- shadow_reviewed_at marks a source as having already been through the
-- promote-or-flag decision once its 48h window closed. Without it, a source
-- that gets flagged as anomalous (and therefore stays 'pending_review')
-- would be re-flagged — and re-logged — on every subsequent review pass
-- forever, since nothing else distinguishes "not old enough yet" from
-- "reviewed and left pending on purpose."
ALTER TABLE source
    ADD COLUMN shadow_reviewed_at TIMESTAMPTZ;

-- The scheduler's hot-path index needs to admit 'pending_review' rows too,
-- now that SelectDueSources polls them during their shadow window — an
-- index whose partial predicate no longer matches the query's WHERE clause
-- silently falls back to a full scan, which docs/06's "the target state —
-- 85% of polls" cost model assumes never happens.
DROP INDEX source_due_idx;
CREATE INDEX source_due_idx ON source (next_poll_at)
WHERE status IN ('active', 'pending_review') AND legal_posture IN ('permitted', 'api_only');

-- Supports SelectShadowSourcesDueForReview: sources whose 48h window has
-- closed and haven't been reviewed yet. Tiny relative to source_due_idx (at
-- most a few rows at a time in steady state) but the table has 1,500+ rows
-- today and this query runs on every review pass.
CREATE INDEX source_shadow_review_idx ON source (created_at)
WHERE status = 'pending_review' AND shadow_reviewed_at IS NULL;
