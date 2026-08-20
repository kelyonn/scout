package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/schema"
)

func testWeights() Weights {
	return Weights{
		Priority: map[string]float64{
			"overall_match": 0.24, "company_quality": 0.14, "learning_opportunity": 0.12,
			"interview_probability": 0.10, "competition_estimate": 0.10, "compensation": 0.09,
			"growth_potential": 0.07, "engineering_culture": 0.06, "ease_of_applying": 0.05,
			"deadline_urgency": 0.03,
		},
		OverallMatch: map[string]float64{
			"skill_match": 0.40, "resume_match": 0.30, "role_family_fit": 0.20, "seniority_fit": 0.10,
		},
		LocationMultipliers:   map[string]float64{"1": 1.20, "2": 1.05, "3": 1.12, "4": 0.90},
		FreshnessHalfLifeDays: 14,
		FreshnessFloor:        0.55,
	}
}

func TestParseWeights(t *testing.T) {
	raw := []byte(`{"priority":{"overall_match":0.24},"overall_match":{"skill_match":0.4},"location_multipliers":{"1":1.2},"freshness_half_life_days":14,"freshness_floor":0.55}`)
	w, err := ParseWeights(raw)
	if err != nil {
		t.Fatalf("ParseWeights: %v", err)
	}
	if w.Priority["overall_match"] != 0.24 {
		t.Errorf("Priority[overall_match] = %v, want 0.24", w.Priority["overall_match"])
	}
	if w.FreshnessHalfLifeDays != 14 {
		t.Errorf("FreshnessHalfLifeDays = %v, want 14", w.FreshnessHalfLifeDays)
	}
}

// TestBengaluruAlwaysRanksHigher is docs/09 section 3.13's own property
// test: "There is a property-based test asserting exactly this over the
// full base range." Two otherwise-identical jobs differing only in
// location tier — Bengaluru must outrank the rest of India at every base
// score.
func TestBengaluruAlwaysRanksHigher(t *testing.T) {
	w := testWeights()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	posted := now // fresh, so freshness_multiplier is 1.0 for both

	user := UserProfile{
		TargetRoles:     []schema.RoleFamily{schema.RoleSWEBackend},
		TargetSeniority: []schema.Seniority{schema.SeniorityInternship},
		SkillLevels:     map[string]int{"go": 5},
	}

	baseJob := schema.NormalizedJob{
		RoleFamily: schema.RoleSWEBackend,
		Seniority:  schema.SeniorityInternship,
		PostedAt:   &posted,
	}

	// Vary the base score via skill overlap (0, 1, or 2 of 2 job skills
	// matched) so the property is checked across several base values, not
	// just one — "over the full base range" per docs/09 section 3.13.
	skillSets := [][]string{nil, {"go"}, {"go", "python"}}
	for _, skills := range skillSets {
		bengaluru := baseJob
		bengaluru.Skills = skills
		bengaluru.LocationTier = 1
		pune := baseJob
		pune.Skills = skills
		pune.LocationTier = 2

		rBengaluru := Compute(bengaluru, user, w, ExternalInputs{}, now)
		rPune := Compute(pune, user, w, ExternalInputs{}, now)

		if rBengaluru.Priority <= rPune.Priority {
			t.Errorf("skills=%v: Bengaluru priority %d should exceed Pune (tier 2) priority %d",
				skills, rBengaluru.Priority, rPune.Priority)
		}
	}
}

// TestPriorityWorkedExample reproduces docs/09 section 3.13's exact numbers:
// base 75, Bengaluru -> 90, Pune -> 79 (the doc's own display rounds
// 78.75, which is what Result.Priority does too — see Compute's
// math.Round before the int16 cast).
func TestPriorityWorkedExample(t *testing.T) {
	w := testWeights()
	if got := math.Round(clamp(75*w.LocationMultipliers["1"]*1.0, 0, 100)); got != 90 {
		t.Errorf("Bengaluru: 75 * 1.20 = %v, want 90", got)
	}
	if got := math.Round(clamp(75*w.LocationMultipliers["2"]*1.0, 0, 100)); got != 79 {
		t.Errorf("Pune (tier 2): 75 * 1.05 = %v, want 79", got)
	}
}

func TestFreshnessMultiplier_WorkedExamples(t *testing.T) {
	w := testWeights()
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		ageDays int
		want    float64
	}{
		{0, 1.00},
		{7, 0.87},
		{14, 0.78},
		{30, 0.65},
		{60, 0.57},
	}
	for _, c := range cases {
		posted := now.AddDate(0, 0, -c.ageDays)
		got, _ := computeFreshness(&posted, false, w, now)
		if diff := got - c.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("age %d days: freshness = %v, want ~%v", c.ageDays, got, c.want)
		}
	}
}

