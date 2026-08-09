package robots

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// NewForTesting builds a Checker identical to New()'s in every respect
// except the dialer, which replaces ssrf.DialContext.
//
// This exists because New()'s production dialer refuses to reach 127.0.0.1 —
// which is exactly what every httptest.Server binds to — so a package other
// than robots that wants to integration-test its own use of a real Checker
// (apps/collector/internal/politeness does, to prove its composition against
// a real robots.txt fetch rather than a mock of one) has no way to construct
// one that can reach its test server. robots' own tests solve this with an
// unexported helper; a cross-package caller cannot reach an unexported
// symbol, so this one is exported instead.
//
// Test-only despite being exported: there is no dedicated visibility level in
// Go for "the module's own test code, and nothing else," and gating this
// behind a real interface boundary in every caller was worse than one
// clearly-named, clearly-documented function. Do not call this from
// production code — it exists to remove the SSRF guard, which is the one
// thing this package's production path must never do.
func NewForTesting(cache Cache, dial func(ctx context.Context, network, addr string) (net.Conn, error), contactURL, operatorEmail string) *Checker {
	return &Checker{
		client:       &http.Client{Transport: newTransport(dial), Timeout: fetchTimeout, CheckRedirect: checkRedirect},
		cache:        cache,
		userAgent:    "Scout/1.0 (+" + contactURL + "; personal job discovery agent)",
		fromHeader:   operatorEmail,
		fetchTimeout: fetchTimeout,
	}
}

// PlainDialContext dials addr directly with no SSRF checks. Paired with
// [NewForTesting] so a caller does not also have to hand-write a dialer just
// to reach a local httptest.Server.
func PlainDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}
