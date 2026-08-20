"""explanation eval — docs/17-testing-qa.md section 6's `explanation`
suite. docs/10-ai-features.md section 9 specifies LLM-as-judge grading;
this implementation uses a deterministic rubric instead, because ADR-016
removed the frontier tier a judge would need to be meaningfully stronger
than the 3B model being judged — grading a model's output with itself is
an echo, not a judgment. The rubric checks exactly what docs/09 section 6
calls out as the failure mode ("generic, unfalsifiable, adds nothing"):
word count, at least one concrete fact from the input actually mentioned,
no banned generic phrase. Revisit this simplification if a genuinely
stronger free-tier model becomes available to route judge calls through.
"""

from __future__ import annotations

from typing import Any

from evals.harness import SuiteResult, load_golden
from scout_brain.explain import MAX_EXPLANATION_WORDS, PROMPT_TEMPLATE
from scout_brain.llm import LLMUnavailableError, OllamaClient

GATES = {"pass_rate": 0.80}

# docs/09 section 6's own "Bad" example: "This is a great opportunity that
# matches your profile well." Generic phrases this rubric rejects
# outright, regardless of what else the explanation says.
BANNED_GENERIC_PHRASES = ["great opportunity", "strong match", "amazing opportunity", "perfect fit", "matches your profile well"]


def _passes_rubric(explanation: str, example: dict[str, Any]) -> bool:
    words = explanation.split()
    if not (0 < len(words) <= MAX_EXPLANATION_WORDS):
        return False

    lowered = explanation.lower()
    if any(phrase in lowered for phrase in BANNED_GENERIC_PHRASES):
        return False

    must_mention_any = example.get("must_mention_any", [])
    return not must_mention_any or any(fact.lower() in lowered for fact in must_mention_any)


def run(llm: OllamaClient) -> SuiteResult:
    examples = load_golden("explanation")
    passed = 0

    for ex in examples:
        prompt = PROMPT_TEMPLATE.format(
            title=ex["title"], seniority=ex["seniority"], work_mode=ex["work_mode"],
            top_factors=", ".join(ex["top_factors"]) if ex["top_factors"] else "none computed yet",
            matched_skills=", ".join(ex["matched_skills"]) if ex["matched_skills"] else "none",
            missing_skills=", ".join(ex["missing_skills"]) if ex["missing_skills"] else "none",
            location=ex["location"], compensation=ex["compensation"],
            company_name=ex["company_name"],
            company_description=(f" — {ex['company_description']}" if ex.get("company_description") else ""),
        )
        try:
            result = llm.generate_json(prompt)
        except LLMUnavailableError as exc:
            return SuiteResult("explanation", len(examples), {}, GATES, skipped=True, skip_reason=str(exc))

        explanation = str(result.data.get("explanation", "")).strip()
        if _passes_rubric(explanation, ex):
            passed += 1

    pass_rate = passed / len(examples) if examples else 0.0
    return SuiteResult("explanation", len(examples), {"pass_rate": pass_rate}, GATES)
