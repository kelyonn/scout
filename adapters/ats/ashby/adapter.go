// Package ashby implements packages/adapter.Adapter for Ashby job boards.
//
// docs/05-source-catalog.md names the GraphQL endpoint
// (jobs.ashbyhq.com/api/non-user-graphql, ApiJobBoardWithTeams) but this
// adapter deliberately uses api.ashbyhq.com/posting-api/job-board/{org}
// instead — a documented deviation, in the style of migration 000004's own
// "SPEC DEVIATION, deliberate" note. Live-verified: it is a plain GET
// returning richer data than the GraphQL route — descriptionPlain (no HTML
// stripping needed), a real publishedAt (so PostedAtEstimated = false,
// unlike Greenhouse's updated_at proxy), structured isRemote/workplaceType,
// and, with ?includeCompensation=true, actual structured compensation text.
// The GraphQL route would also require teaching packages/fetch to POST,
// which it cannot do today (GET-only, http.NoBody) — this route needs no
// such change.
package ashby

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// Adapter implements adapter.Adapter for Ashby.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns an Ashby adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_ashby.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindAshby }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full
// api.ashbyhq.com/posting-api/job-board/{org}?includeCompensation=true
// endpoint (CONTRIBUTING.md's "Adding a source"), not reconstructed from an
// org slug here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("ashby: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// boardResponse is api.ashbyhq.com/posting-api/job-board/{org}'s actual
// response shape — fields beyond what this adapter uses are omitted
// deliberately, matching adapters/ats/greenhouse's own convention.
type boardResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	ID               string             `json:"id"`
	Title            string             `json:"title"`
	Department       string             `json:"department"`
	EmploymentType   string             `json:"employmentType"`
	Location         string             `json:"location"`
	PublishedAt      string             `json:"publishedAt"`
	IsRemote         bool               `json:"isRemote"`
	JobURL           string             `json:"jobUrl"`
	ApplyURL         string             `json:"applyUrl"`
	DescriptionHTML  string             `json:"descriptionHtml"`
	DescriptionPlain string             `json:"descriptionPlain"`
	Compensation     *ashbyCompensation `json:"compensation"`
}

type ashbyCompensation struct {
	ScrapeableCompensationSalarySummary string `json:"scrapeableCompensationSalarySummary"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body boardResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("ashby: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.Jobs))
	for _, j := range body.Jobs {
		postings = append(postings, toPosting(j, raw))
	}
	return postings, nil
}

func toPosting(j ashbyJob, raw *adapter.RawResponse) schema.Posting {
	p := schema.Posting{
		ExternalID:        j.ID,
		URL:               j.JobURL,
		ApplyURL:          j.ApplyURL,
		TitleRaw:          j.Title,
		DescriptionHTML:   j.DescriptionHTML,
		DescriptionText:   j.DescriptionPlain,
		Department:        j.Department,
		EmploymentTypeRaw: j.EmploymentType,
		LocationRaw:       j.Location,
		RemoteHint:        j.IsRemote,
		Adapter:           string(adapter.SourceKindAshby),
		FetchedAt:         raw.FetchedAt,
	}

	if j.Compensation != nil {
		p.CompensationRawText = j.Compensation.ScrapeableCompensationSalarySummary
	}

	// publishedAt is Ashby's own posting timestamp, not an edit proxy —
	// unlike Greenhouse's updated_at, so PostedAtEstimated stays false.
	if t, err := time.Parse(time.RFC3339, j.PublishedAt); err == nil {
		p.PostedAt = &t
		p.PostedAtEstimated = false
	}

	return p
}

// Validate implements docs/06 section 7's per-adapter silent-failure check —
// same structural-plausibility approach as adapters/ats/greenhouse.Validate,
// since Ashby's list endpoint has the same "renamed field comes through as a
// zero value" failure mode translated to a different JSON shape.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("ashby: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("ashby: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("ashby: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
