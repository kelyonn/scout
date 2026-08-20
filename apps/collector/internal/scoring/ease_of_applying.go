// ease_of_applying — docs/09-ranking-scoring.md section 3.10,
// SCOUT-RANK-011. "Inferred from the ATS platform... plus description
// language." Base score comes from the ATS platform (all three this
// project has adapters for are short-form, unlike the doc's own Workday
// counter-example, which needs no adapter this pass has), then description
// language can only ever make the application harder to complete, never
// easier — a platform's own form is the ceiling.
package scoring

import "regexp"

const (
	easeBaseShortForm   = 85 // Greenhouse, Lever: the doc's own named short-form platforms
	easeBaseATSStandard = 70 // Ashby and anything else: resume upload + basic fields, no evidence either way
	easeCoverLetterCap  = 40
	easeAssessmentCap   = 25
)

var (
	coverLetterRequiredPattern = regexp.MustCompile(
		`(?i)(?:required?|must (?:include|submit)|please (?:include|attach|submit)).{0,20}cover letter|cover letter.{0,20}(?:required|mandatory)`,
	)
	assessmentRequiredPattern = regexp.MustCompile(
		`(?i)\btake[\s-]home\b|\bcoding (?:challenge|test|assessment|exercise)\b|\btechnical assessment\b|\bonline assessment\b|\bcomplete an assessment\b`,
	)
)

// computeEaseOfApplying implements docs/09 section 3.10's table as a
// platform baseline capped downward by description language — the two
// harder-to-detect middle rows ("short form, account required" / "3-6
// custom questions") aren't distinguishable from adapter data alone, so
// the platform baseline sits at the closest row this pass can actually
// justify, and only the two clearest, most consequential downgrades
// (a required cover letter, an upfront assessment) adjust it further.
func computeEaseOfApplying(atsPlatform, descriptionText string) (int, map[string]any) {
	base := easeBaseATSStandard
	switch atsPlatform {
	case "ats_greenhouse", "ats_lever":
		base = easeBaseShortForm
	}

	score := base
	requiresCoverLetter := coverLetterRequiredPattern.MatchString(descriptionText)
	requiresAssessment := assessmentRequiredPattern.MatchString(descriptionText)

	if requiresCoverLetter && score > easeCoverLetterCap {
		score = easeCoverLetterCap
	}
	// An upfront assessment is the doc's own more severe row (25 vs 40),
	// and independent of whether a cover letter is also asked for.
	if requiresAssessment && score > easeAssessmentCap {
		score = easeAssessmentCap
	}

	return score, map[string]any{
		"ats_platform":          atsPlatform,
		"requires_cover_letter": requiresCoverLetter,
		"requires_assessment":   requiresAssessment,
	}
}
