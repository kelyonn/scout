# Normalization and Taxonomy — Scout

**Status:** Draft · **Owner:** Discovery · **Last updated:** 2026-08-06

Turning twelve kinds of adapter output into one canonical job record, and the
controlled vocabularies that make ranking possible.

---

## 1. Why this is the hard part

Sources disagree about everything. The same role is described as:

| Field | Greenhouse | Lever | HN comment | Email alert |
| --- | --- | --- | --- | --- |
| Title | "Software Engineering Intern" | "Intern, Software Engineering" | "SWE Intern" | "Software Engineer Intern - Summer 2027" |
| Location | `{"name": "Bengaluru, India"}` | `"Bangalore"` | "Bangalore (hybrid)" | "Bengaluru, Karnataka, India" |
| Compensation | absent | `"₹80,000/mo"` | "80k/month INR" | absent |
| Type | `"Internship"` | `"Intern"` | implied | "Internship" |

Ranking cannot compare these until they are the same shape. Every quality
problem downstream — bad dedup, bad ranking, missed notifications — traces back
to normalization getting something wrong here.

---

## 2. Canonical schema

Defined once in `packages/schema/job.schema.json`, generating Go structs,
Pydantic models, and TypeScript types. Adapters produce this shape or fail.

```jsonc
{
  "identity": {
    "source_id": "uuid",
    "external_id": "5847392",            // ATS-native, when available
    "url": "https://...",                 // as fetched
    "canonical_url": "https://...",       // normalized
    "apply_url": "https://..."
  },
  "company": {
    "name_raw": "Cloudflare, Inc.",
    "domain_hint": "cloudflare.com",
    "ats_token": "cloudflare"
  },
  "content": {
    "title_raw": "Software Engineering Intern, Summer 2027",
    "description_html": "<p>...</p>",
    "description_text": "...",
    "requirements_text": "...",
    "department": "Engineering",
    "employment_type_raw": "Internship"
  },
  "location": {
    "raw": "Remote - India",
    "candidates": [                       // a job may list several
      { "city": "Bengaluru", "region": "Karnataka", "country": "IN" }
    ],
    "remote_hint": true
  },
  "compensation": {
    "raw_text": "₹80,000 per month",
    "min": 80000, "max": null,
    "currency": "INR", "period": "month",
    "confidence": 0.95
  },
  "timing": {
    "posted_at": "2026-08-06T10:52:00Z",
    "posted_at_estimated": false,
    "deadline_at": null,
    "start_date": "2027-05-15"
  },
  "provenance": {
    "adapter": "ats_greenhouse",
    "adapter_version": "1.4.0",
    "fetched_at": "2026-08-06T11:04:32Z",
    "content_hash": "sha256:..."
  }
}
```

**Adapters produce raw values; the normalizer produces canonical ones.** An
adapter must not guess at a location tier or a role family — its job is faithful
extraction. This separation means a classification improvement is one code change
applied by replay, not twelve adapter changes.

---

## 3. URL canonicalization

`SCOUT-NORM-001`. The foundation of exact-match dedup, so it must be exactly
right.

```
1. Lowercase scheme and host. Preserve path case (some ATS tokens are
   case-sensitive).
2. Force https where the host supports it.
3. Strip default ports (:80, :443).
4. Remove tracking parameters:
   utm_*, gh_src, gh_jid (when redundant with the path), lever-source,
   ref, source, src, fbclid, gclid, mc_cid, mc_eid, trk, trackingId,
   refId, position, pageNum, li_fat_id, _hsenc, _hsmi
5. Remove session-like parameters: sessionid, jsessionid, phpsessid, sid
6. Sort remaining query parameters alphabetically.
7. Remove empty parameters.
8. Remove the fragment, EXCEPT where it is load-bearing — Workday and
   SuccessFactors use hash routing, so #/job/... is preserved. Per-host rule.
9. Remove a trailing slash unless the path is exactly "/".
10. Percent-encoding normalized to uppercase hex, and unreserved characters
    decoded.
```

**Per-host overrides** exist because some ATS platforms need special handling.
Greenhouse `gh_jid` is sometimes the only job identifier and must be kept.
Workday's hash fragment is the route. These live in a versioned rules file with
test cases, not in scattered conditionals.

