package normalize

import (
	"html"
	"regexp"
	"strings"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

// StripHTML extracts plain text from an adapter's raw HTML description —
// enough for compensation parsing and full-text storage. This is NOT
// docs/07 section 11's XSS-safe sanitizer (an allowlist of permitted tags
// for rendering, which apps/web will need at P3) — it exists only to turn
// markup into text for this package's own regex-based parsers, which don't
// otherwise care about tag structure but do care about entities like
// "&#8377;" breaking currency detection.
//
// Unescape happens before AND after tag-stripping. Some sources (observed
// live from Greenhouse's board API, not a hypothetical) return their
// content field already entity-escaped — the string contains the literal
// characters "&lt;h2&gt;", not a real "<h2>" tag — so htmlTagPattern finds
// nothing to strip until a first unescape turns those entities back into
// real tags. The second unescape then handles ordinary entities
// (&amp;, &#8377;) that were never part of a tag to begin with. Running
// unescape only once, in either position alone, leaves one of those two
// cases broken — this was caught against real Stripe job postings during
// P1 verification, not written speculatively.
func StripHTML(raw string) string {
	text := html.UnescapeString(raw)
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(text, " "))
}
