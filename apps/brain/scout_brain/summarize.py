"""Job posting TL;DR — packages/queue's TaskSummarize. A factual summary
of the posting itself (what the company does, what the role is,
requirements, pay), not a personalized match narrative — that is
TaskExplain's job (job_score.explanation), a different column on a
different table. Consumes brain_deep TaskSummarize jobs, enqueued on
demand by apps/api's job detail handler the first time a job is actually
viewed — most ingested jobs are never looked at, so summarizing every one
at ingest time would be mostly wasted compute.

**Constructed with a plain, local-only OllamaClient, not
scout_brain.llm.CascadeClient** — unlike TaskClassifyTier2/TaskDedupStage3,
this is not a data-sensitivity requirement (posting text is public; AGENTS.md
rule 9 doesn't apply here the way it does to TaskExplain). It's a latency
one: this is the one brain_deep task on a real interactive path — a user is
looking at a loading state until it completes — and the shared hosted
cascade's `RateLimitedProvider.call` blocks synchronously on its own rpm
window before even attempting a hosted request, shared with whatever bulk
TaskDedupStage3/TaskClassifyTier2 traffic happens to be running at that
moment. A local qwen2.5:3b-instruct call measured ~2.4s and has no such
queueing behind other tasks' rate limits — see worker.py's own comment
and packages/queue's bulkBrainDeepPriority for the queue-ordering half of
this same fix (2026-08-24).
"""

from __future__ import annotations

import logging
from typing import Any

import psycopg
from scout_riverpy import Job

from scout_brain.llm import LLMClient, LLMUnavailableError

logger = logging.getLogger(__name__)

# Structured facts are handed to the model as given, not something it must
# extract from free text — comp figures specifically must never be an LLM
# guess (docs/09's own scoring philosophy: never fabricate a number this
# system will show the user as fact). The model's only job is turning
# already-true facts plus the free-text description into a short,
# readable paragraph. JSON output (a single "summary" key) rather than
# plain text, matching every other consumer's use of
# OllamaClient.generate_json — one client method, one calling convention,
# rather than a second free-text mode for this one caller.
PROMPT_TEMPLATE = """Write a short, factual summary of this job posting for someone deciding whether to read further. 3-5 sentences, plain prose (no headers, no bullet points, no markdown). Cover, in this order: what the company does, what the role actually involves day to day, the key requirements, and the pay (state the given figure exactly if one is provided below; if none is provided, say "not disclosed" — never guess a number).

Company: {company_name}
{company_description_line}
Role: {title} ({seniority}, {work_mode})
Pay: {pay_line}

Full posting text:
{description}

Requirements section:
{requirements}

Return JSON: {{"summary": "<the 3-5 sentence summary>"}}"""

MAX_DESCRIPTION_CHARS = 3000
MAX_REQUIREMENTS_CHARS = 1500


def _pay_line(
    comp_min: float | None,
    comp_max: float | None,
    comp_currency: str | None,
    comp_normalized_inr_month: float | None,
) -> str:
    if comp_normalized_inr_month is not None:
        parts = [f"₹{comp_normalized_inr_month:,.0f}/month (normalized)"]
        if comp_min is not None or comp_max is not None:
            currency = comp_currency or ""
            if comp_min is not None and comp_max is not None:
                parts.append(f"as stated: {currency} {comp_min:,.0f}-{comp_max:,.0f}")
            elif comp_min is not None:
                parts.append(f"as stated: {currency} {comp_min:,.0f}+")
        return ", ".join(parts)
    return "not disclosed by the source"


def _fetch_job(conn: psycopg.Connection, job_id: str) -> tuple[Any, ...] | None:
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT
                j.title, j.seniority::text, j.work_mode::text,
                coalesce(j.description_stripped, j.description_text, ''),
                coalesce(j.requirements_text, ''),
                j.comp_min, j.comp_max, j.comp_currency, j.comp_normalized_inr_month,
                c.canonical_name, c.description
            FROM job j
            JOIN company c ON c.id = j.company_id
            WHERE j.id = %s AND j.deleted_at IS NULL
            """,
            (job_id,),
        )
        return cur.fetchone()


def _write_summary(
    conn: psycopg.Connection, job_id: str, summary: str, model: str
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            """
            UPDATE job
            SET ai_summary = %s, ai_summary_model = %s, ai_summary_generated_at = now(), updated_at = now()
            WHERE id = %s
            """,
            (summary, model, job_id),
        )
    conn.commit()


class SummarizeConsumer:
    def __init__(self, conn: psycopg.Connection, llm: LLMClient) -> None:
        self._conn = conn
        self._llm = llm

    def handle(self, job: Job) -> None:
        job_id = job.args["job_id"]
        fetched = _fetch_job(self._conn, job_id)
        if fetched is None:
            logger.warning("summarize: job %s not found, skipping", job_id)
            return

        (
            title,
            seniority,
            work_mode,
            description,
            requirements,
            comp_min,
            comp_max,
            comp_currency,
            comp_normalized_inr_month,
            company_name,
            company_description,
        ) = fetched

        prompt = PROMPT_TEMPLATE.format(
            company_name=company_name,
            company_description_line=(
                f"About {company_name}: {company_description}"
                if company_description
                else ""
            ),
            title=title,
            seniority=seniority,
            work_mode=work_mode,
            pay_line=_pay_line(
                comp_min, comp_max, comp_currency, comp_normalized_inr_month
            ),
            description=description[:MAX_DESCRIPTION_CHARS],
            requirements=requirements[:MAX_REQUIREMENTS_CHARS]
            or "(not separately stated — see posting text)",
        )

        try:
            result = self._llm.generate_json(prompt, task="summarize")
        except LLMUnavailableError as exc:
            # No fallback content is written — apps/api's detail handler
            # treats a null ai_summary as "not generated yet" either way,
            # so leaving it null here is indistinguishable from not having
            # tried, and a future view will simply re-enqueue and retry.
            logger.warning(
                "summarize: LLM unavailable for job %s (%s), leaving unsummarized",
                job_id,
                exc,
            )
            return

        summary = str(result.data.get("summary", "")).strip()
        if not summary:
            logger.warning(
                "summarize: LLM returned an empty summary for job %s, leaving unsummarized",
                job_id,
            )
            return

        _write_summary(self._conn, job_id, summary, self._llm.model)
        logger.info(
            "summarize: wrote summary for job %s (%d chars)", job_id, len(summary)
        )
