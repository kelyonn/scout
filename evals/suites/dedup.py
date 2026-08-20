"""dedup eval — docs/17-testing-qa.md section 6's `dedup` suite, scoped to
Stage 3's LLM adjudication (scout_brain.dedup_stage3.DedupStage3Consumer) —
the only genuinely uncertain step in three-stage dedup. Stage 1 (exact
URL) and Stage 2 (structural/SimHash) are deterministic and covered by
their own Go unit tests (apps/collector/internal/dedup). Calls the real
production prompt (ADJUDICATION_PROMPT) and applies the same
confidence-threshold merge decision DedupStage3Consumer._adjudicate makes,
so this eval measures the actual merge/no-merge outcome, not just the raw
same_role answer.
"""

from __future__ import annotations

from evals.harness import SuiteResult, load_golden, precision_recall
from scout_brain.dedup_stage3 import ADJUDICATION_PROMPT, LLM_MERGE_CONFIDENCE_THRESHOLD
from scout_brain.llm import LLMUnavailableError, OllamaClient

# docs/17: "dedup precision is gated harder than recall" — a false merge
# is a silent missed opportunity, a missed merge is a visible duplicate.
GATES = {"precision": 0.99, "recall": 0.92}


def run(llm: OllamaClient) -> SuiteResult:
    examples = load_golden("dedup")

    true_positive = false_positive = false_negative = 0

    for ex in examples:
        prompt = ADJUDICATION_PROMPT.format(
            title_a=ex["title_a"], location_a=ex["location_a"], desc_a=ex["desc_a"][:1500],
            title_b=ex["title_b"], location_b=ex["location_b"], desc_b=ex["desc_b"][:1500],
        )
        try:
            result = llm.generate_json(prompt)
        except LLMUnavailableError as exc:
            return SuiteResult("dedup", len(examples), {}, GATES, skipped=True, skip_reason=str(exc))

        same_role = bool(result.data.get("same_role"))
        confidence = float(result.data.get("confidence", 0))
        predicted_merge = same_role and confidence >= LLM_MERGE_CONFIDENCE_THRESHOLD
        expected_merge = bool(ex["expected_same_role"])

        if predicted_merge and expected_merge:
            true_positive += 1
        elif predicted_merge and not expected_merge:
            false_positive += 1
        elif not predicted_merge and expected_merge:
            false_negative += 1
        # true-negative (correctly left distinct) doesn't feed precision/recall

    precision, recall = precision_recall(true_positive, false_positive, false_negative)
    return SuiteResult("dedup", len(examples), {"precision": precision, "recall": recall}, GATES)