**Redirect resolution.** Shortened and tracked links (`lnkd.in`, `bit.ly`, board
redirectors) are followed up to 3 hops before canonicalization, with the SSRF
guard from [06](06-ingestion-pipeline.md) at every hop.

---

## 4. Role and company taxonomy

`SCOUT-NORM-002`. Titles are unreliable, so we map them into a controlled
hierarchy and rank against the hierarchy.

```
swe/
├── general              Software Engineer, SDE, Member of Technical Staff
├── backend              Backend, Server, API, Distributed Systems
├── frontend             Frontend, UI, Web, Client
├── fullstack            Full Stack, Product Engineer
├── mobile               iOS, Android, React Native, Flutter
├── ml/                  Machine Learning, AI, Deep Learning
│   └── research         Research Scientist, Research Engineer (ML)
├── data                 Data Engineer, Analytics Engineer, Data Platform
├── infra/               Infrastructure
│   ├── sre              Site Reliability, Production Engineering
│   ├── devops           DevOps, Build, Release, Developer Productivity
│   ├── platform         Platform, Developer Platform, Internal Tools
│   └── cloud            Cloud, AWS/GCP/Azure Engineer
├── systems              Systems, Kernel, Compiler, Runtime, Database Internals
├── security             Security, AppSec, Infrastructure Security
├── embedded             Embedded, Firmware
├── qa                   QA, SDET, Test Engineering
├── research             Research Engineer (non-ML)
├── advocacy/            Developer-facing engineering — see below
│   ├── devrel           Developer Advocate, Developer Relations, Evangelist
│   ├── devex            Developer Experience, Developer Productivity (external)
│   └── solutions        Solutions Engineer, Sales Engineer, Field Engineer,
│                        Forward Deployed Engineer, Technical Consultant
└── other                Software but unclassified
```

**All twenty leaves are in scope for this user.** The taxonomy exists to enable
differentiated skill matching, not to filter — a backend role should be scored
against backend skills, and a systems role against systems skills.

### The advocacy family

`SCOUT-NORM-002a`. Developer advocacy is a genuine engineering career path, and it
is invisible to a taxonomy built only around implementation roles. Postings use
titles that share almost no vocabulary with `swe.*`:

```
Developer Advocate · Developer Relations Engineer · Developer Evangelist
DevRel Intern · Community Engineer · Technical Community Manager
Developer Experience Engineer · Developer Educator · Technical Content Engineer
Solutions Engineer · Sales Engineer · Field Engineer · Solutions Architect
Forward Deployed Engineer · Implementation Engineer · Technical Consultant
Technical Program Manager (engineering-adjacent)
```

**Three sub-families, because they are genuinely different jobs:**

`advocacy.devrel` is outbound — writing, speaking, demos, sample code, community.
Employers are usually dev-tools, cloud, infra, and API companies, which overlaps
strongly with this user's stated domain interests.

`advocacy.devex` is inward-facing tooling for *external* developers: SDKs, docs
infrastructure, sample applications, developer portals. It sits closest to
`swe.infra.platform` and is often mislabelled as it.

`advocacy.solutions` is customer-facing engineering — pre-sales technical work,
deployments, integrations. **This is the family that makes consultancies and
enterprise vendors legible**, since a large share of their technical intern roles
are effectively solutions work under a different name.

### Two classification hazards, both handled explicitly

**Hazard 1: advocacy titles collide with marketing and sales.** "Developer
Marketing Intern", "Community Manager", and "Technical Recruiter" are not
engineering roles and must not enter the feed. The `advocacy` family therefore
carries the strictest negative-pattern set in the taxonomy:

```yaml
advocacy.devrel:
  strong: ["developer advocate", "developer relations", "developer evangelist",
           "devrel", "developer educator", "community engineer"]
  weak:   ["technical community manager", "developer community.*engineer"]
  negative: ["developer marketing", "community manager$", "social media",
             "content marketing", "growth marketing", "technical recruit",
             "partnerships? manager", "customer success manager"]
  requires: technical_evidence   # see below
```

