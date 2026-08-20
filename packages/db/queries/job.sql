-- Job and job_group queries. See docs/08-dedup-identity.md section 3.1
-- (Stage 1, exact-match dedup) and infra/migrations/000006_job.up.sql.
--
-- Dedup Stage 1 is three independent exact-match checks, tried in this
-- order by the caller (apps/collector/internal/dedup): canonical_url_hash,
-- then (ats_platform, ats_job_id), then content_hash. Only the first two
-- are backed by a unique index (job_canonical_url_idx, job_ats_idx) —
-- content_hash alone is not, since the same posting text can legitimately
-- recur across unrelated jobs at different companies, so it is a lookup
-- key here but never an insert-time constraint.
--
-- None of these queries use `select *` / `returning *` on job: it is a
-- generated tsvector column (search_vector) pgx has no scan type for (OID
-- 3614), so returning it breaks every caller regardless of whether they
-- read it. The column list below is job's full column set minus that one.

-- name: FindJobByCanonicalURLHash :one
select
    id,
    job_group_id,
    company_id,
    primary_source_id,
    canonical_url,
    canonical_url_hash,
    ats_platform,
    ats_job_id,
    content_hash,
    title,
    normalized_title,
    description_html,
    description_text,
    description_stripped,
    requirements_text,
    apply_url,
    role_family,
    role_confidence,
    seniority,
    is_software,
    skills,
    tech_stack,
    location_raw,
    location_city,
    location_region,
    location_country,
    location_tier,
    work_mode,
    visa_sponsorship,
    paid,
    comp_min,
    comp_max,
    comp_currency,
    comp_period,
    comp_normalized_inr_month,
    comp_confidence,
    prestige_exception,
    status,
    posted_at,
    posted_at_estimated,
    deadline_at,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_ghost,
    ghost_reason,
    observation_count,
    embedding,
    embedding_version,
    simhash,
    raw_payload,
    created_at,
    updated_at,
    deleted_at
from job
where canonical_url_hash = sqlc.arg(canonical_url_hash)::bytea
    and deleted_at is NULL;

-- name: FindJobByATSID :one
select
    id,
    job_group_id,
    company_id,
    primary_source_id,
    canonical_url,
    canonical_url_hash,
    ats_platform,
    ats_job_id,
    content_hash,
    title,
    normalized_title,
    description_html,
    description_text,
    description_stripped,
    requirements_text,
    apply_url,
    role_family,
    role_confidence,
    seniority,
    is_software,
    skills,
    tech_stack,
    location_raw,
    location_city,
    location_region,
    location_country,
    location_tier,
    work_mode,
    visa_sponsorship,
    paid,
    comp_min,
    comp_max,
    comp_currency,
    comp_period,
    comp_normalized_inr_month,
    comp_confidence,
    prestige_exception,
    status,
    posted_at,
    posted_at_estimated,
    deadline_at,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_ghost,
    ghost_reason,
    observation_count,
    embedding,
    embedding_version,
    simhash,
    raw_payload,
    created_at,
    updated_at,
    deleted_at
from job
where ats_platform = sqlc.arg(ats_platform)::text
    and ats_job_id = sqlc.arg(ats_job_id)::text
    and deleted_at is NULL;

-- name: FindJobByContentHash :one
-- limit 1: content_hash is not unique by design (see header comment), so
-- this returns the most recently active match rather than an arbitrary one.
select
    id,
    job_group_id,
    company_id,
    primary_source_id,
    canonical_url,
    canonical_url_hash,
    ats_platform,
    ats_job_id,
    content_hash,
    title,
    normalized_title,
    description_html,
    description_text,
    description_stripped,
    requirements_text,
    apply_url,
    role_family,
    role_confidence,
    seniority,
    is_software,
    skills,
    tech_stack,
    location_raw,
    location_city,
    location_region,
    location_country,
    location_tier,
    work_mode,
    visa_sponsorship,
    paid,
    comp_min,
    comp_max,
    comp_currency,
    comp_period,
    comp_normalized_inr_month,
    comp_confidence,
    prestige_exception,
    status,
    posted_at,
    posted_at_estimated,
    deadline_at,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_ghost,
    ghost_reason,
    observation_count,
    embedding,
    embedding_version,
    simhash,
    raw_payload,
    created_at,
    updated_at,
    deleted_at
from job
where content_hash = sqlc.arg(content_hash)::bytea
    and deleted_at is NULL
order by last_seen_at desc
limit 1;

-- name: InsertJobGroup :one
insert into job_group (company_id)
values (sqlc.arg(company_id)::uuid)
returning *;

-- name: SetJobGroupRepresentative :exec
update job_group
set representative_job_id = sqlc.arg(job_id)::uuid
where id = sqlc.arg(id)::uuid;

-- name: TouchJobGroup :exec
-- Called when a re-observed posting attaches to an existing group instead
-- of creating a new one — bumps last_seen_at without touching member_count,
-- since it's the same job, not an additional one merged in.
update job_group
set last_seen_at = now()
where id = sqlc.arg(id)::uuid;

