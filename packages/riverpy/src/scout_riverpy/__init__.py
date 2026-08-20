"""Hand-rolled consumer for River's job table (ADR-003).

The official `riverqueue` PyPI package is insert-only — it can enqueue jobs
for a Go worker to process, but cannot itself run a worker. ADR-003
anticipated exactly this and specifies "a thin client... implementing the
same table contract — roughly 200 lines wrapping
`SELECT ... FOR UPDATE SKIP LOCKED`... deliberately a small, owned piece of
code rather than a dependency." This is that client's consumer half — Go
(packages/queue) owns the insert half via the real river-go library.

Scope, deliberately: this polls rather than reimplementing River's Go
scheduler's LISTEN/NOTIFY wake-up, leader election, or rescue-stuck-jobs
sweep. At Scout's volume (a handful of jobs per poll cycle) a short poll
interval costs nothing and each of those is real infrastructure this
project does not need yet — see ADR-003's own reversal-trigger table for
when that calculus changes.
"""

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import psycopg
from psycopg.rows import dict_row
from psycopg.types.json import Json

__all__ = ["Job", "Worker", "run_worker"]


@dataclass(frozen=True)
class Job:
    """A claimed river_job row — the subset a worker function needs."""

    id: int
    kind: str
    queue: str
    attempt: int
    max_attempts: int
    args: dict[str, Any]


# A Worker takes a claimed Job and either returns normally (success) or
# raises (failure, retried with backoff up to max_attempts).
Worker = Callable[[Job], None]

# Matches River's own default backoff curve closely enough for our volume:
# attempt^4 seconds, capped. River's real formula also jitters; we don't
# bother — at single-digit jobs/minute, thundering-herd retry collision
# isn't a risk this needs to guard against.
_MAX_BACKOFF_SECONDS = 300


def _backoff_seconds(attempt: int) -> int:
    return min(attempt**4, _MAX_BACKOFF_SECONDS)


def _claim(conn: psycopg.Connection, queue: str, worker_id: str, batch_size: int) -> list[Job]:
    """Claims up to batch_size available jobs on queue, FOR UPDATE SKIP
    LOCKED so multiple consumers (or a future second worker process) never
    double-claim a row — the same guarantee River's own Go fetcher gives.

    Also reclaims 'retryable' rows once their backoff (scheduled_at) has
    elapsed — _mark_failed's own state, so a job _mark_failed retries must
    be one this same query can pick back up, or every retry is a dead end.
    """
    with conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            WITH claimed AS (
                SELECT id FROM river_job
                WHERE queue = %(queue)s
                    AND state IN ('available', 'retryable')
                    AND scheduled_at <= now()
                ORDER BY priority, scheduled_at, id
                LIMIT %(batch_size)s
                FOR UPDATE SKIP LOCKED
            )
            UPDATE river_job
            SET state = 'running',
                attempt = river_job.attempt + 1,
                attempted_at = now(),
                attempted_by = array_append(coalesce(attempted_by, '{}'), %(worker_id)s)
            FROM claimed
            WHERE river_job.id = claimed.id
            RETURNING river_job.id, river_job.kind, river_job.queue,
                river_job.attempt, river_job.max_attempts, river_job.args
            """,
            {"queue": queue, "batch_size": batch_size, "worker_id": worker_id},
        )
        rows = cur.fetchall()
    conn.commit()
    return [
        Job(
            id=row["id"],
            kind=row["kind"],
            queue=row["queue"],
            attempt=row["attempt"],
            max_attempts=row["max_attempts"],
            args=row["args"],
        )
        for row in rows
    ]


def _mark_completed(conn: psycopg.Connection, job_id: int) -> None:
    with conn.cursor() as cur:
        cur.execute(
            "UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = %s",
            (job_id,),
        )
    conn.commit()


def _mark_failed(conn: psycopg.Connection, job: Job, error: str) -> None:
    """Discards the job once max_attempts is reached (finalized, terminal —
    the same distinction river_job's own finalized_or_finalized_at_null
    check constraint enforces), otherwise reschedules with backoff.
    """
    error_entry = Json(
        {
            "at": datetime.now(UTC).isoformat(),
            "error": error,
            "attempt": job.attempt,
        }
    )
    if job.attempt >= job.max_attempts:
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE river_job
                SET state = 'discarded', finalized_at = now(),
                    errors = array_append(errors, %s::jsonb)
                WHERE id = %s
                """,
                (error_entry, job.id),
            )
    else:
        next_attempt = datetime.now(UTC) + timedelta(seconds=_backoff_seconds(job.attempt))
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE river_job
                SET state = 'retryable', scheduled_at = %s,
                    errors = array_append(errors, %s::jsonb)
                WHERE id = %s
                """,
                (next_attempt, error_entry, job.id),
            )
    conn.commit()


def run_worker(
    conn_string: str,
    queue: str,
    worker_id: str,
    handler: Worker,
    *,
    poll_interval_seconds: float = 2.0,
    batch_size: int = 8,
    stop: Callable[[], bool] | None = None,
) -> None:
    """Blocks, polling `queue` for available jobs and dispatching each to
    `handler`, until `stop()` returns True (default: never — runs forever).
    One job at a time, in claim order, within this call — apps/brain runs
    one worker process per queue, so the ordering-within-a-batch guarantee
    matches what a single-consumer River worker would give anyway.
    """
    should_stop = stop or (lambda: False)
    with psycopg.connect(conn_string, autocommit=False) as conn:
        while not should_stop():
            jobs = _claim(conn, queue, worker_id, batch_size)
            if not jobs:
                time.sleep(poll_interval_seconds)
                continue
            for job in jobs:
                try:
                    handler(job)
                except Exception as exc:  # noqa: BLE001 - a handler's own error is retry data, not a crash
                    _mark_failed(conn, job, f"{type(exc).__name__}: {exc}")
                else:
                    _mark_completed(conn, job.id)
