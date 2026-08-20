// Package workday implements packages/adapter.Adapter for Workday's CXS
// job search API. Endpoint per docs/05-source-catalog.md:
// {tenant}.wd{N}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs — no
// auth, but POST rather than GET (a JSON search-request body, not query
// parameters), heavily paginated, and per docs/05 "the worst effort-to-
// coverage ratio of anything" this project ingests. Built anyway, per
// docs/19-roadmap.md's explicit instruction not to defer it: it is the
// one adapter that unlocks GCC and enterprise coverage.
//
// Verified live against multiple real tenants: infra/seed/gcc_sources.sql's
// own per-row comments record 21 individually-verified boards (Lowe's,
// NXP, Wells Fargo, BP, and others) from the GCC seeding pass, and this
// was reconfirmed directly (Lowe's, search_text="Bengaluru", 2026-08-19:
// a real HTTP POST through this exact Fetch/Parse/Validate path returned
// 64 current, real Bengaluru postings, not fixture data).
//
// Same real limitation as Workable and SmartRecruiters: the search
// response has no job description. A description needs a second GET per
// posting, to the externalPath this response gives back, which would turn
// one poll into one-plus-N requests against a tenant already expensive to
// paginate. DescriptionHTML stays empty for the same reason it does in
// those two packages.
package workday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// pageSize is the `limit` sent on every page request.
const pageSize = 20

// maxPages bounds the pagination loop — see
// adapters/ats/smartrecruiters' identical constant for the reasoning.
// Lower than SmartRecruiters' because Workday's own page size is smaller
// (20 vs 100): 100 pages here is still 2,000 postings from one tenant in
// one poll.
const maxPages = 100

// Adapter implements adapter.Adapter for Workday.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Workday adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_workday.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindWorkday }

// RequiresOwnFetch implements adapter.OwnFetcher: the CXS endpoint needs a
// POST with a JSON search body, which no conditional GET can produce, so
// the scheduler must call this Adapter's own Fetch directly rather than
// performing its usual generic GET. See that interface's own comment for
// why this is the one adapter in this project that needs it.
func (a *Adapter) RequiresOwnFetch() bool { return true }

// pageFetcher abstracts one HTTP page fetch — same seam and same reason
// as adapters/ats/smartrecruiters.pageFetcher: packages/fetch.Fetcher's
// SSRF guard cannot safely target a local httptest.Server from a
// different package's tests, so production wires this to a.fetcher.Fetch
// and tests wire it to a fake.
type pageFetcher func(ctx context.Context, offset int, hints adapter.FetchHints) (*fetch.Result, error)

// Fetch pages through the CXS jobs endpoint via POST, combining every
// page's jobPostings into one response — see the package comment and
// adapters/ats/smartrecruiters' identical strategy for why pagination is
// resolved here rather than left to Parse. Conditional headers (hints)
// are only sent on page 1: a 304 there means nothing changed and this
// returns immediately without requesting further pages.
//
// searchText, when src.AdapterConfig["search_text"] is a non-empty
// string, narrows every page's search request to Workday's own full-text
// match rather than paginating the tenant's entire global board —
// docs/05-source-catalog.md section 5.2's "location-facet filtering... the
// difference between 30 requests and 3,000." A per-tenant facet ID would
// be more precise, but Workday's location facet is a hierarchical,
// tenant-specific value discoverable only by driving that tenant's own
// search UI (see infra/seed/gcc_sources.sql's own comment for how the
// seeded tenants' facet IDs, where known, were actually obtained) — full-
// text search on a city name is the generic mechanism that needs no
// per-tenant curation, at the cost of missing a posting whose location
// field spells the city differently (e.g. "Bangalore" rather than
// "Bengaluru"). Empty AdapterConfig (the zero value) fetches the whole
// board, unfiltered — unchanged from this adapter's original behavior.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	searchText := searchTextFor(src)

	return fetchPages(ctx, hints, func(ctx context.Context, offset int, h adapter.FetchHints) (*fetch.Result, error) {
		body, err := json.Marshal(searchRequest{AppliedFacets: map[string]any{}, Limit: pageSize, Offset: offset, SearchText: searchText})
		if err != nil {
			return nil, fmt.Errorf("workday: build search request: %w", err)
		}
		return a.fetcher.Fetch(ctx, fetch.Request{
			URL: src.URL, Method: http.MethodPost, Body: body, ContentType: "application/json",
			IfNoneMatch: h.IfNoneMatch, IfModifiedSince: h.IfModifiedSince,
		})
	})
}

