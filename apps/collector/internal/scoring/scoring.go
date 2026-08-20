// Package scoring implements the subset of docs/09-ranking-scoring.md that
// P1 can honestly compute: skill_match (section 3.1), overall_match
// (section 3.12, using Tier 0's role_family/seniority for its
// role_family_fit/seniority_fit terms), deadline_urgency (section 3.11),
// and priority (section 3.13) with the real location and freshness
// multipliers. The other eight subscores — company_quality, compensation,
// learning_opportunity, engineering_culture, growth_potential,
// interview_probability, competition_estimate, ease_of_applying — need data
// this pass doesn't have (a company registry, comparables, public
// engineering signals) and are written as a neutral 50, flagged in
// ScoreInputs so P2 knows exactly which fields are real. Weights are always
// read from the active weight_version row (docs/09 section 4: "Weights ...
// are never hardcoded"), never inlined here.
package scoring

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

// placeholderScore is the neutral value for a subscore P1 cannot compute.
// Neutral, not zero — docs/07 section 4's "unknown is a permitted value and
// is scored neutrally" principle for company_type applies just as much
// here: a subscore we haven't computed must not read as "computed and bad."
const placeholderScore = 50

// Weights is the shape of weight_version.weights — docs/09 section 4's
// literal example, minus the version/source wrapper (those are separate
// columns; see infra/seed/seed.sql).
type Weights struct {
	Priority              map[string]float64 `json:"priority"`
	OverallMatch          map[string]float64 `json:"overall_match"`
	LocationMultipliers   map[string]float64 `json:"location_multipliers"`
	FreshnessHalfLifeDays float64            `json:"freshness_half_life_days"`
	FreshnessFloor        float64            `json:"freshness_floor"`
}

// ParseWeights unmarshals the weight_version.weights JSONB column.
func ParseWeights(raw []byte) (Weights, error) {
	var w Weights
	if err := json.Unmarshal(raw, &w); err != nil {
		return Weights{}, fmt.Errorf("scoring: parse weights: %w", err)
	}
	return w, nil
}

// UserProfile is the subset of user_profile scoring needs.
type UserProfile struct {
	TargetRoles     []schema.RoleFamily
	TargetSeniority []schema.Seniority
	SkillLevels     map[string]int // ontology id -> 0-5, docs/09 section 3.1
}

// ExternalInputs bundles every value Compute needs that isn't already on
// schema.NormalizedJob/UserProfile because it comes from a DB query
// (resume/job embedding cosine, compensation comparables) or a taxonomy
// file (the skill ontology, the company registry's well_known flag) —
// scoring.go itself stays a pure function of its arguments, with callers
// (apps/collector/internal/scheduler/ingest.go) doing the actual I/O.
// Zero-value fields (HasResumeCosine/HasCompensationComparables both
// false, CompanyWellKnown nil) are exactly the "unknown, don't guess"
// state each affected subscore already handles explicitly.
type ExternalInputs struct {
	SkillOntology []taxonomy.Skill

	HasResumeCosine bool
	ResumeCosine    float64
	ResumeText      string

	JobHasCompensation          bool // job.comp_normalized_inr_month is not NULL
	CompensationComparableCount int
	CompensationAtOrBelowCount  int

	CompanyWellKnown *bool
}

// Result is one job_score row's worth of computed values, ready to insert.
type Result struct {
	OverallMatch, SkillMatch, ResumeMatch, CompanyQuality, Compensation,
	LearningOpportunity, EngineeringCulture, GrowthPotential,
	InterviewProbability, CompetitionEstimate, EaseOfApplying,
	DeadlineUrgency, Priority int16
	LocationMultiplier, FreshnessMultiplier float32
	ScoreInputs                             map[string]any
}

