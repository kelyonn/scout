from __future__ import annotations

import hashlib
import os
import uuid

import psycopg
import pytest
from scout_riverpy import Job

from scout_brain.embed_consumer import EmbedConsumer
from scout_brain.embeddings import Embedder
from scout_brain.role_taxonomy import RoleExemplarIndex
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
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping embed consumer tests")
    raise AssertionError("unreachable")  # pragma: no cover


@pytest.fixture(scope="module")
def embedder() -> Embedder:
    # Real model load — shared across this module's tests since it's slow
    # to construct and stateless to reuse (same posture worker.py takes:
    # one Embedder instance for the process's lifetime).
    return Embedder()


@pytest.fixture(scope="module")
def role_index(embedder: Embedder) -> RoleExemplarIndex:
    # Real exemplar embeddings against the real packages/taxonomy/roles.yaml
    # — this is what actually verifies the two languages' data stays in
    # sync, not a mocked index.
    return RoleExemplarIndex(embedder)


def _insert_job_fixture(
    conn: psycopg.Connection,
    *,
    description_text: str | None,
    description_stripped: str | None,
    title: str = "Software Engineering Intern",
    role_confidence: float = 0,
) -> str:
    slug = f"embed-test-{uuid.uuid4().hex[:8]}"
    with conn.cursor() as cur:
        cur.execute(
            """
            INSERT INTO company (slug, canonical_name, normalized_name, discovered_via)
            VALUES (%s, %s, %s, 'seed') RETURNING id
            """,
            (slug, slug, slug),
        )
        (company_id,) = fetchone(cur)

        url = f"https://example.test/{slug}"
        url_hash = hashlib.sha256(url.encode()).digest()
        cur.execute(
            """
            INSERT INTO source (company_id, kind, url, url_hash, legal_posture, status)
            VALUES (%s, 'ats_greenhouse', %s, %s, 'permitted', 'pending_review') RETURNING id
            """,
            (company_id, url, url_hash),
        )
        (source_id,) = fetchone(cur)

        cur.execute("INSERT INTO job_group (company_id) VALUES (%s) RETURNING id", (company_id,))
        (job_group_id,) = fetchone(cur)

        canonical_url = f"{url}/job"
        canonical_url_hash = hashlib.sha256(canonical_url.encode()).digest()
        content_hash = hashlib.sha256(f"{slug}-content".encode()).digest()
        cur.execute(
            """
            INSERT INTO job (
                job_group_id, company_id, primary_source_id,
                canonical_url, canonical_url_hash, content_hash,
                title, normalized_title, description_text, description_stripped,
                apply_url, role_confidence
            ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
            RETURNING id
            """,
            (
                job_group_id, company_id, source_id,
                canonical_url, canonical_url_hash, content_hash,
                title, title.lower(),
                description_text, description_stripped,
                canonical_url, role_confidence,
            ),
        )
        (job_id,) = fetchone(cur)
    conn.commit()
    return str(job_id)


def _cleanup(conn: psycopg.Connection, job_id: str) -> None:
    with conn.cursor() as cur:
        cur.execute("SELECT company_id, job_group_id FROM job WHERE id = %s", (job_id,))
        company_id, job_group_id = fetchone(cur)
        cur.execute("DELETE FROM job WHERE id = %s", (job_id,))
        cur.execute("DELETE FROM job_group WHERE id = %s", (job_group_id,))
        cur.execute("DELETE FROM source WHERE company_id = %s", (company_id,))
        cur.execute("DELETE FROM company WHERE id = %s", (company_id,))
    conn.commit()


def test_handle_writes_real_embedding(embedder: Embedder, role_index: RoleExemplarIndex) -> None:
    conn_string = _conn_string()
    conn = psycopg.connect(conn_string, autocommit=False)
    job_id = _insert_job_fixture(
        conn,
        description_text="Build backend services in Go and Python for a distributed systems team.",
        description_stripped=None,
    )
    try:
        consumer = EmbedConsumer(conn, embedder, role_index)
        consumer.handle(Job(id=1, kind="embed", queue="embed", attempt=1, max_attempts=25, args={"job_id": job_id}))

        with conn.cursor() as cur:
            cur.execute(
                "SELECT embedding_version, embedding IS NOT NULL, vector_dims(embedding) FROM job WHERE id = %s",
                (job_id,),
            )
            version, has_embedding, dims = fetchone(cur)
        assert version == "bge-small-en-v1.5"
        assert has_embedding is True
        assert dims == 384
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_skips_missing_job_without_raising(embedder: Embedder, role_index: RoleExemplarIndex) -> None:
    conn_string = _conn_string()
    conn = psycopg.connect(conn_string, autocommit=False)
    consumer = EmbedConsumer(conn, embedder, role_index)
    # Does not raise — a job for a deleted/nonexistent row is not retry-worthy.
    consumer.handle(
        Job(
            id=1, kind="embed", queue="embed", attempt=1, max_attempts=25,
            args={"job_id": "00000000-0000-0000-0000-000000000000"},
        )
    )
    conn.close()


