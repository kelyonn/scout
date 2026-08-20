-- GCC and enterprise coverage — docs/05-source-catalog.md section 5.2's
-- "curated seed list... ~150 entries... not glamorous, and by far the
-- highest yield per hour spent," now unblocked by the Workday adapter
-- (P3). 39 entities here, not 150 — see this file's own closing comment
-- for why that gap is honest rather than padded with guesses.
--
-- Every URL below was checked live on 2026-08-18 or 2026-08-19 (real,
-- non-empty postings, matching this project's existing verification bar
-- from sources.sql) via two methods:
--   1. Public search: querying each employer's name alongside
--      "myworkdayjobs.com" or "smartrecruiters.com" to find its actual
--      careers portal.
--   2. A single POST/GET to the resulting API endpoint, confirming a
--      real, current, non-empty jobPostings/content array — the same
--      "confirmable with one HEAD request" docs/05 section 5.2's
--      discovery-strategy list describes, extended to a full request
--      since Workday's CXS endpoint 400s on a bare HEAD/GET (see
--      adapters/ats/workday's own package comment, and
--      apps/collector/internal/scheduler's OwnFetcher fix this task also
--      required — a plain GET was never enough to poll a Workday source
--      at all until that fix landed alongside this seed).
--
-- Workday sources set adapter_config = {"search_text": "Bengaluru"} —
-- adapters/ats/workday.Fetch's searchTextFor reads this and narrows every
-- page of a poll to postings matching that text, rather than paginating
-- the tenant's entire global board. Verified end-to-end during discovery
-- (e.g. Lowe's: 11,961 total postings board-wide, 56 with "Bengaluru" —
-- docs/05's literal "3 requests instead of 3,000"). This is full-text
-- search, not a tenant-specific location-facet ID: precise per-tenant
-- facet IDs exist too (confirmed live for Lowe's and Micron by driving
-- each tenant's own search UI and reading the resulting `locations=`
-- query parameter) but require that same manual per-tenant discovery to
-- obtain, which doesn't scale to 19 tenants in one pass. search_text
-- generalizes without per-tenant curation, at the honestly-documented
-- cost of missing a posting whose location field spells the city
-- differently (e.g. "Bangalore" rather than "Bengaluru").
--
-- SmartRecruiters sources (Nagarro, Bosch) have no location-narrowing
-- mechanism — that adapter doesn't support it, and neither board is large
-- enough (924 and 4,819 postings respectively) to make the "3 vs 3,000"
-- problem this file's Workday entries solve actually bite.
--
-- Idempotent — ON CONFLICT DO NOTHING on every insert, safe to re-run,
-- matching sources.sql's own convention. Run via `make seed-sources`.

insert into company (slug, canonical_name, normalized_name, company_type, company_type_source, discovered_via)
values
    ('visa-gcc', 'Visa', 'visa', 'gcc', 'heuristic', 'seed'),
    ('mastercard-gcc', 'Mastercard', 'mastercard', 'gcc', 'heuristic', 'seed'),
    ('micron-gcc', 'Micron Technology', 'micron technology', 'gcc', 'heuristic', 'seed'),
    ('target-gcc', 'Target', 'target', 'gcc', 'heuristic', 'seed'),
    ('gehealthcare-gcc', 'GE HealthCare', 'ge healthcare', 'gcc', 'heuristic', 'seed'),
    ('philips-gcc', 'Philips', 'philips', 'gcc', 'heuristic', 'seed'),
    ('analogdevices-gcc', 'Analog Devices', 'analog devices', 'gcc', 'heuristic', 'seed'),
    ('standardchartered-gcc', 'Standard Chartered', 'standard chartered', 'gcc', 'heuristic', 'seed'),
    ('siemenshealthineers-gcc', 'Siemens Healthineers', 'siemens healthineers', 'gcc', 'heuristic', 'seed'),
    ('morganstanley-gcc', 'Morgan Stanley', 'morgan stanley', 'gcc', 'heuristic', 'seed'),
    ('caterpillar-gcc', 'Caterpillar', 'caterpillar', 'gcc', 'heuristic', 'seed'),
    ('collinsaerospace-gcc', 'Collins Aerospace', 'collins aerospace', 'gcc', 'heuristic', 'seed'),
    ('shell-gcc', 'Shell', 'shell', 'gcc', 'heuristic', 'seed'),
    ('nike-gcc', 'Nike', 'nike', 'gcc', 'heuristic', 'seed'),
    ('unilever-gcc', 'Unilever', 'unilever', 'gcc', 'heuristic', 'seed'),
    ('nxp-gcc', 'NXP Semiconductors', 'nxp semiconductors', 'gcc', 'heuristic', 'seed'),
    ('lowes-gcc', 'Lowe''s', 'lowes', 'gcc', 'heuristic', 'seed'),
    ('wellsfargo-gcc', 'Wells Fargo', 'wells fargo', 'gcc', 'heuristic', 'seed'),
    ('bp-gcc', 'bp', 'bp', 'gcc', 'heuristic', 'seed'),
    ('nagarro', 'Nagarro', 'nagarro', 'services_engineering', 'heuristic', 'seed'),
    ('bosch-gcc', 'Bosch', 'bosch', 'gcc', 'heuristic', 'seed'),
    ('cisco-gcc', 'Cisco', 'cisco', 'gcc', 'heuristic', 'seed'),
    ('jnj-gcc', 'Johnson & Johnson', 'johnson and johnson', 'gcc', 'heuristic', 'seed'),
    ('walmart-gcc', 'Walmart Global Tech', 'walmart global tech', 'gcc', 'heuristic', 'seed'),
    ('hp-gcc', 'HP Inc.', 'hp inc', 'gcc', 'heuristic', 'seed'),
    ('deutschebank-gcc', 'Deutsche Bank', 'deutsche bank', 'gcc', 'heuristic', 'seed'),
    ('barclays-gcc', 'Barclays', 'barclays', 'gcc', 'heuristic', 'seed'),
    ('3m-gcc', '3M', '3m', 'gcc', 'heuristic', 'seed'),
    ('medtronic-gcc', 'Medtronic', 'medtronic', 'gcc', 'heuristic', 'seed'),
    ('jci-gcc', 'Johnson Controls', 'johnson controls', 'gcc', 'heuristic', 'seed'),
    ('amat-gcc', 'Applied Materials', 'applied materials', 'gcc', 'heuristic', 'seed'),
    ('illumina-gcc', 'Illumina', 'illumina', 'gcc', 'heuristic', 'seed'),
    ('danaher-gcc', 'Danaher', 'danaher', 'gcc', 'heuristic', 'seed'),
    ('northerntrust-gcc', 'Northern Trust', 'northern trust', 'gcc', 'heuristic', 'seed'),
    ('baxter-gcc', 'Baxter', 'baxter', 'gcc', 'heuristic', 'seed'),
    ('statestreet-gcc', 'State Street', 'state street', 'gcc', 'heuristic', 'seed'),
    ('stryker-gcc', 'Stryker', 'stryker', 'gcc', 'heuristic', 'seed'),
    ('abbott-gcc', 'Abbott', 'abbott', 'gcc', 'heuristic', 'seed'),
    ('aig-gcc', 'AIG', 'aig', 'gcc', 'heuristic', 'seed')
on conflict (slug) do nothing;

-- Workday CXS sources. url is the {tenant}.{wdN}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs
-- endpoint adapters/ats/workday.siteBaseURLFrom expects; adapter_config
-- carries the location narrowing this file's own header comment explains.
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'ats_workday', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 900, 0.5,
       '{"search_text": "Bengaluru"}'::jsonb, v.note
from company as c
join (values
    ('visa-gcc',                'https://visa.wd5.myworkdayjobs.com/wday/cxs/visa/Visa/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (741 total postings) 2026-08-18.'),
    ('mastercard-gcc',          'https://mastercard.wd1.myworkdayjobs.com/wday/cxs/mastercard/CorporateCareers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (1,127 total postings) 2026-08-18.'),
    ('micron-gcc',              'https://micron.wd1.myworkdayjobs.com/wday/cxs/micron/External/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (2,733 total, 46 Bengaluru) 2026-08-18.'),
    ('target-gcc',              'https://target.wd5.myworkdayjobs.com/wday/cxs/target/targetcareers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (2,000+ total, 37 Bengaluru) 2026-08-18.'),
    ('gehealthcare-gcc',        'https://gehc.wd5.myworkdayjobs.com/wday/cxs/gehc/GEHC_ExternalSite/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (974 total postings) 2026-08-18.'),
    ('philips-gcc',             'https://philips.wd3.myworkdayjobs.com/wday/cxs/philips/jobs-and-careers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (892 total postings) 2026-08-18.'),
    ('analogdevices-gcc',       'https://analogdevices.wd1.myworkdayjobs.com/wday/cxs/analogdevices/External/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (1,048 total postings) 2026-08-18.'),
    ('standardchartered-gcc',   'https://peopleplus.wd3.myworkdayjobs.com/wday/cxs/peopleplus/SCB_Careers/jobs',
        'Public Workday CXS endpoint, no auth. Tenant is a shared HR-services host ("peopleplus"), not a standard.wd* slug — found via search, not guessed. Verified live (56 total postings) 2026-08-18.'),
    ('siemenshealthineers-gcc', 'https://onehealthineers.wd3.myworkdayjobs.com/wday/cxs/onehealthineers/SHSJB/jobs',
        'Public Workday CXS endpoint, no auth. Siemens Healthineers is a separately-listed entity from Siemens AG/Siemens Gamesa, which either 400''d or returned zero postings under every guessed tenant. Verified live (481 total postings) 2026-08-18.'),
    ('morganstanley-gcc',       'https://ms.wd5.myworkdayjobs.com/wday/cxs/ms/External/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (1,386 total, includes real Bengaluru listings) 2026-08-18.'),
    ('caterpillar-gcc',         'https://cat.wd5.myworkdayjobs.com/wday/cxs/cat/CaterpillarCareers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (911 total postings) 2026-08-18.'),
    ('collinsaerospace-gcc',    'https://globalhr.wd5.myworkdayjobs.com/wday/cxs/globalhr/REC_RTX_Ext_Gateway/jobs',
        'Public Workday CXS endpoint, no auth. Collins Aerospace posts through parent RTX''s shared "globalhr" tenant, not a collinsaerospace.wd* slug. Verified live (4,389 total postings) 2026-08-18.'),
    ('shell-gcc',               'https://shell.wd3.myworkdayjobs.com/wday/cxs/shell/ShellCareers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (124 total postings) 2026-08-18.'),
    ('nike-gcc',                'https://nike.wd1.myworkdayjobs.com/wday/cxs/nike/nke/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (784 total postings) 2026-08-18.'),
    ('unilever-gcc',            'https://unilever.wd3.myworkdayjobs.com/wday/cxs/unilever/Unilever_Experienced_Professionals/jobs',
        'Public Workday CXS endpoint, no auth. A separate Unilever_Early_Careers site exists for the graduate/intern track and is deliberately not seeded a second time here — same employer, would double the poll volume for overlapping coverage dedup already handles from one board. Verified live (287 total postings) 2026-08-18.'),
    ('nxp-gcc',                 'https://nxp.wd3.myworkdayjobs.com/wday/cxs/nxp/careers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (785 total postings) 2026-08-18.'),
    ('lowes-gcc',               'https://lowes.wd5.myworkdayjobs.com/wday/cxs/lowes/LWS_External_CS/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (11,961 total, 56 Bengaluru) 2026-08-18 — the largest board in this batch, and the one search_text''s filtering matters most for.'),
    ('wellsfargo-gcc',          'https://wf.wd1.myworkdayjobs.com/wday/cxs/wf/WellsFargoJobs/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (1,681 total, real Bengaluru listings observed directly in the verification response) 2026-08-18.'),
    ('bp-gcc',                  'https://bpinternational.wd3.myworkdayjobs.com/wday/cxs/bpinternational/bpCareers/jobs',
        'Public Workday CXS endpoint, no auth. Verified live (361 total postings) 2026-08-18.')
) as v(slug, url, note) on v.slug = c.slug
on conflict (url_hash) do nothing;

-- Second Workday batch, verified live 2026-08-19. search_text differs
-- per tenant based on which spelling ("Bengaluru" vs "Bangalore") that
-- tenant's own location field actually uses — checked individually for
-- each, not assumed. Cisco, Deutsche Bank, 3M, and Medtronic all use
-- "Bangalore"; Johnson & Johnson, Walmart, HP, and Barclays match on
-- "Bengaluru" directly.
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'ats_workday', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 900, 0.5,
       v.adapter_config::jsonb, v.note
from company as c
join (values
    ('cisco-gcc',      'https://cisco.wd5.myworkdayjobs.com/wday/cxs/cisco/Cisco_Careers/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore", not "Bengaluru" — zero results under the latter despite 1,207 total postings board-wide. Verified live (242 Bangalore) 2026-08-19.'),
    ('jnj-gcc',         'https://jj.wd5.myworkdayjobs.com/wday/cxs/jj/JJ/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (1,788 total, 10 Bengaluru) 2026-08-19.'),
    ('walmart-gcc',     'https://walmart.wd504.myworkdayjobs.com/wday/cxs/walmart/WalmartExternal/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. This file previously noted a real walmart.wd5.myworkdayjobs.com/WalmartExternal tenant that 303-redirected to a maintenance page on every attempt and guessed a transient outage — it was not transient, it was the wrong subdomain. The actual tenant is on wd504, not wd5; Workday tenant numbers are not guessable from the company name and this one only surfaced via a fresh search rather than retrying the same wrong URL. Verified live (2,000+ total, 8 Bengaluru) 2026-08-19.'),
    ('hp-gcc',          'https://hp.wd5.myworkdayjobs.com/wday/cxs/hp/ExternalCareerSite/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (745 total, 64 Bengaluru) 2026-08-19.'),
    ('deutschebank-gcc', 'https://db.wd3.myworkdayjobs.com/wday/cxs/db/DBWebsite/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — zero results under "Bengaluru" despite 1,082 total postings board-wide. Verified live (64 Bangalore, many explicitly tagged "Bangalore Velankani ISC") 2026-08-19.'),
    ('barclays-gcc',    'https://barclays.wd3.myworkdayjobs.com/wday/cxs/barclays/External_Career_Site_Barclays/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (1,045 total, 5 Bengaluru) 2026-08-19.'),
    ('3m-gcc',          'https://3m.wd1.myworkdayjobs.com/wday/cxs/3m/Search/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — verified live (633 total, 7 Bangalore) 2026-08-19.'),
    ('medtronic-gcc',   'https://medtronic.wd1.myworkdayjobs.com/wday/cxs/medtronic/MedtronicCareers/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Most of Medtronic''s real India presence is in Hyderabad (81 postings) rather than Bangalore (4) — kept on the "Bangalore" search_text anyway for consistency with this file''s own Bengaluru-focused convention (docs/01-prd.md) rather than special-casing one tenant''s city. Verified live (1,090 total, 4 Bangalore) 2026-08-19.')
) as v(slug, url, adapter_config, note) on v.slug = c.slug
on conflict (url_hash) do nothing;

-- Third Workday batch, verified live 2026-08-19, same two-step
-- discovery/verification method as every prior batch. Applied Materials
-- and State Street are thin (2 current Bengaluru postings each) but real
-- and current, not stale — kept rather than dropped, matching this
-- file's own precedent (bp-gcc, shell-gcc) of seeding a real small board
-- alongside the large ones rather than only chasing volume.
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'ats_workday', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 900, 0.5,
       v.adapter_config::jsonb, v.note
from company as c
join (values
    ('jci-gcc',          'https://jci.wd5.myworkdayjobs.com/wday/cxs/jci/JCI/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (2,615 total, 5 Bengaluru) 2026-08-19.'),
    ('amat-gcc',         'https://amat.wd1.myworkdayjobs.com/wday/cxs/amat/External/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (1,857 total, 2 Bengaluru — thin but real and current) 2026-08-19.'),
    ('illumina-gcc',     'https://illumina.wd1.myworkdayjobs.com/wday/cxs/illumina/illumina-careers/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (152 total, 23 Bengaluru — Manyata tech park specifically) 2026-08-19.'),
    ('danaher-gcc',      'https://danaher.wd1.myworkdayjobs.com/wday/cxs/danaher/DanaherJobs/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — verified live (1,374 total, 72 Bangalore via the adapter''s own pagination; a single-page spot-check undercounts this one) 2026-08-19.'),
    ('northerntrust-gcc', 'https://ntrs.wd1.myworkdayjobs.com/wday/cxs/ntrs/northerntrust/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — zero results under "Bengaluru". Verified live (647 total, 65 Bangalore) 2026-08-19.'),
    ('baxter-gcc',       'https://baxter.wd1.myworkdayjobs.com/wday/cxs/baxter/baxter/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — zero results under "Bengaluru". Verified live (550 total, 32 Bangalore) 2026-08-19.'),
    ('statestreet-gcc',  'https://statestreet.wd1.myworkdayjobs.com/wday/cxs/statestreet/Global/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (1,320 total, 2 Bengaluru — thin but real and current; much of State Street''s real India volume is in Hyderabad, same pattern as Medtronic above) 2026-08-19.')
) as v(slug, url, adapter_config, note) on v.slug = c.slug
on conflict (url_hash) do nothing;

-- Fourth Workday batch, verified live 2026-08-19 — the same search
-- getting thinner per new company found (2-20 current postings each,
-- down from 50-250+ in the first two batches) is expected: the largest,
-- most obvious GCC employers were found first. Continuing anyway per
-- this file's own standard — real and current beats guessed at any size.
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'ats_workday', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 900, 0.5,
       v.adapter_config::jsonb, v.note
from company as c
join (values
    ('stryker-gcc', 'https://stryker.wd1.myworkdayjobs.com/wday/cxs/stryker/StrykerCareers/jobs',
        '{"search_text": "Bengaluru"}',
        'Public Workday CXS endpoint, no auth. Verified live (1,166 total, 20 Bengaluru) 2026-08-19.'),
    ('abbott-gcc',  'https://abbott.wd5.myworkdayjobs.com/wday/cxs/abbott/abbottcareers/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — zero results under "Bengaluru" despite 2,000+ total postings board-wide. Verified live (4 Bangalore) 2026-08-19.'),
    ('aig-gcc',     'https://aig.wd1.myworkdayjobs.com/wday/cxs/aig/aig/jobs',
        '{"search_text": "Bangalore"}',
        'Public Workday CXS endpoint, no auth. Location field uses "Bangalore" — verified live (461 total, 2 Bangalore — thin but real and current) 2026-08-19.')
) as v(slug, url, adapter_config, note) on v.slug = c.slug
on conflict (url_hash) do nothing;

-- SmartRecruiters sources — same public, unauthenticated posting-search
-- API every other SmartRecruiters-hosted source in this project uses
-- (see adapters/ats/smartrecruiters' own package comment).
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, notes)
select c.id, 'ats_smartrecruiters', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 900, 0.5, v.note
from company as c
join (values
    ('nagarro',    'https://api.smartrecruiters.com/v1/companies/Nagarro1/postings',
        'Public SmartRecruiters posting API, no auth. Docs/05 originally listed Nagarro as Greenhouse/Lever/Workday and it resolved on none of the three during P2 Phase L''s sweep (see sources.sql) — found on SmartRecruiters instead this pass. Verified live (924 total postings) 2026-08-18.'),
    ('bosch-gcc',  'https://api.smartrecruiters.com/v1/companies/BoschGroup/postings',
        'Public SmartRecruiters posting API, no auth. Verified live (4,819 total postings) 2026-08-18.')
) as v(slug, url, note) on v.slug = c.slug
on conflict (url_hash) do nothing;

-- What's not here, and why this is 39 entries rather than the ~150
-- docs/05 section 5.2 targets:
--
-- Real companies attempted and found NOT to resolve on any adapter this
-- project supports (Workday, SmartRecruiters, or the three P1 platforms),
-- so left out rather than guessed at — matching sources.sql's own
-- existing precedent for EPAM/Globant/Publicis Sapient: American Express
-- (own portal — travelhrportal.wd1.myworkdayjobs.com surfaces in search
-- results but is an unrelated third-party HR tenant, not Amex's own),
-- Goldman Sachs, JPMorgan Chase (uses Oracle Cloud, not Workday), Boeing
-- (real boeing.wd1.myworkdayjobs.com/Eng and /MFG tenants exist but
-- return 0-4 total postings each — likely the wrong site name for
-- Boeing's real external board, not yet found), Dell (dell.wd1's
-- External and ExternalNonPublic sites both return a well-formed but
-- always-empty response — the real tenant/site combination wasn't
-- found), PepsiCo and ExxonMobil (neither actually runs on Workday
-- despite surfacing in a myworkdayjobs.com-scoped search — PepsiCo uses
-- mypepsico.com, ExxonMobil's own board is elsewhere), HSBC, Cummins,
-- Citi (a real citi.wd5.myworkdayjobs.com tenant exists but every
-- plausible site name tried — Citi, ExperiencedProfessionals,
-- CitiCareers, External — 404s; the real site name wasn't found), Bank
-- of America (ghr.wd1.myworkdayjobs.com/Lateral-US resolves but is a
-- US-only lateral-hire board, zero India results — the real global/India
-- site wasn't found), UBS (the only myworkdayjobs.com hit in search
-- results is an unrelated company sharing no relation to UBS), Corning,
-- Eaton, Zurich Insurance (none clearly on Workday from search alone),
-- ServiceNow (a myworkdayjobs.com hit surfaced but was Workday Inc.'s
-- own careers site posting a job titled "ServiceNow Developer" — a role
-- about the ServiceNow product, not ServiceNow the company's own tenant),
-- Wells Fargo's own retail arm variations, Continental AG, Schneider
-- Electric, Qualcomm (migrated off Workday to its own portal), AMD,
-- Honeywell (only third-party staffing-partner postings found, not
-- Honeywell's own tenant), Tesco Bengaluru (SmartRecruiters identifier
-- resolves but returns zero current postings), Novo Nordisk, John Deere,
-- Rockwell Automation (the one tenant/site found —
-- External-Rockwell-Automation-Early-Careers — returns zero total
-- postings; an early-careers-only site with nothing posted right now,
-- not the company's real general board), Charles Schwab (not on
-- Workday; the only myworkdayjobs.com hit is the unrelated Les Schwab
-- Tire Centers), and several SuccessFactors-only or bespoke-portal GCCs from docs/05's
-- own table (TCS, Infosys, Wipro, Cognizant, Accenture, Capgemini,
-- Deloitte, EY, KPMG, PwC) that section 6's own coverage table already
-- marks as P5, not P3 — no adapter exists for SuccessFactors or a
-- bespoke portal, so seeding a source row for one would just be a
-- permanently-broken row.
--
-- Walmart Global Tech was in this "not resolved" list as of 2026-08-18,
-- on the theory that the real tenant it found (wd5) was hitting a
-- transient Workday outage. It wasn't transient — it was the wrong
-- subdomain number. The actual tenant is wd504, found via a fresh
-- search rather than retrying the same wrong URL, and is now seeded
-- above. Worth remembering next time a real-looking tenant 303s: try a
-- different wdN before concluding it's down.
--
-- Reaching the full ~150 from here is exactly what docs/05 section 5.2
-- calls it: "a data change, not a code change" — add a (slug, url, note)
-- row to whichever VALUES list above matches the platform, once that
-- company's real tenant/board is found and verified the same way these
-- were. No adapter work is needed unless a genuinely new ATS platform
-- (SuccessFactors, most likely, given how much of docs/05's own table it
-- accounts for) enters scope.
