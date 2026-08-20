// Command discover finds new ATS-hosted company boards worth adding as
// pending_review sources — the "routinely go global and big searches"
// feature: re-run this periodically (cron, systemd timer — see `make
// discover-sources` and CONTRIBUTING.md) and it picks up whatever new
// companies the candidate lists below have gained since the last run,
// verifies each one live, and only inserts the ones with a real
// software/entry-level posting on their board right now.
//
// Candidate sources are pinned to specific, MIT-licensed, actively
// maintained files rather than arbitrary web crawling — this project's own
// legal-compliance posture (docs/14, the politeness gate, SCOUT-LEGAL-*)
// only extends to known, public, unauthenticated ATS APIs, and "global and
// big" doesn't need to mean "crawl anything" to be genuinely large: these
// three files alone list company slugs across Greenhouse, Lever, and Ashby
// worldwide, and the upstream repo adds to them over time.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/adapters/ats/ashby"
	"github.com/kelyon/scout/adapters/ats/greenhouse"
	"github.com/kelyon/scout/adapters/ats/lever"
	"github.com/kelyon/scout/apps/collector/internal/discovery"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/logging"
)

// candidateListURLs are Feashliaa/job-board-aggregator's own per-platform
// company-slug files (MIT licensed) — see infra/seed/sources.sql's Phase M
// comment for the original one-off pull from these same files. Re-fetching
// the live URL rather than a pinned commit is deliberate: this command's
// whole purpose is picking up whatever the upstream repo has added since
// last run.
var candidateListURLs = map[padapter.SourceKind]string{
	padapter.SourceKindGreenhouse: "https://raw.githubusercontent.com/Feashliaa/job-board-aggregator/main/data/greenhouse_companies.json",
	padapter.SourceKindLever:      "https://raw.githubusercontent.com/Feashliaa/job-board-aggregator/main/data/lever_companies.json",
	padapter.SourceKindAshby:      "https://raw.githubusercontent.com/Feashliaa/job-board-aggregator/main/data/ashby_companies.json",
}

// maxNewCandidatesPerRun bounds how many never-before-seen slugs get
// assessed in one run — the candidate lists can contain thousands of new
// entries after a long gap, and each assessment is a real HTTP round trip
// against a shared platform host; this keeps one run polite and bounded
// rather than trying to drain the whole backlog at once. A gap longer than
// one run needs simply shows up as more work next time.
const maxNewCandidatesPerRun = 1500

const assessConcurrency = 10

