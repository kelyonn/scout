# adapters

One module per source type. See
[`docs/05-source-catalog.md`](../docs/05-source-catalog.md) and the "Adding an
adapter" section of [`CONTRIBUTING.md`](../CONTRIBUTING.md).

**Implemented:** Greenhouse, Lever, Ashby (P1), Workable, SmartRecruiters,
Recruitee, Teamtailor, Workday (P3), Keka, Zoho Recruit (P5). All ten are
registered in `apps/collector/cmd/main.go`'s adapter map, and all are
verified against at least one real, live board — see each package's own doc
comment for specifics.

**Career pages (`sitemap`, `html_page`) are the first non-ATS adapter**
(`adapters/web/careerpage`, 2026-10-20): schema.org `JobPosting` JSON-LD plus
sitemap `<lastmod>` ordering, for employers not on a supported ATS. Unlike the
ten ATS adapters it makes many requests per poll, so it is an `OwnFetcher` and
clears every request after the first through `adapter.Source.Gate` — the
politeness gate, evaluated per URL (AGENTS.md rule 1). Registered in
`apps/collector/cmd/main.go` for both kinds. Verified live against 11
employers; `infra/seed/careerpage_sources.sql` lists them with what was
measured, including that global sitemaps give little India yield.

**Zoho Recruit is HTML, not JSON, despite docs/05's original guess.**
Verified 2026-08-27 against two real tenants (increff.zohorecruit.com,
educohire.zohorecruit.com): there is no separate JSON API — the career-site
listing page itself embeds the job array as HTML-entity-encoded JSON inside
a single `<input id="jobs" value="...">`. Per-tenant module configuration
varies which fields that array carries (one tenant's board omitted
`Job_Description` entirely; the other included it), so every field this
adapter reads is optional. `/jobs/Careers` is the one path verified to work
on both tenants — the shorter `/careers` alias that worked for one tenant
timed out entirely for the other, so this adapter always seeds and fetches
`/jobs/Careers`. See the package's own doc comment for the full shape.

**Keka covers one of (at least) two real careers-portal templates.** Verified
2026-08-24 against three real tenants (fynd.keka.com, pennanttech.keka.com,
achiralabs.keka.com): two of the three serve postings from a plain
`/careers/api/jobs/default/active` endpoint this adapter targets; the third
(Fynd's own custom-built careers page) uses a different "embed widget"
variant that needs an organization GUID scraped out of the page's HTML
before an equivalent `/careers/api/embedjobs/...` endpoint resolves — not
supported, a known and bounded gap rather than a silent one. See the
package's own doc comment for the full reasoning.

**Teamtailor was verified 2026-08-19 and the verification found a real bug**,
not just confirmed the adapter worked: the response is a JSON Feed 1.1
document (`{"version", "items": [...]}`), not the bare kebab-case array the
adapter was originally built against. That version would have failed to
parse every real Teamtailor source, 100% of the time — `json.Unmarshal`
into a slice from a top-level JSON object is a hard type error. Rewritten
against two independently-verified real boards
(career.teamtailor.com, southpole.teamtailor.com); see the package's own
doc comment for the full shape.

Layout, one directory per adapter:

```
adapters/<category>/<name>/
├── adapter.go        Fetch, Parse, Validate
├── adapter_test.go
├── fixtures/         recorded responses
└── fixtures/expected/
```

Two rules that are not negotiable:

- **Every outbound request goes through `PolitenessGate.Allow()`.** No direct
  `http.Get`, not even in a test.
- **`Parse` must be pure and deterministic.** That is what makes fixture replay
  possible, and fixture replay is what catches an upstream format change before
  it reaches production.