// Compute implements docs/09 sections 3.1-3.5, 3.9-3.13 — every subscore
// P1 or this pass can honestly populate. now is passed in rather than read
// from time.Now() so freshness (and competition_estimate's discovery-
// latency term) are reproducible in tests.
func Compute(job schema.NormalizedJob, user UserProfile, w Weights, ext ExternalInputs, now time.Time) Result {
	skillMatch, skillInputs := computeSkillMatch(job.Skills, user.SkillLevels)
	roleFamilyFit := fitScore(containsRole(user.TargetRoles, job.RoleFamily))
	seniorityFit := fitScore(containsSeniority(user.TargetSeniority, job.Seniority))
	resumeMatch, resumeInputs := computeResumeMatch(
		ext.HasResumeCosine, ext.ResumeCosine, job.Skills, ext.ResumeText, ext.SkillOntology,
	)

	// resume_match is only included once real resume data exists in some
	// form (an embedding, or resume text to keyword-match against) —
	// otherwise computeResumeMatch's own keyword-fraction-of-empty-text
	// falls straight to 0, and including a hard 0 in this weighted mean
	// (rather than omitting it) would be exactly the placeholder-drags-
	// everything-down bug the renormalization fix exists to prevent, just
	// with a new subscore. hasResumeData mirrors that same omit-don't-
	// substitute rule ExternalInputs' own comment describes for
	// JobHasCompensation below.
	overallMatchValues := map[string]float64{
		"skill_match":     float64(skillMatch),
		"role_family_fit": float64(roleFamilyFit),
		"seniority_fit":   float64(seniorityFit),
	}
	hasResumeData := ext.HasResumeCosine || ext.ResumeText != ""
	if hasResumeData {
		overallMatchValues["resume_match"] = float64(resumeMatch)
	}
	overallMatch := weighted(w.OverallMatch, overallMatchValues)

	deadlineUrgency, deadlineConfidenceLow := computeDeadlineUrgency(job.DeadlineAt, now)
	companyQuality, companyQualityInputs := computeCompanyQuality(job.RoleFamily, job.DescriptionText)
	compensation, compensationInputs := computeCompensation(
		ext.JobHasCompensation, ext.CompensationComparableCount, ext.CompensationAtOrBelowCount,
	)
	learningOpportunity, learningInputs := computeLearningOpportunity(job.Skills, user.SkillLevels, job.DescriptionText)
	easeOfApplying, easeInputs := computeEaseOfApplying(job.ATSPlatform, job.DescriptionText)
	competitionEstimate, competitionInputs := computeCompetitionEstimate(job, ext.CompanyWellKnown, now)

	// engineering_culture, growth_potential, and interview_probability stay
	// absent from this map — genuinely blocked on external data sources
	// (GitHub/blog data, funding/12-month posting history, 200 labelled
	// interview outcomes) this pass has no way to acquire. compensation is
	// conditionally absent too: computeCompensation's placeholderScore
	// return means "unknown," and including that 50 here unconditionally
	// would reintroduce the exact P1 ceiling bug
	// TestPriority_PerfectBengaluruInternshipClearsBengaluruMatchThreshold
	// guards against, just for a new subscore instead of all eight.
	// weighted() renormalizes over exactly the keys actually present below,
	// removing an omitted key's weight from the denominator too, rather
	// than a placeholder silently pulling every score toward 50.
	priorityValues := map[string]float64{
		"overall_match":        overallMatch,
		"deadline_urgency":     float64(deadlineUrgency),
		"company_quality":      float64(companyQuality),
		"learning_opportunity": float64(learningOpportunity),
		"ease_of_applying":     float64(easeOfApplying),
		"competition_estimate": float64(competitionEstimate),
	}
	if !compensationIsPlaceholder(compensationInputs) {
		priorityValues["compensation"] = float64(compensation)
	}
	base := weighted(w.Priority, priorityValues)

	locationMultiplier := locationMultiplierFor(job.LocationTier, w.LocationMultipliers)
	freshnessMultiplier, freshnessInputs := computeFreshness(job.PostedAt, job.PostedAtEstimated, w, now)

	priority := clamp(base*float64(locationMultiplier)*float64(freshnessMultiplier), 0, 100)

	inputs := map[string]any{
		"skill_match":          skillInputs,
		"role_family_fit":      map[string]any{"role_family": job.RoleFamily, "in_target_roles": roleFamilyFit == 100},
		"seniority_fit":        map[string]any{"seniority": job.Seniority, "in_target_seniority": seniorityFit == 100},
		"resume_match":         resumeInputs,
		"deadline_urgency":     map[string]any{"confidence_low": deadlineConfidenceLow},
		"company_quality":      companyQualityInputs,
		"compensation":         compensationInputs,
		"learning_opportunity": learningInputs,
		"ease_of_applying":     easeInputs,
		"competition_estimate": competitionInputs,
		"freshness":            freshnessInputs,
		"location_tier":        job.LocationTier,
		"placeholder_fields": []string{
			"engineering_culture", "growth_potential", "interview_probability",
		},
	}

	return Result{
		OverallMatch:         int16(math.Round(overallMatch)),
		SkillMatch:           int16(skillMatch),          //nolint:gosec // skillMatch is math.Round(100*coverage), bounded to [0,100]
		ResumeMatch:          int16(resumeMatch),         //nolint:gosec // computeResumeMatch clamps to [0,100]
		CompanyQuality:       int16(companyQuality),      //nolint:gosec // weighted average of subweights each in [40,100]
		Compensation:         int16(compensation),        //nolint:gosec // math.Round(100*percentile), bounded to [0,100]
		LearningOpportunity:  int16(learningOpportunity), //nolint:gosec // 100*raw/maxRawPoints, bounded to [0,100]
		EngineeringCulture:   placeholderScore,
		GrowthPotential:      placeholderScore,
		InterviewProbability: placeholderScore,
		CompetitionEstimate:  int16(competitionEstimate), //nolint:gosec // computeCompetitionEstimate clamps to [0,100]
		EaseOfApplying:       int16(easeOfApplying),      //nolint:gosec // starts at a small baseline, only ever capped downward
		DeadlineUrgency:      int16(deadlineUrgency),     //nolint:gosec // one of computeDeadlineUrgency's fixed constants, all within [0,100]
		Priority:             int16(math.Round(priority)),
		LocationMultiplier:   float32(locationMultiplier),
		FreshnessMultiplier:  float32(freshnessMultiplier),
		ScoreInputs:          inputs,
	}
}

