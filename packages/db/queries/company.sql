-- Companies list/detail — docs/04-api-design.md section 4.5's
-- GET /v1/companies and GET /v1/companies/{id}. "Jobs at this company"
-- (docs/04's GET /v1/companies/{id}/jobs) doesn't need a query of its
-- own: GET /v1/jobs already accepts company_id (packages/db/queries/
-- feed.sql's SelectJobFeed, wired at the HTTP layer in
-- apps/api/internal/jobs/handler.go's List) and that's the same
-- ranked-feed shape every other job list in this API already returns.
--
-- No cursor pagination here, unlike the job feed. The company table sits
-- at 1,506 rows today and only 33 have any job at all — this is a slow-
-- moving registry populated by seeding and enrichment jobs, not a feed
-- receiving inserts under live traffic, so the specific failure mode
-- offset pagination has on the job feed (items shifting between pages
-- while scrolling) doesn't apply here the same way. A single bounded
-- page is the honest match for the data's actual size; revisit if the
-- registry ever grows enough that this stops being true.

-- name: SelectCompanies :many
select
    c.id,
    c.canonical_name,
    c.logo_url,
    c.company_type::text as company_type,
    c.size_bucket,
    c.stage,
    c.industries,
    c.quality_score,
    c.hq_city,
    c.hq_country,
    count(j.id) filter (where j.status = 'open' and j.is_software = TRUE) as open_job_count
from company as c
inner join job as j on c.id = j.company_id
where c.deleted_at is NULL
    and (
        sqlc.narg(query)::text is NULL
        or c.normalized_name ilike '%' || sqlc.narg(query)::text || '%'
    )
group by c.id
having count(j.id) filter (where j.status = 'open' and j.is_software = TRUE) > 0
order by open_job_count desc, c.quality_score desc nulls last, c.canonical_name asc
limit sqlc.arg(limit_)::int;

-- name: GetCompanyByID :one
select
    c.id,
    c.canonical_name,
    c.legal_name,
    c.description,
    c.logo_url,
    c.website_url,
    c.linkedin_url,
    c.github_org,
    c.company_type::text as company_type,
    c.size_bucket,
    c.founded_year,
    c.stage,
    c.industries,
    c.tech_stack,
    c.hq_city,
    c.hq_country,
    c.quality_score,
    c.engineering_score,
    count(j.id) filter (where j.status = 'open' and j.is_software = TRUE) as open_job_count
from company as c
left join job as j on c.id = j.company_id
where c.id = sqlc.arg(id)::uuid
    and c.deleted_at is NULL
group by c.id;
