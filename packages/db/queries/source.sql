-- Scheduler queries against the `source` table. See docs/06-ingestion-pipeline.md
-- section 3, and infra/migrations/000004_source.up.sql for the schema.
--
-- Lowercase keywords per AGENTS.md; linted with .sqlfluff-queries rather than
-- the uppercase-migrations config in .sqlfluff.

-- name: SelectDueSources :many
-- The scheduler's hot query, docs/06 section 3.2's literal text plus one
-- deliberate deviation: 'pending_review' is admitted alongside 'active' so a
-- newly registered source's 48h shadow run (docs/05 section 13) actually
-- polls, parses, and dedups like a real source — it is only notifications
-- that stay suppressed during the window, enforced separately at
-- SelectUnnotifiedJobGroups. `for update skip locked` is what lets multiple
-- scheduler instances — today there is only one, but the query is written
-- for the day there is more than one — run without coordinating: each grabs
-- a disjoint batch rather than blocking on rows another instance already
-- claimed.
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
    company_id,
    kind,
    url,
    adapter_config,
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
where status in ('active', 'pending_review')
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

-- name: UpdateSourceAfterPoll :one
-- Persists the outcome of one poll cycle. Every value here is computed by the
-- caller (apps/collector/internal/scheduler) BEFORE this query runs — the
-- interval math (apps/collector/internal/interval), the circuit-breaker state
-- transition, and the change-detection hash comparison
-- (apps/collector/internal/changedetect) are all pure Go, tested in
-- isolation, and this query's only job is to write their combined result.
--
-- total_jobs_found and total_new_jobs are cumulative counters, bumped by
-- however many postings this poll parsed and how many of those turned out to
-- be genuinely new jobs (apps/collector/internal/dedup.Result.IsNewJob) — both
-- 0 on a poll that didn't reach parsing (a 304, an unchanged 200, a fetch
-- failure).
--
-- yield_ratio (P3, docs/16-observability.md): an exponential moving average
-- over "did this poll find >=1 new job", decay 0.01 — an ~100-poll effective
-- window, approximating the schema's own "new jobs / 100 polls" comment
-- without a per-poll history table a true rolling window would need. Updated
-- on every poll, including a fetch failure or an unchanged 200 (new_jobs=0
-- either way): a source that starts failing every poll should show declining
-- yield exactly like one that starts returning nothing new, since both are
-- the "silent degradation" this metric exists to catch. This is also the
-- first time this column has ever been written — it defaulted to 0 from
-- schema creation through P2, which meant interval.Compute's yield_factor
-- input was pinned at 0 for every source (yield_factor = 0.3 + 3.7*e^0 = 4.0,
-- the maximum-poll-rate end of its range) rather than adapting at all.
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
    total_jobs_found = total_jobs_found + sqlc.arg(jobs_found)::bigint,
    total_new_jobs = total_new_jobs + sqlc.arg(new_jobs)::bigint,
    yield_ratio = yield_ratio * 0.99 + (case when sqlc.arg(new_jobs)::bigint > 0 then 1 else 0 end) * 0.01,
    yield_computed_at = now(),
    updated_at = now()
where id = sqlc.arg(id)::uuid
returning yield_ratio;

-- name: QuarantineSource :exec
-- docs/06 section 10: 401/403 means access changed, not a transient blip.
-- Setting status here (rather than only backing off next_poll_at) is what
-- actually stops polling — SelectDueSources's own predicate requires
-- status = 'active', so a quarantined source simply stops being selected
-- until a human reviews and reactivates it.
update source
set status = 'quarantined', updated_at = now()
where id = sqlc.arg(id)::uuid;

-- name: RetireSource :exec
-- docs/06 section 10: three consecutive 404s means the board is gone.
update source
set status = 'retired', updated_at = now()
where id = sqlc.arg(id)::uuid;

-- name: HalveMaxRPS :exec
-- docs/06 section 10's 429 handling: honor Retry-After (via
-- RescheduleSource's next_poll_at) AND halve max_rps permanently — the
-- source told us its previous rate was too fast, and that fact outlives
-- this one poll.
update source
set max_rps = max_rps / 2, updated_at = now()
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

-- name: SelectShadowSourcesDueForReview :many
-- docs/05 section 13 step 5-6: a source registered as 'pending_review' polls
-- silently for 48 hours, then gets promoted or flagged. This is the "48
-- hours have passed and nobody has decided yet" query — the counters it
-- returns are exactly what apps/collector/internal/scheduler's pure
-- decideShadowPromotion function needs to decide, the same
-- read-the-counters-then-write-the-verdict split source.sql already uses for
-- UpdateSourceAfterPoll's interval math.
select
    id,
    url,
    total_polls,
    total_successes,
    total_jobs_found,
    total_new_jobs,
    yield_ratio
from source
where status = 'pending_review'
    and shadow_reviewed_at is NULL
    and legal_posture in ('permitted', 'api_only')
    and created_at <= now() - interval '48 hours'
order by created_at asc
limit sqlc.arg(batch_limit)::int;

-- name: PromoteShadowSource :exec
-- The shadow run looked healthy: real jobs, no red flags. priority_tier
-- comes from decideShadowPromotion's yield-based mapping (docs/05 step 6,
-- "a tier assigned by observed yield") — never tier 1, which stays reserved
-- for manual curation, same reasoning as QuarantineSource reserving
-- 'quarantined' for a human to clear.
update source
set status = 'active', priority_tier = sqlc.arg(priority_tier)::smallint,
    shadow_reviewed_at = now(), updated_at = now()
where id = sqlc.arg(id)::uuid;

-- name: FlagShadowSource :exec
-- The shadow run looked anomalous (zero jobs found, or an implausibly high
-- count — a likely sign of a wrong tenant slug or an adapter matching too
-- broadly). Stays 'pending_review' rather than being quarantined or
-- retired: nothing has actually gone wrong in a way those states describe,
-- a human just needs to look once. shadow_reviewed_at is set regardless, so
-- this source is not re-flagged on every future review pass — see this
-- migration's own comment on why that would otherwise happen. reason is
-- appended to notes for whoever reviews it next; AGENTS.md rule 7 forbids
-- PII here, and there is none to leak — url and counters, nothing a user
-- typed.
update source
set shadow_reviewed_at = now(),
    notes = coalesce(notes || E'\n', '') || sqlc.arg(reason)::text,
    updated_at = now()
where id = sqlc.arg(id)::uuid;
