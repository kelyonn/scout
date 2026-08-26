# adapters

One module per source type. See
[`docs/05-source-catalog.md`](../docs/05-source-catalog.md) and the "Adding an
adapter" section of [`CONTRIBUTING.md`](../CONTRIBUTING.md).

**Implemented:** Greenhouse, Lever, Ashby (P1), Workable, SmartRecruiters,
Recruitee, Teamtailor, Workday (P3), Keka (P5). All nine are registered in
`apps/collector/cmd/main.go`'s adapter map, and all are verified against at
least one real, live board — see each package's own doc comment for
specifics.

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
