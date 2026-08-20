from __future__ import annotations

import hashlib
import os
import uuid

import psycopg
import pytest
from scout_riverpy import Job

from scout_brain.llm import OllamaClient
from scout_brain.summarize import SummarizeConsumer, _pay_line
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
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping summarize tests")
    raise AssertionError("unreachable")  # pragma: no cover


def _insert_job(
    conn: psycopg.Connection,
    *,
    title: str,
    description: str,
    requirements: str = "",
    comp_normalized_inr_month: float | None = None,
    company_description: str = "",
) -> str:
    slug = f"summarize-test-{uuid.uuid4().hex[:8]}"
    with conn.cursor() as cur:
        cur.execute(
            "INSERT INTO company (slug, canonical_name, normalized_name, discovered_via, description) "
            "VALUES (%s, %s, %s, 'seed', %s) RETURNING id",
            (slug, slug, slug, company_description or None),
        )
        (company_id,) = fetchone(cur)
        url = f"https://example.test/{slug}"
        cur.execute(
            "INSERT INTO source (company_id, kind, url, url_hash, legal_posture, status) "
            "VALUES (%s, 'ats_greenhouse', %s, %s, 'permitted', 'pending_review') RETURNING id",
            (company_id, url, hashlib.sha256(url.encode()).digest()),
        )
        (source_id,) = fetchone(cur)
        cur.execute("INSERT INTO job_group (company_id) VALUES (%s) RETURNING id", (company_id,))
        (group_id,) = fetchone(cur)
        canonical_url = f"{url}/job"
        cur.execute(
            """
            INSERT INTO job (
                job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
                content_hash, title, normalized_title, description_stripped, requirements_text,
                apply_url, comp_normalized_inr_month
            )
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
            RETURNING id
            """,
            (
                group_id, company_id, source_id, canonical_url,
                hashlib.sha256(canonical_url.encode()).digest(),
                hashlib.sha256(f"{canonical_url}-content".encode()).digest(),
                title, title.lower(), description, requirements or None,
                canonical_url, comp_normalized_inr_month,
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


def test_pay_line_states_the_given_figure_exactly() -> None:
    line = _pay_line(80000, 100000, "INR", 90000)
    assert "90,000" in line or "90000" in line.replace(",", "")


def test_pay_line_says_not_disclosed_when_absent() -> None:
    assert _pay_line(None, None, None, None) == "not disclosed by the source"


def test_handle_writes_a_real_summary_covering_pay_and_company(llm: OllamaClient) -> None:
    """A real LLM call: the summary must state the exact given comp figure
    (never a hallucinated one) and must not fabricate a number when none
    was provided — the two failure modes docs/09's "never fabricate a
    number" rule exists to prevent, exercised end to end.
    """
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_id = _insert_job(
        conn,
        title="Backend Engineering Intern",
        description=(
            "Join our payments infrastructure team building the systems that process millions of "
            "transactions daily. You'll work on distributed systems, write Go services, and "
            "collaborate with senior engineers on system design."
        ),
        requirements="Currently pursuing a CS degree. Experience with Go or a similar language. "
        "Understanding of distributed systems fundamentals.",
        comp_normalized_inr_month=90000,
        company_description="Acme Corp builds payment infrastructure for online businesses.",
    )
    try:
        consumer = SummarizeConsumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_id, "task": "summarize"}))

        with conn.cursor() as cur:
            cur.execute(
                "SELECT ai_summary, ai_summary_model, ai_summary_generated_at FROM job WHERE id = %s",
                (job_id,),
            )
            summary, model, generated_at = fetchone(cur)

        assert summary is not None and len(summary) > 20
        assert model is not None
        assert generated_at is not None
        # The exact given figure (90,000) should appear somewhere in the
        # summary — not a nearby-but-wrong number, and not omitted.
        assert "90,000" in summary or "90000" in summary.replace(",", "")
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_does_not_fabricate_pay_when_not_disclosed(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    job_id = _insert_job(
        conn,
        title="Frontend Engineering Intern",
        description="Build user-facing features in React and TypeScript for our web application.",
        comp_normalized_inr_month=None,
    )
    try:
        consumer = SummarizeConsumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_id, "task": "summarize"}))

        with conn.cursor() as cur:
            cur.execute("SELECT ai_summary FROM job WHERE id = %s", (job_id,))
            (summary,) = fetchone(cur)

        assert summary is not None
        # A specific rupee/dollar figure appearing here with no source
        # figure given would be a hallucination — the summary should
        # instead say something to the effect of pay not being stated.
        import re

        assert not re.search(r"[₹$]\s?[\d,]{3,}", summary), (
            f"summary contains a specific pay figure with no comp data given: {summary!r}"
        )
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_skips_missing_job_without_raising(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    consumer = SummarizeConsumer(conn, llm)
    consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                         args={"job_id": "00000000-0000-0000-0000-000000000000", "task": "summarize"}))
    conn.close()
