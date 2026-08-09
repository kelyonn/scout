# Runbook: LLM budget exceeded or cost anomaly

**Severity:** SEV3 (warning at 80%) / SEV2 (cost anomaly)
**Alert:** `scout_llm_budget_used_ratio > 0.8`, or daily spend >3× average

The system degrades gracefully rather than breaking, so this is rarely urgent —
but a runaway cost can burn a month's budget in hours, so diagnose promptly.

---

## What is spending

```sql
SELECT task, tier, model,
       count(*) AS calls,
       round(sum(cost_usd), 4) AS cost,
       round(avg(input_tokens)) AS avg_in,
       round(avg(output_tokens)) AS avg_out,
       round(sum(cached::int)::numeric / count(*), 3) AS cache_rate
FROM llm_call
WHERE occurred_at > now() - interval '7 days'
GROUP BY 1, 2, 3 ORDER BY 5 DESC;
```

```sql
-- When did it change?
SELECT date_trunc('day', occurred_at) AS day, tier,
       count(*), round(sum(cost_usd), 4)
FROM llm_call WHERE occurred_at > now() - interval '30 days'
GROUP BY 1, 2 ORDER BY 1 DESC, 2;
```

---

## Diagnose by pattern

| Pattern | Cause | Fix |
| --- | --- | --- |
| Tier 2 call volume up sharply | Escalation rate rose — Tier 0/1 stopped handling cases | Below |
| Tier 3 volume up | User generating many cover letters, or a loop | Check for a retry loop |
| Cache rate collapsed | Prompt version changed, invalidating the cache | Expected after a prompt change; it recovers |
| `avg_in` rose | Truncation limit changed, or descriptions got longer | Check `llm.truncation` config |
| Same prompt repeatedly | Retry loop | Below |
| One task dominates | Usually `explain_ranking` after a mass rescore | Expected; verify it ends |

### Escalation rate rose

```promql
scout_llm_escalation_ratio{from_tier="0"}
scout_llm_escalation_ratio{from_tier="1"}
```

Tier 2 should handle roughly 8% of classifications. Above 25% means Tier 0 rules
or Tier 1 embeddings stopped matching. Causes:

- **New title conventions** in the market (real, seasonal — August brings new
  phrasings). Fix: add patterns to the Tier 0 rules file.
- **A rules file regression.** Check `git log -- packages/taxonomy/`.
- **Embedding model or version mismatch**, so Tier 1 similarity always falls
  below threshold. Check `embedding_version` consistency.

### Retry loop

```sql
SELECT prompt_hash, model, count(*), min(occurred_at), max(occurred_at),
       count(*) FILTER (WHERE error IS NOT NULL) AS errors
FROM llm_call WHERE occurred_at > now() - interval '6 hours'
GROUP BY 1, 2 HAVING count(*) > 20 ORDER BY 3 DESC;
```

The same prompt hash called dozens of times means a job is failing validation and
retrying. Find it:

```sql
SELECT id, queue, attempt, max_attempts, errors FROM river_job
WHERE state = 'retryable' AND queue IN ('score','classify')
ORDER BY attempt DESC LIMIT 10;
```

Cancel the poisoned job, then fix why its output fails schema validation.

---

## Immediate cost control

```bash
# Kill switch — stops all paid calls instantly. Tiers 0-1 continue.
curl -X POST localhost:8080/api/v1/admin/llm/kill-switch -H "Cookie: $SESSION"

# Or disable only Tier 3 (the expensive one)
curl -X PATCH localhost:8080/api/v1/admin/llm/config \
  -d '{"tier3_enabled": false}' -H "Cookie: $SESSION"
```

**What still works with all paid calls disabled:** ingestion, normalization
(Tier 0 rules), classification (rules only, with `low_confidence` flags), dedup
(exact, structural, and semantic — the LLM only handles the 0.8% uncertainty
band, and those default to "distinct", which is the safe direction), scoring
(all 13 subscores are deterministic), notifications with template explanations,
and the entire dashboard.

