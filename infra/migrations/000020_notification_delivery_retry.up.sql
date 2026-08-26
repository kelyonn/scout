-- Fixes a real, verified permanent-loss bug in apps/notifier/internal/deliver:
-- GetUndeliveredNotifications excludes any notification with a
-- notification_delivery row at all (`nd.id is NULL`), including one with
-- status='failed'. There was no code path anywhere that ever revisited a
-- failed delivery. Confirmed live 2026-08-23: the one real notification this
-- system has ever generated (today's digest) failed with Telegram's "Bad
-- Request: chat not found" (a stale placeholder chat_id) and was silently,
-- permanently dropped on the first attempt — not a rare edge case, the
-- default outcome of any transient Telegram failure at all, including one
-- Scout's own compliance posture (rule 6, "every AI call has a degraded
-- mode") would otherwise expect to just retry.
--
-- next_attempt_at is the backoff clock: NULL means "not currently backing
-- off" (either never attempted, or a terminal outcome), set to a future
-- timestamp by a failed send that still has attempts left. deliver.go's
-- retryBackoff computes the delay; this column just stores where it lands.
ALTER TABLE notification_delivery
ADD COLUMN next_attempt_at TIMESTAMPTZ;
