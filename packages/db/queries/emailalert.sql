-- Email-alert ingestion — docs/05-source-catalog.md's "Email alert
-- ingestion, in detail" and docs/14-legal-compliance.md section 5's legal
-- basis. See apps/collector/internal/scheduler/email.go and
-- apps/collector/internal/emailalert.
--
-- A company and a source row must exist before a posting extracted from
-- an alert email can flow through the same dedup/score/write path every
-- other adapter uses (apps/collector/internal/scheduler.processPosting
-- needs a company_id and a source_id) — these two queries are that
-- find-or-create step, company by slug and source by
-- (company, provider)-scoped url.

-- name: FindOrCreateEmailAlertCompany :one
-- discovered_via = 'email_alert' distinguishes a company that has only
-- ever been seen through an alert email from one with a real ATS source
-- (discovered_via = 'seed'/'discovery') — useful for auditing how much of
-- the catalog email ingestion is actually contributing.
with new_company as (
    insert into company (slug, canonical_name, normalized_name, discovered_via)
    values (sqlc.arg(slug)::citext, sqlc.arg(canonical_name)::text, sqlc.arg(normalized_name)::citext, 'email_alert')
    on conflict (slug) do update set slug = excluded.slug
    returning id
)
select id from new_company;

-- name: FindOrCreateEmailAlertSource :one
-- legal_posture = 'email_only' is what keeps this row out of
-- SelectDueSources (packages/db/queries/source.sql filters
-- `legal_posture in ('permitted', 'api_only')`) — this source is never
-- polled by the normal HTTP scheduler; apps/collector/internal/emailalert's
-- IMAP poller is what drives writes against it.
with new_source as (
    insert into source (company_id, kind, url, url_hash, legal_posture, status, adapter_config)
    values (
        sqlc.arg(company_id)::uuid, 'email_alert', sqlc.arg(url)::text, digest(sqlc.arg(url)::text, 'sha256'),
        'email_only', 'active', sqlc.arg(adapter_config)::jsonb
    )
    on conflict (url_hash) do update set url = excluded.url
    returning id
)
select id from new_source;
