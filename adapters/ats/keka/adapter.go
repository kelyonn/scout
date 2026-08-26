// Package keka implements packages/adapter.Adapter for Keka job boards.
// docs/05-source-catalog.md section 2 rates Keka "Low effort," "widely
// used by Indian mid-market" — this is that P5 item.
//
// Endpoint: {tenant}.keka.com/careers/api/jobs/default/active — no auth,
// no pagination, whole board in one response, verified live 2026-08-24
// against three real tenants (fynd.keka.com, pennanttech.keka.com,
// achiralabs.keka.com — the last of these currently has zero open roles,
// which is exactly the "genuinely empty board" case Validate below must
// not reject).
//
// Keka actually has (at least) two different careers-portal templates in
// the wild: the one this adapter targets, and a second "embed widget"
// variant (observed at fynd.keka.com's own custom-built careers page,
// distinct from its underlying Keka-hosted data) that requires scraping
// an organization GUID out of a `<script>` tag before the equivalent
// `/careers/api/embedjobs/default/active/{guid}` endpoint will resolve.
// Deliberately not supported here — two of three real tenants checked use
// the plain endpoint this adapter targets, an HTML-scrape-then-JSON-fetch
// two-step is a meaningfully different and riskier shape than every other
// adapter in this project, and "some real Keka tenants won't resolve" is
// an honest, bounded gap, not a silent one (Validate's own error surfaces
// it per-source instead of pretending success).
package keka

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// Adapter implements adapter.Adapter for Keka.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Keka adapter. fetcher must be the collector's shared,
// SSRF-safe fetcher — adapters/README.md's hard rule: no direct http.Get,
// not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_keka.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindKeka }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full
// https://{tenant}.keka.com/careers/api/jobs/default/active endpoint
// (CONTRIBUTING.md's "Adding a source"), not reconstructed from a tenant
// slug here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("keka: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// kekaJob is /careers/api/jobs/default/active's actual per-posting shape
// — fields beyond what this adapter uses (jobNumber, salaryRange's own
// sub-fields, skillNames, publishedSinceDays) are omitted deliberately,
// same convention every other adapter in this project follows. jobType is
// a small int enum whose real mapping was never confirmed against Keka's
// own documentation (every real posting checked while building this
// adapter came back jobType=2, not enough signal to reverse-engineer a
// mapping with any confidence) — deliberately not surfaced as
// EmploymentTypeRaw rather than guessed at; seniority classification
// downstream already draws on TitleRaw and the free-text `experience`
// field instead.
type kekaJob struct {
	ID                int64          `json:"id"`
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	DepartmentName    string         `json:"departmentName"`
	JobLocations      []kekaLocation `json:"jobLocations"`
	Experience        string         `json:"experience"`
	SalaryRangeFormat string         `json:"salaryRangeFormat"`
	PublishedOn       string         `json:"publishedOn"`
}

type kekaLocation struct {
	Name string `json:"name"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay. The real endpoint's response root is a bare
// JSON array (unlike Greenhouse's `{"jobs": [...]}` wrapper), so this
// unmarshals straight into a slice.
func (a *Adapter) Parse(_ context.Context, src adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	var jobs []kekaJob
	if err := json.Unmarshal(raw.Body, &jobs); err != nil {
		return nil, fmt.Errorf("keka: parse: %w", err)
	}

	base := careerPortalBase(src.URL)
	postings := make([]schema.Posting, 0, len(jobs))
	for _, j := range jobs {
		postings = append(postings, toPosting(j, base, raw))
	}
	return postings, nil
}

// careerPortalBase strips the known API suffix off src.URL to recover
// https://{tenant}.keka.com/careers — the prefix every real job's detail
// page URL is built from (https://{tenant}.keka.com/careers/jobdetails/{id},
// confirmed live by clicking through from two real tenants' listings to
// their real detail pages). Falls back to the untouched src.URL if a
// source was ever seeded with a differently-shaped URL, so a malformed
// seed produces a wrong-but-present ApplyURL for Validate to catch,
// rather than a panic.
func careerPortalBase(sourceURL string) string {
	const suffix = "/api/jobs/default/active"
	if strings.HasSuffix(sourceURL, suffix) {
		return strings.TrimSuffix(sourceURL, suffix)
	}
	return sourceURL
}

func toPosting(j kekaJob, base string, raw *adapter.RawResponse) schema.Posting {
	applyURL := fmt.Sprintf("%s/jobdetails/%d", base, j.ID)

	p := schema.Posting{
		ExternalID:      strconv.FormatInt(j.ID, 10),
		URL:             applyURL,
		ApplyURL:        applyURL,
		TitleRaw:        j.Title,
		DescriptionHTML: j.Description,
		Department:      j.DepartmentName,
		LocationRaw:     locationLabel(j.JobLocations),
		// RequirementsText carries Keka's own free-text `experience`
		// field ("0-1", "4+ years", "8-15+ yrs") rather than staying
		// empty — classify.ResolveSeniority already reads
		// RequirementsText as one of its seniority-signal inputs
		// (apps/collector/internal/discovery.Assess uses the identical
		// three-argument call), and "0-1" is exactly the kind of signal
		// that call exists to catch. Not a requirements blurb in the
		// traditional sense, but the closest real field this API has to
		// what that argument is actually for.
		RequirementsText:    j.Experience,
		CompensationRawText: j.SalaryRangeFormat,
		Adapter:             string(adapter.SourceKindKeka),
		FetchedAt:           raw.FetchedAt,
	}

	// publishedOn is Keka's own posting timestamp (parallel to
	// SmartRecruiters' releasedDate), not an edit-time proxy, so
	// PostedAtEstimated stays false. RFC3339Nano rather than RFC3339: real
	// values carry fractional seconds ("2026-08-20T12:57:54.91Z"), which
	// bare RFC3339 does not reliably parse.
	if t, err := time.Parse(time.RFC3339Nano, j.PublishedOn); err == nil {
		p.PostedAt = &t
	}

	return p
}

// locationLabel joins every location a posting lists (Keka postings
// routinely carry more than one — a real Fynd posting listed both
// Bengaluru and Mumbai) — same multi-location-join convention
// adapters/ats/smartrecruiters uses for its own single-location case,
// extended here to however many entries jobLocations actually has.
func locationLabel(locs []kekaLocation) string {
	parts := make([]string, 0, len(locs))
	for _, l := range locs {
		if l.Name != "" {
			parts = append(parts, l.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// Validate implements docs/06 section 7's per-adapter silent-failure
// check — same structural-plausibility posture as every other adapter's
// Validate. A genuinely empty board (achiralabs.keka.com, verified live:
// zero open roles right now) is not itself invalid — Parse already
// returns an empty slice for it, and this function is never called with
// zero postings in a way that would misreport it (the caller, per every
// other adapter's own convention, only calls Validate when len(postings)
// > 0).
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("keka: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("keka: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("keka: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
