-- docs/04-api-design.md section 4.5: GET/POST/DELETE /v1/me/watchlist.
-- Backs the watchlist_hiring notification trigger (docs/11 section 2) —
-- packages/db/queries/notification.sql's SelectUnnotifiedJobGroups joins
-- this table directly rather than querying it separately per row.

-- name: UpsertWatchlistEntry :one
-- ON CONFLICT DO UPDATE rather than DO NOTHING: re-adding an
-- already-watchlisted company with a different notify_any_role value is a
-- legitimate edit, not a no-op — there is no separate PATCH route in
-- docs/04 for it, so POST has to double as both.
insert into watchlist (user_id, company_id, notify_any_role)
values (sqlc.arg(user_id)::uuid, sqlc.arg(company_id)::uuid, sqlc.arg(notify_any_role)::boolean)
on conflict (user_id, company_id) do update
    set notify_any_role = excluded.notify_any_role
returning *;

-- name: RemoveWatchlistEntry :execrows
delete from watchlist
where user_id = sqlc.arg(user_id)::uuid and company_id = sqlc.arg(company_id)::uuid;

-- name: ListWatchlist :many
select
    w.company_id,
    c.canonical_name as company_name,
    c.logo_url,
    c.company_type::text as company_type,
    w.notify_any_role,
    w.added_at,
    count(j.id) filter (where j.status = 'open' and j.in_scope = TRUE) as open_job_count
from watchlist as w
inner join company as c on w.company_id = c.id
left join job as j on w.company_id = j.company_id
where w.user_id = sqlc.arg(user_id)::uuid
group by w.company_id, c.canonical_name, c.logo_url, c.company_type, w.notify_any_role, w.added_at
order by w.added_at desc;
