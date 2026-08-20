"""Consumer for packages/queue's `embed` queue (Go enqueues, this
processes): fetch the job, compute its embedding, write it back to
`job.embedding`/`job.embedding_version` — then, if Tier 0 left the job at
zero role confidence, run Tier 1 nearest-neighbor classification
(role_taxonomy.py) against that same embedding and write back a better
role_family when one is found. Communicates with Go purely through
Postgres columns — Go's scoring step reads them back on its next pass
(resume_match's semantic half, a corrected role_family), never a
synchronous call in either direction, per ADR-001.
"""

from __future__ import annotations

import logging

import psycopg
from scout_riverpy import Job

from scout_brain.config import EMBEDDING_VERSION
from scout_brain.embeddings import Embedder
from scout_brain.models import JobForEmbedding
from scout_brain.resume_embed import embed_pending_resumes
from scout_brain.role_taxonomy import TIER0_LOW_CONFIDENCE_THRESHOLD, RoleExemplarIndex
from scout_brain.vector_utils import vector_literal

logger = logging.getLogger(__name__)


def _fetch_job(
    conn: psycopg.Connection, job_id: str
) -> tuple[JobForEmbedding, float] | None:
    """Returns the embedding-input record alongside Tier 0's own
    role_confidence, the trigger for whether Tier 1 runs at all.
    """
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT id, normalized_title, description_text, description_stripped, role_confidence
            FROM job
            WHERE id = %s AND deleted_at IS NULL
            """,
            (job_id,),
        )
        row = cur.fetchone()
    if row is None:
        return None
    record = JobForEmbedding(
        id=str(row[0]),
        normalized_title=row[1],
        description_text=row[2],
        description_stripped=row[3],
    )
    return record, float(row[4])


def _write_embedding(
    conn: psycopg.Connection, job_id: str, vector: list[float]
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            """
            UPDATE job
            SET embedding = %s::vector, embedding_version = %s, updated_at = now()
            WHERE id = %s
            """,
            (vector_literal(vector), EMBEDDING_VERSION, job_id),
        )
    conn.commit()


def _write_role_classification(
    conn: psycopg.Connection, job_id: str, role_family: str, confidence: float
) -> None:
    with conn.cursor() as cur:
        cur.execute(
            """
            UPDATE job
            SET role_family = %s, role_confidence = %s, updated_at = now()
            WHERE id = %s
            """,
            (role_family, confidence, job_id),
        )
    conn.commit()


class EmbedConsumer:
    """Holds the one Embedder instance (model loaded once) and the one
    RoleExemplarIndex (also embedded once) that every claimed job reuses —
    matches Job worker's expected signature
    (scout_riverpy.Worker: Callable[[Job], None]).
    """

    def __init__(
        self,
        conn: psycopg.Connection,
        embedder: Embedder,
        role_index: RoleExemplarIndex,
    ) -> None:
        self._conn = conn
        self._embedder = embedder
        self._role_index = role_index

    def handle(self, job: Job) -> None:
        # The `embed` queue carries two payload shapes now — packages/queue's
        # EmbedArgs (job_id set, the common case) and EmbedResumeArgs (no
        # job_id at all, since ADR-015 makes this a single-user system with
        # exactly one resume row). Dispatch on job.kind the same way
        # brain_deep_consumer.py dispatches on args["task"] for its own
        # multi-shape queue.
        if job.kind == "embed_resume":
            embed_pending_resumes(self._conn, self._embedder)
            return

        job_id = job.args["job_id"]
        fetched = _fetch_job(self._conn, job_id)
        if fetched is None:
            # Deleted or never existed by the time this ran — not an error
            # to retry, since retrying can never make the row reappear.
            logger.warning("embed: job %s not found, skipping", job_id)
            return
        record, tier0_confidence = fetched

        text = record.embedding_text()
        if not text:
            logger.warning(
                "embed: job %s has no title/description text, skipping", job_id
            )
            return

        vector = self._embedder.embed(text)
        _write_embedding(self._conn, job_id, vector)
        logger.info("embed: wrote embedding for job %s", job_id)

        # Tier 1 only exists to refine Tier 0's residue — a title with real
        # Tier 0 signal (>= weakConfidence) already has a human-curated
        # pattern behind it, which beats a nearest-neighbor guess.
        if tier0_confidence >= TIER0_LOW_CONFIDENCE_THRESHOLD:
            return
        tier1 = self._role_index.classify(vector)
        if tier1 is None:
            return
        _write_role_classification(
            self._conn, job_id, tier1.role_family, tier1.confidence
        )
        logger.info(
            "embed: tier1 revised job %s role_family to %s (confidence %.2f)",
            job_id,
            tier1.role_family,
            tier1.confidence,
        )
