package scoring

import "testing"

func TestComputeLearningOpportunity_AllSignalsPresent(t *testing.T) {
	skills := []string{"go", "rust", "kubernetes", "python", "kafka"} // 3/5 unfamiliar = 0.6, past the 0.4 peak
	levels := map[string]int{"go": 5, "python": 4}
	desc := "You'll work on distributed systems with a mentor pairing you through code review. " +
		"We contribute to open-source projects and you'll own your own project end to end."

	score, inputs := computeLearningOpportunity(skills, levels, desc)
	if score <= 50 {
		t.Errorf("score = %d, want > 50 with every non-tech-fraction signal present", score)
	}
	if inputs["deep_domain_signal"] != true {
		t.Error("expected deep_domain_signal = true")
	}
	if inputs["mentorship_signal"] != true {
		t.Error("expected mentorship_signal = true")
	}
	if inputs["open_source_signal"] != true {
		t.Error("expected open_source_signal = true")
	}
	if inputs["project_ownership_signal"] != true {
		t.Error("expected project_ownership_signal = true")
	}
}

func TestComputeLearningOpportunity_NoSignalsIsLow(t *testing.T) {
	skills := []string{"go"}
	levels := map[string]int{"go": 5} // 0% unfamiliar — the doc's own "not a learning opportunity, a rejection" case
	score, _ := computeLearningOpportunity(skills, levels, "Standard day-to-day engineering work.")
	if score > 20 {
		t.Errorf("score = %d, want low with 0%% unfamiliar tech and no other signals", score)
	}
}

func TestUnfamiliarTechFraction_PeaksAroundFortyPercent(t *testing.T) {
	// 2 of 5 skills unfamiliar = 0.4, exactly the peak.
	skills := []string{"go", "python", "rust", "kafka", "sql"}
	levels := map[string]int{"go": 5, "python": 4, "sql": 3}
	credit, capped := unfamiliarTechFraction(skills, levels)
	if credit < 0.95 {
		t.Errorf("credit at the peak fraction = %v, want close to 1.0", credit)
	}
	if capped {
		t.Error("capped should be false at exactly the peak")
	}
}

func TestUnfamiliarTechFraction_FullyUnfamiliarScoresLow(t *testing.T) {
	skills := []string{"rust", "erlang", "haskell"}
	levels := map[string]int{"go": 5} // none of the job's skills known — 100% unfamiliar
	credit, capped := unfamiliarTechFraction(skills, levels)
	if credit > 0.2 {
		t.Errorf("credit at 100%% unfamiliar = %v, want low (doc: 'the candidate will not be hired, which is not a learning opportunity')", credit)
	}
	if !capped {
		t.Error("capped should be true past the peak")
	}
}

func TestUnfamiliarTechFraction_FullyFamiliarScoresLow(t *testing.T) {
	skills := []string{"go", "python"}
	levels := map[string]int{"go": 5, "python": 5} // 0% unfamiliar
	credit, _ := unfamiliarTechFraction(skills, levels)
	if credit > 0.05 {
		t.Errorf("credit at 0%% unfamiliar = %v, want ~0 (doc: 'a job requiring nothing you know is not a learning opportunity, it is a rejection')", credit)
	}
}

func TestUnfamiliarTechFraction_EmptySkillsIsZero(t *testing.T) {
	credit, capped := unfamiliarTechFraction(nil, map[string]int{"go": 5})
	if credit != 0 || capped {
		t.Errorf("empty skills: credit=%v capped=%v, want 0/false", credit, capped)
	}
}
