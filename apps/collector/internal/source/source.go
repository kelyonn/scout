// Package source defines the collector's in-process view of a row in the
// `source` table (infra/migrations/000004_source.up.sql).
//
// It is deliberately a subset. The full row carries scheduling and yield
// columns (next_poll_at, yield_ratio, ...) that belong to the scheduler, which
// does not exist yet. This subset is exactly what the politeness gate needs to
// make its six decisions (docs/06-ingestion-pipeline.md section 4), and it grows
// when the scheduler and fetcher land rather than speculatively now.
package source

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"golang.org/x/net/publicsuffix"
)

// LegalPosture mirrors the `legal_posture` enum in
// infra/migrations/000002_enums.up.sql. Kept as a Go type rather than a bare
// string so a typo in a call site is a compile error, not a silent
// prohibited-source bypass.
type LegalPosture string

const (
	// PosturePermitted means robots.txt and terms of service allow automated
	// access. The only posture the gate can pass on legal grounds alone.
	PosturePermitted LegalPosture = "permitted"
	// PostureEmailOnly means fetching is prohibited; the source is ingested via
	// the user's alert email instead. See docs/14-legal-compliance.md section 5.
	PostureEmailOnly LegalPosture = "email_only"
	// PostureAPIOnly means the source is reachable only via an authorized API,
	// not general HTTP fetch.
	PostureAPIOnly LegalPosture = "api_only"
	// PostureProhibited is the default for every newly registered source
	// (migration 000004's deliberate deviation from the original spec) and
	// means the collector refuses it unconditionally.
	PostureProhibited LegalPosture = "prohibited"
)

// Source is the politeness gate's view of one row in the `source` table.
type Source struct {
	ID   string
	Kind string // source_kind enum value, e.g. "ats_greenhouse" — a metrics label

	LegalPosture LegalPosture
	URL          string

	MaxRPS         float64
	MaxConcurrency int

	// RobotsCrawlDelayS is the site's own Crawl-delay directive, honored even
	// though it is non-standard (docs/06 section 4). Nil means the site did not
	// specify one.
	RobotsCrawlDelayS *float64

	// CircuitOpenUntil is nil when the breaker is closed. The gate only reads
	// this value — recording a failure and setting it is the fetcher's job,
	// because only the fetcher knows a request actually failed. Keeping the
	// write out of the gate is what keeps Allow() side-effect-free and cheap to
	// call speculatively.
	CircuitOpenUntil *time.Time
}

// RegisteredDomain returns the eTLD+1 of the source's URL — the unit the rate
// limiter and the per-host concurrency cap key on.
//
// This is deliberately coarser than the hostname. docs/06 section 4 gives the
// reason directly: boards.greenhouse.io serves thousands of Scout's sources,
// and limiting per hostname would still let every one of those sources be rate
// limited independently, which collectively hammers Greenhouse's single origin.
// Limiting per registered domain is what makes "greenhouse.io: 5 rps" a budget
// that actually bounds the traffic Greenhouse's infrastructure sees from us.
func (s Source) RegisteredDomain() (string, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", fmt.Errorf("parse source url: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("source url has no host: %q", s.URL)
	}

	// An IP literal has no eTLD+1: it is not built from labels under a public
	// suffix, it is the address itself. publicsuffix.EffectiveTLDPlusOne does
	// not know this — asked about "127.0.0.1" it treats the string as if it
	// were a domain name, splits it on '.', and returns a nonsense answer like
	// "0.1" instead of an error. Checking for an IP first, before ever calling
	// it, is what keeps every source on a bare IP from being silently folded
	// into the same wrong rate-limit bucket as every other one.
	if net.ParseIP(host) != nil {
		return host, nil
	}

	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		// A bare hostname with no known public suffix — e.g. "localhost" in a
		// test fixture, or an internal tool. Falling back to the hostname itself
		// keeps the gate usable rather than refusing every such source; it is
		// exactly as strict as treating that single host as its own domain.
		return host, nil //nolint:nilerr // deliberate fallback, see above
	}
	return domain, nil
}
