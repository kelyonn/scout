// learning_opportunity — docs/09-ranking-scoring.md section 3.5,
// SCOUT-RANK-006. Every term is regex/keyword over description text or a
// comparison against the user's own skill levels, the same pattern
// skill_match already uses — no new data source needed. The "team size
// 3-15" term (+15 of the doc's 100-point scale) is dropped: postings
// essentially never state team size, so it would be dead weight in every
// real computation rather than a genuine signal some jobs have and others
// don't. The remaining 85 points are rescaled to a 0-100 scale.
package scoring

import (
	"math"
	"regexp"
	"strings"
)

const learningOpportunityMaxRawPoints = 85.0

var (
	mentorshipPattern = regexp.MustCompile(
		`(?i)\bmentor(?:ship)?\b|\bpair(?:ed|ing)?\s+program|\bcode review|\bonboarding buddy`,
	)
	openSourcePattern = regexp.MustCompile(
		`(?i)\bopen[\s-]source\b.{0,40}\bcontribut|\bcontribut\w*.{0,40}\bopen[\s-]source\b`,
	)
	internOwnershipPattern = regexp.MustCompile(
		`(?i)\bown(?:ership)?\b.{0,30}\bproject\b|\byour own project\b|\bintern project\b|\bship\s+(?:a|your)\s+(?:project|feature)\b`,
	)
	deepDomainPattern = regexp.MustCompile(
		`(?i)\bdistributed systems?\b|\bcompilers?\b|\bml infra(?:structure)?\b|\bmachine learning infra|\bdatabase internals\b|\bnetworking\b|\boperating systems?\b|\bkernel\b|\bconsensus\b`,
	)
)

// computeLearningOpportunity implements docs/09 section 3.5's formula
// minus the team-size term. The unfamiliar-technology term is deliberately
// capped, per the doc's own reasoning: "maximum learning score comes from
// a role requiring roughly 30-50% unfamiliar technology... beyond that the
// candidate will not be hired, which is not a learning opportunity."
func computeLearningOpportunity(jobSkills []string, userLevels map[string]int, descriptionText string) (int, map[string]any) {
	var raw float64
	inputs := map[string]any{}

	unfamiliarFraction, unfamiliarCapped := unfamiliarTechFraction(jobSkills, userLevels)
	unfamiliarPoints := 25.0 * unfamiliarFraction
	raw += unfamiliarPoints
	inputs["unfamiliar_tech_fraction"] = unfamiliarFraction
	inputs["unfamiliar_tech_capped"] = unfamiliarCapped

	hasDeepDomain := deepDomainPattern.MatchString(descriptionText)
	if hasDeepDomain {
		raw += 20
	}
	inputs["deep_domain_signal"] = hasDeepDomain

	hasMentorship := mentorshipPattern.MatchString(descriptionText)
	if hasMentorship {
		raw += 20
	}
	inputs["mentorship_signal"] = hasMentorship

	hasOpenSource := openSourcePattern.MatchString(descriptionText)
	if hasOpenSource {
		raw += 10
	}
	inputs["open_source_signal"] = hasOpenSource

	hasOwnership := internOwnershipPattern.MatchString(descriptionText)
	if hasOwnership {
		raw += 10
	}
	inputs["project_ownership_signal"] = hasOwnership

	score := int(math.Round(100 * raw / learningOpportunityMaxRawPoints))
	return score, inputs
}

// unfamiliarPeakFraction is where docs/09 section 3.5's first term peaks —
// "roughly 30-50% unfamiliar technology", taken as its midpoint.
const unfamiliarPeakFraction = 0.4

// unfamiliarTechFraction is docs/09 section 3.5's capped first term,
// returning a 0-1 credit that *peaks* at unfamiliarPeakFraction rather
// than simply capping and holding: the doc is explicit that both
// extremes score low — "a job requiring nothing you know is not a
// learning opportunity, it is a rejection" at 0% unfamiliar, and "the
// candidate will not be hired" at 100% unfamiliar, which is also not a
// learning opportunity in practice. A triangular credit around the peak
// is the simplest curve matching both stated endpoints.
func unfamiliarTechFraction(jobSkills []string, userLevels map[string]int) (credit float64, capped bool) {
	if len(jobSkills) == 0 {
		return 0, false
	}
	unfamiliar := 0
	for _, s := range jobSkills {
		if userLevels[strings.ToLower(s)] == 0 {
			unfamiliar++
		}
	}
	raw := float64(unfamiliar) / float64(len(jobSkills))

	// Piecewise linear, not a single symmetric triangle: the peak sits at
	// 0.4, not at the midpoint of [0,1], so a single shared slope would
	// under-penalize one side. Each side gets its own slope so credit
	// reaches exactly 0 at both raw=0 ("nothing you know... a rejection")
	// and raw=1 ("will not be hired"), and exactly 1 at the peak.
	if raw <= unfamiliarPeakFraction {
		credit = raw / unfamiliarPeakFraction
	} else {
		credit = 1 - (raw-unfamiliarPeakFraction)/(1-unfamiliarPeakFraction)
	}
	if credit < 0 {
		credit = 0
	}
	return credit, raw > unfamiliarPeakFraction
}
