# Testing and Quality Assurance — Scout

**Status:** Draft · **Owner:** QA · **Last updated:** 2026-08-06

---

## 1. What makes testing this system unusual

Scout's failure modes are mostly not crashes. A conventional test suite —
unit tests, integration tests, a few end-to-end flows — would pass while the
product silently degrades. The specific risks:

**Correctness is statistical.** "Did the classifier get it right?" has no single
answer, only a distribution. This needs eval sets and thresholds, not assertions.

**The environment is adversarial and changing.** Sources change their HTML
without notice. Model providers change behavior under the same version string.
Tests must detect drift, not just regression.

**The worst bugs are invisible.** A dedup threshold that is slightly too
aggressive silently merges distinct jobs and the user never learns what they
missed. Nothing errors. Nothing logs.

**Some mistakes are unrecoverable.** A backfill that sends 400 notifications
cannot be undone. That path needs proving, not testing.

The strategy below is shaped around those four facts.

---

## 2. Test pyramid

```
              ╱╲          Manual exploratory        weekly, ~30 min
             ╱  ╲
            ╱ E2E╲        Playwright, 25 flows      ~150s
           ╱──────╲
          ╱  Eval  ╲      Golden sets, 8 suites     ~180s   ← the unusual layer
         ╱──────────╲
        ╱ Integration╲    Real Postgres + Redis     ~120s
       ╱──────────────╲
      ╱    Contract    ╲  Fixtures, schemas, API    ~30s
     ╱──────────────────╲
    ╱        Unit        ╲ Pure logic, 3 languages  ~90s
   ╱──────────────────────╲
```

The eval layer is what distinguishes this from a conventional suite, and it is
where most quality-regression bugs get caught.

---

## 3. Unit tests

Pure functions with no I/O. Fast, numerous, run on save.

| Area | What is tested |
| --- | --- |
| URL canonicalization | 200+ cases including every per-host override |
| Title normalization | Requisition IDs, seasons, abbreviations, unicode |
| Location parsing | Aliases, multi-location, remote detection, tiering |
| Compensation parsing | **Indian numbering (lakh, crore, LPA), currencies, periods** |
| SimHash | Known-similar and known-different pairs |
| Scoring formulas | Every subscore, boundary values, clamping |
| Priority composition | Multiplier application order |
| Trigger evaluation | Every trigger, every threshold boundary |
| Budget logic | Overflow demotion, window rollover |
| Quiet hours | Timezone handling, boundary minutes, the exception rule |
| robots.txt parsing | RFC 9309 conformance suite |
| State machine | Every valid and invalid transition |

**Coverage targets:** 85% for `packages/`, 80% for service business logic, 100%
for scoring formulas, dedup thresholds, notification triggers, and compliance
gates. Coverage is a floor, not a goal — the 100% areas are the ones where a
missed branch has a user-visible consequence.

### Property-based tests

Where invariants matter more than examples, using `rapid` (Go) and `hypothesis`
(Python):

| Property | Statement |
| --- | --- |
| Bengaluru dominance | For any base score b and any weights, tier1(b) > tier2(b) |
| Remote boost | For any b, tier3(b) > tier2(b) |
| Score bounds | Every subscore and priority lands in [0, 100] for any input |
| Freshness monotonicity | For any two ages a < b, freshness(a) ≥ freshness(b) |
| Canonicalization idempotence | canonicalize(canonicalize(u)) == canonicalize(u) |
| Dedup symmetry | similar(a, b) == similar(b, a) |
| Dedup reflexivity | similar(a, a) is always true |
| Merge associativity | Merge order does not change the final grouping |
| Scoring determinism | Same inputs always produce the same output |

The Bengaluru property is the direct encoding of a stated user requirement as an
executable invariant. It cannot be broken by a weight change without failing CI,
which is exactly the guarantee that requirement needs.

---

## 4. Contract tests

The layer that keeps adapters honest.

### Adapter fixtures

