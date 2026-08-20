// Package smartrecruiters implements packages/adapter.Adapter for
// SmartRecruiters job boards. Endpoint per docs/05-source-catalog.md:
// api.smartrecruiters.com/v1/companies/{id}/postings — no auth, but
// paginated (unlike every other P1/P2 adapter, which return a whole board
// in one response), docs/05's own "documented, paginated" note.
//
// Fetch pages through the full result set itself and hands Parse one
// combined response — the adapter.Adapter interface returns exactly one
// RawResponse per Fetch call, so pagination has to be resolved before
// that boundary, not after it, for Parse to stay a pure single-shape
// function like every other adapter's.
//
// Same real limitation as Workable: the postings list has no full
// description field. A description needs a second request per posting,
// to api.smartrecruiters.com/v1/companies/{id}/postings/{id}, which would
// turn one poll into one-plus-N requests and fight the politeness gate.
// DescriptionHTML stays empty here for the same reason it does in
// adapters/ats/workable — see that package's comment for the full
// reasoning.
package smartrecruiters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// pageSize is the `limit` query parameter for every page this adapter
// requests, overriding whatever the source URL's own limit says — a
// consistent, known page size is what makes the offset-increment loop
// below correct regardless of how the source was originally seeded.
const pageSize = 100

// maxPages bounds the pagination loop — docs/05's "high effort" note
// about this API is about description fetches, not list pagination, but
// an unbounded loop against a misbehaving or unexpectedly huge board is
// still a real risk this caps rather than assumes away. 100 pages at 100
// postings each is 10,000 postings from one company in one poll, far
// beyond anything AGENTS.md rule 10's zero-budget scale needs to handle.
const maxPages = 100

// Adapter implements adapter.Adapter for SmartRecruiters.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a SmartRecruiters adapter. fetcher must be the collector's
// shared, SSRF-safe fetcher — adapters/README.md's hard rule: no direct
// http.Get, not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_smartrecruiters.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindSmartRecruiters }

// pageFetcher abstracts one HTTP page fetch. Fetch's production path
// (below) always wires this to a.fetcher.Fetch — the collector's shared,
// SSRF-safe fetcher, adapters/README.md's hard rule. The indirection
// exists so fetchPages' pagination/aggregation loop is unit-testable on
// its own: packages/fetch.Fetcher's SSRF guard rejects loopback addresses
// by design (see that package's own testing_test.go), so it cannot safely
// target a local httptest.Server from a different package's tests, and
// this is the seam that lets adapter_test.go exercise real pagination
// behavior with a fake instead.
type pageFetcher func(ctx context.Context, url string, hints adapter.FetchHints) (*fetch.Result, error)

// Fetch pages through api.smartrecruiters.com/v1/companies/{id}/postings,
// combining every page's `content` into one response. Conditional headers
// (hints) are only sent on the first page: if the board is unchanged
// since the last poll, SmartRecruiters returns 304 and this returns
// immediately without requesting any further pages. If it has changed,
// every subsequent page is fetched fresh (no conditional headers) since
// the full current content is needed regardless of which specific
// postings changed.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	return fetchPages(ctx, src, hints, func(ctx context.Context, url string, h adapter.FetchHints) (*fetch.Result, error) {
		return a.fetcher.Fetch(ctx, fetch.Request{URL: url, IfNoneMatch: h.IfNoneMatch, IfModifiedSince: h.IfModifiedSince})
	})
}

