// Package fetch implements the collector's SSRF-safe, conditional HTTP
// fetcher: docs/06-ingestion-pipeline.md section 5.
//
// Every request this package sends must already have passed
// apps/collector/internal/politeness.Gate.Allow — that is the caller's
// responsibility, not this package's, in the same way robots.Checker and
// ratelimit.Limiter do not check compliance themselves either. This package's
// job is narrower: given permission to fetch, do it safely and cheaply.
//
// "Safely" is SSRF protection (packages/ssrf) and a hard cap
// on response size. "Cheaply" is conditional requests — a 304 costs about 500
// bytes against a 200's couple hundred kilobytes, and docs/06 section 5
// targets 85% of polls landing there.
package fetch

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/kelyon/scout/packages/ssrf"
)

// Mode selects which "Total" timeout tier from docs/06 section 5 applies.
// ModeRendered exists because the table names it, even though nothing in the
// collector fetches rendered HTML yet — there is no headless-browser adapter
// to call it. When one exists, it asks for ModeRendered; everything else uses
// the default.
type Mode int

const (
	// ModeStandard is the default: 30s total.
	ModeStandard Mode = iota
	// ModeRendered is for a future headless-render fetch path: 60s total.
	ModeRendered
)

const (
	tlsHandshakeTimeout   = 5 * time.Second
	responseHeaderTimeout = 10 * time.Second // "first byte"
	totalTimeoutStandard  = 30 * time.Second
	totalTimeoutRendered  = 60 * time.Second
	maxRedirects          = 3
)

// MaxBodyBytes is the hard cap docs/06 section 5 specifies: "Response bodies
// are capped at 10 MB. Anything larger is truncated and flagged."
const MaxBodyBytes = 10 * 1024 * 1024

// Request describes one fetch.
type Request struct {
	// URL must be http or https. Any other scheme is refused before a request
	// is constructed — see SCOUT-LEGAL and the SSRF posture in dial.go.
	URL string

	// Mode selects the total-timeout tier. Zero value is ModeStandard.
	Mode Mode

	// IfNoneMatch, if non-empty, is sent as If-None-Match — normally the
	// previous response's ETag, verbatim including any quoting.
	IfNoneMatch string
	// IfModifiedSince, if non-empty, is sent as If-Modified-Since — normally
	// the previous response's Last-Modified header, verbatim. It is a string
	// rather than a time.Time because HTTP-date values must round-trip exactly
	// for If-Modified-Since to behave as the server intends; reparsing and
	// reformatting a date risks losing precision a strict server cares about.
	IfModifiedSince string

	// Method defaults to GET (the zero value) — every P1/P2 adapter's
	// endpoint is a GET. Set to POST for a source whose API genuinely
	// requires a request body to search or filter, e.g. Workday's CXS
	// endpoint (adapters/ats/workday). The SSRF dialer and every timeout
	// tier apply identically regardless of method; only the request line
	// and body change.
	Method string
	// Body is sent verbatim as the request body when Method is POST.
	// Ignored for GET.
	Body []byte
	// ContentType is sent as the Content-Type header when Body is set.
	ContentType string
}

// Result is what a fetch produced.
type Result struct {
	StatusCode int
	// Body is empty for a 304 (a compliant server sends no body) and for a
	// network-level failure this package already turned into an error instead.
	Body []byte
	// Truncated is true when the response exceeded MaxBodyBytes and Body holds
	// only the first MaxBodyBytes of it. Not an error — docs/06 section 5 calls
	// this "truncated and flagged," and the caller (change detection, the
	// validator) decides what a truncated body means for that source.
	Truncated bool

	// ETag and LastModified are the response's validators, empty if the
	// response did not include them. The caller persists whichever are
	// present onto source.last_etag / source.last_modified for the next poll's
	// conditional request.
	ETag         string
	LastModified string

	// RetryAfter is the raw Retry-After header value, empty if absent. Only
	// meaningful on a 429 (docs/06 section 10) — the caller is responsible
	// for parsing it (it may be either delay-seconds or an HTTP-date) and
	// deciding what to do with it; this package just passes it through.
	RetryAfter string

	// NotModified is a convenience for StatusCode == http.StatusNotModified.
	NotModified bool

	FetchedAt time.Time

	// FinalURL is the request URL after following every redirect (identical
	// to the original URL when there were none) — docs/05-source-catalog.md's
	// "resolve the tracking redirect to reach the canonical URL" for email
	// alert links (apps/collector/internal/emailalert), otherwise invisible:
	// every redirect hop already happens inside net/http's RoundTrip,
	// SSRF-checked via checkRedirect, but only the final response was ever
	// surfaced before this field existed.
	FinalURL string
}

// Fetcher performs SSRF-safe, conditional, size-capped HTTP fetches.
type Fetcher struct {
	client     *http.Client
	userAgent  string
	fromHeader string
}

