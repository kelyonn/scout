package robots

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/kelyon/scout/apps/collector/internal/ssrf"
)

// CacheTTL is how long a fetched result is trusted before being re-fetched.
// docs/06 section 4: "Fetched once per host, cached 24 hours in Redis".
const CacheTTL = 24 * time.Hour

// fetchTimeout bounds the whole robots.txt request. It does not need its own
// entry in docs/06's tiered timeout table — robots.txt is always small — but it
// must not be allowed to hang, since every source polling that host is blocked
// on this same cached answer.
const fetchTimeout = 15 * time.Second

// maxResponseBytes caps what we read from the wire, independent of
// MaxBodyBytes. A server that keeps streaming past 500KB is either
// misconfigured or hostile; either way we stop reading rather than trust
// Content-Length or wait for EOF.
const maxResponseBytes = MaxBodyBytes + 1

// Cache stores a fetched, encoded robots.txt result keyed by host.
//
// The interface exists so Checker can be tested without Redis, and so the
// Postgres fallback docs/06 specifies ("cached 24 hours in Redis with a
// Postgres fallback so a Redis flush does not cause a stampede") can be added
// as a second implementation once the collector has a database client to give
// it — deferred for now rather than built against a query layer that does not
// exist yet.
type Cache interface {
	// Get returns the cached body and true if present and unexpired. It never
	// itself distinguishes "not cached" from "cache unavailable" — both simply
	// return ok=false, and the checker fetches fresh either way.
	Get(ctx context.Context, host string) (body []byte, status int, ok bool)
	// Set stores a fetched result. Errors are the caller's to decide whether to
	// treat as fatal; see [Checker], which does not.
	Set(ctx context.Context, host string, body []byte, status int, ttl time.Duration) error
}

// Checker answers "may we fetch this path" for a given host, fetching and
// caching robots.txt as needed.
type Checker struct {
	client       *http.Client
	cache        Cache
	userAgent    string
	fromHeader   string
	fetchTimeout time.Duration
}

// New returns a Checker that identifies itself per SCOUT-LEGAL-003
// (docs/14-legal-compliance.md): a descriptive User-Agent with a contact URL,
// and a From header with an operator email. Both are required — "we identify
// honestly and provide contact information... a site operator who can reach
// you sends an email instead of a block" (docs/06 section 4).
//
// robots.txt fetches carry the same SSRF posture as every other collector
// request (apps/collector/internal/ssrf) — robots.txt is served by the same
// host a source's content is, so it is exactly as capable of pointing DNS at
// an internal address as the content fetch is.
func New(cache Cache, contactURL, operatorEmail string) *Checker {
	return &Checker{
		client:       &http.Client{Transport: newTransport(ssrf.DialContext), Timeout: fetchTimeout, CheckRedirect: checkRedirect},
		cache:        cache,
		userAgent:    fmt.Sprintf("Scout/1.0 (+%s; personal job discovery agent)", contactURL),
		fromHeader:   operatorEmail,
		fetchTimeout: fetchTimeout,
	}
}

// dialFunc is the shape http.Transport.DialContext expects. newTransport
// takes it as a parameter — rather than hard-coding ssrf.DialContext —
// purely so checker_test.go can build a Checker with every other setting
// identical to production but a dialer that can actually reach an
// httptest.Server, which binds to 127.0.0.1 and is therefore exactly the kind
// of address ssrf.DialContext exists to refuse. There is no exported way to
// construct a Checker with a different dialer; the swap point exists only
// inside the package, for tests.
type dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error)

func newTransport(dial dialFunc) *http.Transport {
	return &http.Transport{DialContext: dial}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("robots.txt: too many redirects for %s", req.URL.Host)
	}
	if len(via) > 0 && req.URL.Scheme != via[0].URL.Scheme {
		return fmt.Errorf("robots.txt: refusing a scheme-changing redirect to %s", req.URL)
	}
	return nil
}

// Allowed reports whether path may be fetched from host, per RFC 9309 and the
// fail-closed policy in the package comment. host is a bare hostname (no
// scheme); scheme selects which robots.txt is fetched (https preferred).
//
// There is no error return. Every failure this method can encounter — a
// network error, a 5xx, an unreachable host — already has a defined answer
// under the fail-closed policy, so an error return would be a case callers
// must handle that can never actually arise. See [Checker.rules].
func (c *Checker) Allowed(ctx context.Context, scheme, host, path string) bool {
	return c.rules(ctx, scheme, host).Allowed(path)
}

// CrawlDelay returns the site's declared Crawl-delay for host, or nil if none
// was declared or the fetch failed closed.
func (c *Checker) CrawlDelay(ctx context.Context, scheme, host string) *float64 {
	return c.rules(ctx, scheme, host).CrawlDelay()
}

// rules never returns nil and never fails in a way the caller must react to:
// a network error or a 5xx resolves to [DisallowAll], exactly as if the site
// had declared it. This is what makes the fail-closed policy structural rather
// than a convention every call site has to remember to apply.
func (c *Checker) rules(ctx context.Context, scheme, host string) *Rules {
	if c.cache != nil {
		if body, status, ok := c.cache.Get(ctx, host); ok {
			return decode(body, status)
		}
	}

	body, status, err := c.fetch(ctx, scheme, host)
	if err != nil {
		// Network failure. Fail closed (package comment) but do not cache the
		// failure — the RFC's 24h cache is for a successful answer, and caching
		// a transient network blip as "disallowed" for a day would quietly
		// starve a source that was never actually blocked.
		return DisallowAll()
	}

	if c.cache != nil {
		// Best-effort. A cache write failure means the next request re-fetches,
		// which is correct behavior, not a degraded one — never a reason to fail
		// the current, already-successful, fetch.
		_ = c.cache.Set(ctx, host, body, status, CacheTTL)
	}

	return decode(body, status)
}

// decode turns a fetched (body, status) pair into Rules per the policy table
// in docs/06 section 4: 2xx is parsed, 4xx is unrestricted, everything else —
// 5xx, or a synthetic status this package never actually produces but a
// forward-compatible cache entry might carry — is disallowed.
func decode(body []byte, status int) *Rules {
	switch {
	case status >= 200 && status < 300:
		return Parse(body)
	case status >= 400 && status < 500:
		return AllowAll()
	default:
		return DisallowAll()
	}
}

func (c *Checker) fetch(ctx context.Context, scheme, host string) (body []byte, status int, err error) {
	u := fmt.Sprintf("%s://%s/robots.txt", scheme, host)

	ctx, cancel := context.WithTimeout(ctx, c.fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build robots.txt request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	if c.fromHeader != "" {
		req.Header.Set("From", c.fromHeader)
	}
	req.Header.Set("Accept-Encoding", "gzip, br")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch robots.txt from %s: %w", host, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("read robots.txt from %s: %w", host, err)
	}

	return data, resp.StatusCode, nil
}
