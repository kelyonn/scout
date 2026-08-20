package normalize

import (
	"regexp"
	"strings"
)

// docs/07 section 4, "Title normalization" — the patterns are given
// verbatim in the doc.
var (
	requisitionIDPattern = regexp.MustCompile(`(?i)\b(req|job|id)[\s#-]*\d{3,}\b`)
	seasonYearPattern    = regexp.MustCompile(`(?i)\b(summer|winter|fall|spring)\s*20\d\d\b`)
	whitespacePattern    = regexp.MustCompile(`\s+`)
)

// abbreviations expands the four the doc names, checked as whole words
// after lowercasing so "SWE" doesn't clobber a word that merely contains
// those letters.
var abbreviations = []struct {
	pattern *regexp.Regexp
	expand  string
}{
	{regexp.MustCompile(`(?i)\bswe\b`), "software engineer"},
	{regexp.MustCompile(`(?i)\bsde\b`), "software development engineer"},
	{regexp.MustCompile(`(?i)\bmle\b`), "machine learning engineer"},
	{regexp.MustCompile(`(?i)\bsre\b`), "site reliability engineer"},
}

// NormalizedTitle is title normalization's output. The stripped components
// are retained per docs/07 section 4 ("not discarded — season and year feed
// start-date inference, and level suffixes feed seniority detection") for
// the caller to use; this package only strips them from the matching text.
type NormalizedTitle struct {
	Normalized string // lowercased, stripped, whitespace-collapsed
	Season     string // "summer", "winter", "fall", "spring"; empty if absent
	Year       string // "2027"; empty if absent
}

// NormalizeTitle implements docs/07 section 4's title-normalization steps,
// in order. Parenthetical-location stripping is skipped here — it needs
// the location field to know what would be a duplicate, which
// apps/collector/internal/normalize/location.go resolves separately from
// the raw title.
func NormalizeTitle(raw string) NormalizedTitle {
	t := raw

	t = requisitionIDPattern.ReplaceAllString(t, "")

	var season, year string
	if m := seasonYearPattern.FindStringSubmatch(t); m != nil {
		season = strings.ToLower(m[1])
		year = extractYear(m[0])
		t = seasonYearPattern.ReplaceAllString(t, "")
	}

	t = strings.ToLower(t)
	for _, ab := range abbreviations {
		t = ab.pattern.ReplaceAllString(t, ab.expand)
	}

	t = whitespacePattern.ReplaceAllString(t, " ")
	t = strings.TrimSpace(t)

	return NormalizedTitle{Normalized: t, Season: season, Year: year}
}

func extractYear(seasonYear string) string {
	i := strings.LastIndexAny(seasonYear, "0123456789")
	if i < 3 {
		return ""
	}
	return seasonYear[i-3 : i+1]
}
