"""Local embeddings via fastembed — bundles ONNX Runtime and the tokenizer
for bge-small-en-v1.5 in one dependency, the simplest path to the exact
model docs/19-roadmap.md's P2 objective specifies (384-dim, local, free).
"""

from __future__ import annotations

from fastembed import TextEmbedding

from scout_brain.config import EMBEDDING_MODEL


class Embedder:
    """Wraps fastembed's TextEmbedding. Construction downloads/loads the
    model once; embed() is cheap per call after that. Not thread-safe for
    concurrent embed() calls on the same instance — apps/brain's embed
    consumer processes one job at a time, so this is never exercised.
    """

    def __init__(self, model_name: str = EMBEDDING_MODEL) -> None:
        self._model = TextEmbedding(model_name)

    def embed(self, text: str) -> list[float]:
        # fastembed's embed() is a generator over a batch; we only ever
        # pass one string, so take the single result.
        (vector,) = self._model.embed([text])
        result: list[float] = vector.tolist()
        return result