Every adapter has recorded real responses in `adapters/{kind}/fixtures/`:

```
adapters/ats/greenhouse/fixtures/
├── standard-board.json          typical, 47 postings
├── empty-board.json             valid, zero postings
├── single-posting.json
├── unicode-heavy.json           CJK, emoji, RTL in descriptions
├── missing-location.json
├── compensation-variants.json   9 comp formats including Indian
├── malformed-html.json          unclosed tags, script injection attempt
├── paginated-large.json         800 postings
└── expected/                    the parse result each must produce
```

Tests replay each fixture and assert an exact parse result. This catches
regressions without touching the network and runs in milliseconds.

**Fixtures are refreshed monthly** with `make fixtures`, which re-records against
live sources. The diff is reviewed in a pull request — a changed fixture means
the source changed its format, which is exactly the signal we want surfaced
before it silently breaks production.

### Schema contracts

| Contract | Enforcement |
| --- | --- |
| Adapter output matches the canonical job schema | Runtime validation in tests |
| Generated Go/Python/TS types match the schema | CI regenerates and diffs |
| OpenAPI spec matches the implementation | CI regenerates and diffs |
| TypeScript client compiles against the spec | `tsc` in CI |
| Breaking API change | Fails CI unless labelled and versioned |
| River queue contract, Go vs. Python client | Cross-language integration test |

---

## 5. Integration tests

Real Postgres and Redis in CI service containers. No mocks for the database,
because most interesting bugs live in the interaction between the query and the
schema.

| Test | Assertion |
| --- | --- |
| Full pipeline | Fixture → observation → job → group → score → notification |
| Transactional enqueue | Rollback leaves neither observation nor queued job |
| Dedup concurrency | 20 workers, same company, concurrent → correct grouping |
| Advisory lock | Two concurrent dedups for one company serialize |
| **Notification uniqueness** | 100 parallel notify attempts → exactly 1 row |
| **Delivery-level dedup** | Device on native push + Web Push → 1 delivery, Web Push `skipped` |
| Push token retirement | Provider returns `UNREGISTERED` → channel disabled, metric incremented, other channels unaffected |
| Device re-registration | Same token posted twice → 1 row updated in place, not 2 rows |
| Telegram webhook auth | Forged callback without the secret token → 401, no state change |
| Auth enforcement | Every state-changing route rejects a request with a missing or wrong bearer token |
| Migration forward | Every migration applies to a production-sized snapshot |
| Migration timing | No migration exceeds 30s on the snapshot |
| Partition maintenance | Creation and drop leave no orphaned data |
| Queue retry | Failing job retries with the correct backoff, then dead-letters |
| Circuit breaker | 5 failures opens, backoff elapses, probe closes |
| Cursor pagination | No item skipped or duplicated while new rows insert mid-scroll |

**The cursor pagination test is worth calling out.** It inserts rows while paging
and asserts every original item appears exactly once. This is the concrete
verification of the design decision in [04](04-api-design.md) that offset
pagination would cause real missed jobs.

---

## 6. Eval suites

Quality measurement for the statistical parts. Run in CI and nightly against
production models.

| Suite | Size | Metric | CI gate |
| --- | --- | --- | --- |
| `classify_role` | 400 jobs | Per-family precision/recall | precision ≥0.97, macro-F1 ≥0.93 |
| `is_software` | 300 jobs | Precision/recall | ≥0.98 / ≥0.97 |
| `seniority` | 250 jobs | Accuracy | ≥0.95 |
| `paid_inference` | 200 jobs | Precision on `paid` | **≥0.99** |
| `location_tier` | 300 strings | Exact accuracy | ≥0.98 |
| `comp_parse` | 250 strings | Value+currency+period exact | ≥0.95 |
| `dedup` | 500 pairs | Precision / recall | ≥0.99 / ≥0.92 |
| `ranking` | 200 jobs | NDCG@20 vs. hand-ordered ideal | ≥0.85 |
| `explanation` | 100 jobs | LLM-judge rubric | ≥4.0/5 |
| `cover_letter` | 50 pairs | LLM-judge rubric | ≥4.0/5 |

