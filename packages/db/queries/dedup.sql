-- Dedup Stage 2 (structural) and the union-find group-merge machinery both
-- stages share. See docs/08-dedup-identity.md sections 3.2 and 4.
--
-- Lowercase keywords per AGENTS.md. apps/collector/internal/dedup owns the
-- Go-side orchestration (Stage 2's synchronous merge); apps/brain's Stage 3
-- consumer (P2 Phase F) runs the equivalent merge sequence itself in
-- Python — ADR-001's "some duplicated utility code across languages,
-- accepted deliberately over building a shared FFI layer" applies here
-- too, since Go and Python never call each other synchronously.

-- name: AdvisoryLockCompanyDedup :exec
-- docs/08 section 4's concurrency control: transaction-scoped, released
-- automatically on commit or rollback, serializing per company rather than
-- globally so dedup still parallelizes across companies.
select pg_advisory_xact_lock(hashtext('dedup:' || sqlc.arg(company_id)::text));

-- name: SelectStage2Candidates :many
-- docs/08 section 3.2's own literal query, plus description_stripped
-- (Gate 3 needs it when a candidate's simhash is NULL — a job inserted
-- before this phase existed), the fields Gate 2/representative-scoring
-- need, and a seniority filter docs/08 section 5's over-merge protection
-- table requires ("Distinct seniority: Never merged") even though the
-- section 3.2 query this is otherwise copied from doesn't show one.
-- Excludes the job just inserted by this same call (excluding_job_id).
select
    id,
    job_group_id,
    normalized_title,
    simhash,
    description_stripped,
    location_city,
    location_tier,
    work_mode
from job
where company_id = sqlc.arg(company_id)::uuid
    and role_family = sqlc.arg(role_family)::role_family
    and seniority = sqlc.arg(seniority)::seniority
    and posted_at > now() - interval '45 days'
    and deleted_at is NULL
    and id != sqlc.arg(excluding_job_id)::uuid
limit 200;

-- name: SelectRecentDescriptionsForCompany :many
-- docs/08 section 3.3's per-company boilerplate learning input — recent
-- postings' raw description text (pre-stripping; stripping is what this
-- data trains). 50 is comfortably above the >=5-posting cold-start floor
-- and bounds the cost of relearning per Stage 2 call.
select description_text
from job
where company_id = sqlc.arg(company_id)::uuid
    and description_text is not NULL
    and deleted_at is NULL
order by posted_at desc nulls last
limit 50;

-- name: SelectJobGroupForMerge :one
select id, first_seen_at, member_count
from job_group
where id = sqlc.arg(id)::uuid;

-- name: ReassignJobsToGroup :exec
-- The union-find "absorb" step: every job in the losing group moves to the
-- surviving group. member_count/first_seen_at on job_group are updated by
-- UpdateJobGroupAfterMerge separately, in the same caller transaction.
update job
set job_group_id = sqlc.arg(keep_group_id)::uuid
where job_group_id = sqlc.arg(absorb_group_id)::uuid;

-- name: UpdateJobGroupAfterMerge :exec
update job_group
set member_count = member_count + sqlc.arg(absorbed_member_count)::int,
    first_seen_at = least(first_seen_at, sqlc.arg(absorbed_first_seen_at)::timestamptz)
where id = sqlc.arg(id)::uuid;

-- name: DeleteJobGroup :exec
delete from job_group where id = sqlc.arg(id)::uuid;

-- name: InsertJobMergeEvent :exec
insert into job_merge_event (job_id, matched_job_id, from_group_id, into_group_id, stage, certainty, signal)
values (
    sqlc.arg(job_id)::uuid, sqlc.arg(matched_job_id)::uuid,
    sqlc.arg(from_group_id)::uuid, sqlc.arg(into_group_id)::uuid,
    sqlc.arg(stage)::text, sqlc.arg(certainty)::real, sqlc.arg(signal)::jsonb
);

-- name: SelectJobsForRepresentativeScoring :many
-- docs/08 section 4's representative-selection inputs, for every job
-- currently in the (post-merge) group.
select
    id,
    length(coalesce(description_text, '')) as description_length,
    (comp_min is not NULL or comp_max is not NULL) as has_structured_comp,
    (location_city is not NULL) as has_structured_location,
    (posted_at is not NULL and not posted_at_estimated) as has_reliable_posted_at,
    last_seen_at
from job
where job_group_id = sqlc.arg(job_group_id)::uuid
    and deleted_at is NULL;
