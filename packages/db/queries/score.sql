-- Scoring queries. See docs/09-ranking-scoring.md and
-- infra/migrations/000008_score.up.sql.

-- name: GetActiveWeightVersion :one
select *
from weight_version
where active = TRUE;

-- name: InsertJobScore :one
-- One row per (job, user, weight_version) — infra/migrations/000008's
-- primary key. A recompute under the same weight_version replaces the row
-- (on conflict do update) rather than erroring; a genuinely new
-- weight_version is a new row entirely, per docs/09 section 1: "a change
-- triggers a rescore with notifications suppressed."
--
-- explanation/explanation_model carry docs/09 section 6's deterministic
-- template fallback (scoring.Explain), written synchronously so a
-- notification is never sent unexplained — packages/queue's TaskExplain
-- upgrades it to an LLM-generated one asynchronously, via a Python-side
-- raw UPDATE (see the note after this query, not a sqlc query of its
-- own), exactly as docs/09 section 7 describes: "a notification can be
-- sent with the deterministic explanation and
-- upgraded when the LLM one arrives." A recompute under an unchanged
-- weight_version resets explanation back to the template on conflict too
-- — the same "notifications suppressed" comment above applies to a
-- stale LLM explanation surviving a rescore just as much as to a stale
-- score.
insert into job_score (
    job_id, user_id, weight_version,
    overall_match, skill_match, resume_match, company_quality, compensation,
    learning_opportunity, engineering_culture, growth_potential,
    interview_probability, competition_estimate, ease_of_applying,
    deadline_urgency, priority,
    location_multiplier, freshness_multiplier,
    explanation, explanation_model,
    score_inputs
) values (
    sqlc.arg(job_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(weight_version)::text,
    sqlc.arg(overall_match)::smallint, sqlc.arg(skill_match)::smallint,
    sqlc.arg(resume_match)::smallint, sqlc.arg(company_quality)::smallint,
    sqlc.arg(compensation)::smallint, sqlc.arg(learning_opportunity)::smallint,
    sqlc.arg(engineering_culture)::smallint, sqlc.arg(growth_potential)::smallint,
    sqlc.arg(interview_probability)::smallint, sqlc.arg(competition_estimate)::smallint,
    sqlc.arg(ease_of_applying)::smallint, sqlc.arg(deadline_urgency)::smallint,
    sqlc.arg(priority)::smallint,
    sqlc.arg(location_multiplier)::real, sqlc.arg(freshness_multiplier)::real,
    sqlc.arg(explanation)::text, sqlc.arg(explanation_model)::text,
    sqlc.arg(score_inputs)::jsonb
)
on conflict (job_id, user_id, weight_version) do update set
    overall_match = excluded.overall_match,
    skill_match = excluded.skill_match,
    resume_match = excluded.resume_match,
    company_quality = excluded.company_quality,
    compensation = excluded.compensation,
    learning_opportunity = excluded.learning_opportunity,
    engineering_culture = excluded.engineering_culture,
    growth_potential = excluded.growth_potential,
    interview_probability = excluded.interview_probability,
    competition_estimate = excluded.competition_estimate,
    ease_of_applying = excluded.ease_of_applying,
    deadline_urgency = excluded.deadline_urgency,
    priority = excluded.priority,
    location_multiplier = excluded.location_multiplier,
    freshness_multiplier = excluded.freshness_multiplier,
    explanation = excluded.explanation,
    explanation_model = excluded.explanation_model,
    score_inputs = excluded.score_inputs,
    computed_at = now()
returning *;

-- job_score.explanation's upgrade path — apps/brain's explain consumer
-- (TaskExplain) replacing InsertJobScore's synchronous template with a
-- real, personalized one — is a Python-side raw UPDATE, same as every
-- other apps/brain write (see summarize.py's _write_summary), not a sqlc
-- query: ADR-001's "Go and Python never call each other synchronously"
-- means Go never calls this path, so there is no Go caller for it to
-- serve.

-- name: GetJobScore :one
select *
from job_score
where job_id = sqlc.arg(job_id)::uuid
    and user_id = sqlc.arg(user_id)::uuid
    and weight_version = sqlc.arg(weight_version)::text;

-- name: SelectCompensationPercentileExact :one
-- docs/09 section 3.4's literal comparable definition. thisComp is the
-- job's own comp_normalized_inr_month; percentile is the fraction of
-- comparables at or below it. Returns raw counts rather than a computed
-- ratio — sqlc's type inference on a division/nullif expression here
-- doesn't resolve to float8 reliably, and the percentile arithmetic is
-- trivial enough that computing it in Go (computeCompensation's caller)
-- is simpler than fighting that in SQL.
select
    count(*)::int as comparable_count,
    count(*) filter (where comp_normalized_inr_month <= sqlc.arg(this_comp)::numeric)::int as at_or_below_count
from job
where role_family = sqlc.arg(role_family)::role_family
    and seniority = sqlc.arg(seniority)::seniority
    and location_country = sqlc.arg(location_country)::bpchar
    and posted_at > now() - interval '180 days'
    and comp_normalized_inr_month is not null
    and deleted_at is null;

-- name: SelectCompensationPercentileBroad :one
-- docs/09 section 3.4's fallback: "below that we fall back to a
-- country-and-seniority prior" — the same query without the role_family
-- filter, used only when the exact comparable set has fewer than 20 rows.
select
    count(*)::int as comparable_count,
    count(*) filter (where comp_normalized_inr_month <= sqlc.arg(this_comp)::numeric)::int as at_or_below_count
from job
where seniority = sqlc.arg(seniority)::seniority
    and location_country = sqlc.arg(location_country)::bpchar
    and posted_at > now() - interval '180 days'
    and comp_normalized_inr_month is not null
    and deleted_at is null;

-- name: SelectResumeJobCosine :one
-- docs/09 section 3.2's semantic term. Zero rows (pgx.ErrNoRows) means at
-- least one side has no embedding yet, or the two were computed under
-- different embedding_versions ("never compare across versions" — the
-- same rule apps/brain's Stage 3 consumer enforces) — the caller treats
-- that exactly like "job has no compensation data": a real, common,
-- honestly-represented unknown, not an error.
select (1 - (r.embedding <=> j.embedding))::float8 as cosine
from resume r, job j
where r.user_id = sqlc.arg(user_id)::uuid
    and j.id = sqlc.arg(job_id)::uuid
    and r.embedding is not null and j.embedding is not null
    and r.embedding_version = j.embedding_version;

-- name: GetResumeRawText :one
select raw_text from resume where user_id = sqlc.arg(user_id)::uuid;

-- name: GetCompanySlug :one
-- Resolves company_id -> slug so the scoring step can look up
-- packages/taxonomy/companies.yaml's well_known flag (competition_estimate's
-- brand-recognition proxy) — that registry is keyed by slug, not UUID.
select slug from company where id = sqlc.arg(id)::uuid;
