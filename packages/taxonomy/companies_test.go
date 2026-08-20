package taxonomy

import "testing"

// validCompanyTypes mirrors infra/migrations/000003_company.up.sql's
// company_type CHECK constraint — a typo here would otherwise only surface
// as a seed-time SQL error.
var validCompanyTypes = map[string]bool{
	"product": true, "services_it": true, "services_consulting": true,
	"services_engineering": true, "gcc": true,
	"core_bfsi": true, "core_manufacturing": true, "core_energy": true,
	"core_telecom": true, "core_retail_cpg": true, "core_healthcare": true,
	"core_aerospace_def": true, "core_logistics": true,
	"research": true, "public_sector": true, "nonprofit": true, "unknown": true,
}

func TestLoadCompanies_AllTypesValid(t *testing.T) {
	companies := LoadCompanies()
	if len(companies) == 0 {
		t.Fatal("LoadCompanies returned no entries")
	}
	for slug, c := range companies {
		if !validCompanyTypes[c.Type] {
			t.Errorf("company %q has invalid company_type %q", slug, c.Type)
		}
		if len(c.HQCountry) != 2 {
			t.Errorf("company %q has non-ISO-3166-1-alpha-2 hq_country %q", slug, c.HQCountry)
		}
	}
}

func TestLoadCompanies_KnownEntry(t *testing.T) {
	companies := LoadCompanies()
	stripe, ok := companies["stripe"]
	if !ok {
		t.Fatal("expected a 'stripe' entry")
	}
	if stripe.Type != "product" {
		t.Errorf("stripe.Type = %q, want product", stripe.Type)
	}
	if !stripe.WellKnown {
		t.Error("stripe.WellKnown = false, want true")
	}
}
