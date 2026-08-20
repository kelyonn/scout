"""One-time resume embedding — not a queue consumer. There is exactly one
resume row (ADR-015, single-user), computed once at brain startup rather
than on every posting, unlike job embeddings which are genuinely per-item
work triggered by real ingestion events.
"""

from __future__ import annotations

import logging

import psycopg

from scout_brain.config import EMBEDDING_VERSION
from scout_brain.embeddings import Embedder
from scout_brain.vector_utils import vector_literal

logger = logging.getLogger(__name__)


def embed_pending_resumes(conn: psycopg.Connection, embedder: Embedder) -> None:
    """Embeds any resume row missing an embedding, or whose embedding is
    stale against the current model version — the same
    recompute-on-version-change posture job embeddings get implicitly
    (a new embed job with a new version, never compared across versions).
    """
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT id, raw_text FROM resume
            WHERE embedding IS NULL OR embedding_version IS DISTINCT FROM %s
            """,
            (EMBEDDING_VERSION,),
        )
        rows = cur.fetchall()

    for resume_id, raw_text in rows:
        vector = embedder.embed(raw_text)
        with conn.cursor() as cur:
            cur.execute(
                "UPDATE resume SET embedding = %s::vector, embedding_version = %s, updated_at = now() WHERE id = %s",
                (vector_literal(vector), EMBEDDING_VERSION, resume_id),
            )
        conn.commit()
        logger.info("resume_embed: wrote embedding for resume %s", resume_id)
