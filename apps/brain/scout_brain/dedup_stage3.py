"""Dedup Stage 3 — semantic + LLM adjudication. docs/08-dedup-identity.md
section 3.4-3.5, SCOUT-DEDUP-013/014. Consumes brain_deep TaskDedupStage3
jobs: apps/collector/internal/dedup's Stage 2 (Go) found a pair with
similar title/location but an inconclusive SimHash distance, and flagged
it here for the semantic check Go can't do (embeddings only exist once
this same queue's `embed` consumer has run for both jobs).

The union-find merge sequence (docs/08 section 4) is reimplemented here
rather than shared with the Go side — ADR-001's "some duplicated utility
code across languages, accepted deliberately over building a shared FFI
layer," since Go and Python never call each other synchronously and this
is the same ~40 lines of SQL either language would need.
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from typing import Any

import psycopg
from scout_riverpy import Job

from scout_brain.llm import LLMClient, LLMUnavailableError

logger = logging.getLogger(__name__)

# docs/08 section 3.4's cosine thresholds.
MERGE_COSINE_THRESHOLD = 0.94
ADJUDICATE_COSINE_FLOOR = 0.88

# docs/08 section 3.5's LLM-confidence merge threshold and certainties.
LLM_MERGE_CONFIDENCE_THRESHOLD = 0.85
SEMANTIC_MERGE_CERTAINTY = 0.90
LLM_MERGE_CERTAINTY = 0.80

MAX_GROUP_MEMBER_COUNT = (
    25  # docs/08 section 5's over-merge guard, same value Go's stage2.go uses.
)

ADJUDICATION_PROMPT = """You determine whether two job postings describe the SAME specific opening at the same company, or two DIFFERENT openings.

Two postings are the SAME opening if they describe one role that one person would be hired into. They are DIFFERENT if a company could hire two different people, one for each — even if the roles are similar.

Consider: team, specialization, level, location, start date, required skills.
Ignore: formatting, length, wording, boilerplate.

Return JSON: {{"same_role": bool, "confidence": 0.0-1.0, "reason": "<20 words"}}

Posting A: {title_a} | {location_a} | {desc_a}
Posting B: {title_b} | {location_b} | {desc_b}"""


@dataclass(frozen=True)
class JobForDedup:
    id: str
    job_group_id: str
    title: str
    location_city: str | None
    description_stripped: str | None
    embedding_version: str | None


def _fetch_job(conn: psycopg.Connection, job_id: str) -> JobForDedup | None:
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT id, job_group_id, title, location_city, description_stripped, embedding_version
            FROM job
            WHERE id = %s AND deleted_at IS NULL
            """,
            (job_id,),
        )
        row = cur.fetchone()
    if row is None:
        return None
    return JobForDedup(
        id=str(row[0]),
        job_group_id=str(row[1]),
        title=row[2],
        location_city=row[3],
        description_stripped=row[4],
        embedding_version=row[5],
    )


def _cosine_similarity(
    conn: psycopg.Connection, job_id_a: str, job_id_b: str
) -> float | None:
    """None means at least one side has no embedding yet — the caller
    raises to trigger a riverpy retry-with-backoff, which is the queued
    system working as intended (the embed job for one or both sides just
    hasn't run yet) rather than an error condition.
    """
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT 1 - (a.embedding <=> b.embedding)
            FROM job a, job b
            WHERE a.id = %s AND b.id = %s
                AND a.embedding IS NOT NULL AND b.embedding IS NOT NULL
                AND a.embedding_version = b.embedding_version
            """,
            (job_id_a, job_id_b),
        )
        row = cur.fetchone()
    return float(row[0]) if row else None


def _merge_job_groups(
    conn: psycopg.Connection,
    job_id: str,
    group_id: str,
    matched_job_id: str,
    matched_group_id: str,
    stage: str,
    certainty: float,
    signal: dict[str, Any],
) -> bool:
    """docs/08 section 4's union-find merge — the Python twin of
    apps/collector/internal/dedup/stage2.go's mergeJobGroups. Returns False
    (no changes made) if the group-size cap would be exceeded, or if either
    group was merged/deleted by a concurrent process between this job being
    enqueued and this handler running — same degrade-and-skip posture as
    _merge's own job/candidate-deleted check just above its call site.
    """
    with conn.cursor() as cur:
        cur.execute(
            "SELECT id, first_seen_at, member_count FROM job_group WHERE id = %s",
            (group_id,),
        )
        new_row = cur.fetchone()
        cur.execute(
            "SELECT id, first_seen_at, member_count FROM job_group WHERE id = %s",
            (matched_group_id,),
        )
        matched_row = cur.fetchone()

    if new_row is None or matched_row is None:
        return False
    new_id, new_first_seen, new_count = new_row
    matched_id, matched_first_seen, matched_count = matched_row

    if new_count + matched_count > MAX_GROUP_MEMBER_COUNT:
        return False

    if matched_first_seen < new_first_seen:
        keep_id, absorb_id, absorb_count, absorb_first_seen = (
            matched_id,
            new_id,
            new_count,
            new_first_seen,
        )
    else:
        keep_id, absorb_id, absorb_count, absorb_first_seen = (
            new_id,
            matched_id,
            matched_count,
            matched_first_seen,
        )

    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job SET job_group_id = %s WHERE job_group_id = %s",
            (keep_id, absorb_id),
        )
        cur.execute(
            "UPDATE job_group SET member_count = member_count + %s, first_seen_at = LEAST(first_seen_at, %s) WHERE id = %s",
            (absorb_count, absorb_first_seen, keep_id),
        )
        cur.execute("DELETE FROM job_group WHERE id = %s", (absorb_id,))
        cur.execute(
            """
            INSERT INTO job_merge_event (job_id, matched_job_id, from_group_id, into_group_id, stage, certainty, signal)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
            """,
            (
                job_id,
                matched_job_id,
                group_id,
                keep_id,
                stage,
                certainty,
                json.dumps(signal),
            ),
        )
    conn.commit()
    _recompute_representative(conn, keep_id)
    return True


def _recompute_representative(conn: psycopg.Connection, group_id: str) -> None:
    """Same scoring formula as stage2.go's representativeScore — every
    current source is a Tier 1/2 direct ATS integration, so that term is a
    constant +40 across every job scored here today.
    """
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT id, length(coalesce(description_text, '')),
                (comp_min IS NOT NULL OR comp_max IS NOT NULL),
                (location_city IS NOT NULL),
                (posted_at IS NOT NULL AND NOT posted_at_estimated),
                last_seen_at
            FROM job WHERE job_group_id = %s AND deleted_at IS NULL
            """,
            (group_id,),
        )
        rows = cur.fetchall()
    if not rows:
        return

    most_recent_id = max(rows, key=lambda r: r[5])[0]
    best_id, best_score = None, -1.0
    for job_id, desc_len, has_comp, has_loc, has_posted, _last_seen in rows:
        score = 40.0
        if desc_len >= 1000:
            score += 25
        if has_comp:
            score += 15
        if has_loc:
            score += 10
        if has_posted:
            score += 10
        if job_id == most_recent_id:
            score += 5
        if score > best_score:
            best_id, best_score = job_id, score

    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job_group SET representative_job_id = %s WHERE id = %s",
            (best_id, group_id),
        )
    conn.commit()


