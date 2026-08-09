# Ranking and Scoring — Scout

**Status:** Draft · **Owner:** Intelligence · **Last updated:** 2026-08-06

The thirteen scores, how each is computed, how they combine, and how the system
learns.

---

## 1. Design principles

**Deterministic where possible.** Eleven of the thirteen subscores are computed
from data with no model call. This makes them fast, free, reproducible, and
debuggable. A score you cannot recompute by hand from the inputs is a score you
cannot fix.

**Every score is explainable.** `score_inputs` stores every value that fed the
computation. When the user asks "why is this 91?", the answer is derivable, not
generated after the fact.

**Job-side and user-side are separated.** Company quality, compensation, and
learning opportunity are properties of the job and are computed once. Skill
match, resume match, and location preference are properties of the pairing and
are computed per user. This split is what makes multi-tenant scoring affordable
later.

**Score, then rank, then threshold.** Notification thresholds apply to the final
priority score. Changing a weight changes what gets notified, so weights are
versioned and a change triggers a rescore with notifications suppressed.

---

## 2. The thirteen scores

All are 0–100 integers.

| # | Score | Side | Method | Cost |
| --- | --- | --- | --- | --- |
| 1 | Overall match | user | Composite of 2, 3, and role fit | free |
| 2 | Skill match | user | Ontology overlap, weighted | free |
| 3 | Resume match | user | Embedding cosine + skill coverage | free |
| 4 | Company quality | job | Data lookup | free |
| 5 | Compensation | job+user | Percentile against comparables | free |
| 6 | Learning opportunity | job | Heuristics on stack, stage, team | free |
| 7 | Engineering culture | job | Public engineering signals | free |
| 8 | Growth potential | job | Company trajectory | free |
| 9 | Interview probability | user | Logistic model on profile fit | free |
| 10 | Competition estimate | job | Inverse of expected applicant volume | free |
| 11 | Ease of applying | job | Application friction | free |
| 12 | Deadline urgency | job | Time remaining | free |
| 13 | **Priority** | user | **Weighted composite × multipliers** | free |

Plus one generated artifact: the **explanation**, which is the only LLM call in
the scoring path.

---

## 3. Score definitions

### 3.1 Skill match (`SCOUT-RANK-002`)

Overlap between required skills and user skills, weighted by requirement strength
and user proficiency, with credit for implied skills.

```
For each job skill s with requirement weight w(s) ∈ {1.0 required, 0.5 preferred,
0.25 mentioned}:

  user_level(s) ∈ [0, 5]
  direct    = user_level(s) / 5
  implied   = max over t in user_skills of (0.6 × implies_strength(t, s) × user_level(t)/5)
  coverage(s) = max(direct, implied)

skill_match = 100 × Σ w(s)·coverage(s) / Σ w(s)
```

The `implied` term means Docker experience partially covers a Kubernetes
requirement, and Python partially covers a "scripting" requirement. Without it,
skill match is brittle against synonym choice and systematically under-scores
qualified candidates.

**Missing skill list** is retained in `score_inputs` and feeds gap analysis.

**Advocacy roles use a different profile of the same formula.** Per
[07](07-normalization-taxonomy.md), developer advocacy rewards breadth where
implementation roles reward depth, so `advocacy.*` families apply a breadth bonus
and admit non-code evidence:

```
if role_family starts with "advocacy":
    breadth   = min(1.0, distinct_user_skills_touching_job_domain / 8)
    comms     = evidence of writing, talks, OSS, or community work in the resume
    skill_match = 100 × (0.55 · Σ w(s)·coverage(s)/Σ w(s)
                       + 0.25 · breadth
                       + 0.20 · comms)
```

Applying the `swe` formula to a Developer Advocate posting would score a
broad-but-shallow candidate poorly for a role where that profile is the
*preferred* one. The `advocacy parity` property test in section 8 exists to catch
this class of mistake, and it is why advocacy was added as a taxonomy family
rather than a keyword filter.

### 3.2 Resume match (`SCOUT-RANK-003`)

