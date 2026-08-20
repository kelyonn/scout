package scoring

import (
	"strings"
	"testing"

	"github.com/kelyon/scout/packages/schema"
)

func TestExplainMentionsMatchedSkillsLocationAndCompensation(t *testing.T) {
	job := schema.NormalizedJob{
		LocationCity: "Bengaluru", LocationTier: 1, WorkMode: schema.WorkOnsite,
		CompNormalizedINRMonth: floatPtr(85000),
	}
	r := Result{
		ScoreInputs: map[string]any{
			"skill_match": map[string]any{
				"job_skills":     []string{"go", "postgresql", "docker"},
				"missing_skills": []string{"docker"},
			},
			"compensation": map[string]any{"confidence_low": false, "percentile": 0.78, "comparable_count": 40},
		},
	}

	got := Explain(job, r)

	if !strings.Contains(got, "Matches 2 of 3 required skills") {
		t.Errorf("Explain() = %q, want a skill-match count", got)
	}
	if !strings.Contains(got, "go") || !strings.Contains(got, "postgresql") {
		t.Errorf("Explain() = %q, want the matched skills named, not the missing one", got)
	}
	if strings.Contains(got, "docker") {
		t.Errorf("Explain() = %q, must not list the missing skill as matched", got)
	}
	if !strings.Contains(got, "Tier 1 location: Bengaluru") {
		t.Errorf("Explain() = %q, want the location tier and city", got)
	}
	if !strings.Contains(got, "85,000") || !strings.Contains(got, "78th percentile") {
		t.Errorf("Explain() = %q, want the comp figure and percentile", got)
	}
}

func TestExplainOmitsPercentileWhenCompensationConfidenceIsLow(t *testing.T) {
	job := schema.NormalizedJob{CompNormalizedINRMonth: floatPtr(50000)}
	r := Result{
		ScoreInputs: map[string]any{
			"compensation": map[string]any{"confidence_low": true, "reason": "fewer than 20 comparables"},
		},
	}

	got := Explain(job, r)

	if !strings.Contains(got, "50,000") {
		t.Errorf("Explain() = %q, want the raw figure stated even without a percentile", got)
	}
	if strings.Contains(got, "percentile") {
		t.Errorf("Explain() = %q, must not claim a percentile when confidence_low", got)
	}
}

func TestExplainOmitsCompensationWhenJobHasNone(t *testing.T) {
	job := schema.NormalizedJob{LocationCity: "Pune", LocationTier: 2}
	r := Result{ScoreInputs: map[string]any{}}

	got := Explain(job, r)

	if strings.Contains(got, "₹") {
		t.Errorf("Explain() = %q, must not fabricate a compensation figure", got)
	}
	if !strings.Contains(got, "Pune") {
		t.Errorf("Explain() = %q, want the location still mentioned", got)
	}
}

func TestExplainSaysRemoteWhenNoCity(t *testing.T) {
	job := schema.NormalizedJob{WorkMode: schema.WorkRemote}
	got := Explain(job, Result{ScoreInputs: map[string]any{}})
	if !strings.Contains(got, "Remote") {
		t.Errorf("Explain() = %q, want Remote mentioned for a remote job with no city", got)
	}
}

func TestExplainTruncatesLongSkillLists(t *testing.T) {
	job := schema.NormalizedJob{}
	r := Result{
		ScoreInputs: map[string]any{
			"skill_match": map[string]any{
				"job_skills":     []string{"go", "python", "rust", "java", "c", "kubernetes", "docker"},
				"missing_skills": []string{},
			},
		},
	}
	got := Explain(job, r)
	if !strings.Contains(got, "Matches 7 of 7 required skills") {
		t.Errorf("Explain() = %q, want the full count even when the shown list is truncated", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("Explain() = %q, want a truncation marker for more than %d skills", got, maxSkillsShown)
	}
}

func TestExplainFallsBackToAGenericLineWhenEverythingIsUnknown(t *testing.T) {
	got := Explain(schema.NormalizedJob{}, Result{ScoreInputs: map[string]any{}})
	if got == "" {
		t.Error("Explain() = \"\", want a non-empty fallback even with no usable inputs")
	}
}

func TestFormatThousands(t *testing.T) {
	cases := map[float64]string{
		999:      "999",
		1000:     "1,000",
		85000:    "85,000",
		1234567:  "1,234,567",
		0:        "0",
		-1200000: "-1,200,000",
	}
	for in, want := range cases {
		if got := formatThousands(in); got != want {
			t.Errorf("formatThousands(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestOrdinal(t *testing.T) {
	cases := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 78: "78th", 100: "100th"}
	for in, want := range cases {
		if got := ordinal(in); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", in, got, want)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }
