// Boilerplate stripping — docs/08 section 3.3, SCOUT-DEDUP-012. The step
// that makes SimHash/semantic comparison safe: without it, two entirely
// different internships at the same company reach ~0.93 similarity because
// they share the same "About Us", benefits list, and EEO statement.
package dedup

import (
	"crypto/sha256"
	"encoding/binary"
	"regexp"
	"strings"
)

// boilerplateMinParagraphLen and boilerplateThreshold are docs/08's own
// spec: paragraphs under 40 characters are too short to be meaningful
// boilerplate signal, and a paragraph appearing in >=60% of a company's
// postings is learned as boilerplate.
const (
	boilerplateMinParagraphLen = 40
	boilerplateThreshold       = 0.60
	// coldStartMinPostings is the point below which only global pattern
	// stripping applies — docs/08: "For a company's first five postings,
	// only global pattern stripping applies."
	coldStartMinPostings = 5
)

// globalBoilerplatePatterns strips EEO/legal boilerplate by regex,
// independent of any per-company learning — docs/08's own list: equal
// opportunity statements, accommodation notices, privacy notices,
// salary-transparency legal disclaimers, and recruiter-agency disclaimers.
// Applied per-paragraph (a whole paragraph is dropped if it matches),
// since these are usually self-contained blocks, not inline phrases.
var globalBoilerplatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)equal opportunity employer`),
	regexp.MustCompile(`(?i)without regard to race,? color,? religion`),
	regexp.MustCompile(`(?i)reasonable accommodation`),
	regexp.MustCompile(`(?i)privacy (notice|policy)`),
	regexp.MustCompile(`(?i)pay transparency`),
	regexp.MustCompile(`(?i)good faith (salary|compensation) range`),
	regexp.MustCompile(`(?i)unsolicited resumes? (from|submitted by) (recruiting|staffing)( or (recruiting|staffing))? agenc`),
	regexp.MustCompile(`(?i)we do not accept (unsolicited )?agency`),
}

// splitParagraphs splits on blank lines, trims, and drops anything under
// boilerplateMinParagraphLen — the same "min 40 characters" filter docs/08
// specifies for both the learning step and the stripping step.
func splitParagraphs(text string) []string {
	raw := regexp.MustCompile(`\n\s*\n`).Split(text, -1)
	paragraphs := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if len(p) >= boilerplateMinParagraphLen {
			paragraphs = append(paragraphs, p)
		}
	}
	return paragraphs
}

// normalizeParagraph is "hash each paragraph after whitespace
// normalization" — collapsing runs of whitespace so two paragraphs that
// differ only in line-wrapping still hash identically.
func normalizeParagraph(p string) string {
	return strings.Join(strings.Fields(p), " ")
}

func paragraphHash(p string) uint64 {
	sum := sha256.Sum256([]byte(normalizeParagraph(p)))
	return binary.BigEndian.Uint64(sum[:8])
}

// LearnBoilerplateHashes implements docs/08's per-company learning step:
// any paragraph hash appearing in >= 60% of postings is boilerplate. Fewer
// than coldStartMinPostings descriptions returns an empty set — the caller
// (StripDescription) falls back to global-pattern-only stripping for that
// case, per the doc's documented cold-start degradation.
//
// Computed on demand from a company's recent postings rather than cached
// in a stored column refreshed by a weekly batch job — docs/08 describes a
// weekly cron, but at this system's actual posting volume, recomputing
// per company each time Stage 2 runs costs low-single-digit milliseconds
// against already-fetched description text, and needs no scheduler this
// project doesn't otherwise have. The learned set itself is identical
// either way; this is a caching decision, not a scope cut.
func LearnBoilerplateHashes(descriptions []string) map[uint64]bool {
	if len(descriptions) < coldStartMinPostings {
		return nil
	}

	counts := make(map[uint64]int)
	for _, desc := range descriptions {
		seen := make(map[uint64]bool)
		for _, p := range splitParagraphs(desc) {
			h := paragraphHash(p)
			if !seen[h] {
				counts[h]++
				seen[h] = true
			}
		}
	}

	threshold := float64(len(descriptions)) * boilerplateThreshold
	hashes := make(map[uint64]bool)
	for h, count := range counts {
		if float64(count) >= threshold {
			hashes[h] = true
		}
	}
	return hashes
}

// StripDescription removes learned per-company boilerplate paragraphs
// (learnedHashes, nil for cold-start companies) and any paragraph matching
// a global EEO/legal pattern, then rejoins what remains. This is
// description_stripped — SimHash Gate 3's and, later, embeddings' input.
func StripDescription(text string, learnedHashes map[uint64]bool) string {
	paragraphs := splitParagraphs(text)
	kept := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		if learnedHashes != nil && learnedHashes[paragraphHash(p)] {
			continue
		}
		if matchesGlobalPattern(p) {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, "\n\n")
}

func matchesGlobalPattern(paragraph string) bool {
	for _, pattern := range globalBoilerplatePatterns {
		if pattern.MatchString(paragraph) {
			return true
		}
	}
	return false
}
