-- User queries. Single-user system (ADR-015) — GetSoleUser is the only
-- lookup the collector and notifier need; there is no multi-user query set
-- because there is no multi-user feature yet.

-- name: GetSoleUser :one
select *
from app_user
order by created_at asc
limit 1;

-- name: GetUserProfile :one
-- target_roles/target_seniority are cast to text[] rather than left as
-- their native role_family[]/seniority[] array types: pgx has no default
-- codec for an ARRAY of a custom enum (it handles the scalar enum fine —
-- every generated *Enum.Scan method proves that — but not the array OID
-- sqlc emits alongside it), so `select *` here fails at scan time with
-- "cannot scan unknown type" for every row, not just an edge case. The
-- caller re-parses each string into a typed enum value itself.
select
    user_id,
    graduation_year,
    degree,
    university,
    target_roles::text[] as target_roles,
    target_seniority::text[] as target_seniority,
    skills,
    skill_levels,
    location_tiers,
    require_paid,
    allow_prestige_unpaid,
    min_comp_inr_month,
    excluded_companies,
    excluded_keywords,
    quiet_hours_start,
    quiet_hours_end,
    digest_time,
    notify_threshold,
    max_notifications_hour,
    max_notifications_day,
    updated_at
from user_profile
where user_id = sqlc.arg(user_id)::uuid;
