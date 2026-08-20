-- Job detail — docs/04-api-design.md section 4.1's GET /v1/jobs/{group_id}.
-- Unlike SelectJobFeed (packages/db/queries/feed.sql), this returns every
-- job_score subscore, not a trimmed feed-card subset — docs/12-frontend-
-- ux.md section 4.3's own rule: "All thirteen scores are shown, always. A
-- composite number without its components is unfalsifiable."

-- name: SelectJobDetail :one
select
    jg.id as job_group_id,
    jg.member_count,
    jg.first_seen_at as group_first_seen_at,
    j.id as job_id,
    j.title,
    j.description_text,
    j.description_html,
    j.requirements_text,
    j.ai_summary,
    j.apply_url,
    j.canonical_url,
    j.role_family::text as role_family,
    j.seniority::text as seniority,
    j.location_raw,
    j.location_city,
    j.location_country,
    j.location_tier,
    j.work_mode::text as work_mode,
    j.visa_sponsorship,
    j.paid::text as paid,
    j.comp_min,
    j.comp_max,
    j.comp_currency,
    j.comp_normalized_inr_month,
    j.skills,
    j.tech_stack,
    j.posted_at,
    j.posted_at_estimated,
    j.deadline_at,
    c.id as company_id,
    c.canonical_name as company_name,
    c.description as company_description,
    c.website_url as company_website_url,
    c.size_bucket as company_size_bucket,
    c.stage as company_stage,
    c.industries as company_industries,
    js.overall_match,
    js.skill_match,
    js.resume_match,
    js.company_quality,
    js.compensation,
    js.learning_opportunity,
    js.engineering_culture,
    js.growth_potential,
    js.interview_probability,
    js.competition_estimate,
    js.ease_of_applying,
    js.deadline_urgency,
    js.priority,
    js.location_multiplier,
    js.freshness_multiplier,
    js.explanation,
    js.score_inputs,
    js.computed_at,
    coalesce(ujs.state::text, 'new')::text as state,
    ujs.found_elsewhere_first,
    ujs.notes,
    ujs.rating
from job_group as jg
inner join job as j on jg.representative_job_id = j.id
inner join company as c on c.id = j.company_id
left join job_score as js on js.job_id = j.id
    and js.user_id = sqlc.arg(user_id)::uuid
    and js.weight_version = sqlc.arg(weight_version)::text
left join user_job_state as ujs on ujs.job_group_id = jg.id
    and ujs.user_id = sqlc.arg(user_id)::uuid
where jg.id = sqlc.arg(job_group_id)::uuid
    -- A job the user has tracked (saved, applied, ...) stays viewable after
    -- it closes — the Pipeline/Archive view (docs/12 section 3's IA) links
    -- to this same detail page, and a closed listing shouldn't 404 an
    -- in-flight or historical application.
    and (j.status = 'open' or ujs.user_id is not null);