`requires: technical_evidence` is a gate unique to this family: the description
must contain a concrete technical signal — a named language, SDK, API, or
"write code" phrasing — before the role is admitted. A "Developer Advocate" post
that never mentions code is a marketing role wearing an engineering title, and
this check is what keeps the feed clean.

**Hazard 2: solutions roles shade into pure sales.** `advocacy.solutions` applies
the same gate. "Sales Engineer" with a quota and no technical requirement is a
sales job; "Sales Engineer" building integration prototypes is engineering. The
description decides, not the title.

Both hazards mean the advocacy family escalates to Tier 2 LLM classification more
often than any other — expect **~25% escalation versus the 8% baseline**. That is
budgeted for in [18](18-cost-model.md) and is the correct trade: precision here is
worth more than the fraction of a cent it costs.

### Skill matching differs for advocacy

Scoring an advocacy role purely on `swe` skills would systematically under-rank it,
so `skill_match` uses a family-specific weighting
([09](09-ranking-scoring.md) section 3.1):

| Signal | `swe.*` | `advocacy.devrel` | `advocacy.solutions` |
| --- | --- | --- | --- |
| Depth in a named language or stack | High | Medium | Medium |
| Breadth across technologies | Low | **High** | **High** |
| Public writing, talks, or content | — | **High** | Low |
| Open-source or community activity | Medium | **High** | Low |
| Communication evidenced in the resume | Low | **High** | **High** |

Breadth beats depth for advocacy: someone who has touched twelve technologies
shallowly is a *better* fit for developer advocacy than someone with one deep
specialisation, which is the exact inverse of the `swe.backend` weighting. A single
undifferentiated skill-match formula gets this backwards.

### Classification method

Per [ADR-005](adr/ADR-005-llm-cascade.md), a cascade:

**Tier 0 — pattern rules.** A curated table of ~400 title patterns per family,
matched after title normalization. Handles about 70% of titles outright.

```yaml
swe.backend:
  strong: ["backend engineer", "back-end engineer", "server engineer",
           "api engineer", "distributed systems engineer"]
  weak:   ["software engineer.*backend", "backend.*intern"]
  negative: ["backend.*designer", "backend.*marketing"]
```

**Tier 1 — embedding nearest-neighbour.** Each family has 20–40 exemplar titles,
embedded once. An unmatched title is embedded and assigned the nearest family if
cosine ≥ 0.72. Handles about 22%.

**Tier 2 — small LLM.** Structured classification with the description as
context, used when the title alone is uninformative — "Member of Technical
Staff", "Engineer I", "Technology Analyst". About 8%.

**Precision target: ≥97% on the golden set.** Measured per family, because an
aggregate number hides a family that is systematically broken.

### Title normalization

Before any classification:

```
"Software Engineering Intern, Summer 2027 (Bengaluru) - REQ12345"
  → strip requisition IDs:      /\b(req|job|id)[\s#-]*\d{3,}\b/i
  → strip parenthetical location when it duplicates the location field
  → strip season/year:          /\b(summer|winter|fall|spring)\s*20\d\d\b/i
  → strip level suffixes for matching (retained separately)
  → collapse whitespace, lowercase
  → expand abbreviations: SWE→software engineer, SDE→software development
    engineer, MLE→machine learning engineer, SRE→site reliability engineer
= "software engineering intern"
```

The stripped components are not discarded — season and year feed the start-date
inference, and level suffixes feed seniority detection.

### Company taxonomy

`SCOUT-NORM-002b`. The brief is explicit that Scout must not be product-company
biased, and "no FAANG bias" is only half of that. A system tuned for venture-backed
product startups misses the other half just as badly — the services firms, the
enterprises whose core business is not software, and the global capability centres
that between them employ most software engineers in Bengaluru.

**Company type is a separate axis from size, stage, and industry.** All four are
stored, and none of them may be used as a proxy for quality.

