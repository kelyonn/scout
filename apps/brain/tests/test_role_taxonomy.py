from __future__ import annotations

import pytest

from scout_brain.embeddings import Embedder
from scout_brain.role_taxonomy import (
    ACCEPT_COSINE_THRESHOLD,
    RoleExemplarIndex,
    load_exemplars,
)


@pytest.fixture(scope="module")
def embedder() -> Embedder:
    return Embedder()


def test_load_exemplars_reads_real_roles_yaml() -> None:
    exemplars = load_exemplars()
    # Every family added in this pass should be present with real titles,
    # not an empty stub — this is what actually catches the two files
    # drifting apart.
    assert "swe.general" in exemplars
    assert "swe.backend" in exemplars
    assert len(exemplars["swe.general"]) >= 3
    for family, titles in exemplars.items():
        assert titles, f"{family} has an exemplars key but no titles"


def test_classify_matches_near_verbatim_title(embedder: Embedder) -> None:
    index = RoleExemplarIndex(embedder)
    result = index.classify(embedder.embed("Software Engineering Intern"))
    assert result is not None
    assert result.role_family == "swe.general"
    assert result.confidence >= ACCEPT_COSINE_THRESHOLD


def test_classify_matches_semantically_similar_but_nonverbatim_title(embedder: Embedder) -> None:
    index = RoleExemplarIndex(embedder)
    # Not a literal exemplar string, but unambiguously a backend role — this
    # is Tier 1's actual value proposition over Tier 0's substring matching.
    result = index.classify(embedder.embed("Intern, Server-Side Software Development"))
    assert result is not None
    assert result.role_family in {"swe.backend", "swe.general"}


def test_classify_rejects_unrelated_text(embedder: Embedder) -> None:
    index = RoleExemplarIndex(embedder)
    result = index.classify(embedder.embed("Regional Sales Manager, Enterprise Accounts"))
    assert result is None
