// Package zohorecruit implements packages/adapter.Adapter for Zoho Recruit
// career sites. docs/05-source-catalog.md section 2 rates Zoho Recruit "Low
// effort," "common at Indian SMBs" — this is the second P5 ATS adapter,
// after Keka.
//
// docs/05's original endpoint guess (`{tenant}.zohorecruit.in/recruit/
// portal/...`, a "public JSON" API) was wrong, the same way SmartRecruiters'
// and Teamtailor's original guesses were — found by inspecting two real
// tenants' live career sites in a browser
// (increff.zohorecruit.com, educohire.zohorecruit.com), not by reading Zoho's
// docs. There is no separate JSON API: the career-site listing page itself
// is HTML, and the job list is embedded as HTML-entity-encoded JSON inside
// a single `<input id="jobs" value="...">` element — confirmed live
// 2026-08-27 against both tenants. `{tenant}.zohorecruit.com/jobs/Careers`
// is the canonical path — verified to work for both; `/careers` (a shorter
// alias that resolved for increff) timed out entirely for educohire, so
// this adapter always seeds and fetches the `/jobs/Careers` form.
//
// Per-tenant module configuration varies which fields the embedded JSON
// carries: increff's board omits Job_Description entirely (title/location/
// experience only), educohire's includes it. Every field this adapter reads
// is therefore treated as optional — a tenant without Job_Description gets
// an honestly empty DescriptionHTML, the same gap Workable/SmartRecruiters/
// Workday already carry for their own N+1 reasons, not a new one.
//
// Apply URLs need only the numeric job id — `/jobs/Careers/{id}` resolves
// identically with or without the SEO slug segment the real site appends
// (confirmed live against both tenants), so this adapter never attempts to
// reproduce Zoho's own slugification.
package zohorecruit

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
)

// Adapter implements adapter.Adapter for Zoho Recruit.
type Adapter struct {
	fetcher *fetch.Fetcher
}

// New returns a Zoho Recruit adapter. fetcher must be the collector's
// shared, SSRF-safe fetcher — adapters/README.md's hard rule: no direct
// http.Get, not even here.
func New(fetcher *fetch.Fetcher) *Adapter {
	return &Adapter{fetcher: fetcher}
}

// Kind identifies this adapter as ats_zohorecruit.
func (a *Adapter) Kind() adapter.SourceKind { return adapter.SourceKindZohoRecruit }

// Fetch calls through fetch.Fetcher against src.URL — the source row's own
// url column, seeded as the full
// https://{tenant}.zohorecruit.com/jobs/Careers listing page
// (CONTRIBUTING.md's "Adding a source"), not reconstructed from a tenant
// slug here.
func (a *Adapter) Fetch(ctx context.Context, src adapter.Source, hints adapter.FetchHints) (*adapter.RawResponse, error) {
	result, err := a.fetcher.Fetch(ctx, fetch.Request{
		URL:             src.URL,
		IfNoneMatch:     hints.IfNoneMatch,
		IfModifiedSince: hints.IfModifiedSince,
	})
	if err != nil {
		return nil, fmt.Errorf("zohorecruit: fetch: %w", err)
	}
	return adapter.NewRawResponse(result), nil
}

// jobsInputRe locates the single `<input id="jobs" ...>` element the real
// career-site page embeds the job array in. Attribute order (value before
// id, observed on both real tenants checked) is not assumed beyond what
// value's own extraction below needs — this regex only needs id="jobs" to
// appear somewhere in the tag.
var jobsInputRe = regexp.MustCompile(`<input[^>]*\bid="jobs"[^>]*>`)

// valueAttrRe pulls the value="..." attribute out of the matched tag. This
// is safe as a simple non-greedy match because the content is HTML-entity
// encoded — a literal `"` never appears inside it, so the first closing
// quote really is the attribute's end.
var valueAttrRe = regexp.MustCompile(`value="([^"]*)"`)

// zohoJob is one entry from the embedded job array. Field presence varies
// by tenant's own module configuration (see package comment) — every field
// is read as optional, and no field's absence is treated as a parse error.
type zohoJob struct {
	ID             string `json:"id"`
	PostingTitle   string `json:"Posting_Title"`
	JobOpeningName string `json:"Job_Opening_Name"`
	JobDescription string `json:"Job_Description"`
	City           string `json:"City"`
	State          string `json:"State"`
	Country        string `json:"Country"`
	WorkExperience string `json:"Work_Experience"`
	JobType        string `json:"Job_Type"`
	RemoteJob      bool   `json:"Remote_Job"`
	DateOpened     string `json:"Date_Opened"`
}

