"""CLI entrypoint for `make evals-report` — docs/runbooks/quality-
regression.md's "per-suite eval diff against the last passing run." Reads
evals/results/*.json (written by run.py, one file per `make evals`
invocation) and diffs the most recent run against the most recent run
before it where every suite passed. If the most recent run is itself the
last passing one, there is nothing to diff against and this says so rather
than comparing a run to itself.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

RESULTS_DIR = Path(__file__).resolve().parent / "results"


def _load_runs() -> list[tuple[str, dict[str, Any]]]:
    files = sorted(RESULTS_DIR.glob("*.json"))
    return [(f.stem, json.loads(f.read_text())) for f in files]


def _all_passed(run: dict[str, Any]) -> bool:
    return all(suite["passed"] for suite in run.values())


def main() -> int:
    runs = _load_runs()
    if not runs:
        print("no results in evals/results/ yet — run `make evals` first", file=sys.stderr)
        return 1

    current_ts, current = runs[-1]
    previous_passing = next(((ts, r) for ts, r in reversed(runs[:-1]) if _all_passed(r)), None)

    if previous_passing is None:
        print(f"current run: {current_ts}")
        print("no earlier passing run to diff against yet.")
        return 0

    previous_ts, previous = previous_passing
    print(f"comparing {current_ts} (current) against {previous_ts} (last passing)\n")

    suites = sorted(set(current) | set(previous))
    for suite in suites:
        cur = current.get(suite)
        prev = previous.get(suite)
        if cur is None or prev is None:
            print(f"{suite}: only present in one run, skipping diff")
            continue
        print(f"{suite}:")
        metrics = sorted(set(cur["metrics"]) | set(prev["metrics"]))
        for metric in metrics:
            cur_val = cur["metrics"].get(metric)
            prev_val = prev["metrics"].get(metric)
            if cur_val is None or prev_val is None:
                continue
            delta = cur_val - prev_val
            flag = " <-- regressed" if delta < -0.03 else ""
            print(f"  {metric}: {prev_val:.3f} -> {cur_val:.3f} ({delta:+.3f}){flag}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
