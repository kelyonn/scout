package taxonomy

import (
	_ "embed"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed skills.yaml
var skillsYAML []byte

// Skill is one ontology entry. Aliases are matched literally with word
// boundaries (docs/07 section 8: "avoids 'R' matching every capital R, and
// 'Go' matching 'going'"), not as regexes — an alias like "c++" is not
// valid regex syntax, and skill aliases don't need pattern matching the way
// role titles do.
type Skill struct {
	ID       string   `yaml:"id"`
	Display  string   `yaml:"display"`
	Aliases  []string `yaml:"aliases"`
	Category string   `yaml:"category"`

	// aliasPatterns are compiled once at load time: one word-boundary regex
	// per alias, the special characters in aliases like "c++" or ".net"
	// escaped so they match literally.
	aliasPatterns []*regexp.Regexp
}

// LoadSkills parses the embedded skill ontology.
func LoadSkills() []Skill {
	var skills []Skill
	if err := yaml.Unmarshal(skillsYAML, &skills); err != nil {
		panic("taxonomy: malformed skills.yaml: " + err.Error())
	}
	for i := range skills {
		skills[i].aliasPatterns = make([]*regexp.Regexp, 0, len(skills[i].Aliases))
		for _, alias := range skills[i].Aliases {
			skills[i].aliasPatterns = append(
				skills[i].aliasPatterns, wordBoundaryPattern(alias),
			)
		}
	}
	return skills
}

// wordBoundaryPattern builds a case-insensitive regex matching alias as a
// whole word/phrase. \b doesn't fire correctly around a leading/trailing
// non-word character (a literal "+" in "c++" is itself a non-word char), so
// boundaries are asserted with lookaround-free (?:^|\W) / (?:\W|$) instead,
// which Go's RE2 engine supports and \b at a symbol boundary does not.
func wordBoundaryPattern(alias string) *regexp.Regexp {
	escaped := regexp.QuoteMeta(strings.TrimSpace(alias))
	return regexp.MustCompile(`(?i)(?:^|\W)` + escaped + `(?:$|\W)`)
}

// Matches reports whether any alias appears in text.
func (s Skill) Matches(text string) bool {
	for _, p := range s.aliasPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}
