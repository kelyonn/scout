package taxonomy

import (
	_ "embed"
	"regexp"

	"gopkg.in/yaml.v3"
)

//go:embed roles.yaml
var rolesYAML []byte

type rolePatternSet struct {
	Strong   []string `yaml:"strong"`
	Weak     []string `yaml:"weak"`
	Negative []string `yaml:"negative"`
	Requires string   `yaml:"requires"`
}

// RoleFamily is one Tier 0 pattern set, compiled once at load time.
// FamilyName is the role_family enum value this pattern set assigns
// (packages/schema.RoleFamily), in the order roles.yaml declares it — file
// order is match priority (docs/07 section 4 comment on roles.yaml).
type RoleFamily struct {
	FamilyName string
	Strong     []*regexp.Regexp
	Weak       []*regexp.Regexp
	Negative   []*regexp.Regexp
	// RequiresTechnicalEvidence is docs/07 section 4's advocacy-only gate
	// ("requires: technical_evidence" in roles.yaml) — false for every
	// swe.* family.
	RequiresTechnicalEvidence bool
}

// LoadRolePatterns returns the ordered list of Tier 0 role-family pattern
// sets from the embedded roles.yaml.
func LoadRolePatterns() []RoleFamily {
	var raw yaml.Node
	if err := yaml.Unmarshal(rolesYAML, &raw); err != nil {
		panic("taxonomy: malformed roles.yaml: " + err.Error())
	}
	if len(raw.Content) == 0 {
		panic("taxonomy: roles.yaml is empty")
	}

	// Decoded via yaml.Node (not a plain map) specifically to preserve
	// declaration order — a Go map randomizes iteration, and match priority
	// (docs/07 section 4: "swe.general ... checked last among strong
	// patterns") depends on file order.
	doc := raw.Content[0]
	families := make([]RoleFamily, 0, len(doc.Content)/2)
	for i := 0; i < len(doc.Content); i += 2 {
		nameNode := doc.Content[i]
		var set rolePatternSet
		if err := doc.Content[i+1].Decode(&set); err != nil {
			panic("taxonomy: malformed roles.yaml entry " + nameNode.Value + ": " + err.Error())
		}
		families = append(families, RoleFamily{
			FamilyName:                nameNode.Value,
			Strong:                    compileAll(set.Strong),
			Weak:                      compileAll(set.Weak),
			Negative:                  compileAll(set.Negative),
			RequiresTechnicalEvidence: set.Requires == "technical_evidence",
		})
	}
	return families
}

func compileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile("(?i)"+p))
	}
	return out
}
