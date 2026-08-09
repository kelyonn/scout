-- Scheduler queries against the `source` table. See docs/06-ingestion-pipeline.md
-- section 3, and infra/migrations/000004_source.up.sql for the schema.
--
-- Lowercase keywords per AGENTS.md; linted with .sqlfluff-queries rather than
-- the uppercase-migrations config in .sqlfluff.

-- name: SelectDueSources :many
-- The scheduler's hot query, unchanged from docs/06 section 3.2's literal
-- text. `for update skip locked` is what lets multiple scheduler instances —
-- today there is only one, but the query is written for the day there is more
-- than one — run without coordinating: each grabs a disjoint batch rather
-- than blocking on rows another instance already claimed.
--
-- The circuit-breaker predicate admits a source whose breaker is open but
-- past its backoff window (half-open: ready for one probe request) as well as
-- one that was never open at all. The politeness gate re-checks the same
-- field before every fetch (apps/collector/internal/politeness), so a source
-- that slips through this filter into a stale circuit-open state is still
-- caught there — this predicate is an optimization that keeps obviously-not-
-- due rows out of the batch, not the sole enforcement point.
select
    id,
    kind,
    url,
    max_rps,
    max_concurrency,
    robots_crawl_delay_s,
    circuit_open_until,
    legal_posture,
    hiring_pattern,
    base_interval_s,
    min_interval_s,
    max_interval_s,
    yield_ratio,
    last_changed_at,
    last_etag,
    last_modified,
    last_content_hash,
    consecutive_failures
from source
where status = 'active'
    and legal_posture in ('permitted', 'api_only')
    and next_poll_at <= now()
    and (circuit_open_until is NULL or circuit_open_until <= now())
order by priority_tier asc, next_poll_at asc
limit sqlc.arg(batch_limit)::int
for update skip locked;

-- name: ClaimSources :exec
-- Called in the same transaction as SelectDueSources, immediately after it,
-- before commit — this is what turns "selected with a row lock" into
-- "reserved" once that lock is released. Postgres's row lock from `for update
-- skip locked` lasts only until the transaction that took it commits; without
-- this second write, the instant this transaction commits every one of these
-- rows is `next_poll_at <= now()` again and the very next scheduler tick (this
-- process's own, 10 seconds later, or another instance's) would select and
-- start fetching them a second time while the first fetch is still in flight.
--
-- Bumping next_poll_at forward is deliberately the SAME mechanism a real
-- reschedule uses, not a separate "claimed" flag: if this process is killed
-- before the poll that follows can write its real outcome via
-- UpdateSourceAfterPoll, the row simply becomes due again once the claim
-- window elapses — the same self-healing property
-- apps/collector/internal/politeness's concurrency slots have via their Redis
-- TTL, applied here via a column this table already had.
update source
set next_poll_at = sqlc.arg(claimed_until)::timestamptz
where id = any(sqlc.arg(ids)::uuid[]);

-- name: UpdateSourceAfterPoll :exec
-- Persists the outcome of one poll cycle. Every value here is computed by the
-- caller (apps/collector/internal/scheduler) BEFORE this query runs — the
-- interval math (apps/collector/internal/interval), the circuit-breaker state
-- transition, and the change-detection hash comparison
-- (apps/collector/internal/changedetect) are all pure Go, tested in
-- isolation, and this query's only job is to write their combined result.
--
-- Scoped to exactly the columns this milestone's slice of the pipeline can
-- legitimately compute. total_jobs_found, total_new_jobs, yield_ratio, and
-- yield_computed_at are deliberately absent: they depend on per-posting
-- extraction, which needs an adapter's Parse — see docs/06 section 8 — and no
-- adapter exists yet. Writing zeros into those columns from here would be
-- indistinguishable from "we checked and found none," which is a false
-- signal; leaving them at the schema's own defaults until the code that can
-- honestly compute them exists is the correct alternative.
update source
set
    next_poll_at = sqlc.arg(next_poll_at)::timestamptz,
    current_interval_s = sqlc.arg(current_interval_s)::int,
    last_polled_at = now(),
    last_etag = sqlc.narg(last_etag)::text,
    last_modified = sqlc.narg(last_modified)::text,
    last_content_hash = sqlc.narg(last_content_hash)::bytea,
    -- Only advanced when this poll's content hash actually differed from the
    -- last one on file — an unchanged poll must not touch it, or every poll
    -- would look "recently active" to interval.RecencyFactor regardless of
    -- whether anything changed.
    last_changed_at = case
        when sqlc.arg(content_changed)::boolean then now()
        else last_changed_at
    end,
    consecutive_failures = sqlc.arg(consecutive_failures)::smallint,
    circuit_open_until = sqlc.narg(circuit_open_until)::timestamptz,
    total_polls = total_polls + 1,
    total_successes = total_successes + case when sqlc.arg(success)::boolean then 1 else 0 end,
    updated_at = now()
where id = sqlc.arg(id)::uuid;

-- name: RescheduleSource :exec
-- For a politeness-gate DEFER or SKIP (docs/06 section 4): the source itself
-- did nothing wrong, its turn has not come up yet — a rate budget, a
-- concurrency cap, or a still-open circuit breaker. This only moves
-- next_poll_at. It deliberately does not touch total_polls,
-- consecutive_failures, or the circuit breaker: no request was ever attempted,
-- so nothing about it succeeded or failed.
update source
set next_poll_at = sqlc.arg(next_poll_at)::timestamptz
where id = sqlc.arg(id)::uuid;

-- name: GetSourceByID :one
-- Used by the restore drill and by tests that need to assert on a source's
-- post-poll state without re-deriving it from SelectDueSources's own
-- predicate (which would exclude a row this exact query is checking).
select *
from source
where id = sqlc.arg(id)::uuid;

-- name: CountActiveSources :one
-- A cheap health signal for the scheduler's own startup log: how large is the
-- pool it is about to start polling. Not a substitute for the yield and
-- latency metrics docs/16-observability.md specifies, which arrive with the
-- observability stack at P3.
select count(*)
from source
where status = 'active'
    and legal_posture in ('permitted', 'api_only');