def _flag_possible_duplicate(
    conn: psycopg.Connection, job_id: str, matched_job_id: str
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            "UPDATE job SET possible_duplicate = TRUE WHERE id IN (%s, %s)",
            (job_id, matched_job_id),
        )
    conn.commit()


class DedupStage3Consumer:
    def __init__(self, conn: psycopg.Connection, llm: LLMClient) -> None:
        self._conn = conn
        self._llm = llm

    def handle(self, job: Job) -> None:
        job_id = job.args["job_id"]
        candidate_job_id = job.args["candidate_job_id"]

        cosine = _cosine_similarity(self._conn, job_id, candidate_job_id)
        if cosine is None:
            # Embeddings not both ready yet — raising triggers riverpy's
            # backoff retry, which is the correct wait, not an error.
            raise RuntimeError(
                f"embeddings not yet available for {job_id} / {candidate_job_id}"
            )

        if cosine >= MERGE_COSINE_THRESHOLD:
            self._merge(
                job_id,
                candidate_job_id,
                "semantic",
                SEMANTIC_MERGE_CERTAINTY,
                {"cosine": cosine},
            )
            return

        if cosine < ADJUDICATE_COSINE_FLOOR:
            logger.info(
                "dedup_stage3: %s vs %s cosine=%.3f -> distinct",
                job_id,
                candidate_job_id,
                cosine,
            )
            return

        self._adjudicate(job_id, candidate_job_id, cosine)

    def _merge(
        self,
        job_id: str,
        candidate_job_id: str,
        stage: str,
        certainty: float,
        signal: dict[str, Any],
    ) -> None:
        job = _fetch_job(self._conn, job_id)
        candidate = _fetch_job(self._conn, candidate_job_id)
        if job is None or candidate is None:
            logger.warning(
                "dedup_stage3: job or candidate deleted before merge, skipping"
            )
            return
        merged = _merge_job_groups(
            self._conn,
            job_id,
            job.job_group_id,
            candidate_job_id,
            candidate.job_group_id,
            stage,
            certainty,
            signal,
        )
        if merged:
            logger.info(
                "dedup_stage3: merged %s into %s's group (%s, certainty %.2f)",
                job_id,
                candidate_job_id,
                stage,
                certainty,
            )
        else:
            logger.info(
                "dedup_stage3: group-size cap hit, skipped merge of %s / %s",
                job_id,
                candidate_job_id,
            )

    def _adjudicate(self, job_id: str, candidate_job_id: str, cosine: float) -> None:
        job = _fetch_job(self._conn, job_id)
        candidate = _fetch_job(self._conn, candidate_job_id)
        if job is None or candidate is None:
            logger.warning(
                "dedup_stage3: job or candidate deleted before adjudication, skipping"
            )
            return

        prompt = ADJUDICATION_PROMPT.format(
            title_a=job.title,
            location_a=job.location_city or "unknown",
            desc_a=(job.description_stripped or "")[:1500],
            title_b=candidate.title,
            location_b=candidate.location_city or "unknown",
            desc_b=(candidate.description_stripped or "")[:1500],
        )

        try:
            result = self._llm.generate_json(prompt, task="dedup_stage3")
        except LLMUnavailableError as exc:
            # ADR-016's degrade posture: an uncertain pair with no LLM
            # available stays distinct rather than guessing.
            logger.warning(
                "dedup_stage3: LLM adjudication unavailable (%s), treating as distinct",
                exc,
            )
            return

        same_role = bool(result.data.get("same_role"))
        confidence = float(result.data.get("confidence", 0))

        if same_role and confidence >= LLM_MERGE_CONFIDENCE_THRESHOLD:
            self._merge(
                job_id,
                candidate_job_id,
                "llm",
                LLM_MERGE_CERTAINTY,
                {
                    "cosine": cosine,
                    "llm_confidence": confidence,
                    "reason": result.data.get("reason"),
                },
            )
        else:
            _flag_possible_duplicate(self._conn, job_id, candidate_job_id)
            logger.info(
                "dedup_stage3: LLM adjudication distinct for %s / %s (same_role=%s confidence=%.2f), flagged possible_duplicate",
                job_id,
                candidate_job_id,
                same_role,
                confidence,
            )
