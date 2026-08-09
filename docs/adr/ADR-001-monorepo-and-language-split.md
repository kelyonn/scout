# ADR-001: Monorepo with a three-language split

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout has four workloads with genuinely different performance profiles:

1. **Massively concurrent I/O** — thousands of simultaneous HTTP fetches, mostly
   idle, each cheap. Memory per connection is the binding constraint.
2. **ML and NLP** — embeddings, classification, structured LLM output. Bound by
   library ecosystem, not by language performance.
3. **Request serving** — low-latency, stateless, moderate concurrency.
4. **UI** — necessarily TypeScript and React.

One language for all four means accepting a bad fit for at least two of them.
Three languages means three toolchains for one developer to maintain.

The canonical job schema must be shared across all of them and must never drift.
Schema drift between a Go writer and a Python reader is a class of bug that costs
hours every time it appears.

## Options considered

### Option A — All Python

**For:** one toolchain, one dependency manager, best ML ecosystem, fastest to
write.
**Against:** the collector is the hardest part of this system and Python is the
worst tool for it. 5,000 concurrent asyncio connections is roughly 400–600MB and
requires care to avoid blocking the loop; Go does the same in ~80MB with no care
at all. On an 8GB VPS that difference is meaningful. HTML parsing and hashing
across 20k observations/day is CPU-bound work that Python does 10–30x slower.

### Option B — All Go

**For:** one toolchain, excellent concurrency, single static binary deploys,
tiny memory footprint, strong typing.
**Against:** the ML story is genuinely painful. Running embeddings means ONNX
Runtime via cgo, which breaks cross-compilation and static linking — the two main
reasons to pick Go for deployment in the first place. `pydantic`-style structured
LLM output, tokenizer parity with the model, and the evaluation tooling all have
no real Go equivalent. We would spend weeks reimplementing infrastructure that
exists and works in Python.

### Option C — All TypeScript

**For:** one language across the stack, good async, huge ecosystem.
**Against:** worst of both — meaningfully slower than Go for the collector,
meaningfully worse than Python for ML. Node's memory per connection sits between
the two. Chosen for team-uniformity reasons that do not apply to one developer
who is comfortable in all three.

### Option D — Go + Python + TypeScript, monorepo

**For:** each workload gets the right tool. Collector and API in Go, brain in
Python, web in TypeScript. Monorepo keeps the schema in one place with generated
bindings, so cross-language changes are atomic in a single commit and a single CI
run.
**Against:** three toolchains, three dependency managers, three CI paths. Higher
context-switching cost. A contributor needs all three installed.

### Option E — Same split, polyrepo

**For:** independent versioning and CI per service.
**Against:** a schema change becomes four coordinated PRs across four
repositories with version-skew windows in between. For one developer this is pure
overhead with no compensating benefit — polyrepo solves a team-coordination
problem we do not have.

## Decision

**Option D.** Monorepo with three languages, split by workload:

| Language | Owns | Rationale |
| --- | --- | --- |
| Go 1.23 | collector, api, notifier | Concurrency, memory efficiency, single-binary deploy |
| Python 3.12 | brain | ML ecosystem is decisive |
| TypeScript | web | Only option |

**Boundary rule:** Go and Python never call each other synchronously. They
communicate through Postgres and the job queue. This means either can be
restarted, redeployed, or rewritten without coordinating with the other, and no
one has to debug a cross-language RPC layer.

**Schema:** `packages/schema` holds JSON Schema definitions as the single source
of truth. Code generation produces Go structs, Pydantic models, and TypeScript
types. CI fails if generated code is out of date with the schema.

**Tooling:** Turborepo orchestrates, Go workspaces for Go modules, `uv` for
Python, `pnpm` for TypeScript. One `make dev` starts everything.

## Consequences

**Positive.** Each workload runs on a runtime suited to it. Schema changes are
atomic. One CI pipeline tests the full system. Cross-cutting refactors are one PR.

**Negative.** Three language runtimes in CI, which costs roughly 90 seconds of
build time. New-contributor setup is heavier. Some duplicated utility code across
languages — accepted deliberately over building a shared FFI layer.

**Neutral.** Docker images differ per service, which we would have anyway.

## Reversal conditions

- If the brain's work turns out to be dominated by orchestration rather than ML,
  fold it into Go and drop Python.
- If collector memory never exceeds ~500MB at Year 1 scale, the Go argument
  weakens and consolidating on Python becomes reasonable.
- If the monorepo's CI exceeds 10 minutes, split the web app out first.

## Migration path

The Postgres boundary between Go and Python means either side can be rewritten in
the other language incrementally, one queue consumer at a time, with no big-bang
cutover.
