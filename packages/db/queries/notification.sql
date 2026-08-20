-- Notification trigger evaluation and delivery queries. See
-- docs/11-notifications.md and infra/migrations/000009_notification.up.sql.
--
-- Split matches apps/notifier's own package split: trigger.go selects
-- unnotified job_group rows and evaluates bengaluru_match/high_score,
-- inserting into `notification`; deliver.go separately picks up
-- `notification` rows with no `notification_delivery` row yet — that
-- absence of a delivery row IS the "queued" state (quiet hours, budget),
-- rather than a separate status column tracking it.

-- name: SelectUnnotifiedJobGroups :many
-- Eligibility (docs/11 section 8 step 2), minus excluded_keywords —
-- P1 checks excluded_companies (a plain array-containment test) but not
-- keyword matching against the title/description, which needs a pattern
-- language this pass doesn't build; user_profile.excluded_keywords defaults
-- empty, so nothing is lost until a user actually populates it.
--
-- The paid clause is docs/07 section 7's actual rule, not a bare
-- `paid = 'paid'`: most ATS platforms (Greenhouse among them) expose no
-- compensation field at all, so nearly every real posting lands
-- `paid = 'unknown'` — a bare equality check filtered out the entire
-- market before a single trigger was ever evaluated, found live against
-- Stripe's board where 0 of 561 postings had paid = 'paid'. docs/07's own
-- "company history" and "size/stage bucket" clauses for admitting
-- `unknown` need a company registry this pass doesn't have; the priority
-- clause is the one input P1 actually has, so it is the one implemented.
-- Explicit `unpaid` is excluded either way.
--
-- seniority_filter_enabled (000012) makes target_seniority a hard gate
-- here, not just overall_match's soft seniority_fit term — a senior/staff
-- role can still clear the priority threshold on company_quality/
-- deadline_urgency/freshness alone, so the soft signal isn't enough on its
-- own for a user who genuinely never wants to see those roles surfaced.
-- Same excluded_companies pattern: array-containment against user_profile,
-- toggle defaults on, flip it off once target_seniority itself widens.
--
-- 'unknown' seniority (classification found no signal) is admitted either
-- way, matching the paid clause's own admit-unknown-rather-than-hide
-- reasoning a few lines up — a role's seniority going undetected must not
-- read as "detected and excluded."
--
-- The source.status <> 'pending_review' clause is docs/05 section 13 step
-- 5's "shadow run for 48 hours — collect but do not notify," enforced here
-- rather than by touching apps/notifier/internal/trigger's
-- suppressNotifications parameter (AGENTS.md rule 3's existing guard, and
-- deliberately left reserved for backfills/rescores — see that package's own
-- comment). Filtering it out of this SELECT instead of marking it notified
-- means an un-promoted group is simply reconsidered on a later tick, the
-- correct behavior for "not yet" rather than backfill's "never" — and it
-- reuses job.primary_source_id, which every job already has, rather than
-- adding new plumbing. A job_group can in principle merge jobs from more
-- than one source, but only representative_job_id's source is checked here:
-- if a shadow source's posting merges into a group an active source already
-- surfaced, that group was already evaluated (and marked notified) the
-- moment the active source's job arrived, so there is nothing left to
-- suppress for it.
select
    jg.id as job_group_id,
    j.id as job_id,
    j.company_id,
    c.canonical_name as company_name,
    j.title,
    j.apply_url,
    j.location_tier,
    j.location_city,
    j.work_mode,
    j.paid,
    j.comp_normalized_inr_month,
    j.posted_at,
    j.posted_at_estimated,
    j.seniority,
    -- Cast to text[]: pgx has no default codec for an ARRAY of a custom
    -- enum (packages/db/queries/user.sql's GetUserProfile hit the same
    -- "cannot scan unknown type" failure first — see its comment), so the
    -- native seniority[] type fails to scan here too without this cast.
    up.target_seniority::text[] as target_seniority,
    js.priority
