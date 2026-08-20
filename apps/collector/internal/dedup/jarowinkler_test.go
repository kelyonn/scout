package dedup

import "testing"

func TestJaroWinkler_IdenticalStrings(t *testing.T) {
	if got := JaroWinkler("software engineer", "software engineer"); got != 1.0 {
		t.Errorf("JaroWinkler(identical) = %v, want 1.0", got)
	}
}

func TestJaroWinkler_EmptyStrings(t *testing.T) {
	if got := JaroWinkler("", ""); got != 1.0 {
		t.Errorf("JaroWinkler(\"\", \"\") = %v, want 1.0", got)
	}
	if got := JaroWinkler("a", ""); got != 0 {
		t.Errorf("JaroWinkler(a, \"\") = %v, want 0", got)
	}
}

// TestJaroWinkler_DocsWorkedExample is docs/08 section 3.2's own literal
// claim: "Software Engineering Intern" vs "...- Summer 2027" scores 0.94.
func TestJaroWinkler_DocsWorkedExample(t *testing.T) {
	got := JaroWinkler(
		"software engineering intern",
		"software engineering intern - summer 2027",
	)
	if got < 0.90 {
		t.Errorf("JaroWinkler(docs example) = %v, want >= 0.90 (docs claims 0.94)", got)
	}
	if got < gate1TitleThreshold {
		t.Errorf("JaroWinkler(docs example) = %v, want >= gate1TitleThreshold (%v) — this exact pair is the doc's own worked example for why the threshold is 0.85", got, gate1TitleThreshold)
	}
}

func TestJaroWinkler_UnrelatedStrings(t *testing.T) {
	got := JaroWinkler("software engineer", "marketing manager")
	if got > gate1TitleThreshold {
		t.Errorf("JaroWinkler(unrelated) = %v, want < gate1TitleThreshold (%v)", got, gate1TitleThreshold)
	}
}

// TestJaroWinkler_CanonicalReferenceValues checks against the two worked
// examples every Jaro-Winkler reference (including Winkler's own 1990
// paper) uses, so a transposition-counting or prefix-bonus bug shows up
// against known-correct numbers rather than only this package's own
// assumptions about itself.
func TestJaroWinkler_CanonicalReferenceValues(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"MARTHA", "MARHTA", 0.961},
		{"DIXON", "DICKSONX", 0.813},
	}
	for _, c := range cases {
		got := JaroWinkler(c.a, c.b)
		if diff := got - c.want; diff > 0.001 || diff < -0.001 {
			t.Errorf("JaroWinkler(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
