-- ADR-020: is_software now means literally that — false for the
-- product.*/consulting.*/business.* families added in 000024, not merely
-- "excluded from the feed." Before this migration the two concepts were
-- silently the same column, which worked only because advocacy.* (not
-- literally software engineering either) also set is_software=true and
-- nothing else ever needed telling "software" apart from "admitted."
--
-- in_scope is the new, honest "show this to the user" gate: true for any
-- role_family Tier 0/1/2 resolved to other than the genuine catch-all/
-- unmatched state (swe.other), false otherwise. Every query that used to
-- hard-filter on is_software now filters on in_scope instead.
--
-- Backfilling from role_family directly, not just copying is_software,
-- also fixes a real pre-existing gap this migration's own investigation
-- found: apps/brain's Tier 1 (embed_consumer.py's embedding-based
-- reclassification) revises role_family on a swe.other row without ever
-- touching is_software, so any job Tier 1 had already correctly promoted
-- out of swe.other before today was still being silently excluded from
-- the feed. This backfill corrects those rows too, not just the new
-- families' rows.
ALTER TABLE job ADD COLUMN in_scope BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE job
SET in_scope = TRUE
WHERE
    role_family <> 'swe.other'::role_family
    OR is_software = TRUE;

DROP INDEX job_feed_idx;
CREATE INDEX job_feed_idx ON job (posted_at DESC)
WHERE status = 'open' AND in_scope = TRUE AND paid = 'paid' AND deleted_at IS NULL;
