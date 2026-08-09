# ADR-005: Tiered model cascade with provider abstraction

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout uses AI for classification, deduplication, scoring, explanation, and
generation. The naive approach — send every job to a frontier model — is both
too expensive and too slow.

Cost arithmetic at Year 1 scale, 150,000 observations per day:

| Approach | Tokens/job | Cost/1M in | Daily cost | Monthly |
| --- | --- | --- | --- | --- |
| Frontier model, every job | ~2,500 | ~$3.00 | ~$1,125 | **~$34,000** |
| Small hosted model, every job | ~2,500 | ~$0.10 | ~$37 | **~$1,125** |
| Cascade (this ADR) | ~200 avg | mixed | ~$1.20 | **~$36** |

Our entire budget is ~$24/month. Only the third row is viable, and it is viable
with room to spare.

There is also a latency argument. A frontier model call is 2–8 seconds. Doing
that per job in the ingest path pushes the notification SLO out by minutes at
volume. Deterministic rules run in microseconds.

And a reliability argument, which matters most: **every LLM provider will have an
outage.** If ranking depends on one, ranking stops. That is unacceptable for a
system whose value is continuous operation.

## Options considered

### Option A — One frontier model for everything

**For:** simplest code, best quality on every task.
**Against:** ~$34,000/month. Not a real option, listed for completeness.

### Option B — One small hosted model for everything

**For:** simple, ~30x cheaper than frontier.
**Against:** still ~$1,125/month, 47x the budget. And it wastes a model call on
decisions a regex answers correctly — "does this title contain 'Intern'" does not
need a neural network.

### Option C — Fully local models

Self-hosted Llama or Qwen class models on the VPS.

**For:** zero marginal cost, no provider dependency, complete privacy.
**Against:** our VPS has no GPU. A 7B model on 4 CPU cores runs at roughly 3–5
tokens/second, which is ~8 minutes for a single classification. A GPU instance is
$200+/month. Small enough models to run on CPU (under 1B) are not good enough for
nuanced classification. Local *embeddings*, however, are entirely practical —
`bge-small-en-v1.5` is 33M parameters and embeds a job in ~15ms on CPU.

### Option D — Tiered cascade

Route each decision to the cheapest tier that can make it reliably, escalating
only on low confidence.

## Decision

**Option D**, with four tiers and a provider abstraction layer.

### The cascade

```
                       ┌─────────────────────────────────────┐
   Every job  ────────▶│ TIER 0 — Deterministic rules        │
                       │ regex, dictionaries, lookup tables  │
                       │ ~0.1ms · $0 · handles ~70%          │
                       └────────────┬────────────────────────┘
                                    │ ambiguous (~30%)
                       ┌────────────▼────────────────────────┐
                       │ TIER 1 — Local embeddings           │
                       │ bge-small-en-v1.5, CPU, batched     │
                       │ ~15ms · $0 · handles ~22%           │
                       └────────────┬────────────────────────┘
                                    │ low confidence (~8%)
                       ┌────────────▼────────────────────────┐
                       │ TIER 2 — Small hosted LLM           │
                       │ Gemini Flash class, structured out  │
                       │ ~800ms · ~$0.10/1M in · ~7.5%       │
                       └────────────┬────────────────────────┘
                                    │ genuinely hard (~0.5%)
                       ┌────────────▼────────────────────────┐
                       │ TIER 3 — Frontier model             │
                       │ user-facing generation only         │
                       │ ~3s · ~$3/1M in · ~0.5%             │
                       └─────────────────────────────────────┘
```

**Roughly 92% of decisions never reach a paid API call.**

### Tier assignment by task

| Task | Tier | Escalation trigger |
| --- | --- | --- |
| Is this software engineering? | 0 → 1 | Title matches no known pattern |
| Is this an internship or new-grad role? | 0 | Rules are reliable here |
| Is this paid? | 0 → 2 | Compensation language is ambiguous |
| Role family classification | 1 → 2 | Top-1 embedding similarity below 0.72 |
| Location tier | 0 → 2 | Location string unparseable |
| Compensation extraction | 0 → 2 | Regex fails but comp language present |
| Duplicate detection | 0 → 1 | Exact and structural both miss |
| Skill extraction | 0 → 1 | — |
| Company quality scoring | 0 | Data lookup, not inference |
| **Ranking explanation** | 2 | Always Tier 2 — user-facing, needs prose |
| Job summary | 2 | Always |
| Cover letter | 3 | Always — quality visibly matters |
| Interview questions | 3 | Always |
| Skill gap analysis | 2 → 3 | User requests depth |
| Resume improvement | 3 | Always |