// searchTextFor reads src.AdapterConfig["search_text"], returning "" (the
// unfiltered, whole-board default) for a missing key, a non-string value,
// or a nil AdapterConfig map — split out from Fetch so this one-line
// decision is directly testable without a real network call.
func searchTextFor(src adapter.Source) string {
	searchText, _ := src.AdapterConfig["search_text"].(string)
	return searchText
}

func fetchPages(ctx context.Context, hints adapter.FetchHints, fetchPage pageFetcher) (*adapter.RawResponse, error) {
	first, err := fetchPage(ctx, 0, hints)
	if err != nil {
		return nil, fmt.Errorf("workday: fetch page 1: %w", err)
	}
	if first.StatusCode == 304 {
		return adapter.NewRawResponse(first), nil
	}

	var combined searchResponse
	if unmarshalErr := json.Unmarshal(first.Body, &combined); unmarshalErr != nil {
		// A malformed page 1 is handed straight to Parse, which reports
		// the same error — no point duplicating that error path here.
		return adapter.NewRawResponse(first), nil //nolint:nilerr // Parse re-parses the same body and surfaces the error there
	}

	for page := 1; page < maxPages && len(combined.JobPostings) < combined.Total; page++ {
		result, fetchErr := fetchPage(ctx, page*pageSize, adapter.FetchHints{})
		if fetchErr != nil {
			return nil, fmt.Errorf("workday: fetch page %d: %w", page+1, fetchErr)
		}
		var next searchResponse
		if unmarshalErr := json.Unmarshal(result.Body, &next); unmarshalErr != nil || len(next.JobPostings) == 0 {
			// A broken or empty page ends pagination with whatever was
			// already collected — partial coverage this round beats
			// none, and the next poll tries again from page 1.
			break
		}
		combined.JobPostings = append(combined.JobPostings, next.JobPostings...)
	}

	body, err := json.Marshal(combined)
	if err != nil {
		return nil, fmt.Errorf("workday: remarshal combined pages: %w", err)
	}
	return &adapter.RawResponse{
		Body: body, StatusCode: first.StatusCode, ETag: first.ETag,
		LastModified: first.LastModified, FetchedAt: first.FetchedAt,
	}, nil
}

// searchRequest is the CXS endpoint's POST body shape. appliedFacets
// empty means "no filters" — the whole board.
type searchRequest struct {
	AppliedFacets map[string]any `json:"appliedFacets"`
	Limit         int            `json:"limit"`
	Offset        int            `json:"offset"`
	SearchText    string         `json:"searchText"`
}

// searchResponse is the CXS endpoint's actual response shape — fields
// beyond what this adapter uses are omitted deliberately, matching every
// other adapter's own convention.
type searchResponse struct {
	Total       int                 `json:"total"`
	JobPostings []workdayJobPosting `json:"jobPostings"`
}

