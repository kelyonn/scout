"""Tier 2 role classification — docs/07-normalization-taxonomy.md section
4's LLM tier. Consumes brain_deep TaskClassifyTier2 jobs: Tier 0 (Go) hit
an advocacy.* title whose description failed the technical-evidence gate
(classify.RoleResult.NeedsTier2Escalation) and needs a second, smarter
look — a real technical signal the regex gate missed is common enough
(docs/07's own "~25% escalation" for this family) to be worth an LLM call
rather than leaving the job at swe.other/confidence 0 permanently.
"""

from __future__ import annotations

import logging

import psycopg
from scout_riverpy import Job

from scout_brain.llm import LLMClient, LLMUnavailableError

logger = logging.getLogger(__name__)

# The full role_family enum (infra/migrations/000002_enums.up.sql), given
# to the LLM as a closed set — it must pick one of these, never invent a
# new label.
ROLE_FAMILIES = [
    "swe.general",
    "swe.backend",
    "swe.frontend",
    "swe.fullstack",
    "swe.mobile",
    "swe.ml",
    "swe.ml.research",
    "swe.data",
    "swe.infra",
    "swe.infra.sre",
    "swe.infra.devops",
    "swe.infra.platform",
    "swe.infra.cloud",
    "swe.systems",
    "swe.security",
    "swe.embedded",
    "swe.qa",
    "swe.research",
    "advocacy.devrel",
    "advocacy.devex",
    "advocacy.solutions",
    "swe.other",
]

# Below this, the interim swe.other/confidence-0 state (docs/07 section 5
# rule 5's "still unresolvable -> unknown, included at reduced score")
# stays — a low-confidence LLM guess is not better than the honest
# unresolved state it would replace.
MIN_CONFIDENCE_TO_ACCEPT = 0.70

PROMPT_TEMPLATE = """You classify a job posting into exactly one role family from this list:
{families}

advocacy.devrel is outbound developer relations (writing, speaking, demos, sample code, community) — admit it ONLY if the description shows real technical work, not just community/event management.
advocacy.devex is inward-facing tooling for external developers (SDKs, docs infrastructure, developer portals).
advocacy.solutions is customer-facing technical engineering (pre-sales, integrations, deployments) — admit it ONLY if there is genuine technical building, not a pure sales/quota role.
swe.other means none of the above fit, or the role is not a software engineering role at all (marketing, sales, recruiting, etc — in which case is_software should be false).

Return JSON: {{"role_family": "<one of the list above>", "confidence": 0.0-1.0, "is_software": bool}}

Title: {title}
Description: {description}"""


def _fetch_job(conn: psycopg.Connection, job_id: str) -> tuple[str, str] | None:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT title, coalesce(description_stripped, description_text, '') FROM job WHERE id = %s AND deleted_at IS NULL",
            (job_id,),
        )
        row = cur.fetchone()
    return (row[0], row[1]) if row else None


def _write_classification(
    conn: psycopg.Connection,
    job_id: str,
    role_family: str,
    confidence: float,
    is_software: bool,
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job SET role_family = %s, role_confidence = %s, is_software = %s, updated_at = now() WHERE id = %s",
            (role_family, confidence, is_software, job_id),
        )
    conn.commit()


class ClassifyTier2Consumer:
    def __init__(self, conn: psycopg.Connection, llm: LLMClient) -> None:
        self._conn = conn
        self._llm = llm

    def handle(self, job: Job) -> None:
        job_id = job.args["job_id"]
        fetched = _fetch_job(self._conn, job_id)
        if fetched is None:
            logger.warning("classify_tier2: job %s not found, skipping", job_id)
            return
        title, description = fetched

        prompt = PROMPT_TEMPLATE.format(
            families=", ".join(ROLE_FAMILIES),
            title=title,
            description=description[:1500],
        )
        try:
            result = self._llm.generate_json(prompt, task="classify_tier2")
        except LLMUnavailableError as exc:
            # ADR-016's degrade posture: stays at Tier 0's swe.other/0
            # rather than guessing.
            logger.warning(
                "classify_tier2: LLM unavailable for job %s (%s), leaving Tier 0 result",
                job_id,
                exc,
            )
            return

        role_family = result.data.get("role_family")
        confidence = float(result.data.get("confidence", 0))
        is_software = bool(result.data.get("is_software", False))

        if role_family not in ROLE_FAMILIES:
            logger.warning(
                "classify_tier2: job %s got an invalid role_family %r, leaving Tier 0 result",
                job_id,
                role_family,
            )
            return
        if confidence < MIN_CONFIDENCE_TO_ACCEPT:
            logger.info(
                "classify_tier2: job %s LLM confidence %.2f below threshold, leaving Tier 0 result",
                job_id,
                confidence,
            )
            return

        _write_classification(self._conn, job_id, role_family, confidence, is_software)
        logger.info(
            "classify_tier2: job %s classified as %s (confidence %.2f)",
            job_id,
            role_family,
            confidence,
        )
