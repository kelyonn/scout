# Source Catalog — Scout

**Status:** Draft · **Owner:** Discovery · **Last updated:** 2026-08-06

Every source Scout monitors, how it is accessed, whether that access is
permitted, and what it costs.

---

## 1. Source tiering

Sources are ranked 1–5 by expected value, which drives poll frequency and
implementation order.

| Tier | Meaning | Poll cadence | Examples |
| --- | --- | --- | --- |
| **1** | Direct ATS for a company with high hiring intent | 5–15 min | Greenhouse board for a company that posted last week |
| **2** | Direct ATS, normal activity | 30–60 min | Most company boards |
| **3** | Aggregators and community with good yield | 1–4 hours | HN Who is Hiring, GitHub repos, VC portfolios |
| **4** | Low-yield or slow-moving | 6–24 hours | Research labs, university boards |
| **5** | Discovery-only, not job-bearing | Weekly | Funding feeds, company directories |

Tier is not static. The scheduler promotes and demotes based on measured yield —
see [06](06-ingestion-pipeline.md).

---

## 2. Applicant Tracking Systems

The highest-value category by a wide margin. ATS platforms are where postings
originate, so they are the fastest and richest source, and most expose stable
public JSON.

| Platform | Access | Endpoint pattern | Quality | Effort | Milestone |
| --- | --- | --- | --- | --- | --- |
| **Greenhouse** | Public JSON API | `boards-api.greenhouse.io/v1/boards/{token}/jobs?content=true` | Excellent — full description, departments, offices, structured | Low | P1 |
| **Lever** | Public JSON API | `api.lever.co/v0/postings/{token}?mode=json` | Excellent — full content, categories, commitment type | Low | P1 |
| **Ashby** | Public GraphQL | `jobs.ashbyhq.com/api/non-user-graphql` (`ApiJobBoardWithTeams`) | Excellent — structured, compensation often present | Low | P1 |
| **Workable** | Public JSON | `apply.workable.com/api/v1/widget/accounts/{token}` | Good | Low | P2 |
| **SmartRecruiters** | Public REST API | `api.smartrecruiters.com/v1/companies/{id}/postings` | Good — documented, paginated | Low | P2 |
| **Recruitee** | Public JSON | `{token}.recruitee.com/api/offers/` | Good | Low | P2 |
| **Teamtailor** | Public JSON | `{token}.teamtailor.com/jobs.json` | Good | Low | P2 |
| **Rippling** | Public JSON | `ats.rippling.com/api/v1/board/{token}/jobs` | Good | Low | P3 |
| **BambooHR** | Public JSON | `{token}.bamboohr.com/careers/list` | Fair — thin descriptions | Low | P3 |
| **Workday** | Public JSON, per-tenant | `{tenant}.wd{N}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs` | Fair — heavy pagination, inconsistent schema | **High** | P3 |
| **iCIMS** | HTML + sitemap | Varies per tenant | Poor — mostly HTML | High | P5 |
| **Jobvite** | HTML + partial JSON | `jobs.jobvite.com/{token}` | Fair | Medium | P5 |
| **SuccessFactors** | HTML, OData sometimes | `career{N}.successfactors.eu/...` | Poor — enterprise, awkward | High | **P5** |
| **Taleo** | HTML | Varies | Poor — legacy, being replaced | High | Deferred |
| **Darwinbox** | Partial JSON | `{tenant}.darwinbox.in/ms/candidate/careers` | Fair — common at Indian enterprises and GCCs | Medium | P5 |
| **Keka** | Public JSON | `{tenant}.keka.com/careers/api/...` | Good — widely used by Indian mid-market | Low | P5 |
| **Zoho Recruit** | Public JSON | `{tenant}.zohorecruit.in/recruit/portal/...` | Good — common at Indian SMBs | Low | P5 |

**Why Greenhouse, Lever, and Ashby come first.** Between them they cover the
large majority of the startup and scale-up market that Scout most wants to reach,
they all expose clean public JSON that needs no HTML parsing, and one adapter
covers thousands of companies. Three adapters in P1 buys more coverage than
twenty career-page scrapers would.

**Why Workday is disproportionately hard, and why we do it anyway.** Every Workday
customer is a separate tenant with its own subdomain, site name, and often its own
facet configuration. The JSON endpoint requires a POST with a search payload,
paginates 20 at a time, and returns descriptions only on a second per-job request.
One large company on Workday can cost 30 requests where Greenhouse costs one.