**`paid_inference` is gated at 0.99** because it is the only eval whose failure
reaches the user as a broken promise. The hard requirement is "paid only"; a
false positive here means an unpaid role on their phone.

**`dedup` precision is gated harder than recall** for the reason in
[ADR-008](adr/ADR-008-three-stage-deduplication.md): a precision failure is a
silent miss, a recall failure is a visible duplicate.

### Golden set maintenance

Golden sets grow from real failures. The rule: **every production quality bug
becomes a golden set entry before it is fixed.** A misclassified job, a wrong
merge, a bad location parse — each is added with the correct label, the fix is
made, and the eval proves it. This is what stops the same class of bug recurring.

Quarterly review re-labels ambiguous entries and prunes anything no longer
representative.

### Nightly drift detection

Model providers change behavior silently under stable version names. A nightly
run of every eval suite against production prompts and models, alerting on any
metric declining more than 3% week over week, is the only way to notice before
users do.

---

## 7. End-to-end tests

Playwright against a fully seeded stack. Twenty-five flows covering:

Authentication (bearer token accepted, absent, and wrong) ·
onboarding · feed load and filter and sort · search · job detail with all scores
· save, dismiss, apply state transitions · pipeline drag and keyboard equivalent
· interview creation · watchlist management · notification channel setup and test
· settings changes triggering rescore · PWA install and offline read · offline
action queue and replay · data export · account deletion.

Each runs against Chromium, Firefox, and WebKit, plus a mobile viewport.

**Chromium stands in for Dia**, the user's actual browser, which Playwright cannot
drive. Dia is Chromium-based so the engine coverage is real, but the browser itself
is verified by hand on the manual pass — see [12](12-frontend-ux.md) section 7.2.
Automated coverage of an engine is not the same as coverage of a browser, and
pretending otherwise is how a shell-specific bug ships.

**Accessibility** runs inside the same suite: `axe-core` on every page, and any
violation fails the build. Keyboard-only traversal is asserted for every primary
flow.

---

## 8. The tests that prevent catastrophe

Four scenarios where a bug is unrecoverable. These get dedicated, explicit tests
that run on every pull request regardless of what changed.

### 8.1 Backfill notification suppression

```
GIVEN 10,000 historical observations spanning 90 days
WHEN a full replay runs with default settings
THEN exactly 0 notifications are created
AND  the suppression counter equals the number of would-be triggers
```

Because a backfill that pages the user 400 times destroys trust permanently, and
no amount of apology recovers it.

### 8.2 Rescore notification suppression

```
GIVEN 5,000 scored jobs and an active weight version
WHEN a new weight version activates and rescoring runs
THEN exactly 0 notifications are created
AND  every job has a score under the new version
```

### 8.3 Notification uniqueness under concurrency

```
GIVEN one job group and one user
WHEN 100 goroutines simultaneously attempt to notify
THEN exactly 1 notification row exists
AND  99 attempts recorded a unique-violation skip
```

### 8.4 Compliance gate

```
GIVEN a source with legal_posture = 'prohibited'
WHEN the scheduler runs, the collector runs, and a manual poll is forced
THEN the HTTP client is invoked exactly 0 times
AND  a compliance refusal is recorded for each attempt
```

Because the cost of getting this wrong is not a bug report, it is a legal letter.

---

## 9. Performance and load testing

| Test | Target | Tool |
| --- | --- | --- |
| API load | 500 rps, p95 <300ms | k6 |
| Search load | 100 rps, p95 <200ms | k6 |
| Ingestion throughput | 5,000 sources/hour sustained | Custom harness against a fixture server |
| Pipeline throughput | 10,000 jobs/hour end to end | Custom harness |
| Embedding throughput | ≥50 jobs/sec batched | Benchmark |
| Database at scale | Every query <100ms on a 50M-row snapshot | `pgbench` + `EXPLAIN` assertions |
| Frontend | Lighthouse budgets from [12](12-frontend-ux.md) | Lighthouse CI |
| Memory soak | 24h run, no growth beyond 10% | Docker stats sampling |

