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
-- established: in_scope and status='open' are not optional (ADR-020 widened
-- what's in scope beyond pure software — product/consulting/business roles
-- now admit too — but an unmatched/excluded job never shows regardless), the
-- rest are optional narg()s that pass through when NULL.
--
-- seniority_filter_enabled (000012) is one of those hard-eligibility checks,
-- not an optional narg — found missing here 2026-10-21 when the dashboard
-- showed senior/staff roles wall to wall despite the user's own
-- target_seniority being {internship, new_grad} and the toggle being on.
-- The notification path enforced this correctly (a senior role never got
-- pushed); this feed query, which this file's own header comment already
-- claimed matched that pattern, simply never implemented it — the only
-- seniority filtering here was the optional `seniorities` narg, which
-- defaults to showing every level when the caller doesn't set it. Same
-- "'unknown' admitted either way" reasoning as notification.sql: a role
-- whose seniority classification failed must not read as "classified and
-- excluded."
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
inner join company as c on j.company_id = c.id
left join job_score as js on j.id = js.job_id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
left join user_job_state as ujs on jg.id = ujs.job_group_id
    and ujs.user_id = sqlc.arg(user_id)::uuid
inner join user_profile as up on up.user_id = sqlc.arg(user_id)::uuid
where j.status = 'open'
    and j.in_scope = TRUE
    and (j.paid = 'paid' or (j.paid = 'unknown' and coalesce(js.priority, 0) >= 92))
    and (
        not up.seniority_filter_enabled
        or j.seniority = any(up.target_seniority)
        or j.seniority = 'unknown'
    )
    and (
        sqlc.narg(role_families)::text [] is NULL
        or j.role_family = any(sqlc.narg(role_families)::text []::role_family [])
    )
    and (
        sqlc.narg(seniorities)::text [] is NULL
        or j.seniority = any(sqlc.narg(seniorities)::text []::seniority [])
    )
    and (
        sqlc.narg(location_tiers)::smallint [] is NULL
        or j.location_tier = any(sqlc.narg(location_tiers)::smallint [])
    )
    and (
        sqlc.narg(work_modes)::text [] is NULL
        or j.work_mode = any(sqlc.narg(work_modes)::text []::work_mode [])
    )
    and (
        sqlc.narg(company_ids)::uuid [] is NULL
        or c.id = any(sqlc.narg(company_ids)::uuid [])
    )
    and (
        sqlc.narg(min_priority)::smallint is NULL
        or coalesce(js.priority, 0) >= sqlc.narg(min_priority)::smallint
    )
    and (
        sqlc.narg(cursor_priority)::smallint is NULL
        or coalesce(js.priority, 0) < sqlc.narg(cursor_priority)::smallint
        or (
            coalesce(js.priority, 0) = sqlc.narg(cursor_priority)::smallint
            and j.id > sqlc.narg(cursor_job_id)::uuid
        )
    )
order by coalesce(js.priority, 0) desc, j.id asc
limit sqlc.arg(limit_)::int;
