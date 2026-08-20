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