from job_group as jg
inner join job as j on jg.representative_job_id = j.id
inner join company as c on j.company_id = c.id
inner join source as src on j.primary_source_id = src.id
inner join job_score as js on j.id = js.job_id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
inner join user_profile as up on sqlc.arg(user_id)::uuid = up.user_id
where jg.notified_at is NULL
    and j.is_software = TRUE
    and (j.paid = 'paid' or (j.paid = 'unknown' and js.priority >= 92))
    and j.status = 'open'
    and src.status <> 'pending_review'
    and not (j.company_id = any(up.excluded_companies))
    and (not up.seniority_filter_enabled or j.seniority = any(up.target_seniority) or j.seniority = 'unknown')
order by js.priority desc
limit sqlc.arg(limit_)::int;

-- name: MarkJobGroupNotified :exec
-- Set once a trigger decision (fire or no-fire) has been made for this
-- group, so it is not re-evaluated every tick. Rescoring an already-decided
-- group is a P2+ feature (weight-version changes trigger a rescore, which
-- would need to clear this) — not built here; see AGENTS.md rule 3's
-- suppress_notifications guard, which apps/notifier/internal/trigger
-- threads through independent of this column.
update job_group
set notified_at = now()
where id = sqlc.arg(id)::uuid;

-- name: InsertNotification :one
-- ON CONFLICT DO NOTHING rather than catching a unique-violation error:
-- both satisfy "already notified, skip" (AGENTS.md rule 2,
-- notification_dedup_idx), but a real constraint-violation error inside a
-- transaction poisons it until rollback, which matters here since a batch
-- of trigger evaluations runs inside one transaction. Returns zero rows on
-- conflict, which the caller (apps/notifier/internal/trigger) reads via
-- pgx.ErrNoRows — that is the "already notified" signal, not an error.
--
-- scheduled_for defaults to now() (coalesce over sqlc.narg) for every
-- existing caller that doesn't set it — INSTANT urgency delivers as soon as
-- GetUndeliveredNotifications sees it, same as before this column was
-- threaded through. docs/11 section 3's BATCHED class ("accumulated and
-- delivered hourly on the hour") is what actually sets it, to the next
-- hour boundary — see apps/notifier/internal/trigger's nextHourBoundary.
insert into notification (user_id, job_group_id, trigger, urgency, payload, priority_at_send, scheduled_for)
values (
    sqlc.arg(user_id)::uuid, sqlc.arg(job_group_id)::uuid, sqlc.arg(trigger)::notification_trigger,
    sqlc.arg(urgency)::text, sqlc.arg(payload)::jsonb, sqlc.arg(priority_at_send)::smallint,
    coalesce(sqlc.narg(scheduled_for)::timestamptz, now())
)
on conflict (user_id, job_group_id, trigger) where job_group_id is not NULL do nothing
returning id;

