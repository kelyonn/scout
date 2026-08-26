from __future__ import annotations

import hashlib
import math
import os
import threading
import uuid

import psycopg
import pytest
from scout_riverpy import Job

from scout_brain.dedup_stage3 import DedupStage3Consumer
from scout_brain.llm import OllamaClient
from tests.dbhelpers import fetchone

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
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping dedup stage3 tests")
    raise AssertionError("unreachable")  # pragma: no cover


def _vector_at_cosine(cosine: float) -> list[float]:
    """A 384-dim unit vector whose cosine similarity with [1, 0, 0, ...] is
    exactly `cosine` — deterministic control over which of docs/08 section
    3.4's three cosine buckets a test pair lands in, rather than hoping a
    real embedding happens to land there.
    """
    sin = math.sqrt(max(0.0, 1 - cosine * cosine))
    return [cosine, sin] + [0.0] * 382


_BASE_VECTOR = [1.0] + [0.0] * 383


def _insert_job_pair(conn: psycopg.Connection, *, title_a: str, title_b: str, cosine: float | None) -> tuple[str, str]:
    """Inserts two jobs in two different job_groups, with real (deterministic)
    embeddings at the given cosine similarity — or no embedding at all on
    job B when cosine is None, for the not-ready-yet test.
    """
    slug = f"stage3-test-{uuid.uuid4().hex[:8]}"
    with conn.cursor() as cur:
        cur.execute(
            "INSERT INTO company (slug, canonical_name, normalized_name, discovered_via) VALUES (%s, %s, %s, 'seed') RETURNING id",
            (slug, slug, slug),
        )
        (company_id,) = fetchone(cur)
        url = f"https://example.test/{slug}"
        cur.execute(
            "INSERT INTO source (company_id, kind, url, url_hash, legal_posture, status) VALUES (%s, 'ats_greenhouse', %s, %s, 'permitted', 'pending_review') RETURNING id",
            (company_id, url, hashlib.sha256(url.encode()).digest()),
        )
        (source_id,) = fetchone(cur)

        job_ids = []
        for suffix, title in (("a", title_a), ("b", title_b)):
            cur.execute("INSERT INTO job_group (company_id) VALUES (%s) RETURNING id", (company_id,))
            (group_id,) = fetchone(cur)
            canonical_url = f"{url}/{suffix}"
            cur.execute(
                """
                INSERT INTO job (job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
                    content_hash, title, normalized_title, apply_url)
                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s) RETURNING id
                """,
                (
                    group_id, company_id, source_id, canonical_url,
                    hashlib.sha256(canonical_url.encode()).digest(),
                    hashlib.sha256(f"{canonical_url}-content".encode()).digest(),
                    title, title.lower(), canonical_url,
                ),
            )
            (job_id,) = fetchone(cur)
            job_ids.append(job_id)
    conn.commit()

    job_a_id, job_b_id = job_ids
    _set_embedding(conn, job_a_id, _BASE_VECTOR)
    if cosine is not None:
        _set_embedding(conn, job_b_id, _vector_at_cosine(cosine))

    return str(job_a_id), str(job_b_id)


def _set_embedding(conn: psycopg.Connection, job_id: str, vector: list[float]) -> None:
    literal = "[" + ",".join(repr(v) for v in vector) + "]"
    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job SET embedding = %s::vector, embedding_version = 'bge-small-en-v1.5' WHERE id = %s",
            (literal, job_id),
        )
    conn.commit()


