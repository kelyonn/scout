# ADR-008: Three-stage deduplication

**Status:** Accepted
**Date:** 2026-08-06

## Context

The brief states the requirement plainly: never notify twice for the same role,
even when URLs differ, titles change, descriptions are edited, or the job appears
on multiple platforms.

This is harder than it sounds. The same Cloudflare internship legitimately
appears as:

- `boards.greenhouse.io/cloudflare/jobs/5847392` — "Software Engineering Intern"
- `cloudflare.com/careers/jobs/5847392` — "Software Engineering Intern, Summer 2027"
- A Hacker News "Who is Hiring" comment — "Cloudflare | SWE Intern | Remote"
- A LinkedIn job alert email — "Software Engineer Intern at Cloudflare"
- A tweet from a Cloudflare recruiter with a shortened link

Five observations, one opportunity, one notification.

The failure modes are asymmetric and that asymmetry drives the design:

- **False positive (wrongly merging two distinct jobs)** — the user never sees
  one of them. This is a silent miss, and silent misses are the exact failure
  Scout exists to prevent. **Cost: high.**
- **False negative (failing to merge duplicates)** — the user gets two
  notifications for one job. Annoying, visible, correctable. **Cost: low.**

Therefore: **bias toward not merging.** When uncertain, keep them separate and
surface the uncertainty in the UI rather than guessing.

A pure-embedding approach fails this test. Cosine similarity between two genuinely
different internships at the same company is often 0.93+, because job descriptions
share enormous boilerplate — the same benefits paragraph, the same EEO statement,
the same "about us" section. Semantic similarity alone would merge "Backend
Intern" with "Frontend Intern" at the same company. It also costs an embedding
lookup on every single job, which is wasteful when 60% of duplicates are provably
identical by URL.

## Options considered

### Option A — Exact matching only (URL, ATS job ID)

**For:** free, certain, no false positives.
**Against:** catches only ~60% of duplicates. Misses every cross-platform case,
which is the case the requirement is actually about.

### Option B — Fuzzy title and company matching

**For:** cheap, catches obvious cross-posting.
**Against:** brittle. "SWE Intern" vs "Software Engineering Intern, Summer 2027"
vs "Intern - Software Development" are the same role and share few tokens.
Meanwhile "Backend Intern" and "Frontend Intern" are different roles and share
most tokens.

### Option C — Embeddings only

**For:** handles rewrites, paraphrase, translation.
**Against:** the boilerplate problem above produces dangerous false positives.
Also the most expensive option per job, applied to cases cheaper methods solve.

### Option D — LLM adjudication on every pair

**For:** highest accuracy on genuinely hard cases.
**Against:** cost is quadratic in candidates, and latency is seconds per pair.
Unusable in the ingest hot path.

### Option E — Three cascading stages, cheapest first

## Decision

**Option E.** Three stages, each running only on what the previous could not
resolve, with an LLM adjudicator reserved for a narrow uncertainty band.

```
  new job
     │
     ▼
┌─────────────────────────────────────────────────────────┐
│ STAGE 1 — EXACT            cost: ~0.1ms   catches ~60%  │
│                                                          │
│ • canonical_url_hash matches an existing job            │
│ • (ats_platform, ats_job_id) matches                    │
│ • content_hash matches                                  │
│                                                          │
│ MATCH → merge with certainty 1.0, done                  │
└───────────────────────┬─────────────────────────────────┘
                        │ no match
                        ▼
┌─────────────────────────────────────────────────────────┐
│ STAGE 2 — STRUCTURAL       cost: ~2ms     catches ~30%  │
│                                                          │
│ Candidate set: same company_id, posted within 45 days   │
│ Required:  normalized_title similarity ≥ 0.85 (Jaro-W)  │
│        AND same role_family                             │
│        AND compatible location (same tier + city, or    │
│            one is remote and the other is remote/hybrid)│
│ Then:      SimHash Hamming distance on description      │
│            shingles, boilerplate stripped               │
│                                                          │
│ distance ≤ 3   → merge, certainty 0.95                  │
│ distance 4–8   → send to Stage 3                        │
│ distance > 8   → send to Stage 3                        │
└───────────────────────┬─────────────────────────────────┘
                        │ unresolved
                        ▼
┌─────────────────────────────────────────────────────────┐
│ STAGE 3 — SEMANTIC         cost: ~20ms    catches ~9%   │
│                                                          │
│ pgvector kNN over job embeddings, filtered to           │
│ same company_id AND same role_family AND 90-day window  │
│ Embeddings computed on BOILERPLATE-STRIPPED text.       │
│                                                          │
│ cosine ≥ 0.94  → merge, certainty 0.90                  │
│ cosine 0.88–0.94 → LLM adjudication (Tier 2)            │
│ cosine < 0.88  → distinct job, new job_group            │
└───────────────────────┬─────────────────────────────────┘
                        │ uncertainty band only (~0.8%)
                        ▼
┌─────────────────────────────────────────────────────────┐
│ ADJUDICATION — Tier 2 LLM   cost: ~$0.0001  ~0.8%       │
│                                                          │
│ Structured output: { same_role: bool, confidence: 0-1,  │
│                      reason: string }                    │
│ confidence ≥ 0.85 AND same_role → merge, certainty 0.80 │
│ otherwise → distinct, and flag for UI review            │
└─────────────────────────────────────────────────────────┘
```

