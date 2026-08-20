// Package recruitee implements packages/adapter.Adapter for Recruitee job
// boards. Endpoint per docs/05-source-catalog.md:
// {token}.recruitee.com/api/offers/ — no auth, a single unpaginated
// response listing every offer with its full description, unlike
// Workable's list-only widget.
package recruitee

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// Adapter implements adapter.Adapter for Recruitee.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Recruitee adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_recruitee.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindRecruitee }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full {token}.recruitee.com/api/offers/
// endpoint (CONTRIBUTING.md's "Adding a source"), not reconstructed from a
// board token here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("recruitee: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// offersResponse is {token}.recruitee.com/api/offers/'s actual response
// shape — fields beyond what this adapter uses are omitted deliberately,
// matching every other adapter's own convention.
type offersResponse struct {
	Offers []recruiteeOffer `json:"offers"`
}

type recruiteeOffer struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Department      string `json:"department"`
	Remote          bool   `json:"remote"`
	EmploymentType  string `json:"employment_type_code"`
	CareersURL      string `json:"careers_url"`
	CareersApplyURL string `json:"careers_apply_url"`
	Description     string `json:"description"`
	Requirements    string `json:"requirements"`
	PublishedAt     string `json:"published_at"`
	City            string `json:"city"`
	Country         string `json:"country"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay. Offers with status other than "published" are
// skipped: Recruitee's public offers endpoint has been observed to
// include drafts and internal-only postings alongside published ones,
// which docs/06's own eligibility posture (only genuinely open, public
// postings) excludes.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body offersResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("recruitee: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.Offers))
	for _, o := range body.Offers {
		if o.Status != "published" {
			continue
		}
		postings = append(postings, toPosting(o, raw))
	}
	return postings, nil
}

func toPosting(o recruiteeOffer, raw *adapter.RawResponse) schema.Posting {
	applyURL := o.CareersApplyURL
	if applyURL == "" {
		applyURL = o.CareersURL
	}

	p := schema.Posting{
		ExternalID:        fmt.Sprintf("%d", o.ID),
		URL:               o.CareersURL,
		ApplyURL:          applyURL,
		TitleRaw:          o.Title,
		DescriptionHTML:   o.Description,
		RequirementsText:  o.Requirements,
		Department:        o.Department,
		EmploymentTypeRaw: o.EmploymentType,
		LocationRaw:       locationLabel(o.City, o.Country),
		RemoteHint:        o.Remote,
		Adapter:           string(adapter.SourceKindRecruitee),
		FetchedAt:         raw.FetchedAt,
	}

	// published_at is Recruitee's own posting timestamp, not an edit-time
	// proxy, so PostedAtEstimated stays false — same reasoning as Lever's
	// createdAt (adapters/ats/lever's own comment).
	if t, err := time.Parse(time.RFC3339, o.PublishedAt); err == nil {
		p.PostedAt = &t
	}

	return p
}

func locationLabel(city, country string) string {
	parts := make([]string, 0, 2)
	for _, s := range []string{city, country} {
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
			return fmt.Errorf("recruitee: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("recruitee: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("recruitee: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
