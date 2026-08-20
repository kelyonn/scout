// Package lever implements packages/adapter.Adapter for Lever job boards.
// Endpoint per docs/05-source-catalog.md: api.lever.co/v0/postings/{token}
// ?mode=json — no auth, a bare JSON array (not wrapped in an envelope object
// the way Greenhouse's {"jobs": [...]} is), no pagination. Live-verified
// against api.lever.co/v0/postings/meesho: createdAt is Lever's own posting
// timestamp, not an updated_at proxy, so unlike Greenhouse this adapter can
// set PostedAtEstimated = false.
package lever

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

// Adapter implements adapter.Adapter for Lever.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Lever adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_lever.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindLever }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full api.lever.co/v0/postings/{token}?mode=json
// endpoint (CONTRIBUTING.md's "Adding a source"), not reconstructed from a
// board token here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("lever: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// leverPosting is api.lever.co/v0/postings/{token}'s actual per-item shape —
// fields beyond what this adapter uses are omitted deliberately, matching
// adapters/ats/greenhouse's own convention.
type leverPosting struct {
	ID               string          `json:"id"`
	Text             string          `json:"text"`
	CreatedAt        int64           `json:"createdAt"` // epoch milliseconds
	HostedURL        string          `json:"hostedUrl"`
	ApplyURL         string          `json:"applyUrl"`
	Country          string          `json:"country"`
	WorkplaceType    string          `json:"workplaceType"`
	DescriptionPlain string          `json:"descriptionPlain"`
	Description      string          `json:"description"` // HTML
	Categories       leverCategories `json:"categories"`
}

type leverCategories struct {
	Commitment string `json:"commitment"`
	Department string `json:"department"`
	Team       string `json:"team"`
	Location   string `json:"location"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var items []leverPosting
	if err := json.Unmarshal(raw.Body, &items); err != nil {
		return nil, fmt.Errorf("lever: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(items))
	for _, item := range items {
		postings = append(postings, toPosting(item, raw))
	}
	return postings, nil
}

func toPosting(item leverPosting, raw *adapter.RawResponse) schema.Posting {
	department := item.Categories.Department
	if department == "" {
		department = item.Categories.Team
	}

	p := schema.Posting{
		ExternalID:        item.ID,
		URL:               item.HostedURL,
		ApplyURL:          item.ApplyURL,
		TitleRaw:          item.Text,
		DescriptionHTML:   item.Description,
		DescriptionText:   item.DescriptionPlain,
		Department:        department,
		EmploymentTypeRaw: item.Categories.Commitment,
		LocationRaw:       item.Categories.Location,
		RemoteHint:        strings.EqualFold(item.WorkplaceType, "remote"),
		Adapter:           string(adapter.SourceKindLever),
		FetchedAt:         raw.FetchedAt,
	}

	if item.CreatedAt > 0 {
		t := time.UnixMilli(item.CreatedAt).UTC()
		p.PostedAt = &t
		p.PostedAtEstimated = false // createdAt is Lever's own posting timestamp, not an edit proxy
	}

	return p
}

// Validate implements docs/06 section 7's per-adapter silent-failure check —
// same structural-plausibility approach as adapters/ats/greenhouse.Validate,
// since Lever's list endpoint has the same "renamed field comes through as a
// zero value" failure mode translated to a different JSON shape.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("lever: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("lever: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("lever: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