**What stops:** LLM explanations and classification of genuinely ambiguous
titles. (Cover letters, interview questions, and resume feedback are cut — see
[ADR-016](../adr/ADR-016-free-tier-llm-cascade.md).)

The product remains fully functional. It just gets less articulate.

---

## There is no cap to raise

This is the part that changed most under
[ADR-016](../adr/ADR-016-free-tier-llm-cascade.md), and getting it wrong wastes
an evening looking for a setting that does not exist.

**Free tiers rate-limit; they do not bill.** There is no `monthly_cap_usd`, no
spend to authorize, and no "raise the cap" lever. What you are looking at is one
of three states, and they need opposite responses:

| State | What happened | Correct response |
| --- | --- | --- |
| **RPM exhausted** | Per-minute allowance hit | **Wait.** It refills in under a minute. The client already queues. Do nothing. |
| **RPD exhausted on one provider** | Daily allowance hit | Confirm rotation moved to the next provider. If it did not, that is the bug. |
| **All providers + local exhausted** | Genuine saturation | Degraded mode is correct and working. Investigate *why* volume is up. |

**Waiting is the right answer far more often than degrading**, which is the
inversion from the paid design. A spend cap is terminal — money is gone and
degrading conserves it. A rate limit is temporary — the allowance refills, and
degrading throws away classification accuracy to save time that nothing needed.
Scoring is not on the notification critical path; every stage is queued precisely
so a 40-second wait costs latency rather than results.

**If you are here because degraded mode triggered**, the question is not "how do
I buy more" but "why is volume 3× normal?" Usual causes, in order:

1. A backfill running without `suppress_notifications` and re-classifying
   everything (AGENTS.md rule 3).
2. Tier 0 rule drift — patterns stopped matching, so everything escalates. Check
   the escalation rate against its 28-day median.
3. A prompt-cache miss storm from a churning prompt version.
4. A genuinely new source category producing titles the rules have never seen.

**The only "more capacity" lever that exists** is adding a third free provider to
the rotation, which is a config change:

```yaml
llm:
  providers:
    - { name: <primary>,   rpm: 15, rpd: 1000 }
    - { name: <secondary>, rpm: 30, rpd: 14000 }
    - { name: <new>,       rpm: 20, rpd: 500 }    # add here
```

Paying for a tier is **out of scope** — the budget is ₹0 and it is a wall, not a
target (AGENTS.md rule 10). If free capacity is genuinely insufficient after
fixing the escalation rate, that is an
[ADR-016](../adr/ADR-016-free-tier-llm-cascade.md) reversal trigger and a
decision to make deliberately, not at 2am.

---

## Optimization levers, in order of impact

1. **Fix the escalation rate.** Moving 10% of traffic from Tier 2 back to Tier 0
   costs nothing and is usually just adding title patterns.
2. **Improve cache hit rate.** Target ≥35%. Check that prompt versions are not
   churning.
3. **Tighten truncation.** Classification uses the first 3,000 characters; the
   signal is in the first 20%. 2,000 is often enough.
4. **Batch more aggressively.** Where the provider supports it, batching 20
   classifications per request cuts per-call overhead substantially.
5. **Switch the Tier 2 primary** to a cheaper provider. This is a config change
   — re-run the eval suites afterward to confirm quality holds.

---

## Verification

```sql
SELECT round(sum(cost_usd), 4) FROM llm_call
WHERE occurred_at > date_trunc('month', now());
```

Confirm the burn rate projects within the cap for the remainder of the month.

---

## Follow-up

1. **Was any output degraded** during the period the kill switch was on?
   Regenerate explanations for jobs scored during that window:
   ```bash
   curl -X POST localhost:8080/api/v1/admin/regenerate-explanations \
     -d '{"since":"<timestamp>"}'
   ```
2. **Add an alert** if this cost pattern was not caught by existing monitoring.
