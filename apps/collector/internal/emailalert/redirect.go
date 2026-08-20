package emailalert

import (
	"context"
	"fmt"

	"github.com/kelyon/scout/packages/fetch"
)

// Fetcher is the subset of *fetch.Fetcher this package needs — the same
// narrow-interface pattern apps/collector/internal/scheduler.Fetcher
// uses, so callers can pass the collector's one shared, SSRF-safe fetcher
// through without this package importing scheduler (which will import
// this one).
type Fetcher interface {
	Fetch(ctx context.Context, r fetch.Request) (*fetch.Result, error)
}

// ResolveCanonicalURL follows trackingURL's redirect chain and returns
// where it actually lands — docs/05-source-catalog.md's "resolve the
// tracking redirect to reach the canonical URL," docs/14-legal-
// compliance.md section 5's boundary ("we do not follow links back into
// LinkedIn to fetch full descriptions" — this resolves the link's
// *destination*, it does not fetch a description from it). Every hop is
// SSRF-checked and capped at 3 redirects by fetch.Fetcher itself; this
// function adds nothing beyond reading FinalURL off the result.
func ResolveCanonicalURL(ctx context.Context, fetcher Fetcher, trackingURL string) (string, error) {
	result, err := fetcher.Fetch(ctx, fetch.Request{URL: trackingURL})
	if err != nil {
		return "", fmt.Errorf("emailalert: resolve %q: %w", trackingURL, err)
	}
	if result.FinalURL == "" {
		return trackingURL, nil
	}
	return result.FinalURL, nil
}
