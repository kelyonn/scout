-- Fixes a real bug found live: SelectShadowSourcesDueForReview judged a
-- source's 48h shadow window using created_at, which silently assumed the
-- collector runs continuously. ADR-018 (laptop-only hosting) made that
-- assumption false — the collector now runs only when the user chooses to
-- start it, so a source seeded days ago and never actually polled until
-- today reads as "48h have passed" the instant the first live run starts,
-- even though it has had zero real poll attempts. Verified live on
-- 2026-08-23: the very first live run this stack ever did immediately
-- mass-flagged ~175 of a 200-source shadow-review batch as "likely broken"
-- for exactly this reason — every one of them had total_polls = 0.
--
-- first_polled_at anchors the window to when a source actually started
-- being attempted, not when its row was created. NULL until the first real
-- poll (success or failure — an attempt is an attempt), set once and never
-- again, via UpdateSourceAfterPoll's own coalesce.
ALTER TABLE source
    ADD COLUMN first_polled_at TIMESTAMPTZ;

-- Same partial-predicate lesson 000015's own comment already states: an
-- index whose predicate doesn't match the query's WHERE clause silently
-- falls back to a full scan. SelectShadowSourcesDueForReview now filters on
-- first_polled_at, not created_at, so the index has to move with it.
DROP INDEX source_shadow_review_idx;
CREATE INDEX source_shadow_review_idx ON source (first_polled_at)
WHERE status = 'pending_review' AND shadow_reviewed_at IS NULL AND first_polled_at IS NOT NULL;

-- Remediation for the incident this migration's own comment describes: the
-- ~175 sources false-flagged today never had a real shadow run at all
-- (total_polls = 0 the whole time) — flagging them was not a real verdict,
-- it was this bug. shadow_reviewed_at is cleared so they requeue for a real
-- review once first_polled_at is actually set by a subsequent poll, and the
-- specific erroneous line is stripped from notes rather than the whole
-- column, since some of these rows carry real curation notes above it
-- (e.g. "Verified live ... via Phase M sweep") that must not be lost.
UPDATE source
SET shadow_reviewed_at = NULL,
    notes = NULLIF(
        regexp_replace(
            notes,
            E'\n?shadow review: 0 successful polls of 0 attempted in 48h — adapter or URL likely broken',
            '',
            'g'
        ),
        ''
    )
WHERE status = 'pending_review'
    AND total_polls = 0
    AND notes LIKE '%shadow review: 0 successful polls of 0 attempted in 48h%';