// computeSkillMatch implements docs/09 section 3.1's formula with the
// simplifications packages/taxonomy/skills.yaml documents: every job skill
// has requirement weight w(s) = 1.0 (no required/preferred/mentioned
// distinction), and there is no `implied` credit — coverage(s) is
// user_level(s)/5 directly, 0 if the user doesn't list the skill at all. An
// empty job skill list has nothing to score against, so it returns the
// neutral placeholder rather than a manufactured 0 or 100.
func computeSkillMatch(jobSkills []string, userLevels map[string]int) (int, map[string]any) {
	if len(jobSkills) == 0 {
		return placeholderScore, map[string]any{"placeholder": true, "reason": "no skills extracted"}
	}

	var sum float64
	missing := make([]string, 0)
	for _, s := range jobSkills {
		level := userLevels[s]
		coverage := float64(level) / 5.0
		sum += coverage
		if level == 0 {
			missing = append(missing, s)
		}
	}
	match := 100 * sum / float64(len(jobSkills))
	return int(math.Round(match)), map[string]any{
		"job_skills":     jobSkills,
		"missing_skills": missing,
	}
}

// fitScore is the "100 if in target, 20 otherwise" simplification of
// docs/09 section 3.12's role_family_fit ("100 if in target_roles, 60 if
// adjacent, 20 otherwise") and seniority_fit terms — the "adjacent" tier
// needs a role-similarity model this pass doesn't have.
func fitScore(inTarget bool) int {
	if inTarget {
		return 100
	}
	return 20
}

