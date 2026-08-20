package taxonomy

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed companies.yaml
var companiesYAML []byte

// Company is one companies.yaml entry — see that file's own header comment
// for what company_type feeds (adapter selection, scheduling, competition
// estimation, coverage auditing — never company_quality, per docs/09
// section 3.3) and what well_known is for (competition_estimate's
// brand-recognition proxy only).
type Company struct {
	Type      string `yaml:"type"`
	HQCountry string `yaml:"hq_country"`
	WellKnown bool   `yaml:"well_known"`
}

// LoadCompanies parses the embedded company registry, keyed by slug.
func LoadCompanies() map[string]Company {
	var companies map[string]Company
	if err := yaml.Unmarshal(companiesYAML, &companies); err != nil {
		panic("taxonomy: malformed companies.yaml: " + err.Error())
	}
	return companies
}
