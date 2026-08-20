"""classify_role eval — docs/17-testing-qa.md section 6's `classify_role`
suite, scoped to Tier 2 (the LLM tier). Tier 0's regex table
(apps/collector/internal/classify) and Tier 1's embedding-exemplar match
(scout_brain.role_taxonomy) are deterministic/local and already covered by
their own unit tests — an LLM eval exists for the one tier that is
actually non-deterministic. Calls apps/brain's real production prompt
(scout_brain.classify_tier2.PROMPT_TEMPLATE) directly against Ollama.

"precision" here is overall accuracy (correct / total) across every
family, not a per-class figure — docs/17's "per-family precision" is the
macro-F1 metric reported alongside it; this simplification is documented
here rather than computing thirteen separate per-family precisions for a
starter golden set too small for most of them to be statistically
meaningful (see evals/README.md on golden-set size).
"""

from __future__ import annotations

from evals.harness import SuiteResult, load_golden, precision_recall
from scout_brain.classify_tier2 import PROMPT_TEMPLATE, ROLE_FAMILIES
from scout_brain.llm import LLMUnavailableError, OllamaClient

GATES = {"precision": 0.97, "macro_f1": 0.93}


def run(llm: OllamaClient) -> SuiteResult:
    examples = load_golden("classify_role")

    correct = 0
    true_positive: dict[str, int] = {}
    false_positive: dict[str, int] = {}
    false_negative: dict[str, int] = {}

    for ex in examples:
        prompt = PROMPT_TEMPLATE.format(
            families=", ".join(ROLE_FAMILIES), title=ex["title"], description=ex["description"][:1500]
        )
        try:
            result = llm.generate_json(prompt)
        except LLMUnavailableError as exc:
            return SuiteResult("classify_role", len(examples), {}, GATES, skipped=True, skip_reason=str(exc))

        # A model that omits role_family entirely is a real miss, not a
        # crash — track it as its own bucket rather than a None key, which
        # would silently corrupt every family's precision/recall count.
        predicted = result.data.get("role_family") or "unclassified"
        expected = ex["expected_role_family"]

        if predicted == expected:
            correct += 1
            true_positive[expected] = true_positive.get(expected, 0) + 1
        else:
            false_positive[predicted] = false_positive.get(predicted, 0) + 1
            false_negative[expected] = false_negative.get(expected, 0) + 1

    families = set(true_positive) | set(false_positive) | set(false_negative)
    f1_scores = []
    for family in families:
        p, r = precision_recall(true_positive.get(family, 0), false_positive.get(family, 0), false_negative.get(family, 0))
        f1_scores.append(2 * p * r / (p + r) if (p + r) else 0.0)
    macro_f1 = sum(f1_scores) / len(f1_scores) if f1_scores else 0.0
    accuracy = correct / len(examples) if examples else 0.0

    return SuiteResult("classify_role", len(examples), {"precision": accuracy, "macro_f1": macro_f1}, GATES)