def test_handle_revises_zero_confidence_job_via_tier1(embedder: Embedder, role_index: RoleExemplarIndex) -> None:
    """A title Tier 0 could not place at all (role_confidence 0, the
    fixture default) but which is a near-verbatim match to one of
    swe.general's own exemplars should get corrected by Tier 1, not left
    at Tier 0's swe.other fallback.
    """
    conn_string = _conn_string()
    conn = psycopg.connect(conn_string, autocommit=False)
    job_id = _insert_job_fixture(
        conn,
        title="Software Engineering Intern",
        description_text="Join our team as a software engineering intern for the summer.",
        description_stripped=None,
        role_confidence=0,
    )
    try:
        consumer = EmbedConsumer(conn, embedder, role_index)
        consumer.handle(Job(id=1, kind="embed", queue="embed", attempt=1, max_attempts=25, args={"job_id": job_id}))

        with conn.cursor() as cur:
            cur.execute("SELECT role_family, role_confidence FROM job WHERE id = %s", (job_id,))
            role_family, role_confidence = fetchone(cur)
        assert role_family == "swe.general"
        assert role_confidence >= 0.72
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_dispatches_embed_resume_kind(embedder: Embedder, role_index: RoleExemplarIndex) -> None:
    """The `embed` queue now carries two payload shapes — job_id-bearing
    EmbedArgs (every test above) and kind-only EmbedResumeArgs
    (packages/queue.EmbedResumeArgs, enqueued by apps/api's resume upload
    handler). A job with kind="embed_resume" and no job_id at all must not
    reach `job.args["job_id"]` — it should route to embed_pending_resumes
    instead, matching worker.py's startup-time call to the same function.

    ADR-015 makes this the *real* single resume row, not a disposable
    fixture — this test writes through it and must restore whatever was
    there before, the same discipline
    apps/api/internal/resume/handler_test.go's snapshotAndRestore uses for
    the same reason. An earlier version of this test skipped that and
    silently clobbered a real uploaded resume.
    """
    conn_string = _conn_string()
    conn = psycopg.connect(conn_string, autocommit=False)
    with conn.cursor() as cur:
        cur.execute("SELECT id FROM app_user ORDER BY created_at ASC LIMIT 1")
        row = cur.fetchone()
        if row is None:
            pytest.skip("no seeded app_user; skipping embed_resume dispatch test")
        (user_id,) = row

        cur.execute("SELECT raw_text FROM resume WHERE user_id = %s", (user_id,))
        existing = cur.fetchone()
        had_resume = existing is not None
        original_raw_text = existing[0] if existing else None

        cur.execute(
            """
            INSERT INTO resume (user_id, raw_text)
            VALUES (%s, %s)
            ON CONFLICT (user_id) DO UPDATE SET raw_text = excluded.raw_text, embedding = NULL, embedding_version = NULL
            """,
            (user_id, "Backend engineer with Go and Python experience."),
        )
    conn.commit()

    try:
        consumer = EmbedConsumer(conn, embedder, role_index)
        # No args at all — proves this path never touches job.args["job_id"].
        consumer.handle(Job(id=1, kind="embed_resume", queue="embed", attempt=1, max_attempts=25, args={}))

        with conn.cursor() as cur:
            cur.execute(
                "SELECT embedding IS NOT NULL, embedding_version FROM resume WHERE user_id = %s", (user_id,)
            )
            has_embedding, version = fetchone(cur)
        assert has_embedding is True
        assert version == "bge-small-en-v1.5"
    finally:
        with conn.cursor() as cur:
            if had_resume:
                cur.execute(
                    """
                    UPDATE resume
                    SET raw_text = %s, embedding = NULL, embedding_version = NULL, updated_at = now()
                    WHERE user_id = %s
                    """,
                    (original_raw_text, user_id),
                )
            else:
                cur.execute("DELETE FROM resume WHERE user_id = %s", (user_id,))
        conn.commit()
        conn.close()


def test_handle_does_not_override_existing_tier0_confidence(embedder: Embedder, role_index: RoleExemplarIndex) -> None:
    """A job Tier 0 already placed with real signal (weakConfidence 0.60 or
    higher) must not be touched by Tier 1, even if the exemplar match would
    disagree — a human-curated pattern beats a nearest-neighbor guess.
    """
    conn_string = _conn_string()
    conn = psycopg.connect(conn_string, autocommit=False)
    job_id = _insert_job_fixture(
        conn,
        title="Security Engineering Intern",
        description_text="Join our security team for the summer.",
        description_stripped=None,
        role_confidence=0.90,
    )
    try:
        with conn.cursor() as cur:
            cur.execute("UPDATE job SET role_family = 'swe.security' WHERE id = %s", (job_id,))
        conn.commit()

        consumer = EmbedConsumer(conn, embedder, role_index)
        consumer.handle(Job(id=1, kind="embed", queue="embed", attempt=1, max_attempts=25, args={"job_id": job_id}))

        with conn.cursor() as cur:
            cur.execute("SELECT role_family, role_confidence FROM job WHERE id = %s", (job_id,))
            role_family, role_confidence = fetchone(cur)
        assert role_family == "swe.security"
        assert role_confidence == pytest.approx(0.90)
    finally:
        _cleanup(conn, job_id)
        conn.close()