func fetchPages(ctx context.Context, src adapter.Source, hints adapter.FetchHints, fetchPage pageFetcher) (*adapter.RawResponse, error) {
	firstURL, err := setQueryParams(src.URL, 0)
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: build page 1 url: %w", err)
	}

	first, err := fetchPage(ctx, firstURL, hints)
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: fetch page 1: %w", err)
	}
	if first.StatusCode == 304 {
		return adapter.NewRawResponse(first), nil
	}

	var combined smartRecruitersResponse
	if unmarshalErr := json.Unmarshal(first.Body, &combined); unmarshalErr != nil {
		// A malformed page 1 is handed straight to Parse, which reports
		// the same error — no point duplicating that error path here.
		return adapter.NewRawResponse(first), nil //nolint:nilerr // Parse re-parses the same body and surfaces the error there
	}

	for page := 1; page < maxPages && len(combined.Content) < combined.TotalFound; page++ {
		pageURL, urlErr := setQueryParams(src.URL, page*pageSize)
		if urlErr != nil {
			return nil, fmt.Errorf("smartrecruiters: build page %d url: %w", page+1, urlErr)
		}
		result, fetchErr := fetchPage(ctx, pageURL, adapter.FetchHints{})
		if fetchErr != nil {
			return nil, fmt.Errorf("smartrecruiters: fetch page %d: %w", page+1, fetchErr)
		}
		var next smartRecruitersResponse
		if unmarshalErr := json.Unmarshal(result.Body, &next); unmarshalErr != nil || len(next.Content) == 0 {
			// A broken or empty page ends pagination with whatever was
			// already collected rather than failing the whole poll —
			// partial coverage this round beats none, and the next poll
			// tries again from page 1.
			break
		}
		combined.Content = append(combined.Content, next.Content...)
	}

	body, err := json.Marshal(combined)
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: remarshal combined pages: %w", err)
	}

	return &adapter.RawResponse{
		Body: body, StatusCode: first.StatusCode, ETag: first.ETag,
		LastModified: first.LastModified, FetchedAt: first.FetchedAt,
	}, nil
}

func setQueryParams(rawURL string, offset int) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	q.Set("limit", strconv.Itoa(pageSize))
	q.Set("offset", strconv.Itoa(offset))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// smartRecruitersResponse is
// api.smartrecruiters.com/v1/companies/{id}/postings' actual response
// shape — fields beyond what this adapter uses are omitted deliberately,
// matching every other adapter's own convention.
type smartRecruitersResponse struct {
	TotalFound int                      `json:"totalFound"`
	Content    []smartRecruitersPosting `json:"content"`
}

type smartRecruitersPosting struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	RefNumber        string                    `json:"refNumber"`
	ReleasedDate     string                    `json:"releasedDate"`
	Location         smartRecruitersLocation   `json:"location"`
	Department       smartRecruitersNamedLabel `json:"department"`
	TypeOfEmployment smartRecruitersNamedLabel `json:"typeOfEmployment"`
	PostingURL       string                    `json:"postingUrl"`
}

type smartRecruitersLocation struct {
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Remote  bool   `json:"remote"`
}

type smartRecruitersNamedLabel struct {
	Label string `json:"label"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay. Operates on Fetch's already-combined,
// single-page-shaped body regardless of how many real pages it took to
// build.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body smartRecruitersResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("smartrecruiters: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.Content))
	for _, c := range body.Content {
		postings = append(postings, toPosting(c, raw))
	}
	return postings, nil
}

func toPosting(c smartRecruitersPosting, raw *adapter.RawResponse) schema.Posting {
	p := schema.Posting{
		ExternalID:        c.ID,
		URL:               c.PostingURL,
		ApplyURL:          c.PostingURL,
		TitleRaw:          c.Name,
		Department:        c.Department.Label,
		EmploymentTypeRaw: c.TypeOfEmployment.Label,
		LocationRaw:       locationLabel(c.Location),
		RemoteHint:        c.Location.Remote,
		Adapter:           string(adapter.SourceKindSmartRecruiters),
		FetchedAt:         raw.FetchedAt,
	}

	// releasedDate is SmartRecruiters' own posting timestamp, not an
	// edit-time proxy, so PostedAtEstimated stays false.
	if t, err := time.Parse(time.RFC3339, c.ReleasedDate); err == nil {
		p.PostedAt = &t
	}

	return p
}

func locationLabel(loc smartRecruitersLocation) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{loc.City, loc.Region, loc.Country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// Validate implements docs/06 section 7's per-adapter silent-failure
// check — same structural-plausibility posture as every other adapter's
// Validate.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("smartrecruiters: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("smartrecruiters: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("smartrecruiters: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
