# Deduplication and Identity Resolution — Scout

**Status:** Draft · **Owner:** Discovery · **Last updated:** 2026-08-06

The algorithms behind "never notify me twice for the same role." Design rationale
is in [ADR-008](adr/ADR-008-three-stage-deduplication.md); this document is the
implementation specification.

---

## 1. Two problems, in order

**Company identity resolution** must happen first, because job dedup filters
candidates by `company_id`. If "Meta" and "Facebook" are two companies, their
duplicate postings will never be compared.

**Job deduplication** groups jobs that are the same opportunity.

Getting company resolution wrong causes job dedup to fail silently, which is why
it gets its own section and its own quality gate.

---

## 2. Company identity resolution

`SCOUT-DEDUP-001` through `SCOUT-DEDUP-005`.

### 2.1 Signals, in confidence order

| Signal | Confidence | Notes |
| --- | --- | --- |
| Registered domain match | 0.98 | `cloudflare.com` — the strongest signal. Uses the public suffix list, so `careers.cloudflare.com` and `cloudflare.com` are one domain. |
| ATS board token match | 0.95 | `greenhouse.io/cloudflare` — a company owns its token |
| Exact normalized name | 0.85 | After suffix stripping |
| Known alias | 0.90 | Curated `company_alias` table |
| Fuzzy name + domain overlap | 0.75 | Trigram similarity ≥ 0.85 plus a shared domain fragment |
| Fuzzy name alone | 0.50 | **Not sufficient to merge** |

### 2.2 Name normalization

```
"Cloudflare, Inc."           → "cloudflare"
"Meta Platforms Ireland Ltd" → "meta platforms"
"Zoho Corporation Pvt. Ltd." → "zoho"
"Tata Consultancy Services"  → "tata consultancy services"  (no suffix present)

Rules:
  lowercase
  strip legal suffixes: inc, incorporated, corp, corporation, ltd, limited,
    llc, llp, plc, gmbh, ag, sa, bv, nv, pty, pvt, private, co, company,
    holdings, group, technologies, technology, labs, systems
    — only when trailing, and only when the remainder is ≥3 characters
  strip punctuation except internal hyphens
  collapse whitespace
  expand common ampersands: "&" → "and"
```

**The suffix-stripping trap.** "Systems" is a legal-ish suffix but "Cisco
Systems" is the company's actual name and "Palantir Technologies" is too.
Stripping is therefore *additive, not destructive*: both the stripped and
unstripped forms are indexed as aliases, and a match on either counts. This
avoids the class of bug where "X Technologies" and "X" are treated as different
companies in one direction and the same in the other.

### 2.3 Resolution algorithm

```
resolve(name_raw, domain_hint, ats_token):

  1. If domain_hint:
       registered = public_suffix_extract(domain_hint)
       hit = company WHERE registered_domain = registered
       if hit: return hit (0.98)

  2. If ats_token:
       hit = company_alias WHERE alias = ats_token AND kind = 'ats_token'
       if hit: return hit.company (0.95)

  3. normalized = normalize_name(name_raw)
     hit = company WHERE normalized_name = normalized
     if hit: return hit (0.85)

  4. hit = company_alias WHERE alias = normalized
     if hit: return hit.company (0.90)

  5. candidates = company WHERE similarity(normalized_name, normalized) > 0.85
                  ORDER BY similarity DESC LIMIT 5
     for c in candidates:
       if shares_domain_fragment(c, domain_hint) OR
          shares_ats_token(c, ats_token):
         return c (0.75)

  6. If candidates and best similarity > 0.92:
       flag for review, create provisionally, link as 'possible_duplicate'

  7. Create a new company (0.40 — provisional)
```

**Step 6 is the safety valve.** Creating a duplicate company is recoverable — a
merge tool fixes it and re-runs dedup. Wrongly merging two companies is worse,
because their jobs conflate and the error is invisible. Bias toward creating.

### 2.4 Company merge

When two companies turn out to be one:

```sql
-- Transactional, auditable, reversible
BEGIN;
  UPDATE job    SET company_id = :keep WHERE company_id = :merge;
  UPDATE source SET company_id = :keep WHERE company_id = :merge;
  INSERT INTO company_alias (company_id, alias, alias_kind, confidence)
    SELECT :keep, normalized_name, 'former_name', 1.0
    FROM company WHERE id = :merge;
  UPDATE company SET deleted_at = now(), merged_into = :keep WHERE id = :merge;
  INSERT INTO company_merge_event (kept_id, merged_id, reason, actor) ...;
COMMIT;
-- then enqueue re-dedup for all affected job groups
```

