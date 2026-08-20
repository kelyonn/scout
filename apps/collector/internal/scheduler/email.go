// Email-alert ingestion — the write-path counterpart to
// apps/collector/internal/emailalert's parsers. docs/05-source-catalog.md's
// "Email alert ingestion, in detail" and docs/14-legal-compliance.md
// section 5 give the legal basis: parsing alert mail the user subscribed to
// is not scraping, provided this stays inside that mail — see
// ResolveCanonicalURL's caller below for the one exception (following the
// tracking link exactly once, to recover the real posting URL, never to
// fetch the description).
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/emailalert"
	"github.com/kelyon/scout/packages/schema"
)

// IngestEmailAlert runs one email-derived posting through the same
// normalize -> classify -> dedup -> score -> write pipeline pollOne uses
// for HTTP sources, after resolving (or creating) the company and source
// rows an alert email doesn't come with already attached. provider is the
// parser's name (emailalert.Provider.Name(), e.g. "linkedin") and is
// recorded on the source's adapter_config for debugging, not used for
// dispatch — the caller already parsed the message before this is called.
//
// This intentionally does not go through runIngestion/pollOne: those exist
// to fetch and change-detect an HTTP source, neither of which applies to a
// posting recovered from an already-arrived email. processPosting is the
// actual shared step — see its own comment for why reusing it here is safe
// (it reads only row.ID and row.CompanyID from the SelectDueSourcesRow it's
// normally given, both of which this constructs directly).
func (s *Scheduler) IngestEmailAlert(ctx context.Context, provider string, posting emailalert.ExtractedPosting) (isNewJob bool, err error) {
	if s.pipeline == nil {
		return false, nil
	}
	if posting.CompanyNameRaw == "" || posting.TitleRaw == "" {
		return false, fmt.Errorf("emailalert: posting missing company name or title")
	}

	// The one sanctioned fetch in this whole path: resolving the
	// tracking redirect to the real posting URL, never the description
	// itself. See this file's package comment and docs/14 section 5.
	canonicalURL, err := emailalert.ResolveCanonicalURL(ctx, s.fetcher, posting.TrackingURL)
	if err != nil {
		return false, fmt.Errorf("emailalert: resolve canonical url: %w", err)
	}

	q := db.New(s.pool)

	// slug is derived deterministically from the company name so that the
	// same employer named consistently across alert emails (or seen again
	// later) collides on FindOrCreateEmailAlertCompany's own
	// on-conflict(slug) rather than spawning a duplicate company row each
	// time. It is not a substitute for real company resolution — a company
	// already known under a differently-derived slug (say, from a
	// Greenhouse board token) will still get a second row here. Accepted
	// gap: email alerts are a discovery channel of last resort, and
	// merging company identities across sources is a separate, harder
	// problem this task doesn't need to solve to be useful.
	slug := slugifyCompanyName(posting.CompanyNameRaw)
	companyID, err := q.FindOrCreateEmailAlertCompany(ctx, db.FindOrCreateEmailAlertCompanyParams{
		Slug:           slug,
		CanonicalName:  posting.CompanyNameRaw,
		NormalizedName: strings.ToLower(strings.TrimSpace(posting.CompanyNameRaw)),
	})
	if err != nil {
		return false, fmt.Errorf("emailalert: find or create company: %w", err)
	}

	adapterConfig, _ := json.Marshal(map[string]string{"provider": provider})
	sourceID, err := q.FindOrCreateEmailAlertSource(ctx, db.FindOrCreateEmailAlertSourceParams{
		CompanyID:     companyID,
		Url:           canonicalURL,
		AdapterConfig: adapterConfig,
	})
	if err != nil {
		return false, fmt.Errorf("emailalert: find or create source: %w", err)
	}

	scoreCtx, err := s.loadScoringContext(ctx)
	if err != nil {
		return false, fmt.Errorf("emailalert: load scoring context: %w", err)
	}

	now := time.Now()
	p := schema.Posting{
		URL:            canonicalURL,
		ApplyURL:       canonicalURL,
		CompanyNameRaw: posting.CompanyNameRaw,
		TitleRaw:       posting.TitleRaw,
		LocationRaw:    posting.LocationRaw,
		Adapter:        "email_alert:" + provider,
		FetchedAt:      now,
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("emailalert: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A partial SelectDueSourcesRow: processPosting reads only ID and
	// CompanyID from it (see that method's own comment). There is no real
	// poll here for the rest of the row's fields to describe.
	row := db.SelectDueSourcesRow{ID: sourceID, CompanyID: companyID}

	// statusCode 200: there is no HTTP response for an email-derived
	// posting, and 200 is the value toInsertObservationParams' status
	// column would hold for a normal successful fetch — the closest
	// honest value to "we have this posting" for a column that assumes
	// one.
	//
	// !ok is not escalated to an error here: it's the same signal
	// runIngestion's own loop treats as "skip, already logged" for every
	// HTTP-sourced posting (normalize/dedup/score/write failures are all
	// non-fatal warnings by design — see processPosting's comment). The
	// one failure mode genuinely distinctive to this path is a duplicate
	// raw_observation: unlike an HTTP source, there is no Layer 2
	// change-detection step upstream to skip re-processing an
	// email-derived posting seen before (a redelivered alert, or the same
	// job appearing in two different emails), so dedup correctly resolves
	// to the existing job while the observation insert legitimately hits
	// raw_observation's per-partition unique index — exactly the
	// "genuine duplicate" case that same comment already documents, not a
	// new failure mode this path invented.
	isNewJob, _ = s.processPosting(ctx, tx, row, p, scoreCtx, 200, now)

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("emailalert: commit tx: %w", err)
	}
	return isNewJob, nil
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyCompanyName lowercases name and collapses everything that isn't
// [a-z0-9] into single hyphens, trimming leading/trailing ones — good
// enough for company.slug's role as a stable dedup key, not a general
// Unicode-transliteration slugifier. A name with no ASCII alphanumerics at
// all (rare, but AGENTS.md's unicode-fixture expectation applies) degrades
// to an empty slug, which FindOrCreateEmailAlertCompany's unique
// constraint on citext would collide across every such company — an
// accepted limitation of a company-identity problem this task doesn't
// solve, same as the slug-derivation comment above.
func slugifyCompanyName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	slug := slugNonAlnum.ReplaceAllString(lower, "-")
	return strings.Trim(slug, "-")
}
