-- The application-tracking pipeline — docs/04-api-design.md section 4.1's
-- POST /v1/jobs/{group_id}/state and section 4.6's GET /v1/applications.
-- See infra/migrations/000014_user_job_state.up.sql.

-- name: GetUserJobState :one
-- No row means the caller's own default applies: 'new', never seen.
-- apps/api/internal/jobs's state handler uses this to validate a
-- transition against the explicit state machine before writing anything.
select *
from user_job_state
where user_id = sqlc.arg(user_id)::uuid
    and job_group_id = sqlc.arg(job_group_id)::uuid;

-- name: UpsertUserJobState :one
-- First transition for a (user, job) pair inserts; every later one updates
-- in place — user_job_state is current-state-only, docs/03's
-- user_job_state_event table (InsertUserJobStateEvent below) is the
-- append-only history. applied_at is set once, on the first transition
-- into 'applied', and never overwritten by a later re-save of the same
-- state — coalesce(applied_at, ...) on conflict preserves whichever value
-- is already there.
insert into user_job_state (
    user_id, job_group_id, state, state_changed_at, found_elsewhere_first,
    notes, rating, applied_at
) values (
    sqlc.arg(user_id)::uuid, sqlc.arg(job_group_id)::uuid,
    sqlc.arg(state)::application_state, now(),
    sqlc.arg(found_elsewhere_first)::boolean, sqlc.narg(notes)::text,
    sqlc.narg(rating)::smallint, sqlc.narg(applied_at)::timestamptz
)
on conflict (user_id, job_group_id) do update set
    state = excluded.state,
    state_changed_at = now(),
    found_elsewhere_first = excluded.found_elsewhere_first or user_job_state.found_elsewhere_first,
    notes = coalesce(excluded.notes, user_job_state.notes),
    rating = coalesce(excluded.rating, user_job_state.rating),
    applied_at = coalesce(user_job_state.applied_at, excluded.applied_at),
    updated_at = now()
returning *;

-- name: InsertUserJobStateEvent :exec
insert into user_job_state_event (user_id, job_group_id, from_state, to_state)
values (
    sqlc.arg(user_id)::uuid, sqlc.arg(job_group_id)::uuid,
    sqlc.narg(from_state)::application_state, sqlc.arg(to_state)::application_state
);

-- name: SelectApplications :many
-- The Pipeline/Saved/Applied views (docs/12-frontend-ux.md sections 4.5
-- and 3's IA). Driven from user_job_state, not job_group — unlike
-- SelectJobFeed (packages/db/queries/feed.sql), a job the user already
-- applied to must stay visible here even after job.status leaves 'open'
-- (a closed listing doesn't erase an in-flight application), so there is
-- deliberately no `j.status = 'open'` filter.
select
    jg.id as job_group_id,
    j.id as job_id,
    c.id as company_id,
    c.canonical_name as company_name,
    j.title,
    j.role_family::text as role_family,
    j.seniority::text as seniority,
    j.location_city,
    j.location_country,
    j.location_tier,
    j.work_mode::text as work_mode,
    j.status::text as job_status,
    j.paid::text as paid,
    j.comp_normalized_inr_month,
    j.skills,
    j.posted_at,
    j.deadline_at,
    j.apply_url,
    coalesce(js.priority, 0)::smallint as priority,
    ujs.state::text as state,
    ujs.state_changed_at,
    ujs.found_elsewhere_first,
    ujs.notes,
    ujs.rating,
    ujs.applied_at
from user_job_state as ujs
inner join job_group as jg on ujs.job_group_id = jg.id
inner join job as j on j.id = jg.representative_job_id
inner join company as c on c.id = j.company_id
left join job_score as js on js.job_id = j.id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
where ujs.user_id = sqlc.arg(user_id)::uuid
    and (
        sqlc.narg(states)::text[] is null
        or ujs.state = any(sqlc.narg(states)::text[]::application_state[])
    )
order by ujs.state_changed_at desc;
