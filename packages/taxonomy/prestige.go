package taxonomy

import (
	_ "embed"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed prestige.yaml
var prestigeYAML []byte

// LoadPrestigeOrgs parses the embedded curated prestige-organization list.
// Panics on malformed YAML — same posture as this package's other Load*
// functions, since the file is compiled in rather than read at runtime, and
// a parse failure here is a build-time bug, not a condition to recover
// from.
func LoadPrestigeOrgs() []string {
	var orgs []string
	if err := yaml.Unmarshal(prestigeYAML, &orgs); err != nil {
		panic("taxonomy: malformed prestige.yaml: " + err.Error())
	}
	return orgs
}

// IsPrestigeOrg reports whether companyName matches an entry in orgs —
// case-insensitive, and a match is any org name appearing as a substring of
// companyName (not the reverse: a short companyName like "AI" must not
// trivially match every multi-word org entry that happens to contain it).
// "DeepMind" matches a posting company name of "Google DeepMind"; "IIT"
// matches "IIT Bombay". An empty companyName never matches.
func IsPrestigeOrg(companyName string, orgs []string) bool {
	name := strings.ToLower(strings.TrimSpace(companyName))
	if name == "" {
		return false
	}
	for _, org := range orgs {
		if strings.Contains(name, strings.ToLower(org)) {
			return true
		}
	}
	return false
}
