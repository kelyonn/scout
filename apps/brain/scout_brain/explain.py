"""Personalized match explanations — docs/09-ranking-scoring.md section 6,
packages/queue's TaskExplain. Upgrades job_score.explanation from
apps/collector/internal/scoring.Explain's synchronous template (written at
score time so a notification is never sent unexplained) to a real,
LLM-generated "why this matches you" sentence. Consumes brain_deep
TaskExplain jobs, enqueued alongside every new job's job_score insert
(apps/collector/internal/scheduler/ingest.go).

**This consumer must always be constructed with a plain, local-only
OllamaClient — never scout_brain.llm.CascadeClient.** job_score's
skill_match/resume_match inputs are derived from the user's resume, and
AGENTS.md rule 9 / ADR-016's data rule require that nothing derived from
the resume ever reaches a hosted provider. scout_brain.worker enforces this
by which client type it constructs for `explain`, not by anything in this
module inspecting the prompt text.
"""

from __future__ import annotations

import logging
from typing import Any

import psycopg
from scout_riverpy import Job

from scout_brain.llm import LLMUnavailableError, OllamaClient

logger = logging.getLogger(__name__)

# docs/09 section 7's priority-weighted subscores this consumer can rank as
# "contributing" — the same key set apps/collector/internal/scoring.Compute
# feeds into its priority weighted mean, so a factor's contribution here
# (weight * value) means the same thing it means in the score itself.
# skill_match and resume_match are deliberately excluded from this ranking:
# their matched/missing skills are always surfaced directly (see
# _matched_and_missing_skills below), never competing for a "top 4" slot
# they'd usually win by construction.
CANDIDATE_FACTOR_KEYS = [
    "overall_match",
    "deadline_urgency",
    "company_quality",
    "learning_opportunity",
    "ease_of_applying",
    "competition_estimate",
    "compensation",
]

MAX_EXPLANATION_WORDS = 45

PROMPT_TEMPLATE = """Explain in ONE sentence (max 45 words) why this internship matches this candidate. Be specific and concrete. Reference actual skills, technologies, and facts from the data below — never invent a detail that isn't there. If something below is a genuine drawback (a missing skill, an unremarkable score), you may say so rather than only listing positives.

Role: {title} ({seniority}, {work_mode})
Top contributing match factors: {top_factors}
Matched skills: {matched_skills}
Missing skills: {missing_skills}
Location: {location}
Compensation: {compensation}
Company: {company_name}{company_description}

Return JSON: {{"explanation": "<one sentence, max 45 words>"}}"""


def _fetch_sole_user_id(conn: psycopg.Connection) -> str | None:
    # ADR-015: single-user system. Mirrors packages/db/queries/user.sql's
    # GetSoleUser — Go and Python each own their side of this lookup
    # rather than one calling the other (ADR-001).
    with conn.cursor() as cur:
        cur.execute("SELECT id FROM app_user ORDER BY created_at ASC LIMIT 1")
        row = cur.fetchone()
    return str(row[0]) if row else None


def _fetch_active_weights(conn: psycopg.Connection) -> tuple[str, dict[str, Any]] | None:
    with conn.cursor() as cur:
        cur.execute("SELECT version, weights FROM weight_version WHERE active = TRUE")
        row = cur.fetchone()
    return (row[0], row[1]) if row else None


