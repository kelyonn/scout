-- GET /v1/search — docs/04-api-design.md section 4.2. Keyword mode only:
-- `mode=semantic`/`hybrid` need a query embedding, which means an
-- embedding-service round trip apps/brain (Python) currently owns, and
-- ADR-001 forbids Go calling Python synchronously. The Go API degrades
-- semantic/hybrid requests to keyword rather than erroring — see
-- apps/api/internal/search's own comment for the exact fallback — so this
-- query only ever needs to serve keyword mode.
--
-- job.search_vector (infra/migrations/000006_job.up.sql) is a generated,
-- GIN-indexed tsvector already weighted title > location > description;
-- company name isn't in it (search_vector is job-scoped), so a company
-- match is a second, explicitly OR'd condition rather than folded into
-- the same tsquery.

-- name: SearchJobs :many
select
    jg.id as job_group_id,
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
    j.deadline_at,
    j.first_seen_at,
    j.apply_url,
    coalesce(js.priority, 0)::smallint as priority,
    coalesce(ujs.state::text, 'new')::text as state,
    ts_rank(j.search_vector, websearch_to_tsquery('english', sqlc.arg(query)::text)) as rank
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
    and (
        j.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
        or c.canonical_name ilike '%' || sqlc.arg(query)::text || '%'
    )
order by rank desc, coalesce(js.priority, 0) desc
limit sqlc.arg(limit_)::int;
