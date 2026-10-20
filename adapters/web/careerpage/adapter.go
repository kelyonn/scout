// Package careerpage implements packages/adapter.Adapter for company career
// sites that are not on a supported ATS: docs/05-source-catalog.md section 3,
// methods 2 (schema.org JSON-LD JobPosting) and 3 (sitemap, using <lastmod>).
// It serves two source kinds from one implementation:
//
//   - sitemap: Source.URL is a sitemap.xml (or sitemap index). Every job URL
//     in it is fetched and its JobPosting JSON-LD extracted.
//   - html_page: Source.URL is a page that embeds JobPosting JSON-LD itself
//     (one job, or a listing with many). If it embeds none and
//     adapter_config.url_include is set, the page's links matching that
//     pattern are followed like sitemap entries.
//
// # Requests and the politeness gate (AGENTS.md rule 1)
//
// Both kinds implement adapter.OwnFetcher because one poll can mean many
// requests. The scheduler passes PolitenessGate.Allow for Source.URL before
// calling Fetch; every request Fetch makes after that goes through
// Source.Gate.Allow first, which is the same gate with the same robots.txt,
// rate, crawl-delay and legal-posture checks. A nil Gate fails closed: Fetch
// makes the root request the scheduler already cleared and nothing else.
// Follow-up URLs are restricted to the source's own registered domain, so a
// poisoned sitemap cannot steer the collector at third-party hosts.
//
// # Change detection
//
// The root document is fetched conditionally (ETag / If-Modified-Since); a
// 304 ends the poll with one request. Job pages are visited newest-<lastmod>
// first and capped at adapter_config.max_pages (default 40, hard cap 200),
// so a large employer costs a bounded number of requests per poll. Use
// adapter_config.url_include to aim the budget at India or early-career
// URLs. <lastmod> also stands in for datePosted when a page omits it,
// flagged PostedAtEstimated.
//
// # adapter_config
//
//	url_include      regexp; only URLs matching it are visited
//	sitemap_include  regexp; for a sitemap index, only matching child sitemaps are read
//	max_pages        job pages visited per poll
//	company_name     CompanyNameRaw when the JSON-LD names no hiringOrganization
package careerpage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

const (
	defaultMaxPages = 40
	hardMaxPages    = 200
	// maxChildSitemaps bounds a sitemap index fan-out: an index can list
	// hundreds of children and each is a gated request.
	maxChildSitemaps = 8
	maxGunzipBytes   = 50 << 20
	bundleMarker     = "scout-careerpage-bundle/1"
)

// Fetcher is the part of *fetch.Fetcher this adapter uses; the interface
// exists so tests can fake the network (the real Fetcher's SSRF guard refuses
// the loopback addresses an httptest.Server binds to).
type Fetcher interface {
	Fetch(ctx context.Context, r fetch.Request) (*fetch.Result, error)
}

// Adapter implements adapter.Adapter for career sites.
type Adapter struct {
	kind    adapter.SourceKind
	fetcher Fetcher
}

// NewSitemap returns the adapter for source_kind = sitemap.
func NewSitemap(f Fetcher) *Adapter { return &Adapter{kind: adapter.SourceKindSitemap, fetcher: f} }

// NewHTMLPage returns the adapter for source_kind = html_page.
func NewHTMLPage(f Fetcher) *Adapter { return &Adapter{kind: adapter.SourceKindHTMLPage, fetcher: f} }

// Kind identifies the source kind this instance serves.
func (a *Adapter) Kind() adapter.SourceKind { return a.kind }

// RequiresOwnFetch implements adapter.OwnFetcher: a poll is several gated
// requests, not one conditional GET.
func (a *Adapter) RequiresOwnFetch() bool { return true }

// bundle is the deterministic envelope Fetch hands to Parse, so Parse stays a
// pure function of bytes. Only pages that carry JobPosting JSON-LD are kept.
type bundle struct {
	Marker string       `json:"marker"`
	Pages  []bundlePage `json:"pages"`
}

type bundlePage struct {
	URL     string            `json:"url"`
	LastMod string            `json:"lastmod,omitempty"`
	JSONLD  []json.RawMessage `json:"jsonld"`
}

