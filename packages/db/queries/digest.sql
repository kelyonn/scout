-- The daily digest — docs/11-notifications.md section 6.5, docs/19-roadmap.md's
-- P3 "Daily digest at 08:00 IST to Telegram." Every query here is scoped to
-- the sole user (ADR-015) and takes an explicit `since`/window boundary
-- computed in Go (apps/notifier/internal/digest), not "the last N days"
-- inline in SQL, so the 08:00 IST boundary logic lives in exactly one place.

-- name: DigestAlreadySentSince :one
-- Idempotency check: has today's digest already gone out. Read-then-write
-- rather than a unique index (notification_dedup_idx doesn't apply — it's
-- scoped to job_group_id, and a digest has none) — safe here because the
-- notifier is a single sequential loop, never concurrent with itself.
select exists(
    select 1 from notification
    where user_id = sqlc.arg(user_id)::uuid
        and trigger = 'digest'
        and created_at >= sqlc.arg(since)::timestamptz
) as sent;

-- name: InsertDigestNotification :one
-- job_group_id is NULL — a digest summarizes many jobs, not one — which is
-- exactly why this is a separate query from InsertNotification rather
-- than that query's job_group_id made nullable: every other caller of
-- InsertNotification relies on it being required.
insert into notification (user_id, job_group_id, trigger, urgency, payload, priority_at_send)
values (sqlc.arg(user_id)::uuid, NULL, 'digest', 'digest', sqlc.arg(payload)::jsonb, NULL)
returning id;

-- name: SelectDigestOvernightJobs :many
-- Same eligibility filter as SelectJobFeed (packages/db/queries/feed.sql):
-- is_software, open, paid-or-high-confidence-unknown. `since` is the
-- previous digest's 08:00 IST boundary, computed in Go.
select
    jg.id as job_group_id,
    c.canonical_name as company_name,
    j.title,
    j.location_city,
    j.work_mode::text as work_mode,
    j.comp_normalized_inr_month,
    coalesce(js.priority, 0)::smallint as priority
from job_group as jg
inner join job as j on jg.representative_job_id = j.id
inner join company as c on c.id = j.company_id
left join job_score as js on js.job_id = j.id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
where j.status = 'open'
    and j.is_software = TRUE
    and (j.paid = 'paid' or (j.paid = 'unknown' and coalesce(js.priority, 0) >= 92))
    and jg.first_seen_at >= sqlc.arg(since)::timestamptz
order by coalesce(js.priority, 0) desc
limit sqlc.arg(limit_)::int;

-- name: CountDigestEligibleJobsSince :one
select count(*) from job_group as jg
inner join job as j on jg.representative_job_id = j.id
where j.status = 'open'
    and j.is_software = TRUE
    and (j.paid = 'paid' or j.paid = 'unknown')
    and jg.first_seen_at >= sqlc.arg(since)::timestamptz;

-- name: SelectDigestClosingSoon :many
-- Tracked jobs (user_job_state, any non-terminal state) with a deadline in
-- the near future — docs/11 section 6.5's "Closing soon" section.
select
    jg.id as job_group_id, c.canonical_name as company_name, j.title, j.deadline_at
from user_job_state as ujs
inner join job_group as jg on jg.id = ujs.job_group_id
inner join job as j on j.id = jg.representative_job_id
inner join company as c on c.id = j.company_id
where ujs.user_id = sqlc.arg(user_id)::uuid
    and ujs.state not in ('rejected', 'withdrawn', 'dismissed', 'accepted', 'expired')
    and j.deadline_at is not null
    and j.deadline_at between sqlc.arg(after)::timestamptz and sqlc.arg(before)::timestamptz
order by j.deadline_at asc
limit sqlc.arg(limit_)::int;

-- name: SelectDigestWeeklyAppliedCount :one
select count(*) from user_job_state_event
where user_id = sqlc.arg(user_id)::uuid
    and to_state = 'applied'
    and occurred_at >= sqlc.arg(since)::timestamptz;

-- name: SelectDigestPipelineCounts :one
select
    count(*) filter (where state = 'interviewing')::int as interviewing_count,
    count(*) filter (where state in ('applied', 'screening'))::int as pending_count
from user_job_state
where user_id = sqlc.arg(user_id)::uuid;