### The boilerplate problem, solved explicitly

Before hashing or embedding, description text passes through boilerplate
stripping, because it is the single largest source of false-positive similarity:

1. Remove any paragraph appearing in ≥5 other postings from the same company
   (learned per company, refreshed weekly). This kills "About Us", benefits, and
   EEO statements automatically without maintaining a blocklist.
2. Remove known legal boilerplate by pattern (EEO, accommodation, privacy).
3. Remove application instructions and "how to apply" sections.
4. Keep: responsibilities, requirements, tech stack, team description.

Measured effect on the golden set: baseline cosine similarity between two
*different* roles at the same company drops from ~0.93 to ~0.71, which is what
makes a 0.94 merge threshold safe.

### Company identity resolution comes first

Stages 2 and 3 both filter by `company_id`, so company resolution must happen
first and must be right. Full algorithm in [08](../08-dedup-identity.md); the
signals are registered domain, normalized legal name, ATS board token, and known
alias table.

### Grouping and merge semantics

Merges use a union-find structure materialized as `job_group`. When job C matches
both group A and group B, A and B merge transitively.

Every merge writes a `job_merge_event` row recording stage, certainty, and the
deciding signal. This exists so that when a merge turns out to be wrong, we can
find out why, and so that merges are reversible — an `unmerge` operation splits a
group and re-runs dedup on the members with an exclusion.

**Notification identity is `job_group_id`.** The uniqueness guarantee is enforced
by a database constraint, not application logic:

```sql
CREATE UNIQUE INDEX notification_dedup_idx
  ON notification (user_id, job_group_id, trigger);
```

This is deliberate. Application-level dedup checks lose races. A unique index does
not.

### Cross-company merges never happen automatically

If the same role appears under two `company_id` values, that is a company
resolution failure, not a job dedup problem. It is flagged for review and fixed
at the company layer. Automatically merging across companies would risk merging
two genuinely different companies' roles, which is the high-cost error.

## Consequences

**Positive.** Roughly 90% of duplicates resolved without an embedding lookup and
99.2% without an LLM call. Bias toward under-merging protects against silent
misses. Every merge is explainable and reversible. The notification guarantee is
a database constraint.

**Negative.** Three code paths to maintain and test. Boilerplate stripping needs
per-company learning, which requires history — new companies get worse dedup for
their first few postings. Thresholds (0.85, 3, 0.94, 0.88) need periodic
recalibration against the golden set. Union-find merges can cascade unexpectedly,
so group size is monitored and any group above 15 members alerts as likely
over-merging.

**Neutral.** The uncertainty band surfaces in the UI as "possibly the same as…",
turning an algorithmic limitation into a small user-facing feature.

## Reversal conditions

- Measured false-positive merge rate above 1% → raise thresholds, widen the
  adjudication band.
- False-negative rate above 10% → lower thresholds or improve boilerplate
  stripping.
- Stage 3 exceeding 50ms p95 → the HNSW index needs tuning or the candidate
  filter is too wide.

## Migration path

Thresholds are configuration, not constants, and are versioned. A threshold
change triggers a re-dedup backfill over the last 90 days with notifications
suppressed. Group merges are reversible, so a bad threshold change is recoverable
rather than permanent.