```
company_type
├── product              Software is the product. SaaS, dev tools, AI, infra,
│                        consumer, marketplaces. Startup through public.
├── services/            Software is delivered to clients
│   ├── it_services      TCS, Infosys, Wipro, HCLTech, Tech Mahindra,
│   │                    LTIMindtree, Cognizant, Mphasis, Persistent, Zensar
│   ├── consulting       Accenture, Deloitte, EY, KPMG, PwC, McKinsey Digital,
│   │                    ZS Associates, BCG X
│   └── engineering_svc  Thoughtworks, EPAM, Globant, Publicis Sapient,
│                        Nagarro, GlobalLogic, Cyient, KPIT, Tata Elxsi
├── gcc                  Global Capability Centre — an offshore engineering arm
│                        of a foreign parent. See below; the most under-served
│                        category and disproportionately Bengaluru-based.
├── core_industry/       Software serves a non-software core business
│   ├── bfsi             Banks, NBFCs, insurers, exchanges, payments
│   ├── manufacturing    Automotive, industrial, electronics, semiconductors
│   ├── energy           Oil and gas, power, renewables
│   ├── telecom          Carriers and network operators
│   ├── retail_cpg       Retail, e-commerce arms of retailers, FMCG
│   ├── healthcare       Hospitals, pharma, medical devices
│   ├── aerospace_def    Aerospace, defence, space
│   └── logistics        Shipping, freight, mobility
├── research             Labs, institutes, universities, national R&D
├── public_sector        Government, PSUs, ISRO/DRDO/C-DAC, UIDAI, NPCI
└── nonprofit            Foundations, OSS orgs, social-impact engineering
```

**Why GCC is its own type rather than a tag.** A Global Capability Centre is a
foreign company's owned engineering centre in India — Walmart Global Tech, Target
in India, Goldman Sachs Bengaluru, Wells Fargo, Shell, Bosch, Continental,
Mercedes-Benz R&D, Rolls-Royce, Micron, Texas Instruments, Applied Materials,
Lowe's, Tesco. They are structurally distinct in four ways that matter for this
system:

1. **They are concentrated in Bengaluru**, which is this user's Tier 1 location.
   Ignoring them means ignoring a large share of the highest-priority roles.
2. **The parent's `company_type` is misleading.** Target is `core_industry.retail_cpg`
   as a business, but Target in India is a software engineering organisation.
   Scoring the Bengaluru entity on the parent's industry gets it wrong.
3. **They run structured, well-paid internship programmes** with real conversion
   rates — often better than the local startup average.
4. **They rarely appear on Greenhouse or Lever**, so ATS enumeration misses them
   entirely. They are found on Workday, SuccessFactors, Taleo, or bespoke portals.

The parent relationship is modelled rather than flattened:
`company.parent_company_id` plus `company_type = 'gcc'`, so "Goldman Sachs" and
"Goldman Sachs Bengaluru" resolve as related-but-distinct rather than merging —
which matters for dedup, since the same role posted by both is one job, while
different roles at each are two.

### Classifying company type

The same cascade as roles, with cheaper signals first:

**Tier 0 — a curated registry.** The top ~400 employers relevant to an Indian CS
student are classified by hand and kept in `packages/taxonomy/companies.yaml`. This
is a small, high-value list that covers the overwhelming majority of postings, and
hand-maintaining it is cheaper and more accurate than inferring it.

