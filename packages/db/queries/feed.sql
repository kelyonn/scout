-- The ranked job feed — docs/04-api-design.md section 3/4.1's
-- GET /v1/jobs. Keyset (cursor) pagination on (priority DESC, job_id ASC
-- as tiebreaker), never offset — the doc's own stated reason: offset
-- pagination on a feed that receives new rows continuously causes items
-- to shift between pages, which for a job feed means genuinely missing
-- postings while scrolling, the exact failure this project exists to
-- prevent.
--
-- Filters follow the same hard-eligibility pattern
-- packages/db/queries/notification.sql's SelectUnnotifiedJobGroups
-- established: is_software and status='open' are not optional (this
-- product is software internships/new-grad roles, full stop), the rest
-- are optional narg()s that pass through when NULL.
--
-- role_family/seniority/work_mode filters are cast through ::text[] then
-- ::role_family[]/::seniority[]/::work_mode[] rather than bound directly
-- as native enum arrays — the same pgx-has-no-codec-for-a-custom-enum-
-- array workaround GetUserProfile's own comment documents; this is a
-- WHERE-clause bind, not a scanned result, so the two-step cast is enough
-- to sidestep it without needing the caller to re-parse anything.

-- name: SelectJobFeed :many
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
    j.paid::text as paid,
    j.comp_normalized_inr_month,
    j.skills,
    j.posted_at,
    j.posted_at_estimated,
    j.deadline_at,
    j.first_seen_at,
    j.apply_url,
    coalesce(js.priority, 0)::smallint as priority,
    js.overall_match,
    js.explanation,
    coalesce(ujs.state::text, 'new')::text as state
from job_group as jg
inner join job as j on jg.representative_job_id = j.id
inner join company as c on c.id = j.company_id
left join job_score as js on js.job_id = j.id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
left join user_job_state as ujs on ujs.job_group_id = jg.id
    and ujs.user_id = sqlc.arg(user_id)::uuid
where j.status = 'open'
    and j.is_software = TRUE
    and (j.paid = 'paid' or (j.paid = 'unknown' and coalesce(js.priority, 0) >= 92))
    and (
        sqlc.narg(role_families)::text[] is null
        or j.role_family = any(sqlc.narg(role_families)::text[]::role_family[])
    )
    and (
        sqlc.narg(seniorities)::text[] is null
        or j.seniority = any(sqlc.narg(seniorities)::text[]::seniority[])
    )
    and (
        sqlc.narg(location_tiers)::smallint[] is null
        or j.location_tier = any(sqlc.narg(location_tiers)::smallint[])
    )
    and (
        sqlc.narg(work_modes)::text[] is null
        or j.work_mode = any(sqlc.narg(work_modes)::text[]::work_mode[])
    )
    and (
        sqlc.narg(company_ids)::uuid[] is null
        or c.id = any(sqlc.narg(company_ids)::uuid[])
    )
    and (
        sqlc.narg(min_priority)::smallint is null
        or coalesce(js.priority, 0) >= sqlc.narg(min_priority)::smallint
    )
    and (
        sqlc.narg(cursor_priority)::smallint is null
        or coalesce(js.priority, 0) < sqlc.narg(cursor_priority)::smallint
        or (
            coalesce(js.priority, 0) = sqlc.narg(cursor_priority)::smallint
            and j.id > sqlc.narg(cursor_job_id)::uuid
        )
    )
order by coalesce(js.priority, 0) desc, j.id asc
limit sqlc.arg(limit_)::int;
