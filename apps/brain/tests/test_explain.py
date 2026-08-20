from __future__ import annotations

import hashlib
import json
import os
import uuid

import psycopg
import pytest
from scout_riverpy import Job

from scout_brain.explain import (
    MAX_EXPLANATION_WORDS,
    ExplainConsumer,
    _compensation_line,
    _enforce_word_limit,
    _location_line,
    _matched_and_missing_skills,
    _top_contributing_factors,
)
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
    pytest.skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL); skipping explain tests")
    raise AssertionError("unreachable")  # pragma: no cover


# --------------------------------------------------------------- pure helpers ---


def test_matched_and_missing_skills_splits_by_missing_set() -> None:
    score_inputs = {"skill_match": {"job_skills": ["go", "postgresql", "docker"], "missing_skills": ["docker"]}}
    matched, missing = _matched_and_missing_skills(score_inputs)
    assert matched == ["go", "postgresql"]
    assert missing == ["docker"]


def test_matched_and_missing_skills_handles_missing_key() -> None:
    matched, missing = _matched_and_missing_skills({})
    assert matched == []
    assert missing == []


def test_top_contributing_factors_excludes_placeholders() -> None:
    weights = {"overall_match": 0.24, "company_quality": 0.14, "growth_potential": 0.07}
    subscores = {
        "overall_match": 90, "deadline_urgency": 40, "company_quality": 50,
        "learning_opportunity": 50, "ease_of_applying": 70, "competition_estimate": 50, "compensation": 50,
    }
    score_inputs = {"placeholder_fields": ["company_quality"]}
    top = _top_contributing_factors(weights, subscores, score_inputs)
    assert not any(f.startswith("company_quality=") for f in top)
    assert top[0].startswith("overall_match=")


def test_top_contributing_factors_excludes_low_confidence_compensation() -> None:
    weights = {"overall_match": 0.24, "compensation": 0.09}
    subscores = {
        "overall_match": 80, "deadline_urgency": 40, "company_quality": 50,
        "learning_opportunity": 50, "ease_of_applying": 50, "competition_estimate": 50, "compensation": 50,
    }
    score_inputs = {"compensation": {"confidence_low": True}}
    top = _top_contributing_factors(weights, subscores, score_inputs)
    assert not any(f.startswith("compensation=") for f in top)


def test_top_contributing_factors_ranks_by_weight_times_value() -> None:
    weights = {"overall_match": 0.10, "company_quality": 0.50}
    subscores = {
        "overall_match": 90, "deadline_urgency": 0, "company_quality": 90,
        "learning_opportunity": 0, "ease_of_applying": 0, "competition_estimate": 0, "compensation": 0,
    }
    score_inputs = {"compensation": {"confidence_low": True}}
    top = _top_contributing_factors(weights, subscores, score_inputs)
    assert top[0].startswith("company_quality="), "higher weight*value should rank first"


def test_top_contributing_factors_caps_at_four() -> None:
    weights = {k: 0.1 for k in ["overall_match", "deadline_urgency", "company_quality", "learning_opportunity", "ease_of_applying", "competition_estimate"]}
    subscores = {k: 50 for k in weights}
    subscores["compensation"] = 50
    top = _top_contributing_factors(weights, subscores, {"compensation": {"confidence_low": True}})
    assert len(top) == 4


def test_location_line_defaults_when_no_city() -> None:
    assert _location_line(None) == "not stated"
    assert _location_line("Bengaluru") == "Bengaluru"


def test_compensation_line_states_figure_or_not_disclosed() -> None:
    assert _compensation_line(None) == "not disclosed by the source"
    assert "90,000" in _compensation_line(90000)


def test_enforce_word_limit_passes_short_text_unchanged() -> None:
    text = "Short and specific."
    assert _enforce_word_limit(text) == text


def test_enforce_word_limit_truncates_long_text() -> None:
    text = " ".join(f"word{i}" for i in range(60))
    result = _enforce_word_limit(text)
    assert len(result.rstrip("…").split()) == MAX_EXPLANATION_WORDS
    assert result.endswith("…")


# ----------------------------------------------------------- integration test ---


def _fetch_sole_user_and_active_weights(conn: psycopg.Connection) -> tuple[str | None, str | None]:
    with conn.cursor() as cur:
        cur.execute("SELECT id FROM app_user ORDER BY created_at ASC LIMIT 1")
        user_row = cur.fetchone()
        cur.execute("SELECT version FROM weight_version WHERE active = TRUE")
        wv_row = cur.fetchone()
    return (str(user_row[0]) if user_row else None), (wv_row[0] if wv_row else None)


