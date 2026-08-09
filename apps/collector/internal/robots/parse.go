// Package robots implements robots.txt fetching, parsing, and caching per
// RFC 9309, as required by SCOUT-LEGAL-001 (docs/14-legal-compliance.md) and
// docs/06-ingestion-pipeline.md section 4.
//
// Three deviations from a bare RFC 9309 implementation are deliberate policy,
// not bugs:
//
//   - Crawl-delay is honored even though RFC 9309 does not define it. "If a
//     site asks us to slow down, we slow down" (docs/06).
//   - A 5xx or unreachable robots.txt is treated as fully disallowed, not as
//     "unrestricted". The RFC only mandates this for 4xx. We fail closed on
//     everything else on purpose: a site whose robots.txt is temporarily broken
//     should not be crawled on the assumption it would have permitted us.
//   - robots.txt bodies are truncated at 500KB before parsing, per the RFC's
//     own guidance for oversized files.
package robots

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// MaxBodyBytes is the truncation limit for a fetched robots.txt body.
// "robots.txt exceeds 500KB | Truncated at 500KB per the RFC" (docs/06 section 4).
const MaxBodyBytes = 500 * 1024

// Rules is a parsed robots.txt file, or the fixed "allow everything" /
// "disallow everything" answer used for the 4xx and 5xx/unreachable cases
// respectively — see [AllowAll] and [DisallowAll].
type Rules struct {
	groups   []group
	sitemaps []string
}

type group struct {
	// agents holds the lowercased product tokens from this group's User-agent
	// lines, e.g. ["scout"] or ["*"]. Multiple consecutive User-agent lines
	// belong to one group and share its rules.
	agents []string
	rules  []compiledRule
	// crawlDelay is the group's own Crawl-delay in seconds, if it declared one.
	crawlDelay *float64
}

type compiledRule struct {
	allow bool
	// pattern is the raw, undecorated rule value (no trailing '$'), used only
	// for the "longest match wins" tie-break — RFC 9309 and every real-world
	// implementation compare the length of the declared pattern, not the length
	// of the substring it happened to match.
	pattern string
	re      *regexp.Regexp
}

// AllowAll is the answer for a 4xx robots.txt response, which RFC 9309 defines
// as "no restrictions" (docs/06 section 4: "robots.txt returns 4xx | Treated as
// unrestricted, per the RFC").
func AllowAll() *Rules { return &Rules{} }

// DisallowAll is the answer for every case this package fails closed on: a 5xx
// response, a network error, or robots.txt being otherwise unreachable. See the
// package comment for why this is stricter than the bare RFC.
func DisallowAll() *Rules {
	return &Rules{
		groups: []group{{
			agents: []string{"*"},
			rules:  []compiledRule{{allow: false, pattern: "/", re: regexp.MustCompile("^/")}},
		}},
	}
}

// Parse reads a robots.txt body and returns the rules it declares.
//
// Parse never fails. A malformed line is skipped — RFC 9309 section 2.2 directs
// parsers to ignore lines they cannot interpret rather than reject the whole
// file, because a robots.txt with one bad line is far more common than one that
// is entirely garbage, and refusing to parse it would fail exactly the sources
// that most need the strict interpretation.
func Parse(body []byte) *Rules {
	if len(body) > MaxBodyBytes {
		body = body[:MaxBodyBytes]
	}

	r := &Rules{}

	var current *group
	// lastWasAgent tracks whether the previous meaningful line was a
	// User-agent line. Consecutive User-agent lines extend the SAME group (a
	// shared rule set for several product tokens); a User-agent line following
	// a rule line starts a NEW group. This is the group-boundary algorithm RFC
	// 9309 section 2.2.1 describes.
	lastWasAgent := false

	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	// robots.txt lines are unbounded in the wild in ways the default 64KB
	// scanner buffer does not tolerate; MaxBodyBytes already caps the whole
	// file, so sizing the line buffer to match costs nothing extra.
	scanner.Buffer(make([]byte, 0, 4096), MaxBodyBytes)

	for scanner.Scan() {
		field, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}

		switch field {
		case "user-agent":
			agent := strings.ToLower(value)
			if current != nil && lastWasAgent {
				current.agents = append(current.agents, agent)
			} else {
				r.groups = append(r.groups, group{agents: []string{agent}})
				current = &r.groups[len(r.groups)-1]
			}
			lastWasAgent = true

		case "disallow", "allow":
			// Rule lines before any User-agent line apply to no declared group
			// and are ignored, per RFC 9309 — a robots.txt author who forgot the
			// User-agent line gets the strict reading, not a guessed one.
			if current == nil {
				lastWasAgent = false
				continue
			}
			current.rules = append(current.rules, compileRule(field == "allow", value))
			lastWasAgent = false

		case "crawl-delay":
			if current == nil {
				lastWasAgent = false
				continue
			}
			if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
				current.crawlDelay = &seconds
			}
			lastWasAgent = false

		case "sitemap":
			// Sitemap directives apply to the whole file, not to one group, and
			// per RFC 9309 may appear anywhere without affecting group
			// boundaries — so lastWasAgent is deliberately left untouched.
			if value != "" {
				r.sitemaps = append(r.sitemaps, value)
			}

		default:
			// Unrecognized field: ignored per RFC 9309, and does not affect
			// group boundaries.
		}
	}

	return r
}

