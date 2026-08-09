# ADR-016: Free-tier and local models; budgets denominated in requests, not rupees

**Status:** Accepted
**Date:** 2026-08-08
**Amends:** [ADR-005](ADR-005-llm-cascade.md) (tiered model cascade)

## Context

[ADR-005](ADR-005-llm-cascade.md) established the cascade — rules, then
embeddings, then a small hosted model, then a frontier model — and it is the
single most valuable decision in the project. At 150k observations/day, sending
everything to a frontier model costs about ₹2,992,000/month; the cascade costs
about ₹260. That analysis is untouched by this ADR and remains correct.

What changes is the last tier and the enforcement mechanism.

The budget is now **₹0** ([ADR-014](ADR-014-zero-cost-hosting.md)). ADR-005
assumed a $20/month cap enforced in the client, degrading to heuristics at 100%.
A $20 cap and a ₹0 budget are not the same design with a different number in it,
because **free tiers do not have spend limits. They have rate limits.**

That distinction is the whole ADR. A spend cap is a scalar you compare against
and stop. A rate limit is a per-provider, per-model, per-window constraint that
you either respect by scheduling or violate and get 429s — and the correct
response to hitting one is usually to *wait*, not to degrade, because the
allowance refills.

## Options considered

### Option A — Keep the paid Tier 3, accept a small monthly cost

**For:** the cascade as designed; frontier prose quality for cover letters and
interview prep.
**Against:** violates the ₹0 constraint outright. Also, every Tier 3 feature
(cover letters, interview questions, resume feedback, offer comparison) is
scheduled at M7 and every one of them already appears on the cut list in
[19-roadmap.md](../19-roadmap.md). Paying for the tier that serves only the
least-load-bearing features is the worst available trade.

### Option B — Local models only, via Ollama

**For:** ₹0 with certainty, no provider, no rate limit, no data leaving the host,
no account to lose. The 24 GB of RAM from ADR-014 comfortably runs an 8B
instruction model.
**Against:** an 8B local model is meaningfully weaker than a hosted small model
at exactly the task that matters most — structured classification of ambiguous
job titles — and it competes for RAM and CPU with Postgres and the embedding
model on the same box. Making it the only option means accepting a measurable
accuracy loss on the Tier 2 escalations that exist *because* they were hard.

### Option C — Free hosted tiers, rotated, with a local fallback

**For:** Google AI Studio, Groq, and OpenRouter's `:free` model variants all
offer free tiers with limits far above Scout's actual volume — the cost model
projects roughly 340 Tier 2 calls/day at Year 1 scale, which fits inside a single
provider's daily allowance with room to spare. Rotating across providers on 429
makes the effective limit the sum, not the minimum.
**Against:** free tiers are withdrawn, rate-limited, and re-scoped without
notice, and most of them reserve the right to train on submitted data. Neither is
acceptable without an explicit response.

## Decision

**Tier 3 is removed. Tier 2 is served by rotated free hosted tiers with a local
Ollama fallback. Budgets are denominated in requests per window, per provider —
not in currency.**

### The cascade as amended

| Tier | What | Cost | Handles |
| --- | --- | --- | --- |
| 0 | Deterministic rules and pattern tables | ₹0 | ~90% of classifications |
| 1 | Local embeddings, `bge-small-en-v1.5` via ONNX, 384-dim | ₹0 | Exemplar matching, semantic dedup |
| 2 | Free hosted model, provider-rotated | ₹0 | The ambiguous residue |
| 2-local | Ollama on the host | ₹0 | When every Tier 2 allowance is exhausted |
| ~~3~~ | ~~Frontier model~~ | — | **Removed** |

Tier 1 is unchanged from ADR-005 and remains the quiet workhorse: local, free,
no network in the dedup hot path, 384 dimensions.

### Provider rotation

Providers are ordered by measured quality on the classification eval set, and the
client walks the list on 429 or on daily-allowance exhaustion. Selection is
behind the same provider interface ADR-005 specified, so adding or dropping one
is configuration.

Which specific providers are configured is deliberately **not** fixed in this
ADR. Free-tier terms change faster than documents do; the list lives in
`infra/config/production.yaml`, and the requirement on it is structural: at least
two independent hosted providers, plus the local fallback, so that no single
provider's withdrawal is an outage.

### Budgets are request-shaped

This replaces `llm.monthly_cap_usd` entirely.

```yaml
llm:
  providers:
    - name: <primary>
      rpm: 15            # requests/minute
      rpd: 1000          # requests/day
      tpm: 250000        # tokens/minute
    - name: <secondary>
      rpm: 30
      rpd: 14000
  local:
    model: <8b-instruct>     # Ollama, always available, never rate limited
  on_exhaustion: degrade     # after every provider AND local are unavailable
```

Three behaviours the client must implement, in this order:

1. **Wait, do not degrade.** A per-minute limit refills in under a minute.
   Scoring is not on the notification critical path — the pipeline is queued at
   every stage precisely so that a 40-second wait costs latency, not results.
   Degrading on an RPM limit throws away accuracy to save time nobody needed.
2. **Rotate on daily exhaustion.** Move to the next provider, then to local.
3. **Degrade only when everything is exhausted**, exactly as ADR-005 specified —
   Tier 0 rules, template explanations, dedup treats uncertain pairs as distinct.

The degraded mode is unchanged. What changes is that reaching it should now be
rare and temporary rather than a month-end cliff.

### Data rule: nothing personal goes to a free tier

Free tiers commonly reserve the right to train on submitted data. Job
descriptions are public documents and sending them is fine. The user's own data
is not.

**The resume, application history, interview notes, and rejection records never
leave the host.** This is not a preference; it becomes AGENTS.md rule 9 and it is
enforced at the LLM client boundary, which refuses to construct a request whose
payload includes text drawn from those tables.

This costs less than it sounds. Resume matching is embedding cosine plus keyword
overlap ([09](../09-ranking-scoring.md) section 3.2) and was already fully local.
The features that genuinely required sending the resume to a model — resume
feedback, cover letter generation — are the Tier 3 features being removed anyway.
The constraint and the removal happen to point the same direction.

## Consequences

**Good:**

- LLM cost goes from ~₹260/month to ₹0 with no change to the pipeline's
  behaviour, because the pipeline never used Tier 3.
- Rotation across independent providers plus a local fallback is *more*
  resilient than the single paid provider ADR-005 assumed, not less. A provider
  outage was previously a degradation; now it is a failover.
- The data rule is a real privacy improvement that the paid design never forced.

**Bad, and accepted:**

- **Cover letters, interview question prediction, resume feedback, and offer
  comparison are cut**, not deferred. They were M7, they were on the cut list,
  and they are the features a student can most easily do better by hand for the
  three applications a week that matter.
- Free-tier limits can be withdrawn with no notice, which the local fallback
  covers at reduced accuracy.
- The local model competes for host resources. It is invoked rarely by design,
  and 24 GB is enough that this is a scheduling detail rather than a conflict.
- Tracking three providers' changing terms is a small recurring chore. The
  quarterly review in [18-cost-model.md](../18-cost-model.md) owns it.

## Reversal triggers

- Every viable free tier disappears simultaneously, and the local model's
  classification precision measures below the ≥97% gate on the golden set. The
  response is a paid Tier 2 at a few rupees a month, not a return to Tier 3.
- A budget appears and the removed generation features turn out to be genuinely
  wanted after the user has lived without them for a season. Living without them
  first is the point — it is the cheapest way to find out whether they were ever
  worth building.
