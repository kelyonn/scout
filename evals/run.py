"""CLI entrypoint for evals/ — `make evals` calls this. Runs every
registered suite (or a subset named on the command line), prints a report,
writes a timestamped result file to evals/results/ (docs/runbooks/
quality-regression.md's "the eval harness writes results to evals/results/
with a timestamp, so the diff is available" — see report.py for the diff
side), and exits nonzero if any suite's gate fails. A suite that skips
(Ollama unreachable) does not fail the run on its own — see
evals/README.md — but prints visibly either way, so a broken Ollama never
reads as a silent green.

Run directly with: `make evals` (all suites) or
`make evals SUITE=classify_role` (one suite). See Makefile.
"""

from __future__ import annotations

import json
import os
import sys
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from evals.suites import classify_role, dedup, explanation
from scout_brain.llm import OllamaClient

SUITES = {
    "classify_role": classify_role,
    "dedup": dedup,
    "explanation": explanation,
}

RESULTS_DIR = Path(__file__).resolve().parent / "results"


def main(argv: list[str]) -> int:
    names = argv or list(SUITES)
    unknown = [n for n in names if n not in SUITES]
    if unknown:
        print(f"unknown suite(s): {', '.join(unknown)} — known: {', '.join(SUITES)}", file=sys.stderr)
        return 2

    llm = OllamaClient(host=os.environ.get("SCOUT_BRAIN_OLLAMA_HOST", "http://localhost:11434"))

    all_passed = True
    record: dict[str, dict[str, Any]] = {}
    for name in names:
        result = SUITES[name].run(llm)
        for line in result.report_lines():
            print(line)
        all_passed = all_passed and result.passed
        record[name] = {
            "size": result.size, "metrics": result.metrics, "gates": result.gates,
            "passed": result.passed, "skipped": result.skipped, "skip_reason": result.skip_reason,
        }

    _write_results(record)
    return 0 if all_passed else 1


def _write_results(record: dict[str, dict[str, Any]]) -> None:
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    timestamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    (RESULTS_DIR / f"{timestamp}.json").write_text(json.dumps(record, indent=2, sort_keys=True))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
