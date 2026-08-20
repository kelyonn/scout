// Package discovery finds new ATS-hosted company boards worth adding as
// pending_review sources, and assesses whether a candidate's board is
// actually worth adding at all — reusing the same classify/normalize logic
// the ingestion pipeline itself trusts, rather than a separate ad-hoc
// heuristic that could quietly disagree with what actually gets surfaced.
//
// This exists because a bulk company-list import (any slug that merely
// resolves to a non-empty ATS board) pulls in plenty of boards with zero
// software/entry-level postings — real companies, real boards, just not
// relevant to what this project is for. AssessRelevance is the gate that
// turns "resolves" into "worth a pending_review row."
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kelyon/scout/apps/collector/internal/classify"
	"github.com/kelyon/scout/apps/collector/internal/normalize"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

// Candidate is one company slug to check against one known ATS platform.
type Candidate struct {
	Slug string
	Kind padapter.SourceKind
}

// relevantSeniorities is the same admit-set
// packages/db/queries/notification.sql's seniority_filter_enabled clause
// uses: internship/new_grad/entry, plus unknown (unresolved, not
// unresolved-and-therefore-excluded — the same "don't hide on
// uncertainty" rule docs/07 section 5 states for seniority generally).
var relevantSeniorities = map[schema.Seniority]bool{
	schema.SeniorityInternship: true,
	schema.SeniorityNewGrad:    true,
	schema.SeniorityEntry:      true,
	schema.SeniorityUnknown:    true,
}

// Result is one candidate's assessment.
type Result struct {
	Candidate  Candidate
	JobCount   int
	Relevant   bool // at least one posting is software + entry-level-ish
	StatusCode int
}

// URLFor builds the same board-API URL every source of this kind uses
// elsewhere in this project (packages/db/queries/source.sql's seeded
// sources use the identical pattern) — kept here so discovery and the
// verification/seed tooling never drift into two different URL shapes for
// the same platform.
func URLFor(slug string, kind padapter.SourceKind) (string, error) {
	switch kind {
	case padapter.SourceKindGreenhouse:
		return fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true", slug), nil
	case padapter.SourceKindLever:
		return fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", slug), nil
	case padapter.SourceKindAshby:
		return fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s?includeCompensation=true", slug), nil
	default:
		return "", fmt.Errorf("discovery: unsupported source kind %q", kind)
	}
}

// Assessor fetches and classifies a candidate's real postings. Roles and
// Skills are loaded once by the caller (taxonomy.LoadRolePatterns/
// LoadSkills) and threaded through rather than reloaded per candidate.
type Assessor struct {
	Fetcher  *fetch.Fetcher
	Adapters map[padapter.SourceKind]padapter.Adapter
	Roles    []taxonomy.RoleFamily
	Skills   []taxonomy.Skill
}

// NewAssessor wires the same three adapters apps/collector/cmd's
// buildPipeline uses, so a candidate's postings are fetched and parsed by
// the exact code that would run if the source were promoted to active.
func NewAssessor(fetcher *fetch.Fetcher, adapters map[padapter.SourceKind]padapter.Adapter) *Assessor {
	return &Assessor{
		Fetcher:  fetcher,
		Adapters: adapters,
		Roles:    taxonomy.LoadRolePatterns(),
		Skills:   taxonomy.LoadSkills(),
	}
}

// Assess fetches a candidate's board and classifies every posting on it,
// reporting relevant=true the moment one posting is both software and
// entry-level-ish. A 404 or empty board is not an error — it is a
// Result{Relevant: false}, the routine "this candidate doesn't pan out"
// outcome discovery exists to filter for.
func (a *Assessor) Assess(ctx context.Context, cand Candidate) (Result, error) {
	res := Result{Candidate: cand}

	url, err := URLFor(cand.Slug, cand.Kind)
	if err != nil {
		return res, err
	}

	ad, ok := a.Adapters[cand.Kind]
	if !ok {
		return res, fmt.Errorf("discovery: no adapter registered for %q", cand.Kind)
	}

	src := padapter.Source{ID: cand.Slug, URL: url}
	raw, err := ad.Fetch(ctx, src, padapter.FetchHints{})
	if err != nil {
		return res, nil //nolint:nilerr // network/404/etc is a routine non-hit, not a discovery-run failure
	}
	res.StatusCode = raw.StatusCode
	if raw.StatusCode != http.StatusOK {
		return res, nil
	}

	postings, err := ad.Parse(ctx, src, raw)
	if err != nil {
		return res, nil //nolint:nilerr // unparsable board is a non-hit, not a run failure
	}
	res.JobCount = len(postings)

	for _, p := range postings {
		normalizedTitle := normalize.NormalizeTitle(p.TitleRaw).Normalized
		role := classify.ClassifyRole(normalizedTitle, p.DescriptionText, a.Roles, a.Skills)
		if !role.IsSoftware {
			continue
		}
		seniority := classify.ResolveSeniority(p.TitleRaw, p.RequirementsText, p.EmploymentTypeRaw)
		if relevantSeniorities[seniority] {
			res.Relevant = true
			return res, nil
		}
	}
	return res, nil
}

// FetchSlugList fetches a JSON array of company slugs from url — the shape
// Feashliaa/job-board-aggregator's data/{greenhouse,lever,ashby}_companies.json
// files use (MIT licensed; see infra/seed/sources.sql's Phase M comment for
// how this project uses that repo — a candidate-slug discovery aid,
// independently re-verified, never copied code or data wholesale).
// Deliberately generic on the URL so tests point it at an httptest.Server
// instead of the real GitHub raw host.
func FetchSlugList(ctx context.Context, client *http.Client, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery: fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery: fetch %s: status %d", url, resp.StatusCode)
	}

	var slugs []string
	if err := json.NewDecoder(resp.Body).Decode(&slugs); err != nil {
		return nil, fmt.Errorf("discovery: decode %s: %w", url, err)
	}
	return slugs, nil
}
