// Command sourceprobe verifies a career-page source live before it is seeded:
// it runs the real career-page adapter against one URL through the real
// politeness gate (robots.txt, rate limit, crawl-delay — AGENTS.md rule 1)
// and prints what the adapter extracted. It writes nothing to the database.
//
//	sourceprobe -kind sitemap -url https://jobs.example.com/sitemap.xml \
//	    -config '{"url_include":"/job/","max_pages":5}'
//
// Needs SCOUT_REDIS_URL, SCOUT_COLLECTOR_CONTACT_URL and
// SCOUT_COLLECTOR_OPERATOR_EMAIL, like the collector itself.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/adapters/web/careerpage"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
	"github.com/kelyon/scout/apps/collector/internal/scheduler"
	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
)

// indiaRe is a rough "is this posting in India" check for the report only;
// real location resolution is the normalizer's job.
var indiaRe = regexp.MustCompile(`(?i)india|\bIN\b|bengaluru|bangalore|hyderabad|pune|mumbai|chennai|gurugram|gurgaon|noida|delhi|kolkata|ahmedabad`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sourceprobe:", err)
		os.Exit(1)
	}
}

func run() error {
	kind := flag.String("kind", "sitemap", "source kind: sitemap or html_page")
	rawURL := flag.String("url", "", "source URL")
	cfgJSON := flag.String("config", "{}", "adapter_config JSON")
	rps := flag.Float64("rps", 0.5, "max_rps for the probe")
	sample := flag.Int("sample", 5, "postings to print")
	flag.Parse()
	if *rawURL == "" {
		return errors.New("-url is required")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*cfgJSON), &cfg); err != nil {
		return fmt.Errorf("-config: %w", err)
	}

	contactURL, operatorEmail := os.Getenv("SCOUT_COLLECTOR_CONTACT_URL"), os.Getenv("SCOUT_COLLECTOR_OPERATOR_EMAIL")
	if contactURL == "" || operatorEmail == "" || os.Getenv("SCOUT_REDIS_URL") == "" {
		return errors.New("SCOUT_REDIS_URL, SCOUT_COLLECTOR_CONTACT_URL and SCOUT_COLLECTOR_OPERATOR_EMAIL must be set")
	}
	opt, err := redis.ParseURL(os.Getenv("SCOUT_REDIS_URL"))
	if err != nil {
		return fmt.Errorf("parse SCOUT_REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opt)
	defer func() { _ = rdb.Close() }()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gate := politeness.New(robots.New(robots.NewRedisCache(rdb), contactURL, operatorEmail), ratelimit.New(rdb), rdb, log)
	fetcher := fetch.New(contactURL, operatorEmail)

	var ad padapter.Adapter
	switch *kind {
	case "sitemap":
		ad = careerpage.NewSitemap(fetcher)
	case "html_page":
		ad = careerpage.NewHTMLPage(fetcher)
	default:
		return fmt.Errorf("unknown -kind %q", *kind)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	src := source.Source{ID: "probe", Kind: *kind, LegalPosture: source.PosturePermitted, URL: *rawURL, MaxRPS: *rps, MaxConcurrency: 1}
	decision, release := gate.Allow(ctx, src)
	if decision.Result != politeness.ResultAllow {
		return fmt.Errorf("politeness gate: %s (%s) — not fetching", decision.Result, decision.Reason)
	}
	defer release(ctx)

	asrc := padapter.Source{ID: "probe", URL: *rawURL, AdapterConfig: cfg, Gate: scheduler.NewRequestGate(gate, src)}
	start := time.Now()
	raw, err := ad.Fetch(ctx, asrc, padapter.FetchHints{})
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	fmt.Printf("fetch: status=%d bytes=%d in %s\n", raw.StatusCode, len(raw.Body), time.Since(start).Round(time.Millisecond))
	if raw.StatusCode != 200 {
		return fmt.Errorf("root returned HTTP %d", raw.StatusCode)
	}
	postings, err := ad.Parse(ctx, asrc, raw)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if err := ad.Validate(ctx, asrc, postings); err != nil {
		fmt.Println("validate:", err)
	}
	india := 0
	for _, p := range postings {
		if indiaRe.MatchString(p.LocationRaw) {
			india++
		}
	}
	fmt.Printf("postings: %d (India-located: %d)\n", len(postings), india)
	for i, p := range postings {
		if i >= *sample {
			break
		}
		fmt.Printf("  - %q | %s | type=%s | comp=%q | posted=%v | company=%q\n",
			p.TitleRaw, p.LocationRaw, p.EmploymentTypeRaw, p.CompensationRawText, p.PostedAt, p.CompanyNameRaw)
	}
	return nil
}