def _cleanup(conn: psycopg.Connection, job_a_id: str, job_b_id: str) -> None:
    # job_group.representative_job_id -> job and job.job_group_id ->
    # job_group form the same circular FK docs/03 calls out — null the
    # representative pointer before deleting jobs, or a merge (which sets
    # it) leaves a dangling reference that blocks the delete.
    with conn.cursor() as cur:
        cur.execute("SELECT DISTINCT company_id FROM job WHERE id IN (%s, %s)", (job_a_id, job_b_id))
        (company_id,) = fetchone(cur)
        cur.execute("SELECT DISTINCT job_group_id FROM job WHERE id IN (%s, %s)", (job_a_id, job_b_id))
        group_ids = [r[0] for r in cur.fetchall()]
        for gid in group_ids:
            cur.execute("UPDATE job_group SET representative_job_id = NULL WHERE id = %s", (gid,))
        cur.execute("DELETE FROM job_merge_event WHERE job_id IN (%s, %s) OR matched_job_id IN (%s, %s)", (job_a_id, job_b_id, job_a_id, job_b_id))
        cur.execute("DELETE FROM job WHERE id IN (%s, %s)", (job_a_id, job_b_id))
        for gid in group_ids:
            cur.execute("DELETE FROM job_group WHERE id = %s", (gid,))
        cur.execute("DELETE FROM source WHERE company_id = %s", (company_id,))
        cur.execute("DELETE FROM company WHERE id = %s", (company_id,))
    conn.commit()


@pytest.fixture
def llm() -> OllamaClient:
    return OllamaClient(host=os.environ.get("SCOUT_TEST_OLLAMA_HOST", "http://localhost:11434"))


def test_handle_raises_when_embedding_not_yet_available(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_a, job_b = _insert_job_pair(conn, title_a="Backend Engineer Intern", title_b="Backend Engineer Intern", cosine=None)
    try:
        consumer = DedupStage3Consumer(conn, llm)
        with pytest.raises(RuntimeError):
            consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                                 args={"job_id": job_a, "candidate_job_id": job_b, "task": "dedup_stage3"}))
    finally:
        _cleanup(conn, job_a, job_b)
        conn.close()


def test_handle_merges_when_cosine_above_merge_threshold(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_a, job_b = _insert_job_pair(conn, title_a="Backend Engineer Intern", title_b="Backend Engineer Intern", cosine=0.97)
    try:
        consumer = DedupStage3Consumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_a, "candidate_job_id": job_b, "task": "dedup_stage3"}))

        with conn.cursor() as cur:
            cur.execute("SELECT job_group_id FROM job WHERE id IN (%s, %s)", (job_a, job_b))
            groups = {r[0] for r in cur.fetchall()}
        assert len(groups) == 1, "cosine >= 0.94 should merge the two jobs into one group"

        with conn.cursor() as cur:
            cur.execute("SELECT stage, certainty FROM job_merge_event WHERE job_id = %s", (job_a,))
            stage, certainty = fetchone(cur)
        assert stage == "semantic"
        assert certainty == pytest.approx(0.90)
    finally:
        _cleanup(conn, job_a, job_b)
        conn.close()


def test_handle_stays_distinct_when_cosine_below_adjudication_floor(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_a, job_b = _insert_job_pair(conn, title_a="Backend Engineer Intern", title_b="Marketing Manager", cosine=0.5)
    try:
        consumer = DedupStage3Consumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_a, "candidate_job_id": job_b, "task": "dedup_stage3"}))

        with conn.cursor() as cur:
            cur.execute("SELECT job_group_id FROM job WHERE id IN (%s, %s)", (job_a, job_b))
            groups = {r[0] for r in cur.fetchall()}
        assert len(groups) == 2, "cosine < 0.88 should never merge"

        with conn.cursor() as cur:
            cur.execute("SELECT possible_duplicate FROM job WHERE id = %s", (job_a,))
            (flagged,) = fetchone(cur)
        assert flagged is False, "the <0.88 bucket is plain distinct, not a possible_duplicate flag"
    finally:
        _cleanup(conn, job_a, job_b)
        conn.close()


