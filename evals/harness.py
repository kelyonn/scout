"""Shared plumbing for evals/suites/*.py — golden-set loading and the
metrics/gate comparison every suite reports through. See
docs/17-testing-qa.md section 6 for what these gates mean and
evals/README.md for which suites exist here and why (a subset of
docs/10-ai-features.md section 9's full list — see that file for the rest).
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

GOLDEN_DIR = Path(__file__).resolve().parent / "golden"


def load_golden(name: str) -> list[dict[str, Any]]:
    """Loads evals/golden/{name}.json — a JSON array of example objects,
    shape defined by the suite that consumes it.
    """
    examples: list[dict[str, Any]] = json.loads((GOLDEN_DIR / f"{name}.json").read_text())
    return examples


@dataclass
class SuiteResult:
    name: str
    size: int
    metrics: dict[str, float]
    gates: dict[str, float]
    skipped: bool = False
    skip_reason: str = ""

    @property
    def passed(self) -> bool:
        # A skip (LLM unreachable) is not a gate failure — see
        # evals/README.md. It still prints, so it is never silently green;
        # `evals/run.py`'s CI job installs and starts Ollama specifically
        # so a skip in CI is itself a signal something is wrong with the
        # environment, not with the model's quality.
        if self.skipped:
            return True
        return all(self.metrics.get(key, 0.0) >= threshold for key, threshold in self.gates.items())

    def report_lines(self) -> list[str]:
        if self.skipped:
            return [f"{self.name}: SKIPPED ({self.skip_reason})"]
        lines = [f"{self.name} (n={self.size}):"]
        for key, threshold in self.gates.items():
            value = self.metrics.get(key, 0.0)
            mark = "PASS" if value >= threshold else "FAIL"
            lines.append(f"  {mark}  {key} = {value:.3f}  (gate >= {threshold:.3f})")
        for key, value in self.metrics.items():
            if key not in self.gates:
                lines.append(f"  info  {key} = {value:.3f}")
        return lines


def precision_recall(true_positive: int, false_positive: int, false_negative: int) -> tuple[float, float]:
    precision = true_positive / (true_positive + false_positive) if (true_positive + false_positive) else 0.0
    recall = true_positive / (true_positive + false_negative) if (true_positive + false_negative) else 0.0
    return precision, recall