It is scheduled for P3 rather than deferred because **Workday, SuccessFactors, and
Taleo are where the enterprises, GCCs, and services firms live**. Greenhouse and
Lever cover the startup market beautifully and cover Walmart Global Tech, Target in
India, Goldman Sachs Bengaluru, Bosch, and Shell not at all. An ATS strategy that
stops at the easy three encodes exactly the product-company bias that
[07](07-normalization-taxonomy.md) exists to prevent — the bias arrives through the
adapter roadmap rather than through the ranking formula, which makes it harder to
notice.

SuccessFactors moves from "someday" to **P5** for the same reason.

### Board token discovery

An ATS adapter is only useful if we know the company's board token. Four
strategies, in order of reliability:

1. **Career page link extraction.** Fetch the company's `/careers` page and look
   for links to any known ATS domain. Yields the token directly. ~70% hit rate.
2. **Common-name probing.** Try the company slug against each ATS with a `HEAD`
   request. A 200 confirms. Rate-limited hard and cached negatively for 30 days
   so we do not repeatedly probe non-existent boards.
3. **Public directories.** Some ATS platforms publish customer lists or have
   discoverable sitemaps.
4. **Manual.** Admin UI for the long tail that matters.

---

## 3. Company career pages

For companies not on a known ATS, or where the ATS is not discoverable.

| Method | Preference | Notes |
| --- | --- | --- |
| RSS/Atom feed | 1st | Rare but ideal. Check `/careers/feed`, `/jobs.rss`, `<link rel=alternate>` |
| JSON-LD `JobPosting` | 2nd | schema.org markup, increasingly common because Google for Jobs rewards it. Structured and reliable. |
| Sitemap | 3rd | `sitemap.xml` with a jobs section, using `<lastmod>` for cheap change detection |
| Structured HTML | 4th | Per-site CSS selectors, stored in `adapter_config` |
| Rendered HTML | Last resort | Headless Chromium. Expensive, fragile, used for fewer than 20 high-value sources. |

**JSON-LD deserves emphasis.** Because Google for Jobs requires `JobPosting`
structured data for inclusion, a large and growing share of career pages embed
clean, machine-readable job data in a `<script type="application/ld+json">` tag.
Parsing it is trivial and it is far more stable than CSS selectors. The
normalization adapter checks for it before anything else.

**Rendered HTML policy.** Headless browsing is capped at 20 sources, each
requiring explicit justification recorded in `source.notes`. Each render costs
roughly 200x a plain fetch in CPU and memory. This is a budget, enforced by a
check in the source registration flow, not a guideline.

---

## 4. Job boards

See [ADR-007](adr/ADR-007-no-tos-violating-scraping.md) — this is the category
where the brief and legal reality conflict.

| Board | ToS posture | Scout's method | Milestone |
| --- | --- | --- | --- |
| **LinkedIn Jobs** | Prohibits automated access | **Email alert ingestion** | P3 |
| **Indeed** | Prohibits; publisher program exists | **Email alerts**, publisher API applied for | P3 |
| **Glassdoor** | Prohibits | **Email alerts** | P5 |
| **Handshake** | Prohibits; university-linked account | **Email alerts only.** Never automated access. | P5 |
| **Wellfound (AngelList)** | Permits with attribution; has a feed | Direct fetch | P2 |
| **Y Combinator Work at a Startup** | Public listing pages, robots-permitted paths | Direct fetch, polite | P2 |
| **Levels.fyi Jobs** | Public, permissive | Direct fetch | P3 |
| **Naukri / Internshala** | Region-critical for India; ToS restrictive | Email alerts | P5 |
| **RemoteOK / WeWorkRemotely** | Public JSON APIs, explicitly offered | Direct API | P2 |
| **Hacker News Who is Hiring** | Official Firebase API | Direct API | P1 |

### Email alert ingestion, in detail

The mechanism that makes prohibited boards accessible legitimately.

```
User configures job alerts on LinkedIn/Indeed/etc.
using alerts@scout.<domain> as the delivery address
                    │
                    ▼
    Inbound mail provider receives it
    (Cloudflare Email Routing → Worker, or Postmark inbound)
                    │
                    ▼
    POST /api/v1/hooks/email  (HMAC-verified)
                    │
                    ▼
    Sender identification → board-specific parser
                    │
                    ▼
    Extract: title, company, location, link, snippet
                    │
                    ▼
    Resolve tracking redirect → canonical URL
                    │
                    ▼
    Try to match the company to a known ATS source.
    If found, fetch the full posting from there instead —
    richer content, and it confirms the posting is live.
                    │
                    ▼
    Standard observation → normalize → dedup → score
```