**The soak test catches what nothing else does.** Slow leaks in the collector's
connection pool or the brain's model cache are invisible in a 90-second test and
fatal after three weeks of unattended operation — which is precisely how Scout
runs.

---

## 10. Chaos testing

Monthly, in staging.

| Experiment | Expected behavior |
| --- | --- |
| Kill Redis | Rate limiting falls back to conservative in-process; **no data loss** |
| Kill the brain mid-batch | Jobs return to the queue and reprocess; no duplicates |
| Kill the notifier with a queued notification | Delivered on restart |
| Block FCM | Telegram still delivers; notification marked success |
| Block both primary channels | Alert fires; notifications queue durably and deliver on recovery |
| Return `UNREGISTERED` for every push token | Tokens retired, user told on other channels, no silent loss |
| Block all LLM providers | Deterministic scoring continues; template explanations |
| Fill the disk to 95% | Auto-prune triggers; alert fires |
| Introduce 500ms latency on all outbound | Timeouts and backoff behave; no cascade |
| Return 500 from 50% of sources | Circuit breakers isolate; other sources unaffected |
| Corrupt one adapter's output | Validation catches it; source quarantined; others continue |
| Restart Postgres | Services reconnect; in-flight work resumes |

The Redis experiment directly verifies the rule from
[ADR-002](adr/ADR-002-postgres-as-the-primary-store.md): if losing Redis loses
data, something was in the wrong place. That rule is only real if it is tested.

---

## 11. CI gates

A pull request cannot merge unless every one of these passes:

```
✓ lint             all three languages
✓ typecheck        including mypy --strict
✓ unit             with coverage thresholds
✓ contract         fixtures, schemas, OpenAPI drift
✓ integration      real Postgres and Redis
✓ evals            all eight quality suites above threshold
✓ e2e              25 Playwright flows, 3 browsers
✓ a11y             zero axe violations
✓ lighthouse       within performance budgets
✓ security         SAST, dependencies, secrets
✓ migrations       apply cleanly, under 30s, on a prod-sized snapshot
✓ catastrophe      the four tests in section 8
```

Roughly 6 minutes wall clock with parallelism.

**Merging with a failing gate requires an explicit override label and a written
reason in the PR.** The override is logged. It exists because sometimes an eval
threshold is genuinely wrong and blocking a fix, but it should be rare enough
that each use is memorable.

---

## 12. Manual testing

Automation cannot judge whether a cover letter reads like a human wrote it, or
whether a ranking *feels* right.

**Weekly, ~30 minutes:**

- Read the top 20 ranked jobs. Does the ordering match intuition? Where it does
  not, is the explanation convincing or is the model wrong?
- Read 5 generated explanations. Specific, or generic filler?
- Read 2 generated cover letters. Would you actually send this?
- Trigger one notification of each type. Correct formatting on native push,
  Telegram, and desktop? Emoji and ₹ amounts render correctly?
- Tap a notification action from the lock screen without unlocking. Does the state
  actually change?
- Walk one full flow in the installed app on an actual Android phone, not an
  emulator. An emulator without Google Play Services will not receive FCM pushes at
  all, and the gesture-bar safe area does not reproduce reliably.
- Press the Android back button from two screens deep. Does it navigate, or exit?
- Open the dashboard in **Dia** and confirm the feed, filters, and job detail render
  and behave correctly. Check whether PWA install and Web Push permission are
  offered; if either is missing, record it as a known limitation rather than
  branching on the browser.

**The recall audit, weekly:** search manually the way you would have before Scout
existed. Every role Scout missed is a data point and a task — identify which
source would have carried it and add it. This is the only way recall gets
measured at all, since you cannot count what you never saw.
