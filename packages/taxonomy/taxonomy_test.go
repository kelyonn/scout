package taxonomy

import "testing"

func TestLoadGazetteer(t *testing.T) {
	g := LoadGazetteer()

	cases := []struct {
		text        string
		wantCountry string
		wantBLR     bool
	}{
		{"Bengaluru", "IN", true},
		{"bangalore", "IN", true},
		{"whitefield", "IN", true},
		{"Electronic City", "IN", true},
		{"Mumbai", "IN", false},
		{"bombay", "IN", false},
		{"gurgaon", "IN", false},
		{"San Francisco", "US", false},
	}
	for _, c := range cases {
		loc, ok := g.Resolve(c.text)
		if !ok {
			t.Errorf("Resolve(%q): not found", c.text)
			continue
		}
		if loc.Country != c.wantCountry {
			t.Errorf("Resolve(%q).Country = %q, want %q", c.text, loc.Country, c.wantCountry)
		}
		if loc.IsBengaluru != c.wantBLR {
			t.Errorf("Resolve(%q).IsBengaluru = %v, want %v", c.text, loc.IsBengaluru, c.wantBLR)
		}
	}

	if _, ok := g.Resolve("Nowhereville"); ok {
		t.Error("Resolve(unknown city) should not match")
	}
}

func TestLoadRolePatterns(t *testing.T) {
	families := LoadRolePatterns()
	if len(families) == 0 {
		t.Fatal("no role families loaded")
	}

	// File-order priority: swe.backend must come before swe.general so a
	// title matching both matches the more specific family first.
	backendIdx, generalIdx := -1, -1
	for i, f := range families {
		switch f.FamilyName {
		case "swe.backend":
			backendIdx = i
		case "swe.general":
			generalIdx = i
		}
	}
	if backendIdx == -1 || generalIdx == -1 {
		t.Fatal("expected both swe.backend and swe.general in roles.yaml")
	}
	if backendIdx > generalIdx {
		t.Error("swe.backend should be ordered before swe.general (specific before catch-all)")
	}

	var backend RoleFamily
	for _, f := range families {
		if f.FamilyName == "swe.backend" {
			backend = f
		}
	}
	if len(backend.Strong) == 0 {
		t.Fatal("swe.backend has no strong patterns")
	}
	if !backend.Strong[0].MatchString("backend engineer intern") {
		t.Error("expected a strong pattern to match 'backend engineer intern'")
	}
}

// TestLoadRolePatterns_IsSoftwareDefault is ADR-020's own correctness
// requirement: a family with no `is_software` key in roles.yaml (every
// swe.*/advocacy.* family declared before 2026-10-21) must default to true,
// so none of those 20 blocks needed editing when the new non-software
// families were added — and a family that explicitly sets `is_software:
// false` must carry that value through, not the default.
func TestLoadRolePatterns_IsSoftwareDefault(t *testing.T) {
	families := LoadRolePatterns()

	byName := make(map[string]RoleFamily, len(families))
	for _, f := range families {
		byName[f.FamilyName] = f
	}

	if sw, ok := byName["swe.backend"]; !ok || !sw.IsSoftware {
		t.Errorf("swe.backend: IsSoftware = %v (ok=%v), want true (no is_software key, should default true)", sw.IsSoftware, ok)
	}
	if pm, ok := byName["product.management"]; !ok || pm.IsSoftware {
		t.Errorf("product.management: IsSoftware = %v (ok=%v), want false (explicit is_software: false)", pm.IsSoftware, ok)
	}
}

func TestLoadSkills(t *testing.T) {
	skills := LoadSkills()
	if len(skills) == 0 {
		t.Fatal("no skills loaded")
	}

	var goSkill, cpp Skill
	for _, s := range skills {
		switch s.ID {
		case "go":
			goSkill = s
		case "cpp":
			cpp = s
		}
	}

	if !goSkill.Matches("experience with Go and distributed systems") {
		t.Error(`expected "go" skill to match "experience with Go"`)
	}
	if goSkill.Matches("we are going to build something") {
		t.Error(`"go" skill should not match "going" (word boundary)`)
	}
	if !cpp.Matches("proficient in C++ and systems programming") {
		t.Error(`expected "cpp" skill to match "C++"`)
	}
}