func main() {
	log := slog.New(logging.Scrub(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(log); err != nil {
		log.Error("discover: exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	contactURL := os.Getenv("SCOUT_COLLECTOR_CONTACT_URL")
	operatorEmail := os.Getenv("SCOUT_COLLECTOR_OPERATOR_EMAIL")
	if contactURL == "" || operatorEmail == "" {
		return errors.New("SCOUT_COLLECTOR_CONTACT_URL and SCOUT_COLLECTOR_OPERATOR_EMAIL must both be set (SCOUT-LEGAL-003)")
	}
	databaseURL := os.Getenv("SCOUT_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("SCOUT_DATABASE_URL is not set")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	existing, err := loadExistingSlugs(ctx, pool)
	if err != nil {
		return fmt.Errorf("load existing slugs: %w", err)
	}
	log.Info("discover: loaded existing companies", "count", len(existing))

	fetcher := fetch.New(contactURL, operatorEmail)
	adapters := map[padapter.SourceKind]padapter.Adapter{
		padapter.SourceKindGreenhouse: greenhouse.New(fetcher),
		padapter.SourceKindLever:      lever.New(fetcher),
		padapter.SourceKindAshby:      ashby.New(fetcher),
	}
	assessor := discovery.NewAssessor(fetcher, adapters)

	httpClient := &http.Client{Timeout: 20 * time.Second}

	var candidates []discovery.Candidate
	for kind, url := range candidateListURLs {
		slugs, fetchErr := discovery.FetchSlugList(ctx, httpClient, url)
		if fetchErr != nil {
			log.Warn("discover: fetch candidate list failed", "kind", kind, "url", url, "err", fetchErr)
			continue
		}
		newCount := 0
		for _, slug := range slugs {
			if existing[slug] {
				continue
			}
			candidates = append(candidates, discovery.Candidate{Slug: slug, Kind: kind})
			newCount++
		}
		log.Info("discover: candidate list fetched", "kind", kind, "total", len(slugs), "new", newCount)
	}

	if len(candidates) > maxNewCandidatesPerRun {
		log.Info("discover: capping this run", "found", len(candidates), "cap", maxNewCandidatesPerRun)
		candidates = candidates[:maxNewCandidatesPerRun]
	}

	results := assessAll(ctx, assessor, candidates, log)

	inserted, err := insertRelevant(ctx, pool, results)
	if err != nil {
		return fmt.Errorf("insert relevant candidates: %w", err)
	}

	relevant := 0
	for _, r := range results {
		if r.Relevant {
			relevant++
		}
	}
	log.Info("discover: run complete",
		"assessed", len(results), "relevant", relevant, "inserted", inserted)
	return nil
}

func assessAll(ctx context.Context, assessor *discovery.Assessor, candidates []discovery.Candidate, log *slog.Logger) []discovery.Result {
	results := make([]discovery.Result, len(candidates))
	sem := make(chan struct{}, assessConcurrency)
	done := make(chan int, len(candidates))

	for i, cand := range candidates {
		sem <- struct{}{}
		go func(i int, cand discovery.Candidate) {
			defer func() { <-sem; done <- i }()
			res, err := assessor.Assess(ctx, cand)
			if err != nil {
				log.Warn("discover: assess failed", "slug", cand.Slug, "kind", cand.Kind, "err", err)
				return
			}
			results[i] = res
		}(i, cand)
	}
	for range candidates {
		<-done
	}
	return results
}

func loadExistingSlugs(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `select slug from company`)
	if err != nil {
		return nil, fmt.Errorf("query existing slugs: %w", err)
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scan slug: %w", err)
		}
		existing[slug] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing slugs: %w", err)
	}
	return existing, nil
}

// insertRelevant writes one company + source row per relevant candidate,
// status pending_review — the same human-review gate every other source in
// this project goes through (CONTRIBUTING.md), never active on insertion.
func insertRelevant(ctx context.Context, pool *pgxpool.Pool, results []discovery.Result) (int, error) {
	inserted := 0
	for _, r := range results {
		if !r.Relevant {
			continue
		}
		url, err := discovery.URLFor(r.Candidate.Slug, r.Candidate.Kind)
		if err != nil {
			continue
		}
		name := displayName(r.Candidate.Slug)
		note := fmt.Sprintf(
			"Public %s board API, no auth. Found by `discover` (%s), %d relevant posting(s) at insertion time.",
			platformLabel(r.Candidate.Kind), time.Now().UTC().Format("2006-01-02"), r.JobCount,
		)

		tag, err := pool.Exec(ctx, `
			with new_company as (
				insert into company (slug, canonical_name, normalized_name, discovered_via)
				values ($1, $2, $3, 'discovery')
				on conflict (slug) do update set slug = excluded.slug
				returning id
			)
			insert into source (company_id, kind, url, url_hash, legal_posture, status, base_interval_s, max_rps, notes)
			select id, $4::source_kind, $5, digest($5, 'sha256'), 'permitted', 'pending_review', 900, 0.5, $6
			from new_company
			on conflict (url_hash) do nothing
		`, r.Candidate.Slug, name, strings.ToLower(name), string(r.Candidate.Kind), url, note)
		if err != nil {
			return inserted, fmt.Errorf("insert %s: %w", r.Candidate.Slug, err)
		}
		if tag.RowsAffected() > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func platformLabel(kind padapter.SourceKind) string {
	s := strings.TrimPrefix(string(kind), "ats_")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var slugWordSplit = regexp.MustCompile(`[_-]+`)

// displayName mechanically derives a readable company name from a slug —
// title-cased words, same approach infra/seed/sources.sql's Phase M batch
// used. Not a substitute for a real canonical name; company.canonical_name
// can be corrected by hand for any source actually promoted to active.
func displayName(slug string) string {
	words := slugWordSplit.Split(slug, -1)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
