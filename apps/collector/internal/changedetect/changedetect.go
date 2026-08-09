// Package changedetect implements layer 2 of the three-layer change detection
// in docs/06-ingestion-pipeline.md section 6.
//
// Layer 1 — HTTP validators (ETag / If-Modified-Since) — is not this package.
// It is a property of the conditional GET apps/collector/internal/fetch
// already performs: a 304 means "done, zero bytes, zero parse" before this
// package is ever called. Layer 3 — structural diff, one hash per posting via
// an adapter's Parse — needs an adapter, which does not exist yet; that layer
// is deferred along with the adapters.
//
// This package is the middle layer: for a 200 response with no ETag support
// (or one whose ETag legitimately changed on every request, which some
// misconfigured origins do), hash the body and compare against the source's
// last hash. Doing this correctly is mostly about NOT hashing the parts of the
// page that change on every render regardless of content — a render
// timestamp, a CSRF token, a "posted 3 minutes ago" string. Miss that and
// Layer 2 never fires: every poll looks different, every poll gets parsed, and
// the entire point of a cheap layer between "free" (a 304) and "expensive" (a
// full parse) is lost.
package changedetect

import (
	"bytes"
	"crypto/sha256"
	"regexp"
	"strings"
)

// The strip rules docs/06 section 6 names, applied in this order because each
// one can leave gaps or partial matches the next depends on being cleaned up
// last: comments and dates and tokens are struck first, relative-time phrases
// second, and whitespace is collapsed only once everything else that could
// leave irregular spacing has already been removed.
var (
	// htmlComment strips <!-- ... --> nodes. (?s) lets '.' cross newlines,
	// since a comment block is very often multi-line in rendered markup.
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

	// isoDate matches an ISO-8601 date or datetime, with or without a time
	// component, offset, or 'Z'. This is the single most common volatile
	// pattern on a career page: "lastUpdated": "2026-08-09T09:15:00Z".
	isoDate = regexp.MustCompile(
		`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)?\b`,
	)

	// rfc1123Date matches the HTTP-date form some pages embed verbatim in
	// visible text, e.g. "Wed, 06 Aug 2026 09:15:00 GMT".
	rfc1123Date = regexp.MustCompile(
		`\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d{2}:\d{2}:\d{2} GMT\b`,
	)

	// monthNameDate matches "August 9, 2026" / "Aug 9 2026" and the reversed
	// "9 August 2026" form.
	monthNameDate = regexp.MustCompile(`(?i)\b(?:` +
		`Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|` +
		`Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?` +
		`)\.?\s+\d{1,2},?\s+\d{4}\b`)
	monthNameDateReversed = regexp.MustCompile(`(?i)\b\d{1,2}\s+(?:` +
		`Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|` +
		`Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?` +
		`)\.?,?\s+\d{4}\b`)

	// volatileKey is the alternation of key families docs/06 section 6 names:
	// "csrf, nonce, _token, and session identifiers in attributes." Scoped to
	// exactly those key names rather than a bare "token", which would strip
	// legitimate content ("this role owns the API token service") that has
	// nothing to do with a page-rendering artifact. Shared between the two
	// patterns below because HTML expresses the same volatile value two
	// different ways and both need to recognize the same key set.
	volatileKey = `csrf[-_]?token|csrf|nonce|_token|sessionid|session[-_]?id|session[-_]?token`

	// volatileAttr matches the JSON/attribute idiom "key=value" or
	// "key: value" (quoted or bare) — e.g. `"csrf_token": "a1b2c3"` or
	// `nonce="a1b2c3"`.
	volatileAttr = regexp.MustCompile(
		`(?i)\b(?:` + volatileKey + `)\s*[:=]\s*["']?[A-Za-z0-9._-]+["']?`,
	)

	// volatileFormField matches the separate HTML-forms idiom, where the key
	// name and its value live in two different attributes of the same tag
	// rather than adjacent to each other:
	// `<input type="hidden" name="csrf_token" value="a1b2c3">`. volatileAttr
	// alone does not see this — "csrf_token" appears only as the *value* of
	// `name=`, with the actual volatile data in a same-tag `value=` that could
	// come before or after it, which is why this is a second pattern rather
	// than a tweak to the first.
	volatileFormField = regexp.MustCompile(
		`(?i)<[^>]*\bname=["']?(?:` + volatileKey + `)["']?[^>]*>`,
	)

	// relativeTime matches "N hours ago" style phrases and their bare
	// counterparts — the "12 jobs found · updated 3 minutes ago" case the
	// package comment calls out by name.
	relativeTime = regexp.MustCompile(
		`(?i)\b\d+\s*(?:second|minute|hour|day|week|month|year)s?\s+ago\b`,
	)
	relativeTimeWords = regexp.MustCompile(`(?i)\b(?:yesterday|today|just now)\b`)

	// whitespaceRun collapses everything Normalize's earlier passes left
	// behind — removed text leaves gaps, and a gap of different width between
	// two otherwise-identical bodies must not count as a content change.
	whitespaceRun = regexp.MustCompile(`\s+`)
)

// Normalize strips the volatile patterns above and collapses whitespace.
//
// It is a text-level transform, not an HTML or JSON parser — deliberately, so
// the same function works whether a source's body is markup or a JSON API
// response, which docs/06 section 6 requires ("The strip rules are per-source-
// kind and extensible via adapter_config"; this is the shared default those
// per-kind rules will extend once an adapter exists to configure them).
func Normalize(body []byte) []byte {
	s := string(body)

	s = htmlComment.ReplaceAllString(s, "")
	s = rfc1123Date.ReplaceAllString(s, "")
	s = isoDate.ReplaceAllString(s, "")
	s = monthNameDate.ReplaceAllString(s, "")
	s = monthNameDateReversed.ReplaceAllString(s, "")
	// Order matters here specifically: volatileFormField strips the whole
	// `<input ... name="csrf_token" ... value="...">` tag, so it must run
	// before volatileAttr would otherwise match just the `name="csrf_token"`
	// fragment inside it and leave the `value="..."` half of the same tag
	// behind as untouched, still-volatile content.
	s = volatileFormField.ReplaceAllString(s, "")
	s = volatileAttr.ReplaceAllString(s, "")
	s = relativeTime.ReplaceAllString(s, "")
	s = relativeTimeWords.ReplaceAllString(s, "")
	s = whitespaceRun.ReplaceAllString(s, " ")

	return []byte(strings.TrimSpace(s))
}

// Hash returns the sha256 digest of the normalized body — the value compared
// against source.last_content_hash. A 32-byte slice, matching that column's
// BYTEA type directly with no encoding step in between.
func Hash(body []byte) []byte {
	sum := sha256.Sum256(Normalize(body))
	return sum[:]
}

// Changed reports whether newBody's content hash differs from lastHash, the
// value previously stored on the source row. A nil or empty lastHash — the
// state of a source's very first successful poll — always reports changed,
// since there is nothing yet to compare against.
func Changed(newBody, lastHash []byte) bool {
	if len(lastHash) == 0 {
		return true
	}
	return !bytes.Equal(Hash(newBody), lastHash)
}