def _fetch_score_context(
    conn: psycopg.Connection, job_id: str, user_id: str, weight_version: str
) -> tuple[Any, ...] | None:
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT
                js.overall_match, js.deadline_urgency, js.company_quality, js.learning_opportunity,
                js.ease_of_applying, js.competition_estimate, js.compensation, js.score_inputs,
                j.title, j.seniority::text, j.work_mode::text, j.location_city,
                j.comp_normalized_inr_month, c.canonical_name, c.description
            FROM job_score js
            JOIN job j ON j.id = js.job_id
            JOIN company c ON c.id = j.company_id
            WHERE js.job_id = %s AND js.user_id = %s AND js.weight_version = %s
                AND j.deleted_at IS NULL
            """,
            (job_id, user_id, weight_version),
        )
        return cur.fetchone()


def _write_explanation(
    conn: psycopg.Connection,
    job_id: str,
    user_id: str,
    weight_version: str,
    explanation: str,
    model: str,
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            """
            UPDATE job_score
            SET explanation = %s, explanation_model = %s
            WHERE job_id = %s AND user_id = %s AND weight_version = %s
            """,
            (explanation, model, job_id, user_id, weight_version),
        )
    conn.commit()


def _matched_and_missing_skills(score_inputs: dict[str, Any]) -> tuple[list[str], list[str]]:
    skill_match = (score_inputs or {}).get("skill_match") or {}
    job_skills = skill_match.get("job_skills") or []
    missing = skill_match.get("missing_skills") or []
    missing_set = set(missing)
    matched = [s for s in job_skills if s not in missing_set]
    return matched, missing


def _top_contributing_factors(
    weights_priority: dict[str, Any], subscores: dict[str, Any], score_inputs: dict[str, Any]
) -> list[str]:
    placeholder_fields = set((score_inputs or {}).get("placeholder_fields") or [])
    compensation_confidence_low = ((score_inputs or {}).get("compensation") or {}).get(
        "confidence_low", True
    )

    contributions: list[tuple[str, float]] = []
    for key in CANDIDATE_FACTOR_KEYS:
        if key in placeholder_fields:
            continue
        if key == "compensation" and compensation_confidence_low:
            continue
        weight = weights_priority.get(key)
        value = subscores.get(key)
        if weight is None or value is None:
            continue
        contributions.append((key, weight * value))

    contributions.sort(key=lambda c: c[1], reverse=True)
    return [f"{key}={subscores[key]}" for key, _ in contributions[:4]]


def _location_line(city: str | None) -> str:
    return city or "not stated"


def _compensation_line(comp_normalized_inr_month: float | None) -> str:
    if comp_normalized_inr_month is None:
        return "not disclosed by the source"
    return f"₹{comp_normalized_inr_month:,.0f}/month (normalized)"


def _enforce_word_limit(explanation: str) -> str:
    words = explanation.split()
    if len(words) <= MAX_EXPLANATION_WORDS:
        return explanation
    logger.warning(
        "explain: LLM explanation exceeded %d words (%d), truncating",
        MAX_EXPLANATION_WORDS,
        len(words),
    )
    return " ".join(words[:MAX_EXPLANATION_WORDS]) + "…"


class ExplainConsumer:
    def __init__(self, conn: psycopg.Connection, llm: OllamaClient) -> None:
        self._conn = conn
        self._llm = llm

    def handle(self, job: Job) -> None:
        job_id = job.args["job_id"]

        user_id = _fetch_sole_user_id(self._conn)
        if user_id is None:
            logger.warning(
                "explain: no app_user row exists yet, skipping job %s", job_id
            )
            return

        active = _fetch_active_weights(self._conn)
        if active is None:
            logger.warning("explain: no active weight_version, skipping job %s", job_id)
            return
        weight_version, weights = active

        fetched = _fetch_score_context(self._conn, job_id, user_id, weight_version)
        if fetched is None:
            # The job_score row this task was enqueued for no longer
            # exists under the active weight_version — a rescore replaced
            # it, or the job was deleted after enqueue. Either way there
            # is nothing to upgrade; skipping is correct, not an error.
            logger.warning(
                "explain: no job_score row for job %s under weight_version %s, skipping",
                job_id,
                weight_version,
            )
            return

        (
            overall_match,
            deadline_urgency,
            company_quality,
            learning_opportunity,
            ease_of_applying,
            competition_estimate,
            compensation,
            score_inputs,
            title,
            seniority,
            work_mode,
            location_city,
            comp_normalized_inr_month,
            company_name,
            company_description,
        ) = fetched

        subscores = {
            "overall_match": overall_match,
            "deadline_urgency": deadline_urgency,
            "company_quality": company_quality,
            "learning_opportunity": learning_opportunity,
            "ease_of_applying": ease_of_applying,
            "competition_estimate": competition_estimate,
            "compensation": compensation,
        }
        matched_skills, missing_skills = _matched_and_missing_skills(score_inputs)
        top_factors = _top_contributing_factors(
            (weights or {}).get("priority", {}), subscores, score_inputs
        )

        prompt = PROMPT_TEMPLATE.format(
            title=title,
            seniority=seniority,
            work_mode=work_mode,
            top_factors=", ".join(top_factors) if top_factors else "none computed yet",
            matched_skills=", ".join(matched_skills) if matched_skills else "none",
            missing_skills=", ".join(missing_skills) if missing_skills else "none",
            location=_location_line(location_city),
            compensation=_compensation_line(comp_normalized_inr_month),
            company_name=company_name,
            company_description=f" — {company_description}"
            if company_description
            else "",
        )

        try:
            result = self._llm.generate_json(prompt, task="explain")
        except LLMUnavailableError as exc:
            # ADR-016's degrade posture: the synchronous template
            # scoring.Explain wrote at score time stays in place — a
            # notification already went out with it, and leaving it
            # untouched is strictly better than overwriting it with
            # nothing. A future retry (riverpy's own backoff, or a later
            # re-enqueue) can still upgrade it.
            logger.warning(
                "explain: LLM unavailable for job %s (%s), leaving template explanation",
                job_id,
                exc,
            )
            return

        explanation = str(result.data.get("explanation", "")).strip()
        if not explanation:
            logger.warning(
                "explain: LLM returned an empty explanation for job %s, leaving template explanation",
                job_id,
            )
            return
        explanation = _enforce_word_limit(explanation)

        _write_explanation(
            self._conn, job_id, user_id, weight_version, explanation, self._llm.model
        )
        logger.info("explain: wrote personalized explanation for job %s", job_id)
