// company_quality (partial) — docs/09-ranking-scoring.md section 3.3,
// SCOUT-RANK-004. Only 3 of the formula's 7 terms are computable this
// pass: tech_stack_modernity and intern_program_maturity (both from the
// job description directly) and domain_interest (from the job's own
// role_family — infra/dev-tools/systems/ML weighted up, per this user's
// stated domain interests). The other 4 (engineering_reputation,
// financial_stability, hiring_momentum, glassdoor_proxy) need GitHub
// API/funding registry/12-month posting history/Glassdoor data this pass
// has no source for, and stay absent from the weighted mean — the same
// renormalization pattern weighted() already applies one level up.
//
// docs/09 section 3.3 is explicit that company_type "appears in no term
// above, and that is deliberate... never a ranking input" — this file
// never reads it, only packages/taxonomy/companies.yaml's well_known flag
// (competition_estimate.go's job, not this file's).
package scoring

import (
	"math"
	"regexp"

	"github.com/kelyon/scout/packages/schema"
)

// companyQualitySubweights are docs/09 section 3.3's own weights for the
// 3 terms this pass computes, out of its full 7-term, 100-point scale
// (10 + 10 + 15 = 35 of the 100 points the full formula defines) —
// weighted() renormalizes over exactly these three, so the ratios between
// them (not their absolute values) are what carries through.
var companyQualitySubweights = map[string]float64{
	"tech_stack_modernity":    10,
	"intern_program_maturity": 10,
	"domain_interest":         15,
}

var modernStackPattern = regexp.MustCompile(
	`(?i)\bgo(?:lang)?\b|\brust\b|\bkubernetes\b|\bk8s\b|\btypescript\b|\bgraphql\b|\bgrpc\b|\breact\b|\bkafka\b|\bterraform\b|\bpostgres(?:ql)?\b`,
)

var internProgramPattern = regexp.MustCompile(
	`(?i)\bstructured (?:intern|internship) program\b|\bintern cohort\b|\bintern class\b|\bdedicated mentor\b|\bintern project\b|\bsummer program\b`,
)

// domainInterestRoleFamilies is docs/09 section 3.3's "infra/dev-tools/AI
// weighted up for this user" — read from the resume-derived skill set this
// project already seeded (systems, distributed systems, security, ML/AI
// were the dominant themes), not a separate user_profile field this pass
// doesn't have.
var domainInterestRoleFamilies = map[schema.RoleFamily]bool{
	schema.RoleSWEInfra:         true,
	schema.RoleSWEInfraSRE:      true,
	schema.RoleSWEInfraDevOps:   true,
	schema.RoleSWEInfraPlatform: true,
	schema.RoleSWEInfraCloud:    true,
	schema.RoleSWESystems:       true,
	schema.RoleSWESecurity:      true,
	schema.RoleSWEML:            true,
	schema.RoleSWEMLResearch:    true,
}

func computeCompanyQuality(roleFamily schema.RoleFamily, descriptionText string) (int, map[string]any) {
	hasModernStack := modernStackPattern.MatchString(descriptionText)
	techStackModernity := 40.0
	if hasModernStack {
		techStackModernity = 100.0
	}

	hasInternProgram := internProgramPattern.MatchString(descriptionText)
	internProgramMaturity := 40.0
	if hasInternProgram {
		internProgramMaturity = 100.0
	}

	inDomain := domainInterestRoleFamilies[roleFamily]
	domainInterest := 60.0
	if inDomain {
		domainInterest = 100.0
	}

	score := weighted(companyQualitySubweights, map[string]float64{
		"tech_stack_modernity":    techStackModernity,
		"intern_program_maturity": internProgramMaturity,
		"domain_interest":         domainInterest,
	})

	return int(math.Round(score)), map[string]any{
		"tech_stack_modernity_signal": hasModernStack,
		"intern_program_signal":       hasInternProgram,
		"in_domain_interest":          inDomain,
		"partial":                     true,
		"missing_terms": []string{
			"engineering_reputation", "financial_stability", "hiring_momentum", "glassdoor_proxy",
		},
	}
}