**Recommended alert configuration** (one-time, ~20 minutes, in the setup
runbook): one alert per role family × location tier. Roughly 12 alerts on
LinkedIn, 8 on Indeed. Overlapping coverage is fine — dedup handles it, and
overlap is what protects against a single alert's criteria being too narrow.

**Redirect resolution.** Alert emails wrap links in tracking redirectors. We
follow up to 3 redirects to reach the canonical URL, with an SSRF guard rejecting
private IP ranges at every hop.

---

## 5. Services firms, GCCs, and core-industry employers

`SCOUT-SRC-005`. The category the brief asked for and the one a startup-shaped
crawler misses entirely. Per [07](07-normalization-taxonomy.md), these employers
are structurally different from product companies, and so are the places they post.

**Why this matters more than its share of the source count suggests.** Global
Capability Centres are concentrated in Bengaluru — this user's Tier 1 location —
and run structured, well-paid internship programmes with real conversion rates.
They are also nearly invisible to the tools students actually use, because they do
not post to Greenhouse and are buried on aggregators. That combination, high value
and low visibility, is exactly the arbitrage Scout exists to capture. It shows up
in `competition_estimate` as a genuine bonus rather than a thumb on the scale.

### 5.1 Indian IT services and consulting

| Employer group | Access | Notes | Milestone |
| --- | --- | --- | --- |
| **TCS** | `nextstep.tcs.com` — bespoke portal | Requires session handling; check robots.txt before anything | P5 |
| **Infosys** | Career portal + Workday for lateral | InfyTQ drives most intern hiring | P5 |
| **Wipro** | Bespoke portal, Elite/WILP programmes | Cycle-driven, bursty | P5 |
| **HCLTech, Tech Mahindra, LTIMindtree, Mphasis** | SuccessFactors / Workday | Covered by the enterprise ATS adapters | P5 |
| **Cognizant, Accenture, Capgemini** | Workday and bespoke | Large Bengaluru and Hyderabad intake | P5 |
| **Deloitte, EY, KPMG, PwC** | Workday / SuccessFactors | Technology-consulting intern tracks | P5 |
| **Thoughtworks, EPAM, Globant, Nagarro, Publicis Sapient** | Greenhouse, Lever, Workday | **Already covered by existing adapters** — they simply need to be in the seed list | P2 |
| **Persistent, Zensar, Cyient, KPIT, Tata Elxsi** | Mixed; several on Darwinbox or Keka | P5 | P5 |

**Thoughtworks and EPAM are the cheapest win in this whole section.** They are
engineering-services firms of genuinely high quality that already use ATS platforms
Scout supports from P1. They were missing from the seed list, not from the
architecture. Adding them is a data change, not a code change — which is a useful
reminder that a coverage gap is not always an engineering gap.

**Cycle awareness.** Services firms hire in bursts tied to campus season rather
than continuously. Adaptive scheduling
([06](06-ingestion-pipeline.md)) would otherwise back these sources off to a 24-hour
interval during a quiet stretch and then miss the opening day of a drive. Sources
tagged `hiring_pattern = 'cyclical'` therefore have a **poll-interval ceiling of 4
hours regardless of recent yield**, and are escalated during known campus windows.

### 5.2 Global Capability Centres

The single largest under-served category for a Bengaluru-based candidate.

| Sector | Examples with large Bengaluru engineering centres |
| --- | --- |
| Retail and CPG | Walmart Global Tech, Target, Lowe's, Tesco, Nike, Unilever |
| BFSI | Goldman Sachs, JPMorgan, Wells Fargo, Morgan Stanley, Standard Chartered, Visa, Mastercard, American Express |
| Industrial and auto | Bosch, Continental, Mercedes-Benz R&D, Volvo, Siemens, ABB, Honeywell, Schneider, John Deere, Caterpillar |
| Semiconductor and hardware | Micron, Texas Instruments, Applied Materials, NXP, Analog Devices, Qualcomm, AMD, Arm |
| Aerospace and energy | Rolls-Royce, Airbus, Collins Aerospace, Shell, bp |
| Healthcare | Philips, GE HealthCare, Siemens Healthineers, Novo Nordisk |

**Discovery strategy**, in order of cost:

1. **A curated seed list.** These are known, finite, and stable. ~150 entries of
   `(parent, india_entity, ats_platform, tenant_id)` maintained by hand. Not
   glamorous, and by far the highest yield per hour spent.
