package taxonomy

import "testing"

func TestLoadPrestigeOrgs(t *testing.T) {
	orgs := LoadPrestigeOrgs()
	if len(orgs) == 0 {
		t.Fatal("LoadPrestigeOrgs returned no entries")
	}

	var sawDeepMind bool
	for _, o := range orgs {
		if o == "DeepMind" {
			sawDeepMind = true
		}
	}
	if !sawDeepMind {
		t.Error(`expected "DeepMind" among the loaded entries`)
	}
}

func TestIsPrestigeOrg(t *testing.T) {
	orgs := LoadPrestigeOrgs()

	cases := []struct {
		name string
		want bool
	}{
		{"Google DeepMind", true},
		{"DeepMind", true},
		{"deepmind", true}, // case-insensitive
		{"IIT Bombay", true},
		{"Indian Institute of Technology, Bombay (an IIT)", true},
		{"NVIDIA Research", true},
		{"Stripe", false},
		{"Cloudflare", false},
		{"", false},    // empty never matches
		{"   ", false}, // whitespace-only never matches
	}
	for _, c := range cases {
		if got := IsPrestigeOrg(c.name, orgs); got != c.want {
			t.Errorf("IsPrestigeOrg(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIsPrestigeOrg_ShortNameDoesNotFalsePositive guards the asymmetry
// IsPrestigeOrg's own comment calls out: a short, generic companyName must
// not match merely because it happens to be a substring of some org entry.
func TestIsPrestigeOrg_ShortNameDoesNotFalsePositive(t *testing.T) {
	orgs := LoadPrestigeOrgs()
	if IsPrestigeOrg("AI", orgs) {
		t.Error(`IsPrestigeOrg("AI") should not match merely because "AI" is a substring of "AI2" or "FAIR" or "Meta AI"`)
	}
}

// TestIsPrestigeOrg_Unicode — a company name with non-ASCII characters must
// not panic or misbehave; it simply doesn't match anything in this list.
func TestIsPrestigeOrg_Unicode(t *testing.T) {
	orgs := LoadPrestigeOrgs()
	if IsPrestigeOrg("Zürich Instruments GmbH — Ünïcödé Co™", orgs) {
		t.Error("unrelated unicode company name should not match")
	}
	// ETH Zürich vs the curated "ETH Zurich" entry (plain ASCII) — the two
	// don't byte-match today; documenting the current (non-)behavior rather
	// than silently assuming diacritic folding happens somewhere.
	if IsPrestigeOrg("ETH Zürich", orgs) {
		t.Log("note: diacritic-accented company names do not match their ASCII curated entry — acceptable for a ~20-entry curated list, not a bug this test enforces either way")
	}
}
