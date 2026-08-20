// Package taxonomy loads the curated gazetteer, role-pattern, and skill
// vocabularies from the YAML files in this directory. See the package
// README for scope and packages/taxonomy/locations.yaml,
// packages/taxonomy/roles.yaml, packages/taxonomy/skills.yaml for the data
// itself and why it's curated rather than the full spec (GeoNames, 400
// patterns, 1,200 skills).
//
// Data is embedded into the binary (go:embed) rather than read from disk at
// runtime — the same reason the Dockerfiles ship no data directory: one
// less path to get wrong in production.
package taxonomy

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed locations.yaml
var locationsYAML []byte

// Location is one gazetteer entry: a canonical place name and the strings
// sources actually use for it.
type Location struct {
	Canonical   string   `yaml:"canonical"`
	Aliases     []string `yaml:"aliases"`
	Region      string   `yaml:"region"`
	Country     string   `yaml:"country"`
	IsBengaluru bool     `yaml:"is_bengaluru"`
}

type aliasMatcher struct {
	pattern *regexp.Regexp
	alias   string
	loc     Location
}

// Gazetteer resolves a location string fragment to its canonical entry.
type Gazetteer struct {
	byAlias map[string]Location
	// patterns is aliasMatcher sorted longest-alias-first, so "san
	// francisco" is tried before a hypothetical shorter alias that happens
	// to also appear in the same string — real location strings carry
	// noise ("Bengaluru, India", "San Francisco (Remote)") that step 1's
	// delimiter split (/, ;, |, "or", "and") doesn't fully clean up, so
	// resolution falls back to a word-boundary substring search.
	patterns []aliasMatcher
}

// LoadGazetteer parses the embedded location data. Panics on malformed YAML
// — a broken data file is a build-time bug, not a runtime condition to
// recover from, the same posture apps/collector/internal/politeness takes
// for a malformed robots.txt rule file.
func LoadGazetteer() *Gazetteer {
	var entries []Location
	if err := yaml.Unmarshal(locationsYAML, &entries); err != nil {
		panic("taxonomy: malformed locations.yaml: " + err.Error())
	}

	g := &Gazetteer{byAlias: make(map[string]Location)}
	for _, entry := range entries {
		for _, alias := range entry.Aliases {
			lower := strings.ToLower(strings.TrimSpace(alias))
			g.byAlias[lower] = entry
			g.patterns = append(g.patterns, aliasMatcher{
				pattern: wordBoundaryPattern(lower),
				alias:   lower,
				loc:     entry,
			})
		}
	}
	sort.Slice(g.patterns, func(i, j int) bool {
		return len(g.patterns[i].alias) > len(g.patterns[j].alias)
	})
	return g
}

// Resolve looks up a location string fragment against the gazetteer: an
// exact match first, then the longest alias found anywhere in the text as a
// whole word/phrase.
func (g *Gazetteer) Resolve(text string) (Location, bool) {
	clean := strings.ToLower(strings.TrimSpace(text))
	if loc, ok := g.byAlias[clean]; ok {
		return loc, ok
	}
	for _, m := range g.patterns {
		if m.pattern.MatchString(clean) {
			return m.loc, true
		}
	}
	return Location{}, false
}