type workdayJobPosting struct {
	Title         string   `json:"title"`
	ExternalPath  string   `json:"externalPath"`
	LocationsText string   `json:"locationsText"`
	PostedOn      string   `json:"postedOn"` // relative text: "Posted 3 Days Ago"
	BulletFields  []string `json:"bulletFields"`
	TimeType      string   `json:"timeType"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay. siteBaseURL is derived from src.URL (the CXS
// API endpoint) rather than stored separately: Workday's public job page
// lives at {tenant}.wd{N}.myworkdayjobs.com/{site}{externalPath}, and
// {tenant}.wd{N}.myworkdayjobs.com/{site} is exactly the CXS URL with its
// trailing /wday/cxs/{tenant}/{site}/jobs replaced by /{site}.
func (a *Adapter) Parse(_ context.Context, src adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body searchResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("workday: parse: %w", err)
	}

	siteBaseURL, err := siteBaseURLFrom(src.URL)
	if err != nil {
		return nil, fmt.Errorf("workday: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.JobPostings))
	for _, j := range body.JobPostings {
		postings = append(postings, toPosting(j, siteBaseURL, raw))
	}
	return postings, nil
}

// cxsPathPattern matches .../wday/cxs/{tenant}/{site}/jobs, capturing
// {site} — the CXS endpoint's own path shape, docs/05's literal endpoint
// pattern.
var cxsPathPattern = regexp.MustCompile(`^/wday/cxs/[^/]+/([^/]+)/jobs$`)

func siteBaseURLFrom(cxsURL string) (string, error) {
	u, err := url.Parse(cxsURL)
	if err != nil {
		return "", fmt.Errorf("parse source url: %w", err)
	}
	m := cxsPathPattern.FindStringSubmatch(u.Path)
	if m == nil {
		return "", fmt.Errorf("source url %q does not look like a Workday CXS jobs endpoint", cxsURL)
	}
	return fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, m[1]), nil
}

func toPosting(j workdayJobPosting, siteBaseURL string, raw *adapter.RawResponse) schema.Posting {
	fullURL := siteBaseURL + j.ExternalPath

	externalID := requisitionID(j.BulletFields)
	if externalID == "" {
		externalID = j.ExternalPath
	}

	p := schema.Posting{
		ExternalID:        externalID,
		URL:               fullURL,
		ApplyURL:          fullURL,
		TitleRaw:          j.Title,
		LocationRaw:       j.LocationsText,
		EmploymentTypeRaw: j.TimeType,
		Adapter:           string(adapter.SourceKindWorkday),
		FetchedAt:         raw.FetchedAt,
	}

	if t, ok := parseRelativePostedOn(j.PostedOn, raw.FetchedAt); ok {
		p.PostedAt = &t
		p.PostedAtEstimated = true // always an approximation — postedOn is relative text, never a real timestamp
	}

	return p
}

// requisitionID picks the first bulletFields entry that looks like a
// requisition number ("R-12345") — Workday's own convention, observed
// across public tenants' postings. Not every posting has one, and this
// is best-effort: a miss just falls back to externalPath in toPosting.
var requisitionPattern = regexp.MustCompile(`^R-\d+$`)

func requisitionID(bulletFields []string) string {
	for _, f := range bulletFields {
		if requisitionPattern.MatchString(f) {
			return f
		}
	}
	return ""
}

// parseRelativePostedOn handles Workday's known postedOn phrasings —
// "Posted Today", "Posted Yesterday", "Posted N+ Days Ago" — against
// fetchedAt as the reference point. An unrecognized phrase returns
// ok=false rather than a guess: docs/07 section 9's "still unknown ->
// omit" rule applies to a relative string this parser doesn't recognize
// exactly as much as to a missing field.
func parseRelativePostedOn(text string, fetchedAt time.Time) (time.Time, bool) {
	text = strings.TrimSpace(text)
	switch {
	case strings.EqualFold(text, "Posted Today"):
		return fetchedAt, true
	case strings.EqualFold(text, "Posted Yesterday"):
		return fetchedAt.AddDate(0, 0, -1), true
	}

	if m := daysAgoPattern.FindStringSubmatch(text); m != nil {
		days, err := strconv.Atoi(m[1])
		if err == nil {
			return fetchedAt.AddDate(0, 0, -days), true
		}
	}
	return time.Time{}, false
}

// daysAgoPattern matches "Posted N Days Ago" and "Posted N+ Days Ago" —
// the trailing "+" (Workday's own "at least this old" marker for postings
// beyond some display cutoff) is ignored; the number itself is already an
// approximation, which is exactly why PostedAtEstimated is always true
// for this adapter.
var daysAgoPattern = regexp.MustCompile(`(?i)^Posted (\d+)\+? Days? Ago$`)

// Validate implements docs/06 section 7's per-adapter silent-failure
// check — same structural-plausibility posture as every other adapter's
// Validate.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("workday: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("workday: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("workday: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