// Fetch fetches Source.URL conditionally, then — for a sitemap, or a page
// with url_include links — visits the job URLs it lists, each cleared by
// src.Gate first.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	cfg, err := parseConfig(src.AdapterConfig)
	if err != nil {
		return nil, fmt.Errorf("careerpage: %w", err)
	}

	root, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("careerpage: fetch: %w", err)
	}
	if root.StatusCode != 200 {
		return adapter.NewRawResponse(root), nil
	}

	b := bundle{Marker: bundleMarker}
	var entries []entry

	switch a.kind {
	case adapter.SourceKindSitemap:
		entries, err = a.sitemapEntries(ctx, src, cfg, root)
		if err != nil {
			return nil, err
		}
	default:
		blocks := extractJSONLD(root.Body)
		if hasJobPosting(blocks) {
			b.Pages = append(b.Pages, bundlePage{URL: src.URL, JSONLD: jobBlocks(blocks)})
		} else if cfg.urlInclude != nil {
			entries = linkEntries(root.Body, src.URL)
		}
	}

	entries = selectEntries(entries, src.URL, cfg)
	visited, failed := 0, 0
	for _, e := range entries {
		page, status, ferr := a.fetchGated(ctx, src, e.loc)
		if errors.Is(ferr, adapter.ErrRequestRefused) {
			continue // robots.txt (or posture) disallows this one URL; others may be fine
		}
		if ferr != nil {
			if ctx.Err() != nil || errors.Is(ferr, errGateUnavailable) {
				return nil, fmt.Errorf("careerpage: %w", ferr)
			}
			failed++
			continue
		}
		if status == 429 {
			// Hand the 429 to the scheduler so it halves max_rps and honors
			// Retry-After, exactly as for a single-request source.
			return adapter.NewRawResponse(page), nil
		}
		visited++
		if status != 200 {
			continue
		}
		if blocks := extractJSONLD(page.Body); hasJobPosting(blocks) {
			b.Pages = append(b.Pages, bundlePage{URL: e.loc, LastMod: e.lastmod, JSONLD: jobBlocks(blocks)})
		}
	}
	if len(entries) > 0 && visited == 0 && failed > 0 {
		return nil, fmt.Errorf("careerpage: all %d job page requests failed", failed)
	}

	body, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("careerpage: encode bundle: %w", err)
	}
	return &adapter.RawResponse{
		Body:         body,
		StatusCode:   200,
		ETag:         root.ETag,
		LastModified: root.LastModified,
		FetchedAt:    root.FetchedAt,
	}, nil
}

var errGateUnavailable = errors.New("no politeness gate available for follow-up requests")

// fetchGated is the only place follow-up requests are made. It fails closed
// when src.Gate is nil.
func (a *Adapter) fetchGated(ctx context.Context, src adapter.Source, rawURL string) (*fetch.Result, int, error) {
	if src.Gate == nil {
		return nil, 0, errGateUnavailable
	}
	release, err := src.Gate.Allow(ctx, rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("gate: %w", err)
	}
	defer release(ctx)
	res, err := a.fetcher.Fetch(ctx, fetch.Request{URL: rawURL})
	if err != nil {
		return nil, 0, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	return res, res.StatusCode, nil
}

// sitemapEntries reads the root document as a sitemap or sitemap index,
// fetching (gated) up to maxChildSitemaps children of an index.
func (a *Adapter) sitemapEntries(ctx context.Context, src adapter.Source, cfg config, root *fetch.Result) ([]entry, error) {
	doc, err := parseSitemap(root.Body)
	if err != nil {
		return nil, fmt.Errorf("careerpage: sitemap: %w", err)
	}
	entries := doc.urls
	children := 0
	for _, c := range doc.sitemaps {
		if cfg.sitemapInclude != nil && !cfg.sitemapInclude.MatchString(c.loc) {
			continue
		}
		if !sameSite(src.URL, c.loc) || children >= maxChildSitemaps {
			continue
		}
		children++
		res, status, ferr := a.fetchGated(ctx, src, c.loc)
		if errors.Is(ferr, adapter.ErrRequestRefused) || (ferr == nil && status != 200) {
			continue
		}
		if ferr != nil {
			return nil, fmt.Errorf("careerpage: child sitemap: %w", ferr)
		}
		child, perr := parseSitemap(res.Body)
		if perr != nil {
			continue // one bad child must not sink the others
		}
		entries = append(entries, child.urls...)
	}
	return entries, nil
}

// selectEntries applies the safety and budget rules: same registered domain,
// http(s) only, url_include, no duplicates, newest <lastmod> first, capped.
func selectEntries(in []entry, rootURL string, cfg config) []entry {
	seen := map[string]bool{rootURL: true}
	out := make([]entry, 0, len(in))
	for _, e := range in {
		if seen[e.loc] || !sameSite(rootURL, e.loc) {
			continue
		}
		if cfg.urlInclude != nil && !cfg.urlInclude.MatchString(e.loc) {
			continue
		}
		seen[e.loc] = true
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].lastmod != out[j].lastmod {
			return out[i].lastmod > out[j].lastmod // ISO-8601 sorts lexically; empty sorts last
		}
		return out[i].loc < out[j].loc
	})
	if len(out) > cfg.maxPages {
		out = out[:cfg.maxPages]
	}
	return out
}

