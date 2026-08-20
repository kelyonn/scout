// competition_estimate (partial) — docs/09-ranking-scoring.md section 3.9,
// SCOUT-RANK-010. "Higher score means less competition." Every term this
// pass can compute from data already in hand is implemented; three of the
// doc's terms are skipped outright rather than guessed at:
// "listed on a major aggregator" (this project tracks no aggregators to
// check against), "mass campus drive" (no reliable signal to detect it
// from a single posting), and "GCC/enterprise on Workday/SuccessFactors"
// (no adapter for either platform exists). Unlike company_quality, this
// is not renormalized through weighted() — docs/09's own formula is a
// flat base+adjustments sum, not a weighted mean of independent terms, so
// the implementation mirrors that shape directly.
package scoring

import (
	"regexp"
	"time"

	"github.com/kelyon/scout/packages/schema"
)

const competitionBase = 50.0

// rareSkillPattern is docs/09's own named examples: "Rust, Erlang, formal
// methods, kernel, compilers" — a specific, less-common skill that
// shrinks the applicant pool.
var rareSkillPattern = regexp.MustCompile(
	`(?i)\brust\b|\berlang\b|\bformal methods?\b|\bkernel\b|\bcompilers?\b|\bhaskell\b|\bocaml\b`,
)

// computeCompetitionEstimate implements the computable subset of docs/09
// section 3.9's formula. wellKnown comes from
// packages/taxonomy/companies.yaml (the brand-recognition proxy this
// pass has, in place of live GitHub-stars/press-mention data) — absent
// (company not in the registry) is treated as neutral, contributing
// neither the -25 nor the +20 term, rather than guessing which way an
// unclassified company should lean.
func computeCompetitionEstimate(
	job schema.NormalizedJob, wellKnown *bool, now time.Time,
) (int, map[string]any) {
	score := competitionBase
	inputs := map[string]any{}

	if wellKnown != nil {
		if *wellKnown {
			score -= 25
		} else {
			score += 20
		}
	}
	inputs["well_known"] = wellKnown

	postedDaysAgo := -1.0
	if job.PostedAt != nil {
		postedDaysAgo = now.Sub(*job.PostedAt).Hours() / 24
		if postedDaysAgo > 7 {
			score -= 20
		}
		if postedDaysAgo >= 0 && postedDaysAgo < 2.0/24 {
			score += 15
		}
	}
	inputs["posted_days_ago"] = postedDaysAgo

	isFullyRemoteNoRestriction := job.WorkMode == schema.WorkRemote && job.LocationCountry == ""
	if isFullyRemoteNoRestriction {
		score -= 10
	}
	inputs["fully_remote_no_restriction"] = isFullyRemoteNoRestriction

	hasRareSkill := rareSkillPattern.MatchString(job.DescriptionText) || skillListHasRareSkill(job.Skills)
	if hasRareSkill {
		score += 15
	}
	inputs["rare_skill_signal"] = hasRareSkill

	// "Posted only to the company's own ATS, not aggregated" — always true
	// for this pipeline today (Greenhouse/Lever/Ashby direct only, no
	// aggregator source exists), so this is a constant rather than a
	// conditional, and documented as such rather than silently included.
	score += 10
	inputs["ats_native_only"] = true

	onsiteInTopTier := (job.WorkMode == schema.WorkOnsite || job.WorkMode == schema.WorkHybrid) &&
		job.LocationTier >= 1 && job.LocationTier <= 2
	if onsiteInTopTier {
		score += 10
	}
	inputs["onsite_top_tier"] = onsiteInTopTier

	isAdvocacy := job.RoleFamily == schema.RoleAdvocacyDevRel ||
		job.RoleFamily == schema.RoleAdvocacyDevEx ||
		job.RoleFamily == schema.RoleAdvocacySolutions
	if isAdvocacy {
		score += 10
	}
	inputs["advocacy_role"] = isAdvocacy

	inputs["partial"] = true
	inputs["missing_terms"] = []string{"major_aggregator_listing", "mass_campus_drive", "gcc_workday_portal"}

	return int(clamp(score, 0, 100)), inputs
}

func skillListHasRareSkill(skills []string) bool {
	for _, s := range skills {
		switch s {
		case "rust", "erlang", "haskell", "compilers":
			return true
		}
	}
	return false
}
