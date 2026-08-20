package scoring

import (
	"testing"

	"github.com/kelyon/scout/packages/taxonomy"
)

func testSkillOntology() []taxonomy.Skill {
	ontology := taxonomy.LoadSkills()
	found := false
	for _, s := range ontology {
		if s.ID == "go" {
			found = true
		}
	}
	if !found {
		panic("test skill ontology missing 'go' — packages/taxonomy/skills.yaml changed shape")
	}
	return ontology
}

func TestComputeResumeMatch_NoCosineFallsBackToKeywordOnly(t *testing.T) {
	ontology := testSkillOntology()
	score, inputs := computeResumeMatch(false, 0, []string{"go", "python"}, "Experienced with Go and Python.", ontology)
	if inputs["semantic_available"] != false {
		t.Error("expected semantic_available = false")
	}
	if score != 100 {
		t.Errorf("score = %d, want 100 (both job skills present in resume, keyword-only formula)", score)
	}
}

func TestComputeResumeMatch_NoCosineNoKeywordsIsZero(t *testing.T) {
	ontology := testSkillOntology()
	score, _ := computeResumeMatch(false, 0, []string{"rust"}, "Experienced with Go and Python.", ontology)
	if score != 0 {
		t.Errorf("score = %d, want 0", score)
	}
}

func TestComputeResumeMatch_FullFormulaWithCosine(t *testing.T) {
	ontology := testSkillOntology()
	// cosine at the ceiling (0.95) -> semantic_norm = 1.0; both skills present -> keyword = 1.0.
	score, inputs := computeResumeMatch(true, cosineCeiling, []string{"go", "python"}, "Experienced with Go and Python.", ontology)
	if score != 100 {
		t.Errorf("score = %d, want 100 (perfect semantic + perfect keyword)", score)
	}
	if inputs["semantic_available"] != true {
		t.Error("expected semantic_available = true")
	}
}

func TestComputeResumeMatch_CosineBelowFloorNormalizesToZero(t *testing.T) {
	ontology := testSkillOntology()
	score, inputs := computeResumeMatch(true, 0.30, nil, "unrelated text", ontology)
	semanticNorm, ok := inputs["semantic_norm"].(float64)
	if !ok || semanticNorm != 0 {
		t.Errorf("semantic_norm = %v, want 0 for cosine below the floor", inputs["semantic_norm"])
	}
	// base weight (0.10) alone still contributes: 100 * 0.10 = 10.
	if score != 10 {
		t.Errorf("score = %d, want 10 (base weight only, semantic and keyword both zero)", score)
	}
}

func TestComputeResumeMatch_PartialKeywordMatch(t *testing.T) {
	ontology := testSkillOntology()
	score, inputs := computeResumeMatch(false, 0, []string{"go", "rust", "kubernetes", "python"}, "Experienced with Go and Python.", ontology)
	keyword, ok := inputs["keyword"].(float64)
	if !ok || keyword != 0.5 {
		t.Errorf("keyword = %v, want 0.5 (2 of 4 job skills present)", inputs["keyword"])
	}
	if score != 50 {
		t.Errorf("score = %d, want 50", score)
	}
}