// New returns a Fetcher that identifies itself per SCOUT-LEGAL-003, the same
// requirement apps/collector/internal/robots.New serves — a descriptive
// User-Agent with a contact URL, and a From header with the operator's email.
func New(contactURL, operatorEmail string) *Fetcher {
	return &Fetcher{
		client:     &http.Client{Transport: newTransport(ssrf.DialContext), CheckRedirect: checkRedirect},
		userAgent:  fmt.Sprintf("Scout/1.0 (+%s; personal job discovery agent)", contactURL),
		fromHeader: operatorEmail,
	}
}

// dialFunc is the shape http.Transport.DialContext expects. newTransport
// takes it as a parameter — rather than hard-coding ssrf.DialContext — purely
// so fetch_test.go can build a Fetcher with every other setting identical to
// production but a dialer that can actually reach an httptest.Server, which
// binds to 127.0.0.1 and is therefore exactly the kind of address
// ssrf.DialContext exists to refuse. There is no exported way to construct a
// Fetcher with a different dialer; the swap point exists only inside the
// package, for tests.
type dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error)

func newTransport(dial dialFunc) *http.Transport {
	return &http.Transport{
		DialContext: dial,
		// #nosec G402 -- MinVersion is set explicitly below; this is not a
		// weakened default, it is the same default net/http already applies,
		// stated so a future edit that loosens it shows up as a diff on this
		// line rather than as a silently absent one.
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ForceAttemptHTTP2:     true,
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	// len(via) is the count of requests already made — 1 while deciding
	// whether to follow the first redirect, 2 for the second, and so on. To
	// "follow at most maxRedirects redirects" (docs/06 section 5's literal
	// wording) the Nth redirect must still be allowed when len(via) == N, so
	// the refusal threshold is maxRedirects+1, not maxRedirects: `>=
	// maxRedirects` would refuse the maxRedirects-th redirect itself and
	// silently cap the real budget one hop short of what the constant says.
	if len(via) > maxRedirects {
		return fmt.Errorf("fetch: too many redirects for %s", req.URL.Host)
	}
	if len(via) > 0 && req.URL.Scheme != via[0].URL.Scheme {
		// SCOUT-LEGAL and the SSRF posture both apply to every hop, not just the
		// first: a redirect from https to http would otherwise be a quiet
		// downgrade with no signal to the caller. Refusing it is safe because
		// every legitimate permitted source docs/13 lists is same-scheme
		// throughout.
		return fmt.Errorf("fetch: refusing a scheme-changing redirect to %s", req.URL)
	}
	return nil
}

// Fetch performs one request. It never returns a partial success as an
// error — a truncated body, a 4xx, a 5xx are all a *Result with the status
// code and whatever body accompanied it; err is reserved for the request never
// completing at all (DNS, SSRF refusal, timeout, connection failure).
func (f *Fetcher) Fetch(ctx context.Context, r Request) (*Result, error) {
	parsed, err := url.Parse(r.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch: parse url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("fetch: scheme %q is not http or https", parsed.Scheme)
	}

	total := totalTimeoutStandard
	if r.Mode == ModeRendered {
		total = totalTimeoutRendered
	}
	ctx, cancel := context.WithTimeout(ctx, total)
	defer cancel()

	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader = http.NoBody
	if method == http.MethodPost && r.Body != nil {
		bodyReader = bytes.NewReader(r.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	if f.fromHeader != "" {
		req.Header.Set("From", f.fromHeader)
	}
	// Setting this ourselves is what disables net/http's automatic transparent
	// gzip decoding — see encoding.go for why that is handled explicitly below
	// rather than left to the standard library.
	req.Header.Set("Accept-Encoding", "gzip, br")
	if r.IfNoneMatch != "" {
		req.Header.Set("If-None-Match", r.IfNoneMatch)
	}
	if r.IfModifiedSince != "" {
		req.Header.Set("If-Modified-Since", r.IfModifiedSince)
	}
	if r.ContentType != "" {
		req.Header.Set("Content-Type", r.ContentType)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	result := &Result{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		RetryAfter:   resp.Header.Get("Retry-After"),
		NotModified:  resp.StatusCode == http.StatusNotModified,
		FetchedAt:    time.Now().UTC(),
		FinalURL:     resp.Request.URL.String(),
	}

	// A compliant server sends no body on a 304, but reading (and discarding,
	// bounded) whatever is there anyway costs nothing and tolerates a
	// non-compliant one without a special case.
	decoded, err := decodeBody(resp.Header.Get("Content-Encoding"), resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch: decode response body: %w", err)
	}
	defer func() { _ = decoded.Close() }()

	body, truncated, err := readCapped(decoded, MaxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch: read response body: %w", err)
	}
	result.Body = body
	result.Truncated = truncated

	return result, nil
}

// readCapped reads at most limit bytes and reports whether more remained.
func readCapped(r io.Reader, limit int64) (data []byte, truncated bool, err error) {
	// limit+1: reading one extra byte is what lets us tell "exactly limit
	// bytes, nothing more" apart from "at least limit+1 bytes" without a
	// second, wasted read call.
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("read capped body: %w", err)
	}
	if int64(len(buf)) > limit {
		return buf[:limit], true, nil
	}
	return buf, false, nil
}