func containsRole(roles []schema.RoleFamily, target schema.RoleFamily) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

func containsSeniority(levels []schema.Seniority, target schema.Seniority) bool {
	for _, s := range levels {
		if s == target {
			return true
		}
	}
	return false
}

// computeDeadlineUrgency implements docs/09 section 3.11. deadline_at is
// almost always unknown for P1's sources (no adapter extracts one yet), so
// the "damped toward 40" branch is the common case.
func computeDeadlineUrgency(deadlineAt *time.Time, now time.Time) (score int, confidenceLow bool) {
	if deadlineAt == nil {
		return 40, true
	}
	daysLeft := deadlineAt.Sub(now).Hours() / 24
	switch {
	case daysLeft < 0:
		return 0, false
	case daysLeft < 3:
		return 100, false
	case daysLeft < 7:
		return 85, false
	case daysLeft < 14:
		return 65, false
	case daysLeft < 30:
		return 40, false
	default:
		return 20, false
	}
}

// locationMultiplierFor reads the multiplier for job.LocationTier from the
// active weight_version. Tier 0 (unresolved — apps/collector/internal/normalize
// couldn't place the location against the gazetteer) gets a neutral 1.0,
// not a value from docs/07 section 6's table, since that table has no
// "unknown" row.
func locationMultiplierFor(tier int16, multipliers map[string]float64) float64 {
	if tier == 0 {
		return 1.0
	}
	if m, ok := multipliers[itoa(tier)]; ok {
		return m
	}
	return 1.0
}

// computeFreshness implements docs/09 section 3.13's exponential-decay
// formula. A nil PostedAt (docs/07 section 9's "still unknown" case, which
// P1's Greenhouse-only pipeline shouldn't actually hit since the ATS always
// supplies a posted date) is treated as one half-life old rather than
// freshly-posted — the damping docs/07 section 9 calls for when falling
// back to first-observation time, approximated here since NormalizedJob
// doesn't carry a first-observation timestamp of its own.
func computeFreshness(postedAt *time.Time, estimated bool, w Weights, now time.Time) (float64, map[string]any) {
	var ageDays float64
	if postedAt == nil {
		ageDays = w.FreshnessHalfLifeDays
	} else {
		ageDays = now.Sub(*postedAt).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
	}
	multiplier := w.FreshnessFloor + (1-w.FreshnessFloor)*math.Exp(-ageDays*math.Ln2/w.FreshnessHalfLifeDays)
	return multiplier, map[string]any{"age_days": ageDays, "posted_at_estimated": estimated}
}

// weighted computes a renormalised weighted mean over exactly the keys
// present in values: for each (key, v) in values with a matching weight,
// it accumulates w*v and w, then divides by the summed weight rather than
// assuming the weights in the active weight_version already total 1.0.
// This is what lets P1 omit subscores it cannot honestly compute (by
// leaving them out of values entirely, not by passing a neutral
// placeholder into this function) without every score being dragged
// toward the placeholder value — see the callers in Compute for exactly
// which keys that applies to today, and the docs/09 section 4 weight
// table for the full set this converges toward as more subscores land.
func weighted(weights map[string]float64, values map[string]float64) float64 {
	var sum, totalWeight float64
	for key, v := range values {
		w, ok := weights[key]
		if !ok {
			continue
		}
		sum += w * v
		totalWeight += w
	}
	if totalWeight == 0 {
		return 0
	}
	return sum / totalWeight
}

// compensationIsPlaceholder reads computeCompensation's own confidence_low
// flag — true exactly when compensation returned placeholderScore (no
// comp_normalized_inr_month, or fewer than 20 comparables), the signal
// Compute uses to omit it from the priority weighted mean rather than
// include a 50 unconditionally.
func compensationIsPlaceholder(inputs map[string]any) bool {
	confidenceLow, _ := inputs["confidence_low"].(bool)
	return confidenceLow
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func itoa(v int16) string {
	return fmt.Sprintf("%d", v)
}