// sameSite reports whether other is an http(s) URL on rootURL's registered
// domain (eTLD+1).
func sameSite(rootURL, other string) bool {
	ru, err1 := url.Parse(rootURL)
	ou, err2 := url.Parse(other)
	if err1 != nil || err2 != nil || (ou.Scheme != "http" && ou.Scheme != "https") {
		return false
	}
	rd, err1 := publicsuffix.EffectiveTLDPlusOne(ru.Hostname())
	od, err2 := publicsuffix.EffectiveTLDPlusOne(ou.Hostname())
	return err1 == nil && err2 == nil && rd == od
}

type config struct {
	urlInclude     *regexp.Regexp
	sitemapInclude *regexp.Regexp
	maxPages       int
	companyName    string
}

func parseConfig(m map[string]any) (config, error) {
	cfg := config{maxPages: defaultMaxPages}
	for key, dst := range map[string]**regexp.Regexp{"url_include": &cfg.urlInclude, "sitemap_include": &cfg.sitemapInclude} {
		if s, _ := m[key].(string); s != "" {
			re, err := regexp.Compile(s)
			if err != nil {
				return cfg, fmt.Errorf("adapter_config.%s: %w", key, err)
			}
			*dst = re
		}
	}
	if n, ok := m["max_pages"].(float64); ok && n >= 1 {
		cfg.maxPages = min(int(n), hardMaxPages)
	}
	cfg.companyName, _ = m["company_name"].(string)
	return cfg, nil
}

// Parse is pure and deterministic. raw.Body is either the bundle Fetch
// produced or — for fixtures and any caller holding a bare page — one HTML
// document, whose URL is taken to be Source.URL.
func (a *Adapter) Parse(_ context.Context, src adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	cfg, err := parseConfig(src.AdapterConfig)
	if err != nil {
		return nil, fmt.Errorf("careerpage: %w", err)
	}

	pages, err := pagesFrom(raw.Body, src.URL)
	if err != nil {
		return nil, fmt.Errorf("careerpage: parse: %w", err)
	}

	var postings []schema.Posting
	seen := map[string]bool{}
	parsedAny, sawJobBlock := false, false
	for _, pg := range pages {
		for _, blk := range pg.JSONLD {
			sawJobBlock = sawJobBlock || bytes.Contains(blk, []byte("JobPosting"))
			doc, ok := decodeLenient(blk)
			if !ok {
				continue
			}
			parsedAny = true
			for _, jp := range findJobPostings(doc) {
				p := toPosting(jp, pg, cfg, a.kind, raw)
				if p.TitleRaw == "" {
					continue
				}
				key := p.ExternalID
				if key == "" {
					key = p.URL
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				postings = append(postings, p)
			}
		}
	}
	if sawJobBlock && !parsedAny {
		return nil, errors.New("careerpage: parse: every JobPosting JSON-LD block was malformed JSON")
	}
	return postings, nil
}

func pagesFrom(body []byte, pageURL string) ([]bundlePage, error) {
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '{' {
		var b bundle
		if err := json.Unmarshal(trimmed, &b); err == nil && b.Marker == bundleMarker {
			return b.Pages, nil
		}
	}
	blocks := extractJSONLD(body)
	if len(blocks) == 0 {
		return nil, nil
	}
	return []bundlePage{{URL: pageURL, JSONLD: blocks}}, nil
}

// Validate implements docs/06 section 7's per-adapter silent-failure check.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("careerpage: source %s: posting %d has an empty title (JSON-LD shape change?)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("careerpage: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("careerpage: source %s: duplicate external id %s", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}

// gunzipIfNeeded decompresses b when it starts with the gzip magic bytes —
// sitemaps are often served as .xml.gz without a Content-Encoding header, so
// the fetcher's own decoding does not apply.
func gunzipIfNeeded(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, maxGunzipBytes))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	return out, nil
}

// parseTime accepts the date forms JSON-LD and sitemaps use in the wild.
func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