```
semantic  = cosine(resume_embedding, job_embedding)          → normalize to 0-100
keyword   = fraction of job's required skills appearing in the resume text
recency   = 1.0 if relevant experience in the last 12 months, else 0.7

resume_match = 100 × (0.55·semantic_norm + 0.35·keyword + 0.10) × recency
```

`semantic_norm` maps cosine from its practical range (0.55–0.95 for related
documents) onto 0–1, because raw cosine never approaches 0 or 1 for real text and
using it directly compresses the whole score into a narrow band.

**ATS keyword reality.** Many companies screen on literal keyword presence, so
`keyword` is weighted heavily despite being crude. The gap between `semantic` and
`keyword` is itself surfaced to the user: "your resume matches this role
conceptually but is missing the literal terms Kubernetes and gRPC" is directly
actionable advice.

### 3.3 Company quality (`SCOUT-RANK-004`)

**Explicitly not a FAANG bias.** The brief is emphatic that small companies must
not be excluded, and a naive "quality" score would rank by fame. This score
measures *signals of a good place to work*, which small companies often win on.

```
company_quality =
    20 × engineering_reputation      // GitHub stars on org repos, conference
                                     // talks, engineering blog activity
  + 15 × funding_health              // raised recently, credible investors,
                                     // OR profitable/bootstrapped
  + 15 × growth_signal               // headcount trend, job posting velocity
  + 15 × glassdoor_proxy             // where available; neutral if absent
  + 10 × tech_stack_modernity        // uses current tooling
  + 10 × intern_program_maturity     // evidence of a real program
  + 15 × domain_interest             // infra/dev-tools/AI weighted up for this user
```

**Size neutrality is enforced by construction.** No term references headcount or
brand recognition. A 20-person developer-tools company with a strong GitHub
presence, recent seed funding, and a modern stack scores in the 70s — competitive
with a large enterprise that has none of those signals. Verified by a fairness
test: the mean `company_quality` across size buckets must not vary by more than
10 points, and CI fails if it does.

**Company-type neutrality needs more care than size neutrality**, because two of
these terms are quietly biased toward venture-backed product companies:

| Term | The bias | The correction |
| --- | --- | --- |
| `funding_health` | A 40-year-old manufacturer has never raised a round, and a PSU never will. Scored naively, both look unfunded rather than stable. | Renamed `financial_stability`. Profitable, public, government-backed, and bootstrapped all score **full marks**. Recent VC funding is one path to a good score, not the definition of one. |
| `growth_signal` | Headcount growth favours startups. A stable 60,000-person services firm is not a worse employer for growing 3% a year. | Measured as *hiring activity relative to the company's own baseline*, not absolute trajectory. |
| `engineering_reputation` | GitHub stars and conference talks favour open-source-forward product companies. A bank's payments platform team may be excellent and invisible. | Absence is **neutral, not negative**. The term can add points and cannot subtract them. |
| `tech_stack_modernity` | Legitimately discriminating — but a GCC running Java 21 and Kubernetes should not lose to a startup because the parent brand feels old-fashioned. | Scored from the job description's actual stack, never from the company's reputation. |

Restated with the corrections:

```
company_quality =
    20 × engineering_reputation      // additive only; absence is neutral
  + 15 × financial_stability         // profitable | public | funded | govt-backed
  + 15 × hiring_momentum             // relative to the company's own baseline
  + 15 × glassdoor_proxy             // where available; neutral if absent
  + 10 × tech_stack_modernity        // from the posting, not the brand
  + 10 × intern_program_maturity     // structured programmes score well here,
                                     // which favours GCCs and enterprises
  + 15 × domain_interest             // infra/dev-tools/AI weighted up for this user
```

Note that `intern_program_maturity` runs the *other* way — large services firms,
GCCs, and enterprises typically have genuinely better-structured internship
programmes than early-stage startups. The score is not tilted toward big companies
either; it simply measures what it says.

**Enforced by a second fairness test.** Mean `company_quality` must not vary by
more than 10 points across `company_type` buckets, exactly as for size buckets, and
CI fails if it does. A product-company bias is the failure mode this system is most
likely to develop on its own, because product companies produce more of the public
signals that are easy to measure — so it gets a test rather than a good intention.

