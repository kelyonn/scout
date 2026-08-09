# Runbook: Quality regression

**Severity:** SEV2 · **Alert:** Eval suite below threshold, over-merge detected,
or the ranking visibly wrong

The hardest class of problem because nothing errors. The system reports healthy
and produces worse results.

---

## Identify what regressed

```bash
make evals                 # run every suite locally against current code
make evals SUITE=dedup     # one suite, verbose
```

Compare against the last passing run in CI. The eval harness writes results to
`evals/results/` with a timestamp, so the diff is available.

| Symptom | Likely suite |
| --- | --- |
| Irrelevant roles in the feed | `classify_role`, `is_software` |
| Unpaid roles notified | `paid_inference` — **treat as SEV1** |
| Wrong location ranking | `location_tier` |
| Duplicates appearing | `dedup` recall |
| Distinct jobs merged | `dedup` precision |
| Ordering feels wrong | `ranking` NDCG |
| Explanations generic or wrong | `explanation` |

---

## Common causes, in order of likelihood

**1. A prompt change.**

```bash
git log --oneline -20 -- packages/prompts/
```

Prompt changes are the most common cause and the cheapest to revert:

```bash
git revert <commit>
docker compose up -d brain
make evals
```

**2. A model provider changed behavior.** The version string is stable; the
behavior is not.

```sql
SELECT model, date_trunc('day', occurred_at) AS day,
       avg(latency_ms), count(*), avg(output_tokens)
FROM llm_call WHERE occurred_at > now() - interval '14 days'
GROUP BY 1, 2 ORDER BY 2 DESC;
```

A shift in average output tokens or latency around the regression date suggests a
provider-side change. Fix: pin to a dated model version if the provider offers
one, or switch to the fallback provider and re-run evals.

**3. A weight version change.**

```sql
SELECT version, source, active, created_at, metrics FROM weight_version
ORDER BY created_at DESC LIMIT 5;
```

Rollback is a single update plus a rescore:

```sql
UPDATE weight_version SET active = false WHERE active;
UPDATE weight_version SET active = true  WHERE version = '<previous>';
```

```bash
curl -X POST localhost:8080/api/v1/admin/rescore \
  -d '{"weight_version":"<previous>","suppress_notifications":true}'
```

**4. A threshold change.** Dedup thresholds, notification thresholds, and
confidence floors are all config. Check `git log -- infra/config/`.

**5. An embedding model change.** If `embedding_version` changed, similarity
comparisons shifted and dedup thresholds calibrated for the old model no longer
apply.

```sql
SELECT embedding_version, count(*) FROM job GROUP BY 1;
```

Mixed versions means a migration is incomplete. Either finish it or roll back —
do not run with a split corpus longer than necessary.

---

## Over-merge specifically

The most damaging regression, because it silently hides jobs.

```sql
-- Groups that are suspiciously large
SELECT g.id, g.member_count, c.canonical_name,
       array_agg(DISTINCT j.title) AS titles
FROM job_group g
JOIN company c ON c.id = g.company_id
JOIN job j ON j.job_group_id = g.id
WHERE g.member_count > 10
GROUP BY 1, 2, 3 ORDER BY 2 DESC LIMIT 20;
```

Read the titles. If a group contains "Backend Intern" and "Frontend Intern",
that is a false merge.

```sql
-- Recent merges by stage and certainty — look for a shift
SELECT stage, width_bucket(certainty, 0, 1, 5)/5.0 AS certainty_bucket, count(*)
FROM job_merge_event
WHERE created_at > now() - interval '7 days'
GROUP BY 1, 2 ORDER BY 1, 2;
```

A spike in low-certainty semantic merges means the threshold is too loose or
boilerplate stripping stopped working.

**Check boilerplate stripping first** — it is the usual culprit:

```sql
SELECT c.canonical_name,
       array_length(c.boilerplate_hashes, 1) AS learned_patterns,
       avg(length(j.description_text)) AS avg_full,
       avg(length(j.description_stripped)) AS avg_stripped
FROM company c JOIN job j ON j.company_id = c.id
WHERE j.first_seen_at > now() - interval '7 days'
GROUP BY 1, 2 HAVING count(*) > 3
ORDER BY (avg(length(j.description_stripped))::float /
          NULLIF(avg(length(j.description_text)), 0)) DESC LIMIT 20;
```

A stripped length close to the full length means stripping is not running.

**Unmerge the bad groups:**

```bash
curl -X POST localhost:8080/api/v1/admin/groups/<id>/unmerge \
  -d '{"job_ids":["..."],"reason":"different roles, backend vs frontend"}'
```

Then raise the threshold in config, redeploy, and re-run the dedup eval.

---

## Verification

```bash
make evals                          # all suites back above threshold
make evals-report                   # per-suite diff vs. the last passing run
```

Then a manual check, because evals measure what you thought to measure:

- Read the top 20 ranked jobs. Does the ordering make sense?
- Read 5 explanations. Specific, or generic?
- Spot-check 10 recent merges. Correct?

---

## Follow-up

1. **Add the failure to the golden set.** Every quality bug becomes a labelled
   example before the fix ships. This is the mechanism that prevents recurrence.
2. **Why did CI not catch it?** If a regression reached production, either the
   eval set lacks coverage of that case or the threshold is too loose. Fix the
   gate, not just the bug.
3. **Estimate user impact.** How many jobs were misclassified or wrongly merged
   during the window? For over-merges, unmerge and re-notify if the affected jobs
   are still open.
4. **Postmortem** for any regression that affected notifications.
