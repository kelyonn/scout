-- Direct company career sites — docs/05-source-catalog.md section 3, read by
-- adapters/web/careerpage (source_kind 'sitemap' or 'html_page', see the
-- Deloitte block below for the latter). For employers whose own career site
-- publishes schema.org JobPosting JSON-LD per job page, discovered either
-- via a sitemap ('sitemap' kind) or by following links off one listing page
-- ('html_page' kind, for a site whose own sitemap doesn't work).
--
-- Every row below was run live on 2026-10-20 with
--   apps/collector/cmd/sourceprobe
-- which drives the real adapter through the real politeness gate (robots.txt,
-- rate limit, User-Agent identification) and parses the result: the sitemap
-- resolved, job pages answered, JobPosting JSON-LD extracted, and Validate
-- passed. Nothing here is guessed.
--
-- What "verified" does and does not mean for India coverage. These are GLOBAL
-- employers; none of their sitemaps can be narrowed to India by URL (the
-- locale segment is a language, not a job location — Cognizant's /india-en/
-- paths list Australian jobs). Each poll reads the newest max_pages job pages
-- by <lastmod>, so a daily poll sees every day's new postings as long as a
-- site posts fewer than max_pages jobs a day, and the normalizer's location
-- tiering ranks the India ones. In the verification sample India-located
-- share was: Ericsson 2/12, HPE 1/12, Nutanix 1/8, the rest 0 in their
-- newest-first sample. Treat these as breadth sources, not as India-targeted
-- ones; the India-heavy long tail is better served by ATS boards.
--
-- Probed and NOT seeded (reason in parentheses):
--   Accenture India jobpostings-sitemap (27,878 URLs, no <lastmod> — no way
--   to find the new ones within a request budget), IBM / Lenovo / Siemens /
--   Walmart / Mphasis / Nykaa / Trellix / Intel / Synopsys (job pages carry no
--   JobPosting JSON-LD, or the site is client-rendered), Wipro / TCS /
--   Infosys / Microsoft / Cisco (no usable robots.txt or sitemap, or the
--   robots response is a redirect).
--
-- ADR-020's Phase 2 (2026-10-21, two passes): probed for management/strategy
-- consulting coverage, since zero such firms existed in any seed file before
-- this work started.
--   Seeded: BCG, ZS Associates (both 'sitemap' kind, below), Deloitte
--   ('html_page' kind, second insert below — apply.deloitte.com's own
--   sitemap.xml resolves (200 OK, gzip header) but the body is genuinely
--   empty on every fetch, not a probing artifact; the SearchJobs listing
--   page itself has no JSON-LD but does list real JobDetail links in its raw
--   HTML, which is exactly what html_page mode's link-following exists for).
--   Blocked by active bot-protection, not pursued per rule 1's "refuse over
--   fetch" — McKinsey (Akamai challenge on every request), Bain (Cloudflare
--   JS challenge).
--   Needs a dedicated adapter, not this one — no JobPosting JSON-LD despite
--   real job content being fetchable: EY (careers.ey.com, Oracle-style ATS,
--   8,906 job URLs — the single largest candidate found, worth a dedicated
--   adapter on its own), Kearney (kearney.recsolu.com, Yello Enterprise,
--   fully client-rendered SPA, no JSON-LD and no API call found), PwC
--   (pwc.com/gx/en/careers/job-results.html — Workday-backed per
--   gx-territories.json, but the Workday CXS API itself was never found;
--   the rendered page server-side-embeds job data as plain text, not
--   JSON-LD — and the India graduate-careers filter returned only one
--   result in the one sample checked, so yield may be low even once an
--   adapter exists), KPMG (kpmgindia.talentrecruit.com, TalentRecruit
--   platform, client-rendered SPA, no JSON-LD; KPMG also runs two Oracle
--   Fusion Cloud HCM sites — oraclecloud.com/hcmUI/CandidateExperience —
--   not investigated).
--   Rejected, not a fit — Accenture Strategy (same Accenture India sitemap
--   already rejected above, no <lastmod>); Tata Strategic Management Group
--   has no independent careers portal at all — tsmg.com redirects to
--   tata.com/tsmg, and its only "careers" link is tata.com's own
--   group-wide jobs page (no JSON-LD, and not consulting-specific — every
--   Tata Group company's openings, not TSMG's).
--   Not reached, not investigated at all: Oliver Wyman (oliverwyman.com —
--   the careers/roles pages found are marketing content with no job-search
--   UI located in the time spent; Oliver Wyman is a Marsh McLennan
--   subsidiary and may share Marsh McLennan's own careers portal, not
--   checked).
--
-- All rows are pending_review: a human promotes a source to active. Polling
-- is gentle by construction — base interval 24h, 0.5 rps, 150 job pages per
-- poll, and every request (not only the sitemap) passes PolitenessGate.Allow.
--
-- Idempotent — ON CONFLICT DO NOTHING on every insert, safe to re-run. Run via
-- `make seed-careerpage-sources`.

insert into company (slug, canonical_name, normalized_name, company_type, company_type_source, discovered_via)
values
    ('adobe', 'Adobe', 'adobe', 'product', 'heuristic', 'seed'),
    ('amd', 'AMD', 'amd', 'product', 'heuristic', 'seed'),
    ('hpe', 'Hewlett Packard Enterprise', 'hewlett packard enterprise', 'product', 'heuristic', 'seed'),
    ('micron-gcc', 'Micron Technology', 'micron technology', 'gcc', 'heuristic', 'seed'),
    ('qualcomm', 'Qualcomm', 'qualcomm', 'product', 'heuristic', 'seed'),
    ('ericsson', 'Ericsson', 'ericsson', 'core_telecom', 'heuristic', 'seed'),
    ('mastercard-gcc', 'Mastercard', 'mastercard', 'gcc', 'heuristic', 'seed'),
    ('sap', 'SAP', 'sap', 'product', 'heuristic', 'seed'),
    ('servicenow', 'ServiceNow', 'servicenow', 'product', 'heuristic', 'seed'),
    ('nutanix', 'Nutanix', 'nutanix', 'product', 'heuristic', 'seed'),
    ('cognizant', 'Cognizant', 'cognizant', 'services_it', 'heuristic', 'seed'),
    ('bcg', 'Boston Consulting Group', 'boston consulting group', 'services_consulting', 'heuristic', 'seed'),
    ('zs-associates', 'ZS Associates', 'zs associates', 'services_consulting', 'heuristic', 'seed'),
    ('deloitte-us', 'Deloitte', 'deloitte', 'services_consulting', 'heuristic', 'seed')
on conflict do nothing;

insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'sitemap', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 86400, 0.5,
       v.adapter_config::jsonb, v.note
from company as c
join (values
    ('adobe', 'https://careers.adobe.com/us/en/sitemap.xml',
        '{"url_include": "/job/", "max_pages": 150}',
        'Phenom-hosted career site; sitemap has <lastmod>, job pages carry JobPosting JSON-LD. Verified live 2026-10-20 (7 postings from 12 newest URLs; the rest were non-job pages). 0/7 India in sample.'),
    ('amd', 'https://careers.amd.com/sitemap.xml',
        '{"max_pages": 150}',
        'Sitemap has <lastmod>; JobPosting JSON-LD on every job page. Verified live 2026-10-20 (7 of 8 sampled). 0 India in sample.'),
    ('hpe', 'https://careers.hpe.com/us/en/sitemap_index.xml',
        '{"url_include": "/job/", "max_pages": 150}',
        'Phenom sitemap index (4 children), <lastmod> present, JSON-LD on job pages, includes interns. Verified live 2026-10-20 (12 postings from 12 pages). 1/12 India (Bengaluru) in sample.'),
    ('micron-gcc', 'https://careers.micron.com/careers/sitemap_index.xml?domain=micron.com',
        '{"max_pages": 150}',
        'Eightfold-hosted career site (same platform as Qualcomm and Ericsson). Verified live 2026-10-20 (10/10 pages gave JSON-LD). 0 India in sample. The Workday source for Micron in gcc_sources.sql covers Bengaluru directly; this is the secondary path.'),
    ('qualcomm', 'https://careers.qualcomm.com/careers/sitemap_index.xml?domain=qualcomm.com',
        '{"max_pages": 150}',
        'Eightfold-hosted. Verified live 2026-10-20 (12/12 pages gave JSON-LD, includes FY27 intern listings). 0 India in sample.'),
    ('ericsson', 'https://jobs.ericsson.com/careers/sitemap_index.xml?domain=ericsson.com',
        '{"max_pages": 150}',
        'Eightfold-hosted. Verified live 2026-10-20 (12/12). 2/12 India (Bengaluru, Noida) in sample; lists trainee and early-career programmes.'),
    ('mastercard-gcc', 'https://careers.mastercard.com/us/en/sitemap_index.xml',
        '{"url_include": "/job/", "max_pages": 150}',
        'Phenom-hosted; <lastmod> present, JSON-LD on job pages. Verified live 2026-10-20 (12/12). 0 India in sample. Mastercard also has a Workday source in gcc_sources.sql with Bengaluru narrowing.'),
    ('sap', 'https://jobs.sap.com/en/jobs/sitemap.xml',
        '{"max_pages": 200, "company_name": "SAP"}',
        'SuccessFactors career site; ~230 job URLs in the English jobs sitemap, no <lastmod> (so no newest-first ordering — the cap covers the whole list). JSON-LD includes baseSalary for intern roles. Verified live 2026-10-20 (10/12). 0 India in sample.'),
    ('servicenow', 'https://careers.servicenow.com/sitemap.xml',
        '{"url_include": "/jobs/", "max_pages": 150}',
        'Sitemap with <lastmod>, JSON-LD on job pages. Verified live 2026-10-20 (8/8). 0 India in sample.'),
    ('nutanix', 'https://careers.nutanix.com/sitemap.xml',
        '{"url_include": "/job", "max_pages": 150}',
        'Sitemap with <lastmod>, JSON-LD on job pages. Verified live 2026-10-20 (8/8). 1/8 India (Bangalore) in sample; Nutanix has a large Bengaluru engineering office.'),
    ('cognizant', 'https://careers.cognizant.com/sitemap.xml',
        '{"url_include": "/india-en/jobs/[0-9]", "max_pages": 150}',
        'Sitemap with <lastmod> (16,884 URLs, 2,088 under /india-en/jobs/). The locale prefix is the site language, NOT the job location — the sample returned Australian, US and Argentine roles. Verified live 2026-10-20 (12/12). 0 India in sample; kept because Cognizant India graduate hiring is large, but expect the location tiering to do the filtering.'),
    ('bcg', 'https://careers.bcg.com/global/en/sitemap_index.xml',
        '{"url_include": "/job/", "max_pages": 150}',
        'ADR-020 Phase 2 — the first management-consulting source seeded. Phenom-hosted (same platform as adobe/hpe/mastercard above); sitemap index (2 children, 1059+ URLs incl. job/event/location pages), JobPosting JSON-LD on job pages, no <lastmod> at the index level so url_include is load-bearing to skip the event/location noise. Verified live 2026-10-21 via sourceprobe through the real gate: 3/12 sampled URLs were real postings (the rest were event/location pages the url_include filter let through at this shallow a sample — expect a higher job-page ratio once polling walks deeper into the per-child sitemaps). 1/3 India (Gurgaon) in sample.'),
    ('zs-associates', 'https://jobs.zs.com/sitemap.xml',
        '{"max_pages": 150}',
        'ADR-020 Phase 2. iCIMS-hosted; sitemap index (1 child, 291 URLs), JobPosting JSON-LD on job pages, robots.txt sets crawl-delay: 5 (the politeness gate observes it). Verified live 2026-10-21 (12/12 pages gave JSON-LD). 3/12 India (Pune) in sample — ZS has a real Pune delivery office, unlike most of this file''s global-HQ-skewed entries.')
) as v(slug, url, adapter_config, note) on v.slug = c.slug
on conflict do nothing;

-- Deloitte is 'html_page' kind, not 'sitemap' — its own sitemap.xml resolves
-- (200 OK) but the body is genuinely empty on every fetch tried; the
-- SearchJobs listing page has no JSON-LD itself but does list real
-- /JobDetail/ links in its raw server-rendered HTML, which html_page mode's
-- link-following (adapter_config.url_include, see adapters/web/careerpage's
-- own doc comment) exists for.
insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, adapter_config, notes)
select c.id, 'html_page', v.url, digest(v.url, 'sha256'), 'permitted', 'pending_review', 86400, 0.5,
       v.adapter_config::jsonb, v.note
from company as c
join (values
    ('deloitte-us', 'https://apply.deloitte.com/en_US/careers/SearchJobs?sort=relevancy',
        '{"url_include": "/JobDetail/", "max_pages": 150}',
        'ADR-020 Phase 2. Avature-backed. Verified live 2026-10-21 via sourceprobe (html_page kind): the one listing page itself yielded 20 postings via its JobDetail links, JobPosting JSON-LD confirmed on each. US-only URL (en_US) — 0 India in sample, since this is the US careers site specifically, not a global one; kept as a breadth source like adobe/amd/etc. above. A Deloitte India-specific portal was not found (www2.deloitte.com/in/en/careers/careers.html 404s) — worth another look before assuming none exists.')
) as v(slug, url, adapter_config, note) on v.slug = c.slug
on conflict do nothing;