-- name: InsertJob :one
insert into job (
    job_group_id, company_id, primary_source_id,
    canonical_url, canonical_url_hash, ats_platform, ats_job_id, content_hash,
    title, normalized_title, description_html, description_text, description_stripped,
    requirements_text, apply_url,
    role_family, role_confidence, seniority, is_software, skills,
    location_raw, location_city, location_region, location_country,
    location_tier, work_mode, visa_sponsorship,
    paid, comp_min, comp_max, comp_currency, comp_period,
    comp_normalized_inr_month, comp_confidence,
    posted_at, posted_at_estimated, deadline_at,
    simhash, raw_payload
) values (
    sqlc.arg(job_group_id)::uuid, sqlc.arg(company_id)::uuid, sqlc.arg(primary_source_id)::uuid,
    sqlc.arg(canonical_url)::text, sqlc.arg(canonical_url_hash)::bytea,
    sqlc.narg(ats_platform)::text, sqlc.narg(ats_job_id)::text, sqlc.arg(content_hash)::bytea,
    sqlc.arg(title)::text, sqlc.arg(normalized_title)::text,
    sqlc.narg(description_html)::text, sqlc.narg(description_text)::text, sqlc.narg(description_stripped)::text,
    sqlc.narg(requirements_text)::text, sqlc.arg(apply_url)::text,
    sqlc.arg(role_family)::role_family, sqlc.arg(role_confidence)::real,
    sqlc.arg(seniority)::seniority, sqlc.arg(is_software)::boolean, sqlc.arg(skills)::text[],
    sqlc.narg(location_raw)::text, sqlc.narg(location_city)::text,
    sqlc.narg(location_region)::text, sqlc.narg(location_country)::bpchar,
    sqlc.narg(location_tier)::smallint, sqlc.arg(work_mode)::work_mode,
    sqlc.narg(visa_sponsorship)::boolean,
    sqlc.arg(paid)::paid_signal, sqlc.narg(comp_min)::numeric, sqlc.narg(comp_max)::numeric,
    sqlc.narg(comp_currency)::bpchar, sqlc.narg(comp_period)::text,
    sqlc.narg(comp_normalized_inr_month)::numeric, sqlc.narg(comp_confidence)::real,
    sqlc.narg(posted_at)::timestamptz, sqlc.arg(posted_at_estimated)::boolean,
    sqlc.narg(deadline_at)::timestamptz,
    sqlc.narg(simhash)::bigint, sqlc.arg(raw_payload)::jsonb
)
returning
    id,
    job_group_id,
    company_id,
    primary_source_id,
    canonical_url,
    canonical_url_hash,
    ats_platform,
    ats_job_id,
    content_hash,
    title,
    normalized_title,
    description_html,
    description_text,
    description_stripped,
    requirements_text,
    apply_url,
    role_family,
    role_confidence,
    seniority,
    is_software,
    skills,
    tech_stack,
    location_raw,
    location_city,
    location_region,
    location_country,
    location_tier,
    work_mode,
    visa_sponsorship,
    paid,
    comp_min,
    comp_max,
    comp_currency,
    comp_period,
    comp_normalized_inr_month,
    comp_confidence,
    prestige_exception,
    status,
    posted_at,
    posted_at_estimated,
    deadline_at,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_ghost,
    ghost_reason,
    observation_count,
    embedding,
    embedding_version,
    simhash,
    raw_payload,
    created_at,
    updated_at,
    deleted_at;

-- name: TouchJob :exec
-- A re-observation of an existing job: bump last_seen_at and
-- observation_count. Nothing else changes — Stage 1 dedup only recognizes
-- exact repeats, so there is no new content to merge in.
update job
set last_seen_at = now(), observation_count = observation_count + 1
where id = sqlc.arg(id)::uuid;

-- name: GetJobByID :one
select
    id,
    job_group_id,
    company_id,
    primary_source_id,
    canonical_url,
    canonical_url_hash,
    ats_platform,
    ats_job_id,
    content_hash,
    title,
    normalized_title,
    description_html,
    description_text,
    description_stripped,
    requirements_text,
    apply_url,
    role_family,
    role_confidence,
    seniority,
    is_software,
    skills,
    tech_stack,
    location_raw,
    location_city,
    location_region,
    location_country,
    location_tier,
    work_mode,
    visa_sponsorship,
    paid,
    comp_min,
    comp_max,
    comp_currency,
    comp_period,
    comp_normalized_inr_month,
    comp_confidence,
    prestige_exception,
    status,
    posted_at,
    posted_at_estimated,
    deadline_at,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_ghost,
    ghost_reason,
    observation_count,
    embedding,
    embedding_version,
    simhash,
    raw_payload,
    created_at,
    updated_at,
    deleted_at
from job
where id = sqlc.arg(id)::uuid;