func TestComputeSkillMatch_EmptyIsPlaceholder(t *testing.T) {
	score, inputs := computeSkillMatch(nil, map[string]int{"go": 5})
	if score != placeholderScore {
		t.Errorf("empty job skills: score = %d, want placeholder %d", score, placeholderScore)
	}
	if inputs["placeholder"] != true {
		t.Error("expected placeholder flag in score inputs")
	}
}

func TestComputeSkillMatch_FullCoverage(t *testing.T) {
	score, _ := computeSkillMatch([]string{"go"}, map[string]int{"go": 5})
	if score != 100 {
		t.Errorf("full-level match: score = %d, want 100", score)
	}
}

func TestComputeSkillMatch_NoOverlap(t *testing.T) {
	score, _ := computeSkillMatch([]string{"rust"}, map[string]int{"go": 5})
	if score != 0 {
		t.Errorf("no overlap: score = %d, want 0", score)
	}
}

// TestWeighted_RenormalisesOverPresentKeysOnly is the regression test for
// the bug that shipped in the previous pass: with the placeholder
// subscores present in the values map (the old behavior), a perfect
// Bengaluru intern capped out at priority 69.7 — below both
// bengaluru_match (78) and high_score (88), so no P1 notification could
// ever fire. weighted() must divide by the weight of only the keys
// actually present, not by 1.0.
func TestWeighted_RenormalisesOverPresentKeysOnly(t *testing.T) {
	weights := map[string]float64{"a": 0.4, "b": 0.3, "c": 0.3}

	// All three keys present: an ordinary weighted mean.
	full := weighted(weights, map[string]float64{"a": 100, "b": 100, "c": 100})
	if math.Abs(full-100) > 0.01 {
		t.Errorf("all keys present: weighted = %v, want 100", full)
	}

	// "c" omitted entirely (not passed as a placeholder) must renormalise
	// over a+b's combined weight (0.7), not silently divide by 1.0 as if
	// "c" contributed 0.
	partial := weighted(weights, map[string]float64{"a": 100, "b": 100})
	if math.Abs(partial-100) > 0.01 {
		t.Errorf("c omitted: weighted = %v, want 100 (renormalised over a+b), got the old bug's value if this is ~70", partial)
	}
}

// TestPriority_PerfectBengaluruInternshipClearsBengaluruMatchThreshold is
// the end-to-end version of the same regression: a job that is, by every
// P1-computable signal, an ideal match must actually clear docs/11 section
// 2's bengaluru_match threshold (priority >= 78). Before the renormalisation
// fix this scored 69.7 and could never notify at all, regardless of how
// good the match was — verified by hand against infra/seed/seed.sql's
// literal weights before this fix landed.
func TestPriority_PerfectBengaluruInternshipClearsBengaluruMatchThreshold(t *testing.T) {
	w := testWeights()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	posted := now

	user := UserProfile{
		TargetRoles:     []schema.RoleFamily{schema.RoleSWEBackend},
		TargetSeniority: []schema.Seniority{schema.SeniorityInternship},
		SkillLevels:     map[string]int{"go": 5, "docker": 5},
	}
	job := schema.NormalizedJob{
		RoleFamily:      schema.RoleSWEBackend,
		Seniority:       schema.SeniorityInternship,
		Skills:          []string{"go", "docker"},
		LocationTier:    1, // Bengaluru
		PostedAt:        &posted,
		ATSPlatform:     "ats_greenhouse",
		DescriptionText: "Join our backend team building services in Go on Kubernetes. Our structured internship program pairs every intern with a dedicated mentor and real project ownership from day one.",
	}
	// A "perfect match" now has to mean a real resume backing it up, and
	// real signal across every dimension P2 added, not just skill_match —
	// resume_match, company_quality, learning_opportunity, and
	// ease_of_applying are all genuine, always-relevant subscores once
	// real data exists (P2 Phases C/H/I/J), so this fixture provides
	// what a real posting/resume pair would: modern-stack and structured-
	// program language, a known ATS, and resume text the keyword check can
	// actually match against.
	ext := ExternalInputs{ResumeText: "Experienced with Go and Docker.", SkillOntology: testSkillOntology()}

	got := Compute(job, user, w, ext, now)
	if got.Priority < 78 {
		t.Errorf("priority = %d, want >= 78 (bengaluru_match threshold) for a perfect match — "+
			"got the pre-fix ceiling of ~70 if this fails", got.Priority)
	}
}