// Parse is pure and deterministic — adapters/README.md's hard rule, the
// basis for fixture replay. It extracts the embedded job array out of the
// HTML page rather than unmarshaling the body directly (the body is a full
// career-site page, not a bare JSON document).
func (a *Adapter) Parse(_ context.Context, src adapter.Source, raw *adapter.RawResponse) ([]schema.Posting, error) {
	tag := jobsInputRe.FindString(string(raw.Body))
	if tag == "" {
		return nil, fmt.Errorf("zohorecruit: source %s: no id=\"jobs\" input found in the response — page shape may have changed", src.ID)
	}
	m := valueAttrRe.FindStringSubmatch(tag)
	if m == nil {
		return nil, fmt.Errorf("zohorecruit: source %s: id=\"jobs\" input has no value attribute", src.ID)
	}

	var jobs []zohoJob
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &jobs); err != nil {
		return nil, fmt.Errorf("zohorecruit: parse: %w", err)
	}

	base := careerSiteBase(src.URL)
	postings := make([]schema.Posting, 0, len(jobs))
	for _, j := range jobs {
		postings = append(postings, toPosting(j, base, raw))
	}
	return postings, nil
}

// careerSiteBase recovers https://{tenant}.zohorecruit.{com,in} from
// src.URL — apply URLs are built from this rather than from src.URL
// directly, so a source seeded with a trailing path segment (or without
// one) still produces a correct apply link. Falls back to the untouched
// src.URL if it doesn't parse, so a malformed seed produces a
// wrong-but-present ApplyURL for Validate to catch rather than a panic.
func careerSiteBase(sourceURL string) string {
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return sourceURL
	}
	return u.Scheme + "://" + u.Host
}

func toPosting(j zohoJob, base string, raw *adapter.RawResponse) schema.Posting {
	title := j.PostingTitle
	if title == "" {
		title = j.JobOpeningName
	}

	applyURL := ""
	if j.ID != "" {
		applyURL = fmt.Sprintf("%s/jobs/Careers/%s?source=CareerSite", base, j.ID)
	}

	p := schema.Posting{
		ExternalID: j.ID,
		URL:        applyURL,
		ApplyURL:   applyURL,
		TitleRaw:   title,
		// DescriptionHTML stays empty on tenants whose module
		// configuration doesn't include Job_Description in the listing
		// payload (increff.zohorecruit.com, confirmed live) — the same
		// honest N+1 gap Workable/SmartRecruiters/Workday already
		// carry, not a new one; a per-job detail fetch would close it
		// but isn't built.
		DescriptionHTML: j.JobDescription,
		LocationRaw:     locationLabel(j.City, j.State, j.Country),
		// RequirementsText carries Zoho's own free-text Work_Experience
		// field ("1-3 year", "5-8 years"), the same convention
		// adapters/ats/keka uses for its own `experience` field —
		// classify.ResolveSeniority reads this as a seniority signal.
		RequirementsText:  j.WorkExperience,
		EmploymentTypeRaw: j.JobType,
		RemoteHint:        j.RemoteJob,
		Adapter:           string(adapter.SourceKindZohoRecruit),
		FetchedAt:         raw.FetchedAt,
	}

	// Date_Opened is a bare date ("2026-08-26"), not a timestamp — Zoho's
	// own stated open date, not an edit-time proxy, so PostedAtEstimated
	// stays false.
	if t, err := time.Parse("2006-01-02", j.DateOpened); err == nil {
		p.PostedAt = &t
	}

	return p
}

// locationLabel joins whichever of city/state/country a posting has —
// same convention adapters/ats/workable's own locationLabel uses.
func locationLabel(city, state, country string) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{city, state, country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// Validate implements docs/06 section 7's per-adapter silent-failure check
// — same structural-plausibility posture as every other adapter's
// Validate. A genuinely empty board is not itself invalid — Parse already
// returns an empty slice for it, and the caller only calls Validate when
// len(postings) > 0, per every other adapter's own convention.
func (a *Adapter) Validate(_ context.Context, src adapter.Source, postings []schema.Posting) error {
	seen := make(map[string]bool, len(postings))
	for i, p := range postings {
		if p.TitleRaw == "" {
			return fmt.Errorf("zohorecruit: source %s: posting %d has an empty title (possible field rename upstream)", src.ID, i)
		}
		if p.ApplyURL == "" {
			return fmt.Errorf("zohorecruit: source %s: posting %d has an empty apply URL", src.ID, i)
		}
		if p.ExternalID != "" {
			if seen[p.ExternalID] {
				return fmt.Errorf("zohorecruit: source %s: duplicate external id %s in one response", src.ID, p.ExternalID)
			}
			seen[p.ExternalID] = true
		}
	}
	return nil
}
