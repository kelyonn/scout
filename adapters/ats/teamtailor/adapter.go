// Package teamtailor implements packages/adapter.Adapter for Teamtailor
// job boards. Endpoint per docs/05-source-catalog.md:
// {token}.teamtailor.com/jobs.json — no auth.
//
// Verified live against two real boards (career.teamtailor.com,
// southpole.teamtailor.com, 2026-08-19): the response is a JSON Feed 1.1
// document (https://jsonfeed.org/version/1.1), NOT a bare array —
// {"version", "title", "home_page_url", "feed_url", "items": [...]}. Each
// item is snake_case at the top level (id, title, url, date_published,
// content_html), plus a nested schema.org JobPosting blob under
// "_jobposting" whose only field this adapter needs is jobLocation (an
// array — Teamtailor postings commonly list several cities at once, e.g.
// "Milan, Florence, London, Berlin" as four separate jobLocation entries
// for one posting).
//
// This adapter originally assumed a bare array with kebab-case fields
// (created-at, apply-url, remote-status, department) and an "internal"
// flag to filter preview-only postings — none of that exists in the real
// feed; a public jobs.json only ever contains public postings, so there
// is nothing to filter, and there is no department field anywhere in
// either verified board's response. That version would have failed to
// parse every real Teamtailor source (json.Unmarshal into a slice from a
// top-level object is a hard type error), 100% of the time, silently
// until the first live poll.
package teamtailor

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

// Adapter implements adapter.Adapter for Teamtailor.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Teamtailor adapter. fetcher must be the collector's
// shared, SSRF-safe fetcher — adapters/README.md's hard rule: no direct
// http.Get, not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_teamtailor.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindTeamtailor }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full {token}.teamtailor.com/jobs.json
// endpoint (CONTRIBUTING.md's "Adding a source"), not reconstructed from a
// board token here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("teamtailor: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// teamtailorFeed is {token}.teamtailor.com/jobs.json's top-level shape —
// a JSON Feed 1.1 document, not a bare array.
type teamtailorFeed struct {
	Items []teamtailorItem `json:"items"`
}

type teamtailorItem struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	URL           string               `json:"url"`
	DatePublished string               `json:"date_published"`
	ContentHTML   string               `json:"content_html"`
	JobPosting    teamtailorJobPosting `json:"_jobposting"`
}

// teamtailorJobPosting is the schema.org JobPosting blob Teamtailor embeds
// per item under "_jobposting" — only JobLocation is used; every other
// field observed on two real boards (datePosted, description,
// hiringOrganization, identifier, title) duplicates a top-level field
// this adapter already reads more directly.
type teamtailorJobPosting struct {
	JobLocation []teamtailorJobLocation `json:"jobLocation"`
}

type teamtailorJobLocation struct {
	Address teamtailorAddress `json:"address"`
}

type teamtailorAddress struct {
	AddressLocality string `json:"addressLocality"`
	AddressCountry  string `json:"addressCountry"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay.
func (a *Adapter) Parse(_ context.Context, _ adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var feed teamtailorFeed
	if err := json.Unmarshal(raw.Body, &feed); err != nil {
		return nil, fmt.Errorf("teamtailor: parse: %w", err)
	}

	postings := make([]schema.Posting, 0, len(feed.Items))
	for _, item := range feed.Items {
		postings = append(postings, toPosting(item, raw))
	}
	return postings, nil
}

func toPosting(item teamtailorItem, raw *adapter.RawResponse) schema.Posting {
	locations := make([]string, 0, len(item.JobPosting.JobLocation))
	for _, loc := range item.JobPosting.JobLocation {
		if label := locationLabel(loc.Address.AddressLocality, loc.Address.AddressCountry); label != "" {
			locations = append(locations, label)
		}
	}
	// Multiple jobLocation entries are one posting open in several cities
	// at once (verified live — e.g. one "Senior Lead, AI Engineering"
	// posting listing Milan/Florence/London/Berlin as four separate
	// entries), not four postings. schema.Posting.LocationRaw's own doc
	// comment says the normalizer resolves/tiers a job that lists several,
	// so a single "; "-joined string is the adapter's job here, same as
	// every other multi-location signal in this codebase.
	locationRaw := strings.Join(locations, "; ")

	p := schema.Posting{
		ExternalID:      item.ID,
		URL:             item.URL,
		ApplyURL:        item.URL, // the feed exposes no separate apply-url; the job URL is where a candidate applies
		TitleRaw:        item.Title,
		DescriptionHTML: item.ContentHTML,
		LocationRaw:     locationRaw,
		RemoteHint:      strings.Contains(strings.ToLower(locationRaw), "remote"),
		Adapter:         string(adapter.SourceKindTeamtailor),
		FetchedAt:       raw.FetchedAt,
	}

	// date_published is Teamtailor's own posting timestamp, not an
	// edit-time proxy, so PostedAtEstimated stays false.
	if t, err := time.Parse(time.RFC3339, item.DatePublished); err == nil {
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
			return fmt.Errorf("teamtailor: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("teamtailor: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("teamtailor: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