The dividing principle: **Tier 3 is reserved for output the user reads as
writing.** Machine decisions never justify frontier cost; human-facing prose
sometimes does.

### Provider abstraction

All model access goes through `packages/llm` exposing `Complete`, `Structured`,
and `Embed`. Providers are configuration, not code:

```yaml
tiers:
  tier2:
    primary:   { provider: google,    model: gemini-flash }
    fallback:  [ { provider: openai,  model: gpt-mini },
                 { provider: groq,    model: llama-70b } ]
  tier3:
    primary:   { provider: anthropic, model: claude-sonnet }
    fallback:  [ { provider: openai,  model: gpt-full } ]
```

On provider failure, the client fails over to the next entry. If every provider
for a tier fails, the task **degrades rather than errors**: classification falls
back to Tier 0 rules with a `low_confidence` flag, and explanations render as
"Explanation pending" with a retry queued. Ranking never stops.

### Cost control

Enforced in the client, not by convention:

1. **Hard monthly cap** (default $20). At 80% a warning alert fires. At 100%
   Tier 2 and Tier 3 are disabled and the system runs on Tiers 0–1 only. It keeps
   working; it just gets less eloquent.
2. **Per-task token ceilings.** Job descriptions truncate to 4,000 tokens for
   classification — the signal is in the first 20% and the rest is boilerplate.
3. **Aggressive caching.** Responses cached on a hash of `(prompt, model,
   version)`. Reposted jobs, which are common, cost nothing the second time.
4. **Batching.** Embeddings batch to 64. Classifications batch to 20 per request
   where the provider supports it.
5. **Full cost attribution.** Every call records tokens and cost against a task
   type, so `SELECT task, sum(cost) FROM llm_call GROUP BY 1` answers "what is
   expensive" immediately.

### Embeddings

`bge-small-en-v1.5`, 384 dimensions, 33M parameters, running locally on CPU via
ONNX Runtime. ~15ms per job batched, zero marginal cost, no network dependency in
the ingest hot path.

Rejected: `text-embedding-3-small` (hosted; adds network latency and cost to the
hot path, and would make dedup depend on an external service — unacceptable since
dedup runs inside a transaction). Rejected: `bge-m3` (1024 dims, far better
multilingual quality, but 8x slower on CPU and we are English-dominant).

`embedding_version` is a column. Changing models means a background re-embed job,
and similarity comparisons are always scoped to a single version so a partial
migration cannot silently corrupt dedup.

## Consequences

**Positive.** ~$36/month instead of ~$34,000. 92% of decisions are instant and
free. No single provider outage stops the system. Every AI decision is
attributable and auditable. Local embeddings mean the ingest hot path has no
external dependency.

**Negative.** More complex than one model call — four tiers, escalation logic, and
confidence calibration to maintain. Tier 0 rules need occasional updating as
title conventions drift. Confidence thresholds need periodic recalibration
against the golden set. Local embedding inference consumes ~600MB RAM and
competes with Postgres for CPU during batch runs; scheduled off-peak.

**Neutral.** The cascade is itself a quality signal: a spike in Tier 2 escalation
rate means the world changed — new title conventions, a new ATS format — and is
worth alerting on.

## Reversal conditions

- Frontier model prices drop ~50x → collapse tiers and simplify.
- Tier 0 accuracy on the golden set falls below 90% → rules are drifting, move
  more work to Tier 1.
- Tier 2 escalation rate exceeds 25% → thresholds are miscalibrated or the world
  changed; investigate rather than accept the cost.
- A GPU becomes affordable (under $50/month) → move Tier 2 fully local.

## Migration path

Because everything routes through `packages/llm`, changing a provider or model is
a config change. Changing the *cascade shape* means editing the router, which is
one file with a decision table and full test coverage against the golden set.