2. **ATS tenant enumeration.** Most run Workday or SuccessFactors; the tenant slug
   is usually guessable from the brand and confirmable with one `HEAD` request.
3. **Location-facet filtering.** Their global boards carry thousands of roles, so
   fetch with a Bengaluru/India location facet rather than paginating everything.
   This is the difference between 30 requests and 3,000.
4. **NASSCOM and GCC-association member lists** for discovering new entrants.

**Entity resolution matters here.** "Target" and "Target in India" must resolve as
parent-and-subsidiary rather than merging, or a Minneapolis role and a Bengaluru
role collapse into one group and the location tier — the strongest signal in the
whole ranking — becomes wrong. Handled by `parent_company_id` and covered by a
dedicated dedup test in [08](08-dedup-identity.md).

### 5.3 Core-industry employers

Indian companies whose core business is not software but who employ software
engineers: Reliance (Jio Platforms), Tata Motors, Mahindra, L&T Technology
Services, Adani, Airtel, Asian Paints, ITC, Maruti Suzuki, plus PSUs such as BEL,
HAL, and ISRO centres.

Mostly bespoke portals or Darwinbox. Lower posting volume, so a 12-hour poll
interval is adequate — with the cyclical ceiling applied during campus season.

### 5.4 Indian campus platforms

The functional equivalent of Handshake in India, and genuinely important for a
final-year student.

| Platform | Posture | Method | Milestone |
| --- | --- | --- | --- |
| **Superset** | Institute-linked account, ToS restrictive | **Email alerts only** — same reasoning as Handshake ([ADR-007](adr/ADR-007-no-tos-violating-scraping.md)) | P5 |
| **Unstop** (ex-Dare2Compete) | Public listings; check robots.txt per path | Polite direct fetch if permitted, else email alerts | P5 |
| **Internshala** | ToS restrictive | Email alerts | P5 |
| **University placement portal** | Institute credentials | **Never automated.** Email alerts, or manual entry. | P5 |

**Superset and the university portal are treated exactly like Handshake** — they
are tied to institutional identity, and losing that account during placement season
costs more than any coverage gain. This is the same bright line as
[ADR-007](adr/ADR-007-no-tos-violating-scraping.md), applied to the Indian
equivalents rather than only the American ones.

### 5.5 Developer advocacy sources

`advocacy.*` roles ([07](07-normalization-taxonomy.md)) concentrate in places that
general job sources cover poorly.

| Source | Access | Notes |
| --- | --- | --- |
| **DevRel Collective job board** | Public | Small volume, very high relevance |
| **DevRelX / DevRel Careers** | Public listings | Aggregates advocacy roles specifically |
| **CNCF, Linux Foundation, Apache job boards** | Public | OSS-adjacent advocacy and devex roles |
| **Dev-tool company career pages** | Existing ATS adapters | Vercel, Supabase, Netlify, Postman, Twilio, Stripe, MongoDB, Redis, HashiCorp, Grafana, Neon — most already on Greenhouse, Lever, or Ashby |

**The last row is the important one.** Most developer advocacy internships are at
companies Scout already polls; they were being missed at the *classification* step,
not the discovery step. Adding the `advocacy` family to the taxonomy surfaces roles
already sitting in the database. The dedicated boards above are a useful supplement,
not the main fix.

---

## 6. Where each company type is actually found

A consolidated view, because "which adapter finds which kind of employer" is the
question that determines whether coverage is even.

| `company_type` | Primary discovery | Secondary | Covered from |
| --- | --- | --- | --- |
| `product` | Greenhouse, Lever, Ashby | YC, Wellfound, HN | **P1** |
| `services_engineering` | Greenhouse, Lever, Workday | Career pages | **P2** |
| `services_it` | Bespoke portals, SuccessFactors | Campus platforms | P5 |
| `services_consulting` | Workday, SuccessFactors | Career pages | P5–P5 |
| `gcc` | **Workday, SuccessFactors** | Curated seed list, LinkedIn email alerts | **P3** |
| `core_*` | Bespoke portals, Darwinbox | Career pages, email alerts | P5 |
| `research` | Institution pages, mailing lists | Section 9 | P5 |
| `public_sector` | Government portals | Section 9 | P5 |

**Read this table as a coverage-risk map.** Everything landing in P5–P5 is a
category Scout is blind to until then, and the weekly recall audit is bucketed by
`company_type` precisely so those blind spots are visible as numbers rather than
discovered by accident in November.

---

## 7. Startup ecosystems and funding