**`company_type` appears in no term above, and that is deliberate.** It is used for
adapter selection, scheduling, competition estimation, and coverage auditing. It is
never a ranking input, in either direction.

### 3.4 Compensation (`SCOUT-RANK-005`)

Percentile against comparable roles, not absolute value, because ₹80,000/month is
excellent in India and poor in San Francisco.

```
comparables = jobs WHERE role_family = X
                     AND seniority = Y
                     AND location_country = Z
                     AND posted_at > now() - 180 days
                     AND comp_normalized_inr_month IS NOT NULL

percentile = rank of this job within comparables
compensation = 100 × percentile

If unknown:
  compensation = 50 (neutral), confidence flag set — an unknown salary should
  neither help nor hurt, and a 0 would wrongly bury most of the market.
```

A minimum of 20 comparables is required; below that we fall back to a
country-and-seniority prior rather than computing a percentile from 3 data
points.

### 3.5 Learning opportunity (`SCOUT-RANK-006`)

```
+25  technologies in the job that the user does not yet have
     (capped — a job requiring nothing you know is not a learning
      opportunity, it is a rejection)
+20  problem domain depth (distributed systems, compilers, ML infra,
     databases, networking score high; CRUD scores low)
+20  mentorship signals ("mentor", "pair", "code review", "onboarding buddy")
+15  team size 3-15 (small enough for real ownership, large enough for support)
+10  open-source contribution as part of the role
+10  explicit intern project ownership language
```

The first term is deliberately capped: maximum learning score comes from a role
requiring roughly 30–50% unfamiliar technology, not 100%. Beyond that the
candidate will not be hired, which is not a learning opportunity.

### 3.6 Engineering culture (`SCOUT-RANK-007`)

Inferred from public signals, with an honest confidence caveat.

```
+25  active engineering blog (posts in the last 6 months)
+20  meaningful open-source presence (org repos with external contributors)
+15  conference talks by engineers
+15  job description discusses technical practice (testing, code review,
     design docs, on-call rotation) rather than only perks
+15  no red-flag language ("rockstar", "ninja", "work hard play hard",
     "wear many hats" at a large company, "fast-paced" three or more times)
+10  transparency signals (public roadmap, public postmortems, published
     engineering levels)
```

**Confidence is low for small companies with no public presence, and the UI says
so** rather than presenting a low score as a judgment. A stealth startup with no
blog is not necessarily a bad engineering culture; it is an unknown one. Absent
signals produce a neutral 50 with a `low_confidence` flag, never a low score.

### 3.7 Growth potential (`SCOUT-RANK-008`)

```
+30  funding stage trajectory (seed→A→B recently, or profitable and growing)
+25  headcount growth rate (from job posting volume over 12 months)
+20  market position (category leader, or fast-growing category)
+15  intern-to-full-time conversion evidence
+10  the role's team is expanding (multiple openings on the same team)
```

### 3.8 Interview probability (`SCOUT-RANK-009`)

A calibrated estimate of the chance of reaching a first-round interview.

**Before 200 labelled outcomes** — a hand-tuned logistic function:

```
z = −1.2
  + 2.1 × (skill_match / 100)
  + 1.4 × (resume_match / 100)
  − 0.9 × (company_selectivity)        // 0-1, from size and brand
  + 0.7 × (location_tier == 1 or 2)    // local candidates have an edge
  + 0.5 × (posted within 48 hours)     // early applications convert better
  − 0.6 × (applicant_volume_estimate)  // 0-1

interview_probability = 100 / (1 + e^(−z))
```

**After 200 labelled outcomes** — logistic regression trained on the user's own
application history, with the hand-tuned version as a prior. Logistic regression
rather than a gradient-boosted tree because the coefficients are interpretable,
which matters when the output is shown to the user as advice.

**Calibration is checked, not assumed.** Predicted probability is bucketed and
compared against observed outcomes; the Brier score is tracked. A model claiming
70% that converts at 20% is worse than useless because it drives bad
prioritization.

### 3.9 Competition estimate (`SCOUT-RANK-010`)

**Higher score means less competition**, so it composes with the others without a
sign flip.