def test_handle_llm_adjudicates_and_merges_genuinely_same_role(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_a, job_b = _insert_job_pair(
        conn,
        title_a="Software Engineering Intern, Backend",
        title_b="Backend Software Engineering Intern",
        cosine=0.90,
    )
    # Give the LLM real, genuinely-same-role description text to adjudicate
    # on — the cosine band alone (deterministically forced above) is what
    # routes this to the LLM step; the LLM's own judgment is real.
    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job SET description_stripped = %s WHERE id = %s",
            ("Build backend services in Go for our payments team, working with senior engineers on production systems.", job_a),
        )
        cur.execute(
            "UPDATE job SET description_stripped = %s WHERE id = %s",
            ("Build backend services in Go for our payments team, working with senior engineers on production systems.", job_b),
        )
    conn.commit()

    try:
        consumer = DedupStage3Consumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_a, "candidate_job_id": job_b, "task": "dedup_stage3"}))

        with conn.cursor() as cur:
            cur.execute("SELECT job_group_id FROM job WHERE id IN (%s, %s)", (job_a, job_b))
            groups = {r[0] for r in cur.fetchall()}
        assert len(groups) == 1, "a real LLM call on near-identical postings should judge them the same role and merge"
    finally:
        _cleanup(conn, job_a, job_b)
        conn.close()


def test_merge_waits_for_a_concurrent_holder_of_the_same_companys_dedup_lock(llm: OllamaClient) -> None:
    """A real, not-mocked proof that the merge path takes
    apps/collector/internal/dedup/stage2.go's own advisory lock
    (hashtext('dedup:' || company_id)) rather than just documenting the
    intent: a second connection takes that exact lock manually and holds
    it open; the merge (on a separate connection, in a background thread)
    must not complete until the second connection releases it. Before the
    fix this added, dedup_stage3.py never touched this lock at all, so
    this test would have raced (and this assertion could fail either way,
    depending on timing) rather than deterministically block.
    """
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_a, job_b = _insert_job_pair(conn, title_a="Backend Engineer Intern", title_b="Backend Engineer Intern", cosine=0.97)
    lock_conn = psycopg.connect(_conn_string(), autocommit=False)
    try:
        with conn.cursor() as cur:
            cur.execute("SELECT company_id FROM job WHERE id = %s", (job_a,))
            (company_id,) = fetchone(cur)

        with lock_conn.cursor() as cur:
            cur.execute(
                "SELECT pg_advisory_xact_lock(hashtext('dedup:' || %s::uuid::text))",
                (company_id,),
            )

        consumer = DedupStage3Consumer(conn, llm)
        merge_done = threading.Event()

        def run_merge() -> None:
            consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                                 args={"job_id": job_a, "candidate_job_id": job_b, "task": "dedup_stage3"}))
            merge_done.set()

        thread = threading.Thread(target=run_merge, daemon=True)
        thread.start()
        try:
            # The merge must still be blocked on lock_conn's held lock —
            # a short, generous wait, not a race we're hoping to win.
            still_blocked = not merge_done.wait(timeout=0.5)
            assert still_blocked, "merge completed before the concurrent lock holder released it"

            # Checked via lock_conn, not conn: conn is mid-execute in the
            # background thread (blocked server-side on the advisory
            # lock), and psycopg serializes concurrent use of one
            # connection object across threads — reading through conn here
            # would itself block on that same in-flight call, which can
            # only resolve after lock_conn.rollback() below, deadlocking
            # this test against itself rather than testing anything.
            with lock_conn.cursor() as cur:
                cur.execute("SELECT job_group_id FROM job WHERE id IN (%s, %s)", (job_a, job_b))
                groups_while_locked = {r[0] for r in cur.fetchall()}
            assert len(groups_while_locked) == 2, "merge must not proceed while the company's dedup lock is held elsewhere"
        finally:
            lock_conn.rollback()  # releases the advisory lock

        assert merge_done.wait(timeout=5.0), "merge never completed after the lock was released"
        thread.join(timeout=5.0)

        with conn.cursor() as cur:
            cur.execute("SELECT job_group_id FROM job WHERE id IN (%s, %s)", (job_a, job_b))
            groups_after = {r[0] for r in cur.fetchall()}
        assert len(groups_after) == 1, "merge should complete once the lock is free"
    finally:
        lock_conn.close()
        _cleanup(conn, job_a, job_b)
        conn.close()
