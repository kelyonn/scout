"""Tier 1 classification: embedding nearest-neighbor against per-family
exemplar titles, docs/07 section 4's design. Reads exemplars straight out of
packages/taxonomy/roles.yaml's `exemplars` key — the same file Go's
role.go loader reads for Tier 0's strong/weak/negative patterns, so the
family list can never drift between the two languages. Go's yaml.Unmarshal
ignores the `exemplars` key (not present in its own struct), so one file
stays the single source of truth.

Only meant to refine Tier 0's residue: a title Tier 0 could not place at
all (role_confidence 0 — apps/collector/internal/classify's strongConfidence
0.90 / weakConfidence 0.60 mean anything nonzero already has real signal).
Cosine similarity 0.72+ to accept, per docs/07's own threshold.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

import numpy as np
import yaml

from scout_brain.embeddings import Embedder

# Tier 0's own zero-confidence fallback (apps/collector/internal/classify's
# swe.other default) is the only case Tier 1 exists to improve — anything
# with real Tier 0 signal (>= weakConfidence 0.60) already has a family a
# human curated a pattern for, which beats a nearest-neighbor guess.
TIER0_LOW_CONFIDENCE_THRESHOLD = 0.60

# docs/07 section 4's acceptance threshold for Tier 1.
ACCEPT_COSINE_THRESHOLD = 0.72

_DEFAULT_ROLES_YAML = (
    Path(__file__).resolve().parents[3] / "packages" / "taxonomy" / "roles.yaml"
)


def load_exemplars(path: Path = _DEFAULT_ROLES_YAML) -> dict[str, list[str]]:
    with path.open() as f:
        raw = yaml.safe_load(f)
    return {
        family: data.get("exemplars", [])
        for family, data in raw.items()
        if data.get("exemplars")
    }


@dataclass(frozen=True)
class Tier1Result:
    role_family: str
    confidence: float


class RoleExemplarIndex:
    """Embeds every family's exemplar titles once at construction and
    answers nearest-neighbor queries against them. Rebuilding costs a
    couple hundred milliseconds for ~90 short titles — cheap enough that
    apps/brain just does it once at process startup rather than caching
    embeddings in a table, per the plan's own "embedded once at brain
    startup (or cached in a small table)" — startup is simpler and this
    process restarts rarely.
    """

    def __init__(
        self, embedder: Embedder, exemplars: dict[str, list[str]] | None = None
    ) -> None:
        exemplars = exemplars if exemplars is not None else load_exemplars()
        self._families: list[str] = []
        vectors: list[list[float]] = []
        for family, titles in exemplars.items():
            for title in titles:
                self._families.append(family)
                vectors.append(embedder.embed(title))
        self._matrix = np.array(vectors, dtype=np.float32)
        # bge-small's outputs are already near-unit-norm, but normalizing
        # explicitly makes the dot product below exactly cosine similarity
        # regardless of that implementation detail.
        norms = np.linalg.norm(self._matrix, axis=1, keepdims=True)
        norms[norms == 0] = 1.0
        self._matrix = self._matrix / norms

    def classify(self, job_embedding: list[float]) -> Tier1Result | None:
        if not self._families:
            return None
        query = np.array(job_embedding, dtype=np.float32)
        norm = np.linalg.norm(query)
        if norm == 0:
            return None
        query = query / norm

        similarities = self._matrix @ query
        best_idx = int(np.argmax(similarities))
        best_score = float(similarities[best_idx])
        if best_score < ACCEPT_COSINE_THRESHOLD:
            return None
        return Tier1Result(role_family=self._families[best_idx], confidence=best_score)