Re-dedup after a company merge is mandatory: jobs that could not be compared
before are now candidates.

### 2.5 Deliberate non-merges

| Case | Treatment |
| --- | --- |
| Subsidiaries (Google / YouTube / DeepMind) | **Separate companies**, linked by a `parent_company_id`. They hire separately with different processes and cultures, and the user cares about the distinction. |
| Regional entities (Microsoft India / Microsoft Corp) | **One company.** Same hiring org for our purposes. Region is captured in the job's location. |
| Consultancy staffing a client role | **The consultancy is the employer.** Recording the client as employer would be wrong and would break watchlists. Client name goes in a `client_hint` field. |
| Acquired companies still hiring separately | Separate until their ATS consolidates, then merged with an alias. |

---

## 3. Job deduplication

### 3.1 Stage 1 — Exact

`SCOUT-DEDUP-010`. Cost ~0.1ms. Resolves ~60%.

```sql
SELECT job_group_id FROM job
WHERE deleted_at IS NULL
  AND ( canonical_url_hash = :url_hash
     OR (ats_platform = :ats AND ats_job_id = :ats_id)
     OR content_hash = :content_hash )
LIMIT 1;
```

Any hit merges with certainty 1.0.

The `content_hash` clause catches an important case: the same posting served
under two different URLs by the same company (a `/careers/` path and a
`/jobs/` path) with byte-identical content.

### 3.2 Stage 2 — Structural

`SCOUT-DEDUP-011`. Cost ~2ms. Resolves ~30%.

**Candidate generation** — cheap filters first, so the expensive comparison runs
on a handful of rows:

```sql
SELECT id, job_group_id, normalized_title, simhash, location_city,
       location_tier, work_mode
FROM job
WHERE company_id = :company_id            -- indexed
  AND role_family = :role_family          -- indexed
  AND posted_at > now() - interval '45 days'
  AND deleted_at IS NULL
LIMIT 200;
```

**Gate 1 — title similarity.** Jaro-Winkler on normalized titles, threshold 0.85.
Jaro-Winkler rather than Levenshtein because it weights common prefixes, and job
titles share prefixes ("Software Engineering Intern" vs "Software Engineering
Intern - Summer 2027" scores 0.94; Levenshtein would rate it much lower on raw
edit distance).

**Gate 2 — location compatibility.**

```
compatible(a, b) :=
     (a.city == b.city)
  OR (a.work_mode == 'remote' AND b.work_mode == 'remote')
  OR (a.work_mode == 'remote' AND b.tier == a.tier)   -- one lists a hub office
  OR (a.city IS NULL XOR b.city IS NULL)              -- one omitted location
```

**Gate 3 — SimHash Hamming distance** over the boilerplate-stripped description.

```
64-bit SimHash over 5-token shingles, weighted by term frequency.
distance ≤ 3  → merge, certainty 0.95
distance 4-8  → escalate to Stage 3
distance > 8  → escalate to Stage 3
```

SimHash rather than MinHash because it produces a single 64-bit integer, which
indexes in Postgres as a plain `BIGINT` and compares with a XOR and a popcount.
MinHash would need multiple hash bands and a more complex index.

### 3.3 Boilerplate stripping

`SCOUT-DEDUP-012`. The step that makes semantic comparison safe.

Without it, two entirely different internships at the same company reach ~0.93
cosine similarity because they share the same 800-word "About Cloudflare",
benefits list, and EEO statement.

**Per-company learned boilerplate:**

```
Weekly, per company with ≥5 postings:
  1. Split every description into paragraphs (min 40 characters).
  2. Hash each paragraph after whitespace normalization.
  3. Any paragraph hash appearing in ≥60% of that company's postings
     is boilerplate.
  4. Store the hash set in company.boilerplate_hashes.
  5. On new postings, strip paragraphs whose hash is in the set.
```

This learns "About Us", benefits, EEO, and application instructions
automatically, per company, with no maintained blocklist — which matters because
every company's boilerplate is different and it changes.