-- name: GetUndeliveredNotifications :many
-- A notification with no notification_delivery row yet is, by
-- construction, still queued — see this file's header comment.
-- scheduled_for <= now() is what actually holds a BATCHED notification
-- back until its hour boundary — INSTANT rows have scheduled_for = their
-- own created_at (via InsertNotification's coalesce-to-now default), so
-- this clause is a no-op for them.
--
-- user_id filter added here rather than left to the caller's own
-- `if n.UserID != userID { continue }` (which apps/notifier/internal/deliver
-- used to do entirely in Go): ADR-015 means this never mattered for a
-- production result (there is exactly one user), but it was a real gap
-- for test isolation — this query previously returned every user's
-- undelivered notifications regardless of the userID argument, so two
-- packages' tests running concurrently against the shared local Postgres
-- (go test ./... parallelizes across packages) and reusing the same real
-- sole app_user row could see each other's fixtures, corrupting budget
-- counts. Same root-cause fix as the flaky scheduler cross-package test
-- pollution mentioned in HANDOFF.md, applied here.
select
    n.id,
    n.user_id,
    n.job_group_id,
    n.trigger,
    n.urgency,
    n.payload,
    n.priority_at_send,
    n.created_at
from notification as n
left join notification_delivery as nd on n.id = nd.notification_id
where n.user_id = sqlc.arg(user_id)::uuid
    and nd.id is NULL
    and n.scheduled_for <= now()
order by n.created_at asc
limit sqlc.arg(limit_)::int;

-- name: SelectDeadlineReminderCandidates :many
-- docs/11 section 2's deadline_approaching trigger, split at the schema
-- level into deadline_t72h/deadline_t24h (infra/migrations/000016 — see
-- its own comment for why two trigger values rather than one). "Tracked"
-- reuses digest.sql's SelectDigestClosingSoon filter verbatim (any
-- non-terminal user_job_state) rather than duplicating a different
-- definition of "saved."
--
-- Returns every candidate inside the 72h window on every sweep — no
-- "already reminded" column here, unlike MarkJobGroupNotified's job_group
-- gate. That gate exists because SelectUnnotifiedJobGroups scans the whole
-- unnotified backlog and needs to shrink it permanently; this query is
-- already scoped to a small tracked set (saved/applied jobs with a
-- deadline), so re-checking it every tick costs nothing, and
-- notification_dedup_idx via InsertNotification's ON CONFLICT DO NOTHING is
-- what actually decides whether either reminder is new
-- (apps/notifier/internal/trigger's DeadlineSweeper attempts both every
-- sweep and lets the insert's row count answer that).
select
    jg.id as job_group_id,
    c.canonical_name as company_name,
    j.title,
    j.apply_url,
    j.location_city,
    j.comp_normalized_inr_month,
    j.deadline_at
from user_job_state as ujs
inner join job_group as jg on jg.id = ujs.job_group_id
inner join job as j on j.id = jg.representative_job_id
inner join company as c on c.id = j.company_id
where ujs.user_id = sqlc.arg(user_id)::uuid
    and ujs.state not in ('rejected', 'withdrawn', 'dismissed', 'accepted', 'expired')
    and j.deadline_at is not null
    and j.deadline_at > now()
    and j.deadline_at <= now() + interval '72 hours';

-- name: GetTelegramChannel :one
select *
from notification_channel
where user_id = sqlc.arg(user_id)::uuid
    and kind = 'telegram'
    and enabled = TRUE;

-- name: UpsertTelegramChannel :one
-- Re-running the onboarding flow updates the existing row in place rather
-- than accumulating duplicates — notification_channel_single_idx (docs/03
-- section 7) allows only one telegram channel per user, so this is the
-- write side of that constraint.
insert into notification_channel (user_id, kind, config, enabled, verified_at)
values (sqlc.arg(user_id)::uuid, 'telegram', sqlc.arg(config)::jsonb, TRUE, now())
on conflict (user_id, kind) where kind in ('telegram', 'email', 'discord')
do update set config = excluded.config, enabled = TRUE, verified_at = now()
returning *;

-- name: InsertNotificationDelivery :one
insert into notification_delivery (
    notification_id, channel_id, status, attempts, error, sent_at, latency_ms, provider_msg_id
) values (
    sqlc.arg(notification_id)::uuid, sqlc.arg(channel_id)::uuid, sqlc.arg(status)::text,
    sqlc.arg(attempts)::smallint, sqlc.narg(error)::text, sqlc.narg(sent_at)::timestamptz,
    sqlc.narg(latency_ms)::int, sqlc.narg(provider_msg_id)::text
)
returning *;

-- name: CountDeliveriesSince :one
-- The hourly/daily budget check (docs/11 section 4) — counts successful
-- sends only ('sent' or 'delivered'), not attempts, since a failed send
-- never reached the user and must not count against their budget.
select count(*)
from notification_delivery as nd
inner join notification as n on nd.notification_id = n.id
where n.user_id = sqlc.arg(user_id)::uuid
    and nd.status in ('sent', 'delivered')
    and nd.sent_at >= sqlc.arg(since)::timestamptz;

-- name: CountDeliveriesForTriggerSince :one
select count(*)
from notification_delivery as nd
inner join notification as n on nd.notification_id = n.id
where n.user_id = sqlc.arg(user_id)::uuid
    and n.trigger = sqlc.arg(trigger)::notification_trigger
    and nd.status in ('sent', 'delivered')
    and nd.sent_at >= sqlc.arg(since)::timestamptz;