```
base = 50

−25  company is widely known (brand recognition proxy)
−20  posted more than 7 days ago (the queue is already deep)
−15  listed on a major aggregator (higher visibility)
−10  fully remote with no geographic restriction (global applicant pool)
+20  company is small or obscure
+15  requires a specific, less common skill (Rust, Erlang, formal methods,
     kernel, compilers)
+15  discovered within 2 hours of posting
+10  posted only to the company's own ATS, not aggregated
+10  location is Tier 1/2 with an onsite requirement (smaller pool)
−20  mass campus drive (services firms hiring thousands per cycle)
+10  GCC or enterprise role posted only on a Workday/SuccessFactors portal
     (low aggregator visibility, and most students never look there)
+10  advocacy role (a much smaller applicant pool than equivalent SWE roles)

competition_estimate = clamp(base + adjustments, 0, 100)
```

**The last three adjustments are where widening the company and role taxonomy pays
off.** A Bengaluru GCC role on a bespoke Workday portal is genuinely
less contested than the same-quality role on a startup's Greenhouse board, because
almost nobody is watching those portals — and Scout is. Conversely a mass campus
drive is high-volume by construction and honestly scored as such, even though the
company may be excellent.

Note that `−25 company is widely known` uses **brand recognition, not size**. A
70,000-person services firm most students have never heard of does not take that
penalty; a 300-person YC company everyone follows on X does.

This score is where Scout's structural advantage shows up. A job discovered 20
minutes after posting on a small company's own Greenhouse board — invisible to
aggregators — scores high here, and that is precisely the highest-expected-value
application available.

### 3.10 Ease of applying (`SCOUT-RANK-011`)

```
100  one-click / single form, no account required
 85  short form, account required
 70  ATS with resume upload and basic fields
 55  ATS with resume upload plus 3-6 custom questions
 40  long custom questionnaire, or a required cover letter
 25  requires an assessment or coding test up front
 10  email application with unclear requirements
```

