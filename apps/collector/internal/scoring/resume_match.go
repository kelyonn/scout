// resume_match — docs/09-ranking-scoring.md section 3.2, SCOUT-RANK-003.
// Ships in this pass despite a migration comment elsewhere marking it P3
// — see the plan's own scope decision on why that blocker doesn't apply
// to a single-user system with the resume text already in hand. cosine
// comes from packages/db/queries/score.sql's SelectResumeJobCosine
// (pgvector, computed once per job); keyword reuses
// packages/skills.Extract against the resume's own raw
// text, the same ontology-matching skill_match already uses.
package scoring

import (
	"math"

	"github.com/kelyon/scout/packages/taxonomy"
)

const (
	resumeMatchSemanticWeight = 0.55
	resumeMatchKeywordWeight  = 0.35
	resumeMatchBaseWeight     = 0.10

	// cosineFloor/cosineCeiling are docs/09's own stated "practical range
	// for related documents" — raw cosine never approaches 0 or 1 for real
	// text, so using it directly would compress the whole score into a
	// narrow band.
	cosineFloor   = 0.55
	cosineCeiling = 0.95
)

// computeResumeMatch implements docs/09 section 3.2's formula. hasCosine
// false means at least one embedding doesn't exist yet (SelectResumeJobCosine
// returned no rows) — resume_match falls back to the keyword term alone
// rather than the full formula, since a missing semantic term shouldn't
// silently zero out a job the keyword match clearly fits. recency is
// always 1.0 here: this single-user system's resume is current by
// construction (seeded from the user's own up-to-date resume text, not
// something that goes stale between scoring runs the way a real
// "relevant experience in the last 12 months" check would need date
// parsing to detect) — a documented simplification, not a computed value.
func computeResumeMatch(
	hasCosine bool, cosine float64, jobSkills []string, resumeText string, skillOntology []taxonomy.Skill,
) (int, map[string]any) {
	const recency = 1.0

	keyword := keywordFraction(jobSkills, resumeText, skillOntology)

	if !hasCosine {
		// Renormalize over just the keyword term, same pattern weighted()
		// already uses elsewhere: divide by the weight of what's actually
		// present rather than assuming the missing term contributes 0.
		score := 100 * keyword * recency
		return int(math.Round(clamp(score, 0, 100))), map[string]any{
			"keyword": keyword, "semantic_available": false, "recency": recency,
		}
	}

	semanticNorm := clamp((cosine-cosineFloor)/(cosineCeiling-cosineFloor), 0, 1)
	score := 100 * (resumeMatchSemanticWeight*semanticNorm + resumeMatchKeywordWeight*keyword + resumeMatchBaseWeight) * recency
	return int(math.Round(clamp(score, 0, 100))), map[string]any{
		"cosine": cosine, "semantic_norm": semanticNorm, "keyword": keyword,
		"semantic_available": true, "recency": recency,
	}
}

// keywordFraction is docs/09's "fraction of the job's required skills
// appearing in the resume text" — matched via the same ontology
// word-boundary aliasing skill_match uses, not a raw substring check.
func keywordFraction(jobSkills []string, resumeText string, skillOntology []taxonomy.Skill) float64 {
	if len(jobSkills) == 0 {
		return 0
	}
	resumeSkillSet := make(map[string]bool)
	for _, s := range skillOntology {
		if s.Matches(resumeText) {
			resumeSkillSet[s.ID] = true
		}
	}
	present := 0
	for _, s := range jobSkills {
		if resumeSkillSet[s] {
			present++
		}
	}
	return float64(present) / float64(len(jobSkills))
}