**Global pattern stripping** additionally removes, by regex:
equal-opportunity statements, accommodation notices, privacy notices,
"we are an equal opportunity employer" variants, salary-transparency legal
disclaimers, and recruiter-agency disclaimers.

**Measured effect on the golden set:**

| Comparison | Cosine before stripping | After |
| --- | --- | --- |
| Same job, two sources | 0.97 | 0.98 |
| Different roles, same company | **0.93** | **0.71** |
| Different companies, same role | 0.88 | 0.79 |

The middle row is the whole point. A 0.94 merge threshold is unusable at 0.93
baseline and safe at 0.71.

**New companies have no learned boilerplate.** For a company's first five
postings, only global pattern stripping applies, and the semantic threshold is
raised to 0.96 to compensate. This is a documented cold-start degradation, and it
biases toward under-merging, which is the safe direction.

### 3.4 Stage 3 — Semantic

`SCOUT-DEDUP-013`. Cost ~20ms. Resolves ~9%.

```sql
SELECT id, job_group_id, 1 - (embedding <=> :query_embedding) AS cosine
FROM job
WHERE company_id = :company_id
  AND role_family = :role_family
  AND embedding_version = :version        -- never compare across versions
  AND posted_at > now() - interval '90 days'
  AND deleted_at IS NULL
ORDER BY embedding <=> :query_embedding
LIMIT 10;
```

| Cosine | Action | Certainty |
| --- | --- | --- |
| ≥ 0.94 | Merge | 0.90 |
| 0.88 – 0.94 | LLM adjudication | — |
| < 0.88 | Distinct | — |

Embeddings are computed on `description_stripped`, prefixed with the normalized
title, so the title carries weight in the similarity.

**`embedding_version` filtering is not optional.** Vectors from two different
models are not comparable, and comparing them produces meaningless similarities
that would silently corrupt dedup during a model migration.

### 3.5 LLM adjudication

`SCOUT-DEDUP-014`. Cost ~$0.0001. Resolves the ~0.8% uncertainty band.

```
System: You determine whether two job postings describe the SAME specific
opening at the same company, or two DIFFERENT openings.

Two postings are the SAME opening if they describe one role that one person
would be hired into. They are DIFFERENT if a company could hire two different
people, one for each — even if the roles are similar.

Consider: team, specialization, level, location, start date, required skills.
Ignore: formatting, length, wording, boilerplate.

Return JSON: {"same_role": bool, "confidence": 0.0-1.0, "reason": "<20 words"}

Posting A: {title} | {location} | {first 1500 chars of stripped description}
Posting B: {title} | {location} | {first 1500 chars of stripped description}
```

```
same_role AND confidence ≥ 0.85  → merge, certainty 0.80
otherwise                        → distinct, flag `possible_duplicate` in the UI
```

The UI flag turns the residual uncertainty into a small feature: "This may be the
same as [other job]" with a one-click confirm or reject. A user confirmation
writes a `manual` merge event with certainty 1.0 and becomes a labelled training
example for threshold recalibration.

---

## 4. Grouping

### Union-find with transitive merging

Job C matching both group A and group B implies A and B are the same group.

```
merge(job, matched_job):
  ga = group_of(job)
  gb = group_of(matched_job)
  if ga == gb: return

  keep, absorb = (ga, gb) if ga.first_seen_at <= gb.first_seen_at else (gb, ga)

  UPDATE job SET job_group_id = keep.id WHERE job_group_id = absorb.id
  UPDATE job_group SET member_count = member_count + absorb.member_count,
                       first_seen_at = LEAST(first_seen_at, absorb.first_seen_at)
    WHERE id = keep.id
  DELETE FROM job_group WHERE id = absorb.id
  INSERT INTO job_merge_event (...)
  recompute_representative(keep)
```

**The older group always wins.** This preserves `first_seen_at`, which matters
because notification dedup and freshness scoring both key on it — merging into
the newer group would make an old job look new and could trigger a duplicate
notification.

### Concurrency control

Two workers processing two postings from the same company simultaneously could
each create a group for the same opportunity. Prevented with a Postgres advisory
lock for the duration of the dedup stage:

```sql
SELECT pg_advisory_xact_lock(hashtext('dedup:' || :company_id));
```

