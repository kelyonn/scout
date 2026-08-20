package fetch

import (
	"context"
	"net"
	"net/http"
)

// newTestFetcher builds a Fetcher identical to New()'s in every respect
// except the dialer — dial replaces safeDialContext so tests can reach an
// httptest.Server, which binds to 127.0.0.1 and would otherwise be refused by
// the real SSRF guard. See the comment on [dialFunc] in fetch.go.
//
// This file has the _test.go suffix, so it never compiles into the production
// binary and New() remains the only way anything outside this package can
// construct a Fetcher.
func newTestFetcher(dial dialFunc) *Fetcher {
	return &Fetcher{
		client:     &http.Client{Transport: newTransport(dial), CheckRedirect: checkRedirect},
		userAgent:  "Scout/1.0 (+https://scout.example/bot; personal job discovery agent)",
		fromHeader: "operator@example.com",
	}
}

// plainDialContext dials addr directly with no SSRF checks, for tests that
// exercise Fetch's own logic (conditional headers, decompression, truncation,
// redirects) against a local httptest.Server rather than the dialer.
func plainDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}
