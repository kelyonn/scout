package scoring

import (
	"testing"
	"time"

	"github.com/kelyon/scout/packages/schema"
)

func TestComputeCompetitionEstimate_WellKnownCompanyLowersScore(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	job := schema.NormalizedJob{PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3}
	wellKnown := true

	score, inputs := computeCompetitionEstimate(job, &wellKnown, now)
	if score >= int(competitionBase) {
		t.Errorf("score = %d, want < base (%v) for a well-known company", score, competitionBase)
	}
	if got, ok := inputs["well_known"].(*bool); !ok || !*got {
		t.Error("expected well_known input to reflect true")
	}
}

func TestComputeCompetitionEstimate_ObscureCompanyRaisesScore(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	job := schema.NormalizedJob{PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3}
	wellKnown := false

	score, _ := computeCompetitionEstimate(job, &wellKnown, now)
	if score <= int(competitionBase) {
		t.Errorf("score = %d, want > base (%v) for a not-well-known company", score, competitionBase)
	}
}

func TestComputeCompetitionEstimate_UnknownCompanyIsNeutral(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	job := schema.NormalizedJob{PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3}

	scoreKnownNil, _ := computeCompetitionEstimate(job, nil, now)
	// Base (50) + ats_native_only (+10) with no other signals firing.
	if scoreKnownNil != 60 {
		t.Errorf("score = %d, want 60 (base 50 + constant ats_native_only 10, no brand-recognition adjustment)", scoreKnownNil)
	}
}

func TestComputeCompetitionEstimate_OldPostingLowersScore(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -10) // > 7 days
	job := schema.NormalizedJob{PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3}

	score, inputs := computeCompetitionEstimate(job, nil, now)
	if score >= 60 {
		t.Errorf("score = %d, want < 60 (the neutral+ats-native baseline) for a 10-day-old posting", score)
	}
	if days, ok := inputs["posted_days_ago"].(float64); !ok || days < 9 {
		t.Errorf("posted_days_ago = %v, want ~10", inputs["posted_days_ago"])
	}
}

func TestComputeCompetitionEstimate_RareSkillRaisesScore(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	job := schema.NormalizedJob{
		PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3,
		DescriptionText: "You'll write systems code in Rust and work on our kernel-level agent.",
	}

	score, inputs := computeCompetitionEstimate(job, nil, now)
	if score <= 60 {
		t.Errorf("score = %d, want > 60 (neutral+ats-native baseline) with a rare-skill signal", score)
	}
	if inputs["rare_skill_signal"] != true {
		t.Error("expected rare_skill_signal = true")
	}
}

func TestComputeCompetitionEstimate_AdvocacyRoleRaisesScore(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	job := schema.NormalizedJob{
		PostedAt: &posted, WorkMode: schema.WorkOnsite, LocationTier: 3,
		RoleFamily: schema.RoleAdvocacyDevRel,
	}

	score, inputs := computeCompetitionEstimate(job, nil, now)
	if score <= 60 {
		t.Errorf("score = %d, want > 60 (neutral+ats-native baseline) for an advocacy role", score)
	}
	if inputs["advocacy_role"] != true {
		t.Error("expected advocacy_role = true")
	}
}

func TestComputeCompetitionEstimate_ScoreClampedToValidRange(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	posted := now.AddDate(0, 0, -1)
	wellKnown := true
	job := schema.NormalizedJob{
		PostedAt: &posted, WorkMode: schema.WorkRemote, LocationCountry: "", LocationTier: 3,
	}
	score, _ := computeCompetitionEstimate(job, &wellKnown, now)
	if score < 0 || score > 100 {
		t.Errorf("score = %d, want within [0, 100]", score)
	}
}