**Tier 1 — domain and description heuristics.** Corporate suffixes, `.co.in` versus
`.com`, self-description phrases ("we help our clients", "global capability
centre", "our engineering centre in Bengaluru"), and NIC/SIC-style industry hints.

**Tier 2 — small LLM**, given the company description and website copy. Runs once
per company, not per job, and the result is cached until the description changes —
so this is a negligible cost even at Tier 2 rates.

**Unknown is a permitted value and is scored neutrally.** An unclassified company
must never be ranked below a classified one, because that would quietly reintroduce
the bias this taxonomy exists to prevent — well-known companies are easier to
classify, so penalising `unknown` is a fame bias in disguise.

### What company type is and is not used for

| Used for | Not used for |
| --- | --- |
| Choosing the right ingestion adapter | Any term in `company_quality` |
| Setting the poll schedule (services firms post in bursts) | Filtering the feed |
| Calibrating `competition_estimate` — a mass campus drive has different odds than a 12-person startup | Any ranking penalty |
| Explaining a role in context | Notification thresholds |
| Coverage auditing: are we missing a whole category? | |

**The coverage audit is the point.** The weekly recall audit in
[16](16-observability.md) is measured **per company type**, so a systematic blind
spot — no GCC roles for three weeks, say — becomes visible instead of hiding inside
a healthy aggregate recall number. An aggregate that looks fine while an entire
category is missing is precisely the silent failure this taxonomy is meant to catch.

---

## 5. Seniority detection

`SCOUT-NORM-003`. The hard filter that determines whether a role is relevant at
all.

| Class | Signals |
| --- | --- |
| `internship` | "intern", "internship", "co-op", "coop", "trainee", "summer analyst", "industrial trainee", "apprentice" (context-dependent) |
| `new_grad` | "new grad", "new graduate", "university grad", "campus hire", "graduate engineer", "GET", "entry level" + graduation-year language, "0-1 years" |
| `entry` | "junior", "associate", "I", "SDE-1", "L3" without new-grad framing |
| `mid`/`senior`/`staff` | Explicit levels, or "3+ years", "5+ years" |

**Ambiguity is common and consequential.** "Graduate Engineer Trainee" is a new
grad role in India and a mid-level role in some European contexts. "Associate
Software Engineer" ranges from new grad to 3 years depending on the company.

Resolution rules, in order:
1. Explicit years-of-experience in requirements beats title language. "5+ years"
   overrides "Associate".
2. Explicit graduation-year language ("graduating in 2027") is decisive for
   `new_grad`.
3. Indian context: "GET", "Graduate Engineer Trainee", "Management Trainee
   (Technology)" map to `new_grad`.
4. Unresolvable → Tier 2 LLM with the requirements section as context.
5. Still unresolvable → `unknown`, which is **included in the feed at reduced
   score, not excluded.** Excluding on uncertainty causes silent misses, which is
   the failure mode we care most about.

---

## 6. Location normalization and tiering

`SCOUT-NORM-004` / `SCOUT-RANK-LOC`.

### Parsing

Location strings are chaotic: `"Bengaluru, India"`, `"Bangalore"`,
`"BLR"`, `"Remote - India"`, `"Hybrid: Bangalore/Hyderabad"`,
`"Multiple locations"`, `"Anywhere"`, `""`.

```
1. Split on delimiters (/, ;, |, "or", "and") → multiple candidate locations.
2. Detect remote/hybrid keywords → work_mode.
3. Geocode each candidate against a local gazetteer
   (GeoNames cities > 15k population + a curated India supplement).
4. Resolve aliases: Bangalore = Bengaluru, Bombay = Mumbai,
   Calcutta = Kolkata, Madras = Chennai, Gurgaon = Gurugram,
   NCR = Delhi NCR, Vizag = Visakhapatnam, Trivandrum = Thiruvananthapuram.
5. Assign each candidate a tier.
6. Job tier = BEST (lowest number) tier among candidates.
```

**Local gazetteer, not a geocoding API.** ~50MB of data, sub-millisecond lookups,
zero cost, no rate limit, no network dependency in the pipeline. A geocoding API
would add latency and cost to every single job for no accuracy benefit at city
granularity.

### Tiering

| Tier | Definition | Multiplier |
| --- | --- | --- |
| **1** | Bengaluru / Bangalore, including Whitefield, Electronic City, Koramangala, and other named localities | **1.20** |
| **2** | Any other Indian city | **1.05** |
| **3** | Remote (fully remote, any company location) | **1.12** |
| **4** | International | **0.90** |

**Remote outranks the rest of India but not Bengaluru** (1.12 vs 1.05 vs 1.20).
This encodes the brief's "significant ranking boost" for remote while preserving
"Bangalore should always rank higher" when opportunities are otherwise equal.

**Hybrid** takes the tier of its physical location, with a small penalty relative
to fully remote at the same location, because it constrains the user's options.

**Work-mode interaction:**

| Physical | Mode | Effective tier | Note |
| --- | --- | --- | --- |
| Bengaluru | onsite | 1 | Ideal |
| Bengaluru | hybrid | 1 | Ideal |
| Bengaluru | remote | 1 | Best of both |
| Mumbai | onsite | 2 | |
| Any | remote (India-eligible) | 3 | Boosted |
| San Francisco | onsite | 4 | Visa dependency |
| San Francisco | remote (global) | 3 | Treated as remote — this is the case worth catching |

That last row is important: a US company hiring globally-remote is a Tier 3
opportunity for this user, not Tier 4, and getting it wrong would systematically
under-rank some of the best available roles.

**Visa sponsorship** is detected from description language ("visa sponsorship
available", "we sponsor H-1B", "must have work authorization" as a negative
signal) and applies a ±0.05 adjustment within Tier 4.

---

## 7. Compensation parsing

`SCOUT-NORM-005`. The hard requirement is "paid only", so this gate decides
whether the user ever sees a role.

### Extraction

```
Patterns, in confidence order:

1. Structured field from the ATS         confidence 1.00
2. Explicit range with currency+period   confidence 0.95
   "₹80,000 - ₹1,00,000 per month"
   "$8,500/month"  "INR 8-10 LPA"
3. Single value with currency+period     confidence 0.90
4. Value with inferable period           confidence 0.75
   "₹80,000 monthly stipend"
5. Range without explicit currency,      confidence 0.60
   inferred from location
6. Vague positive ("competitive stipend",
   "paid internship")                    confidence 0.50, paid=true, no amount
7. Nothing                               confidence 0.00, paid=unknown
```

**Indian numbering must be handled explicitly.** `1,00,000` is one lakh, not one
hundred. `8 LPA` is 800,000 INR per year. `₹50K/month` is 50,000. A parser
written for Western conventions gets all three wrong, and getting compensation
wrong on Indian roles would break the primary market.

```
lakh / L / lac    → × 100,000
crore / Cr        → × 10,000,000
LPA               → lakhs per annum
K (after number)  → × 1,000
```

### Normalization

Everything converts to `comp_normalized_inr_month` for comparability:

```
hourly  → × 160 (working hours/month)
weekly  → × 4.33
annual  → ÷ 12
stipend_total → ÷ estimated_duration_months (default 3 for summer internships)
foreign currency → × monthly-averaged FX rate (cached, refreshed daily)
```

**FX uses a monthly average, not a spot rate.** Spot rates would cause a job's
score to fluctuate day to day for no real reason, which makes scores
non-reproducible and confuses the calibration.

### The paid determination

```
paid = 'paid'    if compensation extracted with confidence ≥ 0.50
                 OR employment_type indicates a paid classification
                 OR the company is in a "known always pays" list
                 OR the country/role combination has a legal minimum
                    (US internships at for-profit companies, EU, etc.)

paid = 'unpaid'  if explicitly stated ("unpaid", "no stipend",
                 "for academic credit only", "volunteer")

paid = 'unknown' otherwise
```

**Handling `unknown`, which is the majority case.** Most postings do not state
compensation. Excluding them would discard most of the market; including them
unqualified would violate the hard requirement.

Resolution: `unknown` roles are **shown in the dashboard** with a "compensation
not stated" badge, and are **eligible for notification only if** the company has
a history of paid roles, or the company is in a size/stage bucket where unpaid
would be unusual, or the priority score is otherwise very high (≥92). This
inference is recorded in `score_inputs` so it is auditable.

Explicit `unpaid` roles are excluded from notification entirely, unless
`prestige_exception` is set — see [05](05-source-catalog.md) section 9.

---

## 8. Skill extraction

`SCOUT-NORM-006`. Feeds skill match, resume match, and gap analysis.

**Method:** dictionary-first, embeddings for the tail.

A curated skill ontology (~1,200 entries) with aliases and relationships:

```yaml
- id: golang
  display: Go
  aliases: [go, golang, "go lang"]
  category: language
  related: [concurrency, grpc, kubernetes]

- id: kubernetes
  display: Kubernetes
  aliases: [k8s, kube]
  category: infrastructure
  implies: [docker, containers, yaml]
```

`implies` matters for matching: a job asking for Kubernetes is partially
satisfied by Docker experience, and the resume matcher uses these edges rather
than requiring exact overlap.

**Extraction pipeline:**
1. Alias-match against the ontology over the requirements section, with
   word-boundary matching (avoids "R" matching every capital R, and "Go" matching
   "going").
2. Weight by section — skills in "Requirements" count double those in "Nice to
   have"; skills in "About the team" count half.
3. Extract unmatched capitalized technical-looking tokens and cluster them
   weekly. Recurring unknowns are surfaced for ontology addition, which is how the
   ontology stays current without manual curation.

**Why not pure LLM extraction.** Cost (every job × every skill) and consistency —
an LLM will call the same thing "JS", "JavaScript", and "Javascript" across three
jobs, which makes skill matching and gap analysis unreliable. A dictionary
guarantees canonical IDs.

---

## 9. Posted-date inference

`SCOUT-NORM-007`. Freshness drives ranking and the notification SLO, so a wrong
posted date directly harms the product.

| Signal | Confidence | Notes |
| --- | --- | --- |
| ATS-provided `created_at` / `published_at` | 1.00 | Trust it |
| JSON-LD `datePosted` | 0.95 | Standard field |
| Explicit date in the page | 0.85 | Parsed with a date library, timezone-aware |
| Relative text ("3 days ago") | 0.80 | Computed against fetch time |
| Sitemap `<lastmod>` | 0.60 | Page modification, not necessarily posting |
| **First observation time** | 0.40 | Fallback: we saw it now, so it exists now |

When falling back to first-observation time, `posted_at_estimated = true` and the
freshness multiplier is damped, because treating a job we just discovered from a
dormant source as "posted 2 minutes ago" would rank a two-month-old posting above
a genuinely new one.

**Repost detection.** Some companies close and repost a job to appear fresh. If a
job group's canonical URL was previously seen and closed, and reappears with a new
posted date, we retain the *original* first-seen date and flag `is_ghost` for
review. The user should not be re-notified about a role they already saw and
declined.

---

## 10. Ghost posting detection

`SCOUT-NORM-008`. Postings that stay live without active hiring.

| Signal | Weight |
| --- | --- |
| Open more than 120 days | 0.30 |
| Reposted more than 3 times in 12 months | 0.25 |
| Description unchanged more than 180 days | 0.15 |
| Company has more than 40 open roles with fewer than 100 employees | 0.20 |
| "Evergreen", "always hiring", "talent pool", "future opportunities" | 0.35 |
| No specific team or project named | 0.10 |

Score above 0.5 sets `is_ghost = true`.

**Ghosts are downranked, not hidden** (a −15 point priority penalty). Some
evergreen postings are real, and hiding them risks a silent miss. The dashboard
shows a "may be evergreen" badge so the user can decide.

---

## 11. HTML sanitization

Every byte from the internet is hostile.

**Storage:** HTML is sanitized with a strict allowlist before it touches the
database — `p, br, ul, ol, li, strong, em, b, i, h1–h6, a[href], code, pre,
blockquote, table, thead, tbody, tr, td, th`. Everything else is stripped.
`href` values are validated as `http`/`https` only, and `rel="noopener
noreferrer nofollow"` is forced on every link.

**Rendering:** sanitized again client-side, and rendered through a component that
cannot execute script. Defense in depth, because a bug in the storage sanitizer
should not become stored XSS.

**Text extraction** for embeddings and search happens after sanitization, with
entity decoding and whitespace normalization.

---

## 12. Quality gates

A normalized job must pass all of these to enter the pipeline. Failures are
stored with a reason for review rather than silently dropped.

| Gate | Rule | On failure |
| --- | --- | --- |
| Has a title | Non-empty after normalization | Reject, log |
| Has an apply URL | Valid absolute http(s) URL | Reject, log |
| Company resolved | `company_id` assigned | Hold for company resolution retry |
| Title is plausible | 3–200 characters | Reject, log |
| Not obvious spam | Not matching a spam pattern list | Reject, log |
| Is software | `is_software = true` | Store, exclude from feed |
| Description present | ≥100 characters, or an ATS source we trust | Store, flag `thin_content` |

`thin_content` jobs — common from HN comments and email alerts — get an enrichment
attempt: resolve the company's ATS and fetch the full posting. If that succeeds,
the thin observation and the rich one merge into one group and the rich one
becomes the representative.
