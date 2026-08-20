from __future__ import annotations

import json
import os
import time
import uuid
from typing import Any

import psycopg
import pytest

from scout_riverpy import Job, run_worker

CANDIDATES = [
    os.environ.get("SCOUT_TEST_DATABASE_URL"),
    "postgres://scout:scout_local_dev_only@localhost:5433/scout?sslmode=disable",
    "postgres://scout:scout_ci@localhost:5432/scout?sslmode=disable",
]


def _conn_string() -> str:
    for candidate in CANDIDATES:
        if not candidate:
            continue
        try:
            with psycopg.connect(candidate, connect_timeout=1):
                return candidate
        except psycopg.OperationalError:
            continue
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping riverpy tests")
    raise AssertionError("unreachable")  # pragma: no cover


def _insert_job(
    conn_string: str, *, queue: str, kind: str, args: dict[str, Any], max_attempts: int = 25
) -> None:
    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute(
            """
            INSERT INTO river_job (queue, kind, args, max_attempts)
            VALUES (%s, %s, %s, %s)
            """,
            (queue, kind, json.dumps(args), max_attempts),
        )


def _job_state(conn_string: str, job_id: int) -> tuple[str, int]:
    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute("SELECT state, attempt FROM river_job WHERE id = %s", (job_id,))
        row = cur.fetchone()
        assert row is not None
        return row[0], row[1]


def _cleanup(conn_string: str, queue: str) -> None:
    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute("DELETE FROM river_job WHERE queue = %s", (queue,))


def test_successful_job_marked_completed() -> None:
    conn_string = _conn_string()
    queue = f"riverpy_test_{uuid.uuid4().hex[:8]}"
    _insert_job(conn_string, queue=queue, kind="noop", args={"n": 1})

    seen: list[Job] = []
    calls = {"n": 0}

    def handler(job: Job) -> None:
        seen.append(job)

    def stop() -> bool:
        calls["n"] += 1
        return calls["n"] > 1  # one poll iteration is enough — job is already inserted

    try:
        run_worker(conn_string, queue, "test-worker", handler, poll_interval_seconds=0.01, stop=stop)
    finally:
        pass

    assert len(seen) == 1
    assert seen[0].args == {"n": 1}
    assert seen[0].attempt == 1

    state, attempt = _job_state(conn_string, seen[0].id)
    assert state == "completed"
    assert attempt == 1
    _cleanup(conn_string, queue)


def test_failed_job_retries_with_backoff_before_max_attempts() -> None:
    conn_string = _conn_string()
    queue = f"riverpy_test_{uuid.uuid4().hex[:8]}"
    _insert_job(conn_string, queue=queue, kind="noop", args={}, max_attempts=5)

    def handler(job: Job) -> None:
        raise RuntimeError("boom")

    calls = {"n": 0}

    def stop() -> bool:
        calls["n"] += 1
        return calls["n"] > 1

    run_worker(conn_string, queue, "test-worker", handler, poll_interval_seconds=0.01, stop=stop)

    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute("SELECT state, attempt, scheduled_at > now() FROM river_job WHERE queue = %s", (queue,))
        row = cur.fetchone()
        assert row is not None
        state, attempt, scheduled_in_future = row
    assert state == "retryable"
    assert attempt == 1
    assert scheduled_in_future is True
    _cleanup(conn_string, queue)


def test_retryable_job_reclaimed_once_backoff_elapses() -> None:
    conn_string = _conn_string()
    queue = f"riverpy_test_{uuid.uuid4().hex[:8]}"
    _insert_job(conn_string, queue=queue, kind="noop", args={}, max_attempts=5)

    def failing_handler(job: Job) -> None:
        raise RuntimeError("boom")

    calls = {"n": 0}

    def stop_after_one() -> bool:
        calls["n"] += 1
        return calls["n"] > 1

    run_worker(conn_string, queue, "test-worker", failing_handler, poll_interval_seconds=0.01, stop=stop_after_one)
    state, attempt = _job_state(conn_string, _job_id_for_queue(conn_string, queue))
    assert state == "retryable"
    assert attempt == 1

    # attempt=1 backoff is 1**4 = 1 second (_backoff_seconds) — wait it out.
    time.sleep(1.1)

    seen: list[Job] = []

    def succeeding_handler(job: Job) -> None:
        seen.append(job)

    calls2 = {"n": 0}

    def stop_after_one_2() -> bool:
        calls2["n"] += 1
        return calls2["n"] > 1

    run_worker(conn_string, queue, "test-worker", succeeding_handler, poll_interval_seconds=0.01, stop=stop_after_one_2)

    assert len(seen) == 1
    assert seen[0].attempt == 2
    state, attempt = _job_state(conn_string, seen[0].id)
    assert state == "completed"
    assert attempt == 2
    _cleanup(conn_string, queue)


def test_failed_job_discarded_at_max_attempts() -> None:
    conn_string = _conn_string()
    queue = f"riverpy_test_{uuid.uuid4().hex[:8]}"
    _insert_job(conn_string, queue=queue, kind="noop", args={}, max_attempts=1)

    def handler(job: Job) -> None:
        raise RuntimeError("boom")

    calls = {"n": 0}

    def stop() -> bool:
        calls["n"] += 1
        return calls["n"] > 1

    run_worker(conn_string, queue, "test-worker", handler, poll_interval_seconds=0.01, stop=stop)

    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute("SELECT state, finalized_at IS NOT NULL FROM river_job WHERE queue = %s", (queue,))
        row = cur.fetchone()
        assert row is not None
        state, finalized = row
    assert state == "discarded"
    assert finalized is True
    _cleanup(conn_string, queue)


def test_only_claims_jobs_on_its_own_queue() -> None:
    conn_string = _conn_string()
    queue_a = f"riverpy_test_a_{uuid.uuid4().hex[:8]}"
    queue_b = f"riverpy_test_b_{uuid.uuid4().hex[:8]}"
    _insert_job(conn_string, queue=queue_a, kind="noop", args={"which": "a"})
    _insert_job(conn_string, queue=queue_b, kind="noop", args={"which": "b"})

    seen: list[Job] = []

    def handler(job: Job) -> None:
        seen.append(job)

    calls = {"n": 0}

    def stop() -> bool:
        calls["n"] += 1
        return calls["n"] > 1

    run_worker(conn_string, queue_a, "test-worker", handler, poll_interval_seconds=0.01, stop=stop)

    assert len(seen) == 1
    assert seen[0].args == {"which": "a"}

    state_b, _ = _job_state(
        conn_string,
        _job_id_for_queue(conn_string, queue_b),
    )
    assert state_b == "available"

    _cleanup(conn_string, queue_a)
    _cleanup(conn_string, queue_b)


def _job_id_for_queue(conn_string: str, queue: str) -> int:
    with psycopg.connect(conn_string, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute("SELECT id FROM river_job WHERE queue = %s", (queue,))
        row = cur.fetchone()
        assert row is not None
        job_id: int = row[0]
        return job_id
