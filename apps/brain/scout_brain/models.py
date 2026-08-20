"""Pydantic models mirroring the subset of packages/schema's Go structs
apps/brain reads and writes. Hand-written rather than generated from a
shared JSON Schema — the same "add tooling when something needs it" call
packages/schema/README already makes about full JSON-Schema-driven codegen
for the Go side. If a second Python consumer of these shapes ever exists,
that's the trigger to build the real codegen pipeline; one consumer doesn't
justify it.
"""

from __future__ import annotations

from pydantic import BaseModel


class JobForEmbedding(BaseModel):
    """The columns embed_consumer needs to build embedding input text."""

    id: str
    normalized_title: str
    description_text: str | None = None
    description_stripped: str | None = None

    def embedding_text(self) -> str:
        """bge-small-en-v1.5 is retrieval-tuned; title-prefixed body text is
        the standard pattern for that family of models, and matches
        docs/08's own spec for dedup Stage 3's embedding input ("computed on
        description_stripped prefixed with normalized title").
        description_stripped is empty until dedup Stage 2's boilerplate
        stripping exists, so this falls back to description_text — the
        embedding gets recomputed (a new embed job, new embedding_version)
        once Stage 2 lands and stripped text becomes available, same as any
        other upstream data-quality improvement would require.
        """
        body = self.description_stripped or self.description_text or ""
        return f"{self.normalized_title}\n{body}".strip()


class RoleClassification(BaseModel):
    """Tier 1's output — written back to `job` only when it disagrees with
    Tier 0 at higher confidence, per docs/07's own design.
    """

    job_id: str
    role_family: str
    confidence: float
