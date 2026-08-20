// Package skills extracts ontology-matched skills from free text — docs/07
// section 8's dictionary-first pipeline, minus section-weighting ("skills
// in Requirements count double those in Nice to have") and the `implies`
// graph, both explicitly cut for P1 (see packages/taxonomy/skills.yaml's
// header comment). Every match is treated as w(s) = 1.0 in the skill_match
// formula (docs/09 section 3.1) — a documented simplification, not an
// omission.
//
// Lives in packages/, not apps/collector/internal/, because it has two
// callers on opposite sides of Go's internal-import boundary:
// apps/collector (job postings, via classify/scoring) and apps/api (resume
// text, via the resume upload handler) — the same extraction logic for
// both, not a second implementation apps/api would otherwise need.
package skills

import "github.com/kelyon/scout/packages/taxonomy"

// Extract returns the ontology IDs of every skill whose alias appears in
// text, in taxonomy.LoadSkills' declared order.
func Extract(text string, ontology []taxonomy.Skill) []string {
	var found []string
	for _, s := range ontology {
		if s.Matches(text) {
			found = append(found, s.ID)
		}
	}
	return found
}
