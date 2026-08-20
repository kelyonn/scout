package scoring

import "testing"

func TestComputeCompensation_NoCompDataReturnsPlaceholder(t *testing.T) {
	score, inputs := computeCompensation(false, 0, 0)
	if score != placeholderScore {
		t.Errorf("score = %d, want placeholder %d", score, placeholderScore)
	}
	if inputs["confidence_low"] != true {
		t.Error("expected confidence_low = true")
	}
}

func TestComputeCompensation_TooFewComparablesReturnsPlaceholder(t *testing.T) {
	score, inputs := computeCompensation(true, 5, 3)
	if score != placeholderScore {
		t.Errorf("score = %d, want placeholder %d (below the 20-comparable floor)", score, placeholderScore)
	}
	if inputs["confidence_low"] != true {
		t.Error("expected confidence_low = true")
	}
}

func TestComputeCompensation_ExactlyAtFloorComputesPercentile(t *testing.T) {
	// 15 of 20 comparables at or below this job's comp = 75th percentile.
	score, inputs := computeCompensation(true, compensationMinComparables, 15)
	if score != 75 {
		t.Errorf("score = %d, want 75", score)
	}
	if inputs["confidence_low"] != false {
		t.Error("expected confidence_low = false at the floor")
	}
}

func TestComputeCompensation_TopOfMarketScoresHigh(t *testing.T) {
	score, _ := computeCompensation(true, 50, 50) // at or above every comparable
	if score != 100 {
		t.Errorf("score = %d, want 100", score)
	}
}

func TestComputeCompensation_BottomOfMarketScoresLow(t *testing.T) {
	score, _ := computeCompensation(true, 50, 0) // below every comparable
	if score != 0 {
		t.Errorf("score = %d, want 0", score)
	}
}
