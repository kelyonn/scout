-- Google Calendar integration — apps/api/internal/googlecalendar. One
-- credential row per user, matching every other single-user assumption
-- in this schema (ADR-015).

-- name: UpsertGoogleCalendarCredential :one
insert into google_calendar_credential (user_id, access_token, refresh_token, token_expiry, calendar_id)
values (sqlc.arg(user_id)::uuid, sqlc.arg(access_token)::text, sqlc.arg(refresh_token)::text, sqlc.arg(token_expiry)::timestamptz, sqlc.arg(calendar_id)::text)
on conflict (user_id) do update set
    access_token = excluded.access_token,
    -- Google's OAuth token endpoint only returns a refresh_token on the
    -- very first consent (or when access_type=offline + prompt=consent
    -- forces re-issue) — a later exchange can legitimately return an
    -- empty refresh_token, and overwriting a real one with empty would
    -- silently break every future refresh. coalesce with the existing
    -- row's value rather than excluded's whenever the new one is empty.
    refresh_token = case when excluded.refresh_token = '' then google_calendar_credential.refresh_token else excluded.refresh_token end,
    token_expiry = excluded.token_expiry,
    updated_at = now()
returning *;

-- name: GetGoogleCalendarCredential :one
select * from google_calendar_credential where user_id = sqlc.arg(user_id)::uuid;

-- name: DeleteGoogleCalendarCredential :exec
delete from google_calendar_credential where user_id = sqlc.arg(user_id)::uuid;

-- name: UpdateGoogleCalendarTokens :exec
-- Called after a refresh-token round trip, which never returns a new
-- refresh_token (Google only issues one on initial consent) — only
-- access_token/token_expiry change here, deliberately narrower than
-- UpsertGoogleCalendarCredential above.
update google_calendar_credential
set access_token = sqlc.arg(access_token)::text,
    token_expiry = sqlc.arg(token_expiry)::timestamptz,
    updated_at = now()
where user_id = sqlc.arg(user_id)::uuid;

-- name: UpsertGoogleCalendarEvent :exec
insert into google_calendar_event (job_group_id, user_id, google_event_id)
values (sqlc.arg(job_group_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(google_event_id)::text)
on conflict (job_group_id) do update set
    google_event_id = excluded.google_event_id,
    updated_at = now();

-- name: GetGoogleCalendarEvent :one
select * from google_calendar_event where job_group_id = sqlc.arg(job_group_id)::uuid;

-- name: DeleteGoogleCalendarEventsForUser :exec
-- Run on disconnect — the stored google_event_id values are meaningless
-- once the credential that could act on them is gone, and keeping them
-- would make a future reconnect silently "update" events in whatever
-- Google account gets connected next.
delete from google_calendar_event where user_id = sqlc.arg(user_id)::uuid;

-- name: SelectApplicationsWithDeadlines :many
-- The push direction's source data: every tracked (saved and beyond)
-- job that has a deadline. Deliberately not the full job feed — only
-- things the user actually saved are worth putting on their real
-- calendar, the same "this is a real event, not a suggestion" bar
-- app/(app)/calendar/page.tsx's own frontend query already applies by
-- pulling from fetchApplications rather than fetchJobs.
select
    jg.id as job_group_id,
    j.title,
    c.canonical_name as company_name,
    j.deadline_at,
    j.apply_url
from user_job_state as ujs
inner join job_group as jg on jg.id = ujs.job_group_id
inner join job as j on j.id = jg.representative_job_id
inner join company as c on c.id = j.company_id
where ujs.user_id = sqlc.arg(user_id)::uuid
    and ujs.state not in ('dismissed', 'rejected', 'withdrawn', 'expired')
    and j.deadline_at is not null
    and j.deadline_at > now();
