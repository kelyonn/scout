// Package emailalert implements docs/05-source-catalog.md's "Email alert
// ingestion, in detail" and the legal basis in
// docs/14-legal-compliance.md section 5: parsing job alert emails from
// LinkedIn, Indeed, Glassdoor, and Handshake — platforms whose own boards
// AGENTS.md rule 1a's sibling rule (ADR-007) forbids scraping — as the
// user's own mail, which every email client already does and which the
// platform itself initiated by sending it. This is what unlocks the
// "5-10% of postings concentrated in small companies that post only to
// LinkedIn and never adopt an ATS" ADR-007 names as the residual coverage
// gap without it.
//
// This package owns parsing only: turning one raw email into zero or more
// ExtractedPosting values. It does not touch the database or the network
// beyond what a caller explicitly asks it to do — the IMAP poll
// (imap.go) and the write path
// (apps/collector/internal/scheduler/email.go) are deliberately separate,
// the same reasoning adapters/README.md gives for Parse being pure:
// fixture replay only works if parsing has no side effects.
//
// **Boundaries this package holds, per docs/14 section 5:** it extracts a
// tracking link and resolves it to a canonical URL (redirect.go); it does
// not crawl back into the source platform for a fuller description, and
// it does not retain raw email content beyond what a posting's own fields
// need.
package emailalert

import "time"

// Message is one email, already MIME-decoded to its HTML body — imap.go
// hands these to Extract; fixtures hand them directly to a provider's
// Parse in tests, so this shape is also fixture replay's unit.
type Message struct {
	From       string
	Subject    string
	HTML       string
	ReceivedAt time.Time
}

// ExtractedPosting is one job found inside an alert email — deliberately
// thinner than schema.Posting (no DescriptionHTML beyond the alert's own
// snippet, no compensation, no department): an alert email's own content
// is thinner than a real ATS response, and this type says so structurally
// rather than padding absent fields. apps/collector/internal/scheduler's
// email.go is what turns one of these, plus a resolved canonical URL,
// into a real schema.Posting.
type ExtractedPosting struct {
	TitleRaw       string
	CompanyNameRaw string
	LocationRaw    string
	// TrackingURL is the raw link exactly as it appeared in the alert
	// email — usually wrapped in the sending platform's own click-tracking
	// redirector. redirect.go resolves it to a canonical URL; nothing in
	// this package follows it itself.
	TrackingURL string
	// Snippet is the alert's own short preview text, if any — the only
	// description-like content an alert email typically carries. May be
	// empty; never a fabricated summary.
	Snippet string
}

// Provider extracts postings from one platform's alert email format.
// Every implementation in this package (linkedin.go, indeed.go,
// glassdoor.go, handshake.go) follows the same shape: Matches identifies
// the sender, Parse does the extraction.
type Provider interface {
	// Name is the provider identifier used in source.adapter_config and
	// logging — "linkedin", "indeed", "glassdoor", "handshake".
	Name() string
	// Matches reports whether fromHeader (the email's raw From header)
	// belongs to this provider's known sending addresses.
	Matches(fromHeader string) bool
	// Parse extracts every posting found in msg. Pure and deterministic —
	// the same rule adapters/README.md states for an ATS adapter's own
	// Parse, and for the identical reason: fixture replay.
	Parse(msg Message) ([]ExtractedPosting, error)
}

// Providers is every registered Provider, checked in this order by
// MatchProvider. Order does not matter today (each provider's Matches is
// scoped to its own sending domains, so at most one ever matches a given
// message), but is fixed rather than map-iteration-order to keep a future
// ambiguous-match bug reproducible.
var Providers = []Provider{
	linkedinProvider,
	indeedProvider,
	glassdoorProvider,
	handshakeProvider,
}

// MatchProvider finds the Provider whose Matches accepts fromHeader, if
// any.
func MatchProvider(fromHeader string) (Provider, bool) {
	for _, p := range Providers {
		if p.Matches(fromHeader) {
			return p, true
		}
	}
	return nil, false
}
