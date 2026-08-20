from __future__ import annotations

import hashlib
import os
import uuid

import psycopg
import pytest
from scout_riverpy import Job

from scout_brain.classify_tier2 import ClassifyTier2Consumer
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
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping classify tier2 tests")
    raise AssertionError("unreachable")  # pragma: no cover


def _insert_job(conn: psycopg.Connection, *, title: str, description: str) -> str:
    slug = f"tier2-test-{uuid.uuid4().hex[:8]}"
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
        cur.execute("INSERT INTO job_group (company_id) VALUES (%s) RETURNING id", (company_id,))
        (group_id,) = fetchone(cur)
        canonical_url = f"{url}/job"
        cur.execute(
            """
            INSERT INTO job (job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
                content_hash, title, normalized_title, description_stripped, apply_url, role_family, role_confidence)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, 'swe.other', 0)
            RETURNING id
            """,
            (
                group_id, company_id, source_id, canonical_url,
                hashlib.sha256(canonical_url.encode()).digest(),
                hashlib.sha256(f"{canonical_url}-content".encode()).digest(),
                title, title.lower(), description, canonical_url,
            ),
        )
        (job_id,) = fetchone(cur)
    conn.commit()
    return str(job_id)


def _cleanup(conn: psycopg.Connection, job_id: str) -> None:
    with conn.cursor() as cur:
        cur.execute("SELECT company_id, job_group_id FROM job WHERE id = %s", (job_id,))
        company_id, group_id = fetchone(cur)
        cur.execute("UPDATE job_group SET representative_job_id = NULL WHERE id = %s", (group_id,))
        cur.execute("DELETE FROM job WHERE id = %s", (job_id,))
        cur.execute("DELETE FROM job_group WHERE id = %s", (group_id,))
        cur.execute("DELETE FROM source WHERE company_id = %s", (company_id,))
        cur.execute("DELETE FROM company WHERE id = %s", (company_id,))
    conn.commit()


@pytest.fixture
def llm() -> OllamaClient:
    return OllamaClient(host=os.environ.get("SCOUT_TEST_OLLAMA_HOST", "http://localhost:11434"))


def test_handle_promotes_advocacy_role_with_real_technical_evidence(llm: OllamaClient) -> None:
    """A real LLM call: Tier 0 left this at swe.other/0 because the
    technical-evidence gate is title+description-regex only and missed a
    less-common phrasing of real technical work — Tier 2 should recognize
    the actual content and promote it.
    """
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_id = _insert_job(
        conn,
        title="Developer Advocate",
        description=(
            "As a developer advocate on our developer relations team, you'll represent the voice of "
            "our developer community: writing technical blog posts, giving conference talks, and "
            "building sample applications and integrations against our GraphQL API in TypeScript and "
            "Python to show developers how to get the most out of our SDKs."
        ),
    )
    try:
        consumer = ClassifyTier2Consumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_id, "task": "classify_tier2"}))

        with conn.cursor() as cur:
            cur.execute("SELECT role_family, role_confidence, is_software FROM job WHERE id = %s", (job_id,))
            role_family, confidence, is_software = fetchone(cur)
        assert role_family in {"advocacy.devrel", "advocacy.devex"}
        assert confidence >= 0.70
        assert is_software is True
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_leaves_tier0_result_when_genuinely_non_technical(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_id = _insert_job(
        conn,
        title="Developer Advocate",
        description="You'll plan community events, manage our social media presence, and run our monthly newsletter.",
    )
    try:
        consumer = ClassifyTier2Consumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_id, "task": "classify_tier2"}))

        with conn.cursor() as cur:
            cur.execute("SELECT role_family, is_software FROM job WHERE id = %s", (job_id,))
            role_family, is_software = fetchone(cur)
        # Either the LLM correctly declines to promote it (stays swe.other
        # from Tier 0) or explicitly classifies it as non-software — both
        # are the correct outcome; only an admitted advocacy.* family with
        # is_software=True would be wrong here.
        if role_family in {"advocacy.devrel", "advocacy.devex", "advocacy.solutions"}:
            assert is_software is False, "a genuinely non-technical role should never be admitted as is_software=True"
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_skips_missing_job_without_raising(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    consumer = ClassifyTier2Consumer(conn, llm)
    consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                         args={"job_id": "00000000-0000-0000-0000-000000000000", "task": "classify_tier2"}))
    conn.close()