Newly funded startups hire immediately and are under-monitored, which makes them
disproportionately valuable — good roles with 50:1 rather than 1000:1 ratios.

| Source | Access | What we extract | Milestone |
| --- | --- | --- | --- |
| **Y Combinator company directory** | Public JSON behind the directory | Company list, batch, status, ATS links | P2 |
| **Techstars portfolio** | Public HTML | Company list | P5 |
| **Sequoia / Accel / a16z / Lightspeed / Peak XV portfolios** | Public HTML pages | Company list, sector | P5 |
| **Crunchbase** | Paid API; free tier is very limited | Funding events | Deferred — cost |
| **TechCrunch / Entrackr / Inc42 funding feeds** | Public RSS | Indian funding news → new companies | P5 |
| **Product Hunt** | Public GraphQL API | Emerging companies | P5 |
| **GitHub trending organizations** | GitHub API | Companies with active engineering | P5 |

**These are discovery sources, not job sources.** They feed the company registry,
which then gets its own ATS sources. The pipeline:

```
funding announcement → company name + domain extracted
        → company identity resolved or created
        → career page located
        → ATS token discovered
        → source registered, tier 2
        → normal polling begins
```

This is the mechanism behind `US-DISC-02` ("new companies discovered
automatically"). Target: 50+ new companies per week with no manual entry.

**Indian ecosystem emphasis.** Entrackr, Inc42, and YourStory cover Indian
funding far better than TechCrunch, and Indian startups are Tier 1/2 by location
preference. These get higher priority than their global equivalents.

---

## 8. Community and social

| Source | Access | Signal quality | Volume | Milestone |
| --- | --- | --- | --- | --- |
| **HN "Who is Hiring"** | Firebase API, official and free | High — dense, direct from engineers | ~800 comments/month | P1 |
| **HN "Who wants to be hired"** | Same | Inverse signal — indicates hiring companies | Low | P5 |
| **GitHub hiring repos** | GitHub REST/GraphQL API | High — e.g. SimplifyJobs/Summer-Internships, curated lists | Moderate | P2 |
| **GitHub org announcements** | Repository watch, releases, discussions | Moderate | Low | P5 |
| **Reddit** | Official API (OAuth, free tier adequate) | Mixed — needs filtering | Moderate | P5 |
| **X / Twitter** | API v2 — **$200/month for meaningful access** | Moderate | Low | **Deferred, see below** |
| **Discord** | Bot in servers the user has joined | Variable | Low | P5 |
| **Telegram channels** | Bot API, public channels | Variable — some good Indian job channels | Moderate | P5 |
| **Slack communities** | Only where a bot is permitted | Low | Low | P5 |

**On X/Twitter.** The brief asks for it. The API v2 Basic tier is $200/month,
which is more than 8x Scout's entire infrastructure budget, for a source whose
signal is duplicative — recruiters who tweet a role have almost always also
posted it to their ATS, which we poll faster. The free tier does not permit
search. **Recommendation: skip X entirely at MVP.** Revisit only if the user is
willing to fund it, or if `nitter`-style public access becomes reliable again
(it is currently not). Listed in [22-open-questions](22-open-questions.md).

**Reddit subreddits worth monitoring:** `r/cscareerquestions`,
`r/cscareerquestionsIN`, `r/developersIndia`, `r/csMajors`, `r/forhire`,
`r/InternshipHunt`. Reddit's free API tier (100 queries/minute for OAuth apps) is
sufficient. Signal-to-noise is poor, so a strict classification filter is applied
before anything from Reddit reaches ranking.

**GitHub hiring repositories** are unusually good value. Repositories like
`SimplifyJobs/Summer2027-Internships` are community-curated, structured
(markdown tables or JSON), updated constantly, and diffable via the GitHub API's
commit endpoint — we can fetch only what changed. One adapter, high yield, and
the data is already semi-structured.

---

## 9. Research organizations

Often unpaid or stipend-based, and this is where the "prestige exception" in the
PRD applies.

| Source | Access | Notes |
| --- | --- | --- |
| Google Research / DeepMind | Careers pages, some on Greenhouse | Paid, competitive |
| Microsoft Research | MS careers portal | Paid |
| Meta AI (FAIR) | Meta careers | Paid |
| NVIDIA Research | Workday | Paid |
| Allen Institute (AI2) | Greenhouse | Paid |
| EleutherAI, HuggingFace | Careers pages, Discord | Varies |
| **IISc, IITs** | University portals | Often stipend-only — **prestige exception candidates** |
| **ISRO, DRDO, C-DAC** | Government portals, slow-moving | Stipend; prestigious in India |
| Max Planck, INRIA, ETH | University job boards | Paid, international |
| Google Summer of Code | Official API/site | Paid, annual cycle |
| MITACS Globalink | Program site | Paid, annual, India-eligible |
| Outreachy | Official site | Paid |

**Prestige exception rule.** A role with `paid = 'unpaid'` or `'unknown'` may
still notify if `prestige_exception = true`. That flag is set only when the
organization appears in a curated prestige list, and the notification is
explicitly labelled "Unpaid / stipend — flagged for prestige". The curated list
starts with roughly 40 entries (top research labs, major national institutes,
recognized programs like GSoC and Outreachy) and is a versioned file in the repo,
reviewable in a pull request rather than buried in a database.

---

## 10. Hackathons

The brief notes that companies recruit through hackathons. True, but the signal
is indirect.

| Source | Access | What we extract |
| --- | --- | --- |
| MLH event list | Public JSON/HTML | Sponsors → companies actively recruiting |
| Devfolio | Public API | Indian hackathons, sponsors |
| Devpost | Public API | Sponsors, prize partners |
| Unstop | Public listings | India-specific, often has direct hiring challenges |

**Treatment:** hackathon sponsors are a *company discovery* signal and a *hiring
intent* signal, not a job source. A company sponsoring a student hackathon is
demonstrably recruiting students. The pipeline adds them to the registry and
promotes their existing sources to a higher tier for 60 days.

**Exception:** Unstop and Devfolio sometimes host direct hiring challenges that
are genuine job postings. Those are ingested as jobs.

---

## 11. Source registry summary

Target composition at each milestone:

| Category | P1 | P3 | P5 | Year 1 |
| --- | --- | --- | --- | --- |
| ATS boards (Greenhouse/Lever/Ashby) | 250 | 600 | 1,200 | 1,800 |
| Other ATS | 0 | 150 | 400 | 500 |
| Career pages (feed/JSON-LD/sitemap) | 20 | 100 | 250 | 350 |
| Job boards (direct-permitted) | 3 | 8 | 12 | 15 |
| Email alert streams | 0 | 20 | 30 | 30 |
| Community (HN, GitHub, Reddit) | 5 | 15 | 40 | 60 |
| Ecosystem / discovery | 2 | 10 | 25 | 40 |
| Research | 0 | 15 | 40 | 60 |
| Hackathon | 0 | 4 | 10 | 15 |
| **Total** | **280** | **922** | **2,007** | **2,870** |

---

## 12. Cost per source class

Per-poll cost, used by the scheduler to decide what is worth polling often.

| Class | Bandwidth | CPU | LLM | Notes |
| --- | --- | --- | --- | --- |
| Conditional 304 | ~0.5 KB | ~0.1ms | none | The target state — 85% of polls |
| ATS JSON (changed) | 50–500 KB | ~5ms | none | Parse only |
| JSON-LD page | 100–800 KB | ~10ms | none | HTML fetch, JSON extract |
| Sitemap | 10–200 KB | ~2ms | none | Very cheap, `lastmod` filtering |
| Structured HTML | 100 KB–2 MB | ~30ms | none | CSS selection |
| **Rendered HTML** | 2–10 MB | **~2,000ms** | none | 200x cost — capped at 20 sources |
| Email alert | ~20 KB | ~5ms | occasional | Push, not poll — zero polling cost |
| New job processing | — | ~50ms | ~$0.00002 | Normalize + classify + embed + score |

At 400 sources with 85% returning 304, daily bandwidth is roughly 2–4 GB. Well
within any VPS allowance.

---

## 13. Adding a new source

The process, which should stay this cheap:

1. **Determine access method** — check for feed, then JSON-LD, then sitemap, then
   HTML, in that order.
2. **Check legality** — fetch robots.txt, review ToS, set `legal_posture`. If
   `prohibited`, stop and record why.
3. **Pick or write an adapter** — an existing adapter covers most cases;
   `adapter_config` handles per-source variation.
4. **Register** with a conservative rate limit and `status = 'pending_review'`.
5. **Shadow run for 48 hours** — collect but do not notify. Verify parse
   correctness against manual inspection.
6. **Promote** to `active` with a tier assigned by observed yield.

Steps 1–4 for a company already on a known ATS take about 30 seconds and are
fully automated by the discovery pipeline. Steps 5–6 are automated too, with an
alert if the shadow run produces anomalous results (zero jobs, or a suspiciously
high count).
