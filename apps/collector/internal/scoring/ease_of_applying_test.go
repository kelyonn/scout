package scoring

import "testing"

func TestComputeEaseOfApplying_GreenhouseBaseline(t *testing.T) {
	score, _ := computeEaseOfApplying("ats_greenhouse", "Standard application process.")
	if score != easeBaseShortForm {
		t.Errorf("score = %d, want %d", score, easeBaseShortForm)
	}
}

func TestComputeEaseOfApplying_LeverBaseline(t *testing.T) {
	score, _ := computeEaseOfApplying("ats_lever", "Standard application process.")
	if score != easeBaseShortForm {
		t.Errorf("score = %d, want %d", score, easeBaseShortForm)
	}
}

func TestComputeEaseOfApplying_AshbyBaseline(t *testing.T) {
	score, _ := computeEaseOfApplying("ats_ashby", "Standard application process.")
	if score != easeBaseATSStandard {
		t.Errorf("score = %d, want %d", score, easeBaseATSStandard)
	}
}

func TestComputeEaseOfApplying_RequiredCoverLetterCaps(t *testing.T) {
	score, inputs := computeEaseOfApplying("ats_greenhouse", "Please include a cover letter with your application.")
	if score != easeCoverLetterCap {
		t.Errorf("score = %d, want %d", score, easeCoverLetterCap)
	}
	if inputs["requires_cover_letter"] != true {
		t.Error("expected requires_cover_letter = true")
	}
}

func TestComputeEaseOfApplying_AssessmentCapsLowerThanCoverLetter(t *testing.T) {
	score, inputs := computeEaseOfApplying("ats_greenhouse", "Please include a cover letter. You'll also complete a take-home coding challenge.")
	if score != easeAssessmentCap {
		t.Errorf("score = %d, want %d (assessment is the more severe cap)", score, easeAssessmentCap)
	}
	if inputs["requires_assessment"] != true {
		t.Error("expected requires_assessment = true")
	}
}

func TestComputeEaseOfApplying_NoDowngradeSignalsStaysAtBaseline(t *testing.T) {
	score, inputs := computeEaseOfApplying("ats_ashby", "We look forward to your application.")
	if score != easeBaseATSStandard {
		t.Errorf("score = %d, want %d", score, easeBaseATSStandard)
	}
	if inputs["requires_cover_letter"] != false || inputs["requires_assessment"] != false {
		t.Error("expected both downgrade flags false")
	}
}
