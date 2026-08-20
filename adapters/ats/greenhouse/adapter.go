// Package greenhouse implements packages/adapter.Adapter for
// Greenhouse job boards. Endpoint per docs/05-source-catalog.md:
// boards-api.greenhouse.io/v1/boards/{token}/jobs?content=true — no auth,
// no pagination (the whole board comes back in one response), which is
// also why this adapter has no "paginated" fixture: the shape doesn't
// exist for this ATS. It has no "compensation-variants" fixture for the
// same reason — Greenhouse's public API has no compensation field;
// whatever a posting says about pay lives in free text inside `content`,
// which this adapter unescapes once (see toPosting) and otherwise passes
// through for apps/collector/internal/normalize to parse downstream.
package greenhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// Adapter implements adapter.Adapter for Greenhouse.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Greenhouse adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_greenhouse.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindGreenhouse }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, which for a Greenhouse source is seeded as the full
// boards-api.greenhouse.io endpoint (CONTRIBUTING.md's "Adding a source"),
// not reconstructed from a board token here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("greenhouse: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// boardResponse is boards-api.greenhouse.io's actual response shape —
// fields beyond what this adapter uses are omitted deliberately, not
// missed; adding one back is a one-line change if a normalization need
// arises for it.
type boardResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

type greenhouseJob struct {
	ID            int64            `json:"id"`
	Title         string           `json:"title"`
	UpdatedAt     string           `json:"updated_at"`
	AbsoluteURL   string           `json:"absolute_url"`
	Content       string           `json:"content"`
	Location      greenhouseLoc    `json:"location"`
	Departments   []greenhouseDept `json:"departments"`
	RequisitionID string           `json:"requisition_id"`
	// CompanyName is Greenhouse's own per-job field, present when a board's
	// token doesn't match its display name (a numeric or otherwise
	// internal-looking token, or a multi-brand board where postings span
	// more than one legal entity — "Stitch Fix Annex" on a board whose
	// token is a bare numeric id is a real example this field exists to
	// catch). schema.Posting.CompanyNameRaw was defined but never
	// populated by any adapter until now.
	CompanyName string `json:"company_name"`
}

type greenhouseLoc struct {
	Name string `json:"name"`
}

type greenhouseDept struct {
	Name string `json:"name"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var body boardResponse
	if err := json.Unmarshal(raw.Body, &body); err != nil {
		return nil, fmt.Errorf("greenhouse: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(body.Jobs))
	for _, j := range body.Jobs {
		postings = append(postings, toPosting(j, raw))
	}
	return postings, nil
}

func toPosting(j greenhouseJob, raw *adapter.RawResponse) schema.Posting {
	department := ""
	if len(j.Departments) > 0 {
		department = j.Departments[0].Name
	}

	p := schema.Posting{
		ExternalID:     fmt.Sprintf("%d", j.ID),
		URL:            j.AbsoluteURL,
		ApplyURL:       j.AbsoluteURL,
		CompanyNameRaw: j.CompanyName,
		TitleRaw:       j.Title,
		// Greenhouse's own board API returns `content` HTML-entity-escaped
		// a level too far — the string is literally "&lt;p&gt;...&lt;/p&gt;",
		// not a real "<p>...</p>" tag, observed live against real postings
		// (Twilio, checked directly against the API while building the job
		// detail page's rendering — apps/collector/internal/normalize's
		// StripHTML already documents the identical quirk from Stripe
		// during P1). A single UnescapeString here is enough: entities that
		// are genuinely part of the content (&amp;, &nbsp;) survive it
		// unescaped-tags become real tags, real entities stay real
		// entities, and the result is valid HTML ready to store and later
		// render — unlike StripHTML's own double-unescape, which exists
		// only to make its regex tag-stripper see the tags in the first
		// place before discarding them, not to keep them.
		DescriptionHTML: html.UnescapeString(j.Content),
		Department:      department,
		LocationRaw:     j.Location.Name,
		Adapter:         string(adapter.SourceKindGreenhouse),
		FetchedAt:       raw.FetchedAt,
	}

	// Greenhouse's public list endpoint has no field distinguishing "first
	// posted" from "last edited" — updated_at is the only structured
	// timestamp it exposes. Used as posted_at per docs/07 section 9's "JSON
	// field present" tier, but flagged estimated since it is not
	// guaranteed to be the original posting date the way an ATS-native
	// created_at would be.
	if t, err := time.Parse(time.RFC3339, j.UpdatedAt); err == nil {
		p.PostedAt = &t
		p.PostedAtEstimated = true
	}

	return p
}

// Validate implements docs/06 section 7's per-adapter silent-failure check.
// It cannot do the rolling-30-day-median comparison the doc's "item count
// anomaly" mechanism describes — that needs source.total_jobs_found
// history, which this interface doesn't pass in and which lives at the
// scheduler/source layer, not here. A genuinely empty board is not itself
// invalid (a company can have zero open roles), so this checks structural
// plausibility instead: the kind of silent failure a JSON field rename
// produces — parsing succeeds, but the renamed field comes through as a
// zero value on every posting, which is exactly the "selectors match
// nothing" failure mode docs/06 section 7's table describes, translated to
// a JSON API. A duplicate external ID across postings in one response
// would mean the same job page came back twice, which is not a shape a
// working board API produces.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("greenhouse: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("greenhouse: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("greenhouse: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
