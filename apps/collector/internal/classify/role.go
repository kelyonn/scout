package classify

import (
	"regexp"

	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

// Confidence values for Tier 0's two match strengths. docs/07 section 4
// doesn't assign numbers to "strong" vs "weak" — these are this package's
// own scale, documented here rather than left implicit.
const (
	strongConfidence float32 = 0.90
	weakConfidence   float32 = 0.60
)

// RoleResult is Tier 0 role classification's output.
type RoleResult struct {
	Family     schema.RoleFamily
	Confidence float32
	IsSoftware bool
	// NeedsTier2Escalation is true when a strong/weak title match landed on
	// a requires:technical_evidence family (advocacy.*) but the
	// description-level gate failed — docs/07 section 4's own "~25%
	// escalation versus the 8% baseline" for this family specifically. The
	// title isn't admitted as that family here (Family/Confidence/IsSoftware
	// are the same zero-value swe.other fallback as an outright non-match),
	// but the caller (ingest.go) enqueues a Tier 2 LLM job rather than
	// treating it as a plain unresolved title, since a real technical
	// signal the regex gate missed is common enough to be worth a second,
	// smarter look.
	NeedsTier2Escalation bool
}

// ClassifyRole implements docs/07 section 4's Tier 0 pattern-rule tier:
// strong patterns checked first across all families in file order (more
// specific families before the swe.general catch-all), falling back to weak
// patterns only if no family strong-matched — matching roles.yaml's own
// documented priority. A negative pattern vetoes that family regardless of
// an otherwise-matching strong or weak pattern.
//
// descriptionText and skillOntology exist only for the
// requires:technical_evidence gate (advocacy.* families) — every swe.*
// family ignores them entirely.
func ClassifyRole(normalizedTitle, descriptionText string, families []taxonomy.RoleFamily, skillOntology []taxonomy.Skill) RoleResult {
	if result, matched := classifyPass(normalizedTitle, descriptionText, families, skillOntology, strongConfidence, true); matched {
		return result
	}
	if result, matched := classifyPass(normalizedTitle, descriptionText, families, skillOntology, weakConfidence, false); matched {
		return result
	}
	return RoleResult{Family: schema.RoleSWEOther, Confidence: 0, IsSoftware: false}
}

func classifyPass(
	normalizedTitle, descriptionText string, families []taxonomy.RoleFamily, skillOntology []taxonomy.Skill,
	confidence float32, strong bool,
) (RoleResult, bool) {
	for _, f := range families {
		if anyMatch(f.Negative, normalizedTitle) {
			continue
		}
		patterns := f.Weak
		if strong {
			patterns = f.Strong
		}
		if !anyMatch(patterns, normalizedTitle) {
			continue
		}
		if f.RequiresTechnicalEvidence && !HasTechnicalEvidence(descriptionText, skillOntology) {
			return RoleResult{Family: schema.RoleSWEOther, Confidence: 0, IsSoftware: false, NeedsTier2Escalation: true}, true
		}
		return RoleResult{Family: schema.RoleFamily(f.FamilyName), Confidence: confidence, IsSoftware: true}, true
	}
	return RoleResult{}, false
}

func anyMatch(patterns []*regexp.Regexp, text string) bool {
	for _, p := range patterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}
