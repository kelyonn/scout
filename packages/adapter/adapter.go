// Package adapter defines the contract every source adapter implements —
// docs/06-ingestion-pipeline.md section 8, the interface given there
// verbatim. Fetch must call through packages/fetch.Fetcher, never a direct
// http.Get (adapters/README.md's hard rule — the same one AGENTS.md rule 1
// states for the politeness gate generally). Parse must be pure and
// deterministic, which is what makes fixture-based testing possible:
// record a real response once, replay it in CI forever.
package adapter

import (
	"context"
	"time"

	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// SourceKind mirrors the source_kind enum
// (infra/migrations/000002_enums.up.sql) — a typed string so this package
// has no dependency on packages/db/gen, matching packages/schema's own
// reasoning for RoleFamily/Seniority/etc.
type SourceKind string

// SourceKindGreenhouse is the source_kind enum value for Greenhouse boards.
const SourceKindGreenhouse SourceKind = "ats_greenhouse"

// SourceKindLever is the source_kind enum value for Lever boards.
const SourceKindLever SourceKind = "ats_lever"

// SourceKindAshby is the source_kind enum value for Ashby boards.
const SourceKindAshby SourceKind = "ats_ashby"

// SourceKindWorkable is the source_kind enum value for Workable boards.
const SourceKindWorkable SourceKind = "ats_workable"

// SourceKindSmartRecruiters is the source_kind enum value for
// SmartRecruiters boards.
const SourceKindSmartRecruiters SourceKind = "ats_smartrecruiters"

// SourceKindRecruitee is the source_kind enum value for Recruitee boards.
const SourceKindRecruitee SourceKind = "ats_recruitee"

// SourceKindTeamtailor is the source_kind enum value for Teamtailor boards.
const SourceKindTeamtailor SourceKind = "ats_teamtailor"

// SourceKindWorkday is the source_kind enum value for Workday boards.
const SourceKindWorkday SourceKind = "ats_workday"

// FetchHints carries the previous poll's conditional-request validators,
// mirroring fetch.Request's own fields — an adapter's Fetch translates
// these into the fetch.Request it actually sends.
type FetchHints struct {
	IfNoneMatch     string
	IfModifiedSince string
}

// RawResponse is Fetch's output: raw bytes plus the validators the caller
// persists for the next poll's conditional request. Deliberately the same
// shape as fetch.Result rather than introducing a second type for what
// remains fetch.Result's own information — see NewRawResponse.
type RawResponse struct {
	Body         []byte
	StatusCode   int
	ETag         string
	LastModified string
	// RetryAfter is the raw Retry-After header value, empty if absent —
	// carried through so a 429 reaching the scheduler via OwnFetcher's own
	// Fetch (rather than the scheduler's generic conditional GET) still
	// drives handleRateLimited's backoff, exactly as fetch.Result's own
	// field does for every other source.
	RetryAfter string
	FetchedAt  time.Time
}

// NewRawResponse adapts a fetch.Result into a RawResponse. Every adapter's
// Fetch ends with this call, which is what keeps "call through fetch.Fetcher"
// a checkable rule rather than a convention each adapter re-implements.
func NewRawResponse(r *fetch.Result) *RawResponse {
	return &RawResponse{
		Body:         r.Body,
		StatusCode:   r.StatusCode,
		ETag:         r.ETag,
		LastModified: r.LastModified,
		RetryAfter:   r.RetryAfter,
		FetchedAt:    r.FetchedAt,
	}
}

// Source is the subset of the source table an adapter needs to fetch and
// parse — narrower than db.Source so this package doesn't depend on
// packages/db/gen either.
type Source struct {
	ID            string
	URL           string
	AdapterConfig map[string]any // board token and other per-source overrides
}

// Adapter is docs/06 section 8's interface, verbatim.
type Adapter interface {
	Kind() SourceKind
	// Fetch performs the network request(s) and returns raw bytes plus validators.
	Fetch(ctx context.Context, src Source, hints FetchHints) (*RawResponse, error)
	// Parse converts raw bytes into postings. Must be pure and deterministic.
	Parse(ctx context.Context, src Source, raw *RawResponse) ([]schema.Posting, error)
	// Validate reports whether a parse result is plausible for this source.
	Validate(ctx context.Context, src Source, postings []schema.Posting) error
}

// OwnFetcher is an optional interface an Adapter implements when a single
// conditional GET to Source.URL — the scheduler's own default fetch,
// generic across every other adapter in this project — cannot produce a
// usable response for it. Workday's CXS endpoint (POST, a JSON search
// body, paginated; see adapters/ats/workday) is the first and, as of
// docs/05-source-catalog.md section 5.2, only example.
//
// Deliberately narrow and opt-in rather than a method every Adapter must
// implement: every other adapter here is correctly represented by "one
// conditional GET, handed to Parse," which is exactly what lets their own
// integration tests fake the scheduler's fetch interface instead of
// needing a real fetch.Fetcher — a concrete type whose SSRF guard refuses
// loopback addresses, so it cannot be pointed at an httptest.Server from
// another package (see packages/fetch/testing_test.go's own comment on
// why that escape hatch is deliberately test-file-scoped to package fetch
// itself). Routing every registered adapter through its own Fetch would
// break that boundary for all of them; this interface keeps the blast
// radius to the adapters that actually need it.
type OwnFetcher interface {
	// RequiresOwnFetch reports whether the scheduler must call this
	// Adapter's own Fetch directly rather than performing its own generic
	// conditional GET. Always true for anything implementing this
	// interface at all — it exists as a method (not just the interface's
	// presence) so a future adapter can implement OwnFetcher while still
	// opting out per-instance if it ever has reason to.
	RequiresOwnFetch() bool
}
