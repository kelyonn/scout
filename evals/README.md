# evals

Golden sets and eval harnesses. See [`docs/17-testing-qa.md`](../docs/17-testing-qa.md)
section 6 and [`docs/10-ai-features.md`](../docs/10-ai-features.md) section 9 for the
full suite list and what these gates mean; this directory implements a subset of it.

## Running

```bash
make evals                    # every suite
make evals SUITE=dedup        # one suite
make evals-report             # diff the latest run against the last passing one
```

`make evals` calls `apps/brain`'s own Python environment (`uv run`), with this
directory added to `PYTHONPATH` so `evals.*` is importable alongside
`scout_brain` — see the Makefile for the exact invocation. Each run writes a
timestamped result file to `evals/results/` (gitignored — run history, not
source), which `make evals-report` reads.

## What exists

| Suite | Size | Gate | What it calls |
| --- | --- | --- | --- |
| `classify_role` | 22 jobs | precision ≥0.97, macro-F1 ≥0.93 | `scout_brain.classify_tier2`'s real prompt |
| `dedup` | 16 pairs | precision ≥0.99, recall ≥0.92 | `scout_brain.dedup_stage3`'s real adjudication prompt + merge threshold |
| `explanation` | 15 jobs | pass rate ≥0.80 | `scout_brain.explain`'s real prompt, graded by a deterministic rubric |

Each suite calls the **actual production prompt** from the module it evaluates —
not a copy — so a prompt edit is measured by the same eval that gates it.

**Not implemented here:** `is_software`, `seniority`, `paid_inference`,
`location_tier`, `comp_parse`, `ranking`, `advocacy-classification`. The first
five are deterministic Go/rule-based logic (Tier 0 classification, the
normalize package) already covered by ordinary unit tests with fixed-answer
assertions — an eval suite with a precision/recall gate earns its keep on the
genuinely non-deterministic LLM-touching parts, not on a parser that either
gets a test case right or doesn't. `ranking`'s NDCG suite needs a hand-ordered
ideal ranking over real jobs, which doesn't exist yet with only P1/P2 built.
`advocacy-classification` is currently exercised inside `classify_role`'s
golden set (the `advocacy.*` examples) rather than as a separate suite.

**`explanation`'s rubric is deterministic, not LLM-as-judge**, which
docs/10-ai-features.md section 9 specifies. ADR-016 removed the frontier tier a
judge would need to be meaningfully stronger than the 3B model being judged —
grading a model's own output with itself is an echo, not a judgment. The
rubric instead checks exactly what docs/09 section 6 calls out as the failure
mode: word count, at least one concrete fact from the input actually
mentioned, no banned generic phrase ("great opportunity", "strong match", …).
Revisit if a genuinely separate, stronger free-tier model becomes available to
route judge calls through.

## Honest current state

As of 2026-08-18, running `make evals` with **no hosted LLM provider
configured** (the common case — see [ADR-016](../docs/adr/ADR-016-free-tier-llm-cascade.md)
and `infra/config/llm_providers.yaml`) exercises only the cascade's local
Ollama fallback tier (`qwen2.5:3b-instruct`), and at that tier:

- `classify_role` fails its gate (measured ~0.82 precision, ~0.74 macro-F1
  against 0.97/0.93) — a 3B local model is measurably weaker than the hosted
  small models Tier 2 is designed around, exactly the tradeoff
  [ADR-016](../docs/adr/ADR-016-free-tier-llm-cascade.md) names as accepted.
- `dedup` passes precision (1.0) but fails recall (~0.56 against 0.92) — the
  model tends to under-merge relative to `LLM_MERGE_CONFIDENCE_THRESHOLD`,
  i.e. it get the same-role/different-role call right more often than it
  states ≥0.85 confidence in it. Under-merging is the *safer* failure
  direction (AGENTS.md rule 4: "bias dedup toward under-merging" — a false
  merge is silent, a missed merge is visible), so this is a real quality gap
  worth closing but not the dangerous direction to have it in.
- `explanation` passes (~0.87 against 0.80).

**This is real, measured behavior, not a bug in the harness** — the gate
thresholds are left at the values docs/17 specifies rather than loosened to
make a local-only run pass, because the whole point of a gate is to say
honestly when quality doesn't clear the bar. Configuring at least one hosted
provider (`SCOUT_BRAIN_GEMINI_API_KEY`, `SCOUT_BRAIN_GROQ_API_KEY`, or
`SCOUT_BRAIN_OPENROUTER_API_KEY`) routes Tier 2 calls to a stronger model
first, which is expected to close most of this gap — that has not yet been
measured against these golden sets.

**Golden sets are starter-sized, not the docs' full target (400/500 jobs).**
Precision/recall on 15-22 examples moves in large, noisy steps — one wrong
call shifts `classify_role` precision by ~4.5 points. The rule in
[`docs/17-testing-qa.md`](../docs/17-testing-qa.md) is the growth path: **every
production quality bug becomes a golden-set entry before it is fixed**, so
these grow from real failures rather than being backfilled with more
synthetic examples up front.

## CI

Wired as the `evals` job in `.github/workflows/ci.yml`, which installs Ollama,
pulls `qwen2.5:3b-instruct`, and runs `make evals`. Given the section above,
this job is expected to show red on `classify_role` and `dedup` until either
hosted provider secrets are added to the repo or Tier 2's local-only quality
improves — that is the gate doing its job, not a broken workflow. If the repo
gains `SCOUT_BRAIN_GEMINI_API_KEY`/`SCOUT_BRAIN_GROQ_API_KEY`/
`SCOUT_BRAIN_OPENROUTER_API_KEY` as GitHub Actions secrets, the CI job passes
them through automatically (see the workflow) and exercises the real cascade
instead of the local-only floor.
