// Package workable implements packages/adapter.Adapter for Workable job
// boards. Endpoint per docs/05-source-catalog.md:
// apply.workable.com/api/v1/widget/accounts/{token} — no auth, a single
// unpaginated response listing every open job, the same shape family as
// Greenhouse and Lever.
//
// One real limitation, not an oversight: the widget endpoint returns only
// list-level fields — no job description at all. A full description needs
// a second request per job, to
// apply.workable.com/api/v1/widget/accounts/{token}/jobs/{shortcode},
// which would multiply one poll into one-plus-N requests against a single
// source and fight the politeness gate's whole reason for existing.
// DescriptionHTML stays empty for every Workable-sourced posting; Tier 0
// classification and scoring both already treat a missing description as
// a real, honestly-represented "unknown" rather than a bug (docs/07
// section 5).
package workable

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

// Adapter implements adapter.Adapter for Workable.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Workable adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_workable.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindWorkable }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full
// apply.workable.com/api/v1/widget/accounts/{token} endpoint
// (CONTRIBUTING.md's "Adding a source"), not reconstructed from a board
// token here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("workable: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// widgetResponse is apply.workable.com/api/v1/widget/accounts/{token}'s
// actual response shape — fields beyond what this adapter uses are
// omitted deliberately, matching every other adapter's own convention.
type widgetResponse struct {
	Jobs []workableJob `json:"jobs"`
}

type workableJob struct {
	Title          string `json:"title"`
	Shortcode      string `json:"shortcode"`
	EmploymentType string `json:"employment_type"`
	Telecommute    bool   `json:"telecommute"`
	Department     string `json:"department"`
	URL            string `json:"url"`
	ApplicationURL string `json:"application_url"`
	PublishedOn    string `json:"published_on"` // date only: "2026-08-01"
	Country        string `json:"country"`
	City           string `json:"city"`
	State          string `json:"state"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body widgetResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("workable: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.Jobs))
	for _, j := range body.Jobs {
		postings = append(postings, toPosting(j, raw))
	}
	return postings, nil
}

func toPosting(j workableJob, raw *adapter.RawResponse) schema.Posting {
	applyURL := j.ApplicationURL
	if applyURL == "" {
		applyURL = j.URL
	}

	p := schema.Posting{
		ExternalID:        j.Shortcode,
		URL:               j.URL,
		ApplyURL:          applyURL,
		TitleRaw:          j.Title,
		Department:        j.Department,
		EmploymentTypeRaw: j.EmploymentType,
		LocationRaw:       locationLabel(j.City, j.State, j.Country),
		RemoteHint:        j.Telecommute,
		Adapter:           string(adapter.SourceKindWorkable),
		FetchedAt:         raw.FetchedAt,
	}

	// published_on is Workable's own posting date (date-only, no time
	// component) — a real posting timestamp, not an edit-time proxy like
	// Greenhouse's updated_at, so PostedAtEstimated stays false.
	if t, err := time.Parse("2006-01-02", j.PublishedOn); err == nil {
		p.PostedAt = &t
	}

	return p
}

func locationLabel(city, state, country string) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{city, state, country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// Validate implements docs/06 section 7's per-adapter silent-failure
// check — same structural-plausibility posture as every other adapter's
// Validate (see adapters/ats/greenhouse's own comment for the full
// reasoning): an empty title or apply URL means a field got renamed
// upstream, and a duplicate external id in one response means the same
// job came back twice.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("workable: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("workable: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("workable: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