def _insert_job_and_score(conn: psycopg.Connection, user_id: str, weight_version: str) -> str:
    slug = f"explain-test-{uuid.uuid4().hex[:8]}"
    with conn.cursor() as cur:
        cur.execute(
            "INSERT INTO company (slug, canonical_name, normalized_name, discovered_via, description) "
            "VALUES (%s, %s, %s, 'seed', %s) RETURNING id",
            (slug, slug, slug, "Acme Corp builds payment infrastructure for online businesses."),
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
                content_hash, title, normalized_title, description_stripped, location_city,
                apply_url, comp_normalized_inr_month
            )
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
            RETURNING id
            """,
            (
                group_id, company_id, source_id, canonical_url,
                hashlib.sha256(canonical_url.encode()).digest(),
                hashlib.sha256(f"{canonical_url}-content".encode()).digest(),
                "Backend Engineering Intern", "backend engineering intern",
                (
                    "Join our payments infrastructure team building systems that process millions of "
                    "transactions daily. You'll write Go services and work on distributed systems."
                ),
                "Bengaluru", canonical_url, 90000,
            ),
        )
        (job_id,) = fetchone(cur)

        score_inputs = {
            "skill_match": {"job_skills": ["go", "postgresql", "docker"], "missing_skills": ["docker"]},
            "compensation": {"confidence_low": False, "percentile": 0.78, "comparable_count": 40},
            "placeholder_fields": ["engineering_culture", "growth_potential", "interview_probability"],
        }
        cur.execute(
            """
            INSERT INTO job_score (
                job_id, user_id, weight_version,
                overall_match, skill_match, resume_match, company_quality, compensation,
                learning_opportunity, engineering_culture, growth_potential,
                interview_probability, competition_estimate, ease_of_applying,
                deadline_urgency, priority, explanation, explanation_model, score_inputs
            )
            VALUES (%s, %s, %s, 85, 67, 50, 70, 80, 60, 50, 50, 50, 55, 65, 40, 72, %s, %s, %s)
            """,
            (
                job_id, user_id, weight_version,
                "Matches 2 of 3 required skills (go, postgresql).", "template",
                json.dumps(score_inputs),
            ),
        )
    conn.commit()
    return str(job_id)


def _cleanup(conn: psycopg.Connection, job_id: str) -> None:
    with conn.cursor() as cur:
        cur.execute("SELECT company_id, job_group_id FROM job WHERE id = %s", (job_id,))
        row = cur.fetchone()
        if row is None:
            return
        company_id, group_id = row
        cur.execute("DELETE FROM job_score WHERE job_id = %s", (job_id,))
        cur.execute("UPDATE job_group SET representative_job_id = NULL WHERE id = %s", (group_id,))
        cur.execute("DELETE FROM job WHERE id = %s", (job_id,))
        cur.execute("DELETE FROM job_group WHERE id = %s", (group_id,))
        cur.execute("DELETE FROM source WHERE company_id = %s", (company_id,))
        cur.execute("DELETE FROM company WHERE id = %s", (company_id,))
    conn.commit()


@pytest.fixture
def llm() -> OllamaClient:
    return OllamaClient(host=os.environ.get("SCOUT_TEST_OLLAMA_HOST", "http://localhost:11434"))


def test_handle_writes_a_personalized_explanation_referencing_a_matched_skill(llm: OllamaClient) -> None:
    """A real LLM call, mirroring test_summarize.py's posture: the
    explanation must be short (docs/09's 45-word cap), must reference at
    least one of the matched skills actually given (not the missing one),
    and must not be a generic phrase the spec calls out as bad ("great
    opportunity", "strong match").
    """
    conn = psycopg.connect(_conn_string(), autocommit=False)
    user_id, weight_version = _fetch_sole_user_and_active_weights(conn)
    if user_id is None or weight_version is None:
        conn.close()
        pytest.skip("no app_user/active weight_version seeded (run `make seed`); skipping explain integration test")

    job_id = _insert_job_and_score(conn, user_id, weight_version)
    try:
        consumer = ExplainConsumer(conn, llm)
        consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                             args={"job_id": job_id, "task": "explain"}))

        with conn.cursor() as cur:
            cur.execute(
                "SELECT explanation, explanation_model FROM job_score WHERE job_id = %s AND user_id = %s AND weight_version = %s",
                (job_id, user_id, weight_version),
            )
            explanation, model = fetchone(cur)

        assert explanation is not None and len(explanation) > 10
        assert model is not None and model != "template"
        assert len(explanation.split()) <= MAX_EXPLANATION_WORDS
        assert "go" in explanation.lower() or "postgresql" in explanation.lower(), (
            f"expected a matched skill referenced by name: {explanation!r}"
        )
        lowered = explanation.lower()
        assert "great opportunity" not in lowered and "strong match" not in lowered
    finally:
        _cleanup(conn, job_id)
        conn.close()


def test_handle_skips_missing_job_score_without_raising(llm: OllamaClient) -> None:
    conn = psycopg.connect(_conn_string(), autocommit=False)
    user_id, weight_version = _fetch_sole_user_and_active_weights(conn)
    if user_id is None or weight_version is None:
        conn.close()
        pytest.skip("no app_user/active weight_version seeded (run `make seed`); skipping explain integration test")

    consumer = ExplainConsumer(conn, llm)
    consumer.handle(Job(id=1, kind="brain_deep", queue="brain_deep", attempt=1, max_attempts=25,
                         args={"job_id": "00000000-0000-0000-0000-000000000000", "task": "explain"}))
    conn.close()