// parseLine splits one robots.txt line into a lowercased field name and a
// trimmed value, stripping comments. ok is false for blank lines, comment-only
// lines, and lines with no ':' separator — all of which are simply skipped.
func parseLine(line string) (field, value string, ok bool) {
	// A comment consumes the rest of the line. The '#' is not escapable inside
	// a robots.txt value per the RFC's grammar, so a bare split is correct here.
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF")) // tolerate a leading BOM
	if line == "" {
		return "", "", false
	}

	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}

	field = strings.ToLower(strings.TrimSpace(line[:i]))
	value = strings.TrimSpace(line[i+1:])
	return field, value, true
}

// compileRule turns a raw Allow/Disallow pattern into a matcher.
//
// robots.txt patterns are always anchored at the start of the path, support
// '*' as "match any sequence of characters", and an trailing '$' anchors the
// end. An empty Disallow value is a well-known special case meaning "disallow
// nothing" (RFC 9309 section 2.2.2) — represented here as a pattern that never
// matches, which is simpler than special-casing it at every call site.
func compileRule(allow bool, pattern string) compiledRule {
	if pattern == "" {
		return compiledRule{allow: allow, pattern: "", re: regexp.MustCompile(`\x00never-matches\x00`)}
	}

	anchoredEnd := strings.HasSuffix(pattern, "$")
	body := pattern
	if anchoredEnd {
		body = body[:len(body)-1]
	}

	var sb strings.Builder
	sb.WriteByte('^')
	for i, part := range strings.Split(body, "*") {
		if i > 0 {
			sb.WriteString(".*")
		}
		sb.WriteString(regexp.QuoteMeta(part))
	}
	if anchoredEnd {
		sb.WriteByte('$')
	}

	return compiledRule{allow: allow, pattern: pattern, re: regexp.MustCompile(sb.String())}
}

// Allowed reports whether path may be fetched under this ruleset, for our own
// product token.
//
// Group selection: the most specific group whose token is a prefix of "scout"
// (case-insensitively) — this is the standard robots.txt matching rule and is
// how a site that declares `User-agent: Scout` gets treated more specifically
// than one that only declares `User-agent: *`. If no group names us
// specifically, the `*` group is used. If neither exists, nothing restricts the
// path and it is allowed — an empty or group-less robots.txt imposes no
// restriction.
//
// Within the selected group: the rule with the longest declared pattern that
// matches wins; a tie between an Allow and a Disallow of equal length is
// resolved in favor of Allow, per RFC 9309 section 2.2.2.
func (r *Rules) Allowed(path string) bool {
	g := r.selectGroup()
	if g == nil {
		return true
	}

	var (
		bestLen   = -1
		bestAllow = true
	)
	for _, rule := range g.rules {
		if !rule.re.MatchString(path) {
			continue
		}
		l := len(rule.pattern)
		switch {
		case l > bestLen:
			bestLen, bestAllow = l, rule.allow
		case l == bestLen && rule.allow:
			// Equal length: Allow wins the tie regardless of encounter order.
			bestAllow = true
		}
	}
	if bestLen < 0 {
		return true
	}
	return bestAllow
}

// CrawlDelay returns the selected group's Crawl-delay in seconds, or nil if
// none was declared. Honored even though it is a non-standard extension — see
// the package comment.
func (r *Rules) CrawlDelay() *float64 {
	if g := r.selectGroup(); g != nil {
		return g.crawlDelay
	}
	return nil
}

// Sitemaps returns every Sitemap URL declared in the file. Not consumed by
// anything yet — sitemap-driven source discovery arrives with the scheduler —
// but the directive applies file-wide regardless of group, so it is captured
// now rather than parsed twice later.
func (r *Rules) Sitemaps() []string { return r.sitemaps }

const ourToken = "scout"

func (r *Rules) selectGroup() *group {
	// A more specific match beats a less specific one, where specificity is
	// the length of the declared token — "Scout" (5) outranks "S" (1), and
	// either outranks "*". This mirrors the real-world convention beyond what
	// docs/06's "Scout group, falling back to *" states explicitly, which
	// covers the two cases Scout's own robots.txt entries actually use.
	var best *group
	bestSpecificity := -1

	for i := range r.groups {
		g := &r.groups[i]
		for _, agent := range g.agents {
			specificity := -1
			switch {
			case agent == "*":
				specificity = 0
			case strings.HasPrefix(ourToken, agent):
				specificity = len(agent)
			}
			if specificity > bestSpecificity {
				bestSpecificity, best = specificity, g
			}
		}
	}
	return best
}
