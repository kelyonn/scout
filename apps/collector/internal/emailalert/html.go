package emailalert

import (
	"html"
	"regexp"
	"strings"
)

// tagPattern strips every HTML tag — used to turn a bounded HTML window
// into plain text runs for company/location extraction, and to clean an
// anchor's inner HTML down to its visible title text.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// stripTags removes every tag and HTML-unescapes the remainder, collapsing
// internal whitespace — job alert templates routinely wrap even plain
// text in nested <span>/<font> tags for email-client styling.
func stripTags(s string) string {
	return collapseSpace(html.UnescapeString(tagPattern.ReplaceAllString(s, " ")))
}

var spacePattern = regexp.MustCompile(`\s+`)

func collapseSpace(s string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(s, " "))
}

// textRunsAfter returns up to n non-empty plain-text runs found in the
// windowChars of HTML immediately following index start — the shared
// heuristic every provider parser in this package uses to recover
// company/location from an alert email's markup: these values are almost
// never in the same tag as the job title, but they are reliably nearby.
// Best-effort by construction; see each provider file's own comment for
// its calibration caveat.
func textRunsAfter(htmlBody string, start, windowChars, n int) []string {
	end := start + windowChars
	if end > len(htmlBody) {
		end = len(htmlBody)
	}
	if start >= end {
		return nil
	}
	window := htmlBody[start:end]

	// Split on tag boundaries first so a run never spans two visually
	// distinct elements (a title tag butting against a location tag with
	// no whitespace between them, which real email HTML does).
	segments := tagPattern.Split(window, -1)
	runs := make([]string, 0, n)
	for _, seg := range segments {
		text := collapseSpace(html.UnescapeString(seg))
		if text == "" {
			continue
		}
		runs = append(runs, text)
		if len(runs) == n {
			break
		}
	}
	return runs
}
