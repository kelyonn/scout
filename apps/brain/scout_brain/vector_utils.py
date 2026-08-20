"""Shared pgvector-literal formatting — split out of embed_consumer.py so
resume_embed.py can use it without embed_consumer.py -> resume_embed.py ->
embed_consumer.py becoming a circular import (embed_consumer.py started
calling into resume_embed.py once the `embed` queue's embed_resume kind
needed to dispatch there).
"""

from __future__ import annotations


def vector_literal(values: list[float]) -> str:
    """pgvector accepts a bracketed comma-separated literal cast to
    ::vector — simplest path to writing one from psycopg without adding the
    separate `pgvector` package purely for this single write pattern.
    """
    return "[" + ",".join(repr(v) for v in values) + "]"
