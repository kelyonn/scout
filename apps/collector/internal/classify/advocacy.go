// The advocacy.* technical-evidence gate — docs/07-normalization-taxonomy.md
// section 4's "The advocacy family", Hazard 1/2: "requires:
// technical_evidence is a gate unique to this family: the description must
// contain a concrete technical signal — a named language, SDK, API, or
// 'write code' phrasing — before the role is admitted."
package classify

import (
	"regexp"

	"github.com/kelyon/scout/packages/skills"
	"github.com/kelyon/scout/packages/taxonomy"
)

// writeCodePhrasing covers the doc's own "'write code' phrasing" example
// plus the handful of equivalent ways a posting states the same thing —
// not an exhaustive list, since the named-language/SDK/API check (the
// skill ontology match below) is what carries most of the gate's actual
// coverage.
var writeCodePhrasing = regexp.MustCompile(
	`(?i)\bwrit(?:e|ing) code\b|\bship(?:ping)? code\b|\bcode samples?\b|\bsample (?:code|application)s?\b|\bpull requests?\b|\bopen[\s-]source contributions?\b`,
)

// HasTechnicalEvidence implements the gate: true if text names a concrete
// skill from the ontology (a language, SDK, framework, or tool — reusing
// packages/taxonomy/skills.yaml's own alias vocabulary rather than a
// second hand-written list, per this pass's own scope decision) or
// contains "write code"-equivalent phrasing.
func HasTechnicalEvidence(text string, skillOntology []taxonomy.Skill) bool {
	if writeCodePhrasing.MatchString(text) {
		return true
	}
	return len(skills.Extract(text, skillOntology)) > 0
}