Transaction-scoped, so it releases automatically on commit or rollback — no lock
leak on crash. Serializing per company rather than globally means dedup still
parallelizes across companies, which is where the throughput is.

### Representative selection

The group's displayed record. Scored by:

```
+40  from a Tier 1/2 direct ATS source (most authoritative)
+25  description length ≥ 1000 characters
+15  has structured compensation
+10  has a structured location
+10  has a reliable posted_at (not estimated)
+5   most recently observed
−20  from an email alert or community source (thinnest content)
```

The representative is recomputed on every merge, so adding a richer source
upgrades what the user sees.

---

## 5. Over-merge protection

The dangerous error, since a wrongly merged job is a silently missed opportunity.

| Guard | Rule |
| --- | --- |
| Group size cap | Any group above 15 members alerts. Above 25, merging into it is blocked pending review. |
| Cross-role-family | Never merged automatically. Different families means different roles. |
| Cross-company | Never merged automatically. That is a company resolution bug and is fixed there. |
| Distinct locations, both explicit | Requires cosine ≥ 0.97 rather than 0.94 |
| Distinct seniority | Never merged (`internship` and `new_grad` are different roles) |
| Merge rate anomaly | If daily merges exceed 3σ above the 30-day mean, alert — usually indicates a threshold or embedding regression |

### Unmerge

Every merge is reversible:

```
POST /api/v1/admin/groups/{id}/unmerge
{ "job_ids": ["..."], "reason": "different teams" }
```

Removes the specified jobs into a new group, records the reversal on the original
`job_merge_event`, and adds an exclusion pair so future dedup will not re-merge
them. The exclusion is what makes the fix stick.

---

## 6. Notification identity

The end-to-end guarantee for `US-NOTIF-02`.

```sql
CREATE UNIQUE INDEX notification_dedup_idx
  ON notification (user_id, job_group_id, trigger)
  WHERE job_group_id IS NOT NULL;
```

Enforced by the database, not by application logic, because an
application-level "check then insert" loses races and this requirement has zero
tolerance.

**When two groups merge after both were notified** — a genuine possibility with
late-arriving semantic matches — the merge handler:

1. Keeps the earliest notification.
2. Marks the later one `superseded`.
3. Reassigns the later notification's `job_group_id` to the kept group, which
   would violate the unique index, so it is instead soft-deleted with a reason.

The user has already received two notifications at that point; we cannot unsend
them. What we can do is measure it. `scout_late_merge_duplicate_total` counts
these, and a nonzero value means Stage 2 or 3 thresholds are too conservative.
Target: fewer than 1 per month.

---

## 7. Quality measurement

`SCOUT-DEDUP-QA`. Dedup quality is measured continuously, not assumed.

**The golden set:** 500 hand-labelled job pairs, maintained in
`evals/dedup/golden.jsonl`:

- 150 true duplicates across sources (the primary case)
- 100 different roles at the same company (the false-positive trap)
- 100 same role at different companies
- 50 reposts of the same role at different times
- 50 title-rewritten versions of the same role
- 50 genuinely ambiguous pairs with a recorded human judgment and rationale

**CI gate:** the eval harness runs on every pull request touching dedup.

| Metric | Target | CI fails below |
| --- | --- | --- |
| Precision (merges that are correct) | ≥ 0.99 | 0.98 |
| Recall (duplicates found) | ≥ 0.92 | 0.88 |
| F1 | ≥ 0.95 | 0.93 |
| Stage 1 coverage | ~0.60 | — |
| LLM escalation rate | ≤ 0.02 | 0.05 |

**Precision is gated harder than recall, deliberately.** A precision failure is a
silently missed job. A recall failure is a duplicate notification the user can
see and report. The asymmetry in the gate mirrors the asymmetry in the cost.

**Production monitoring:**

| Metric | Alert |
| --- | --- |
| `scout_dedup_merge_total{stage}` | Distribution shift week over week |
| `scout_job_group_size` | Any group > 15 |
| `scout_dedup_llm_escalation_ratio` | > 5% |
| `scout_late_merge_duplicate_total` | > 1/month |
| User "this is a duplicate" reports | Any — each becomes a golden-set entry |

The last row closes the loop. Every duplicate the user reports is added to the
golden set with the correct label, so the same failure cannot recur unnoticed.