Inferred from the ATS platform (Greenhouse and Lever forms are short; Workday
requires account creation and is long) plus description language ("please include
a cover letter", "complete our take-home").

Combined with score 12 (urgency), this drives a real recommendation: "high match,
5-minute application, closes in 3 days — do this one now."

### 3.11 Deadline urgency (`SCOUT-RANK-012`)

```
If deadline_at is known:
   days_left ≥ 30  → 20
   14-30           → 40
   7-14            → 65
   3-7             → 85
   < 3             → 100
   passed          → 0 (and status → expired)

If unknown (the common case):
   Estimate from posting age against the company's historical
   time-to-close for similar roles. Default assumption: 30 days.
   Confidence is flagged low and the score is damped toward 40.
```

### 3.12 Overall match (`SCOUT-RANK-001`)

A composite specifically of *fit*, separate from desirability. Answers "is this
the right kind of role for me?" rather than "is this a good opportunity?"

```
overall_match = 0.40 × skill_match
              + 0.30 × resume_match
              + 0.20 × role_family_fit      // 100 if in target_roles,
                                            // 60 if adjacent, 20 otherwise
              + 0.10 × seniority_fit        // 100 if in target_seniority
```

Kept separate from priority because the user needs both: "this is a perfect fit
at a mediocre company" and "this is a stretch at a great company" are different
situations requiring different decisions, and one blended number hides that.

### 3.13 Priority (`SCOUT-RANK-013`)

The ordering value and the notification trigger.

```
base = 0.24 × overall_match
     + 0.14 × company_quality
     + 0.12 × learning_opportunity
     + 0.10 × interview_probability
     + 0.10 × competition_estimate
     + 0.09 × compensation
     + 0.07 × growth_potential
     + 0.06 × engineering_culture
     + 0.05 × ease_of_applying
     + 0.03 × deadline_urgency

priority = clamp(base × location_multiplier × freshness_multiplier, 0, 100)
```

**Location multiplier** from the user profile:

| Tier | Multiplier |
| --- | --- |
| 1 — Bengaluru | 1.20 |
| 2 — Rest of India | 1.05 |
| 3 — Remote | 1.12 |
| 4 — International | 0.90 (±0.05 for visa sponsorship) |

**Freshness multiplier** — exponential decay with a 14-day half-life:

```
freshness = 0.55 + 0.45 × exp(−age_days × ln(2) / 14)

age 0 days   → 1.00
age 7 days   → 0.87
age 14 days  → 0.78
age 30 days  → 0.65
age 60 days  → 0.57
```

Floored at 0.55 rather than decaying to zero, because a strong 45-day-old
opportunity is still worth showing — it should be outranked by fresh
opportunities, not erased.

**Verifying the Bengaluru requirement.** Two otherwise identical jobs, one in
Bengaluru and one in Pune, with base 75:

```
Bengaluru: 75 × 1.20 × 1.00 = 90
Pune:      75 × 1.05 × 1.00 = 79
```

Bengaluru wins by 11 points. Because the multiplier is applied after the weighted
sum rather than as an additive bonus, this holds at every base score, which is
what "always ranks higher" requires. There is a property-based test asserting
exactly this over the full base range.

---

## 4. Weight management

Weights live in `weight_version` and are never hardcoded.

```json
{
  "version": "v1-hand-tuned-2026-08",
  "source": "hand_tuned",
  "weights": {
    "priority": { "overall_match": 0.24, "company_quality": 0.14, ... },
    "location_multipliers": { "1": 1.20, "2": 1.05, "3": 1.12, "4": 0.90 },
    "freshness_half_life_days": 14,
    "freshness_floor": 0.55
  }
}
```

Changing weights:

1. New `weight_version` row, `active = false`.
2. Rescore a sample of 1,000 jobs under the new version.
3. Diff the rankings: what moved into and out of the notification threshold?
4. Review the diff. Unexpected movement means the weights are wrong.
5. Activate; full rescore runs with **notifications suppressed**.
6. Previous version retained for one week for rollback.

Step 3 is the important one. A weight change that looks reasonable in the
abstract often produces obviously wrong results on real data, and seeing which
specific jobs cross the notification threshold is the fastest way to catch it.

---

## 5. The learning loop

`SCOUT-RANK-LEARN`. Ships at P5, after enough signal exists.

### Signals

| Signal | Label | Weight | Notes |
| --- | --- | --- | --- |
| Applied | strong positive | 1.0 | The clearest signal available |
| Saved | positive | 0.6 | Interest without commitment |
| Dwell > 30s | weak positive | 0.3 | Read it properly |
| Clicked from notification | weak positive | 0.2 | |
| Dismissed | negative | −0.8 | Explicit rejection |
| Marked irrelevant | strong negative | −1.0 | |
| Impression, no interaction, 7 days | weak negative | −0.2 | |
| Notification ignored 24h | weak negative | −0.3 | |
| **Interview reached** | very strong positive | 1.5 | The outcome that matters |
| **Offer** | strongest positive | 2.0 | |

### Model

**Learning to rank** with LambdaMART (LightGBM), trained on pairwise preferences
within a time window — did the user prefer job A over job B when both were shown
together? Pairwise rather than pointwise because the task is ranking, and a
pointwise model optimizes a proxy.

**Features:** all 13 subscores, their raw inputs, company attributes, temporal
features (day of week, time since posting), and interaction history with the
company.

**Training cadence:** weekly, once ≥200 labelled examples exist. Retrain
requires:

| Gate | Requirement |
| --- | --- |
| Sample size | ≥200 labels, ≥50 positive |
| Offline NDCG@20 | ≥ current production model |
| Calibration | Brier score not worse than current |
| Fairness | Small-company representation within 10% of current |
| Sanity | Bengaluru preference preserved (the property test) |

Failing any gate means the new model is discarded and the current one continues.

### Cold start and safety

The learned model does not replace the hand-tuned weights. It **blends**:

```
priority_final = (1 − α) × priority_hand_tuned + α × priority_learned

α = min(0.7, n_labels / 1000)
```

α caps at 0.7, so the hand-tuned weights always retain 30% influence. This is a
deliberate guardrail against the model overfitting to a temporary preference or
collapsing onto a narrow slice of the market. It also means the encoded
requirements — Bengaluru first, paid only — cannot be learned away.

### The feedback trap

A learned ranker trained only on what it showed you will narrow. If the model
stops surfacing ML roles, the user never interacts with ML roles, and the model
learns that ML roles are irrelevant.

**Mitigation: 10% exploration.** One in ten feed slots is filled by a job that
scores well on a *different* dimension than the user's revealed preference —
higher-variance picks, deliberately outside the current model's comfort zone.
Exploration slots are labelled in `user_feedback.context` so they can be excluded
from training bias analysis, and their interaction rate is monitored as a check
on whether the model has over-narrowed.

---

## 6. Explanations

The only LLM call in the scoring path, and required for every job.

```
System: Explain in ONE sentence (max 45 words) why this internship matches
this candidate. Be specific and concrete. Reference actual skills,
technologies, and facts from the data. Never invent details. Never use
generic phrases like "great opportunity" or "strong match" without saying
why. If something is a genuine drawback, say so.

Input: {top 4 contributing subscores with values and their raw inputs,
        matched skills, missing skills, location, compensation, company facts}
```

**Good:** "Strong backend match — Go and distributed systems are your top two
skills, and Cloudflare's edge platform is real systems work. Remote with an India
team at ₹80k/month."

**Bad:** "This is a great opportunity that matches your profile well." (Generic,
unfalsifiable, adds nothing to the number it accompanies.)

Explanations are cached on `(job_id, user_id, weight_version)` and regenerated
only when the score materially changes (by more than 5 points).

**Deterministic fallback.** When the LLM tier is unavailable or the budget cap is
hit, a template-based explanation is generated from the same inputs: "Matches 7
of 9 required skills (Go, Rust, Kubernetes, …). Tier 1 location: Bengaluru.
₹85k/month, 78th percentile for backend internships in India." Less fluent,
equally informative, and it means a provider outage never leaves a score
unexplained.

---

## 7. Score computation flow

```
New job arrives
     │
     ├─▶ Job-side scores (once per job, cached)
     │     company_quality, learning_opportunity, engineering_culture,
     │     growth_potential, competition_estimate, ease_of_applying
     │     Cost: ~3ms
     │
     ├─▶ User-side scores (per user)
     │     skill_match, resume_match, compensation_percentile,
     │     interview_probability, deadline_urgency
     │     Cost: ~5ms
     │
     ├─▶ Composite: overall_match, priority
     │     Cost: <1ms
     │
     └─▶ Explanation (Tier 2 LLM, async)
           Cost: ~800ms, ~$0.00002
```

**Total synchronous cost: ~9ms per job per user.** The explanation is generated
asynchronously so it never delays the notification path — a notification can be
sent with the deterministic explanation and upgraded when the LLM one arrives.

**Recomputation triggers:** profile change, resume update, weight version change,
job content change, or a 7-day staleness refresh for deadline urgency and
freshness.

---

## 8. Evaluation

`evals/ranking/` contains:

**Golden ranking set** — 200 jobs with a hand-assigned ideal ordering, updated
quarterly. Measured with NDCG@10, NDCG@20, and Spearman correlation.

**Property tests** — invariants that must hold for any weight configuration:

| Property | Assertion |
| --- | --- |
| Bengaluru dominance | For identical jobs, tier 1 > tier 2, at every base score |
| Remote boost | Tier 3 > tier 2 for identical jobs |
| Paid requirement | No `unpaid` job without `prestige_exception` exceeds the notify threshold |
| Freshness monotonicity | Older is never higher, all else equal |
| Score bounds | Every subscore and priority in [0, 100] |
| Size fairness | Mean `company_quality` varies ≤10 points across size buckets |
| **Company-type fairness** | Mean `company_quality` varies ≤10 points across `company_type` buckets |
| **Advocacy parity** | An `advocacy.*` role and an equivalent `swe.*` role at the same company score within 10 points for a matching profile |
| Determinism | Same inputs produce the same score, always |

**Regression detection** — a nightly rescore of 500 fixed jobs, alerting if any
score shifts by more than 10 points without a weight version change. This catches
data drift and dependency changes that no unit test would.
