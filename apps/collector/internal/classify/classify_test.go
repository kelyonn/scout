package classify

import (
	"testing"

	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

func TestResolveSeniority(t *testing.T) {
	cases := []struct {
		name              string
		title             string
		requirementsText  string
		employmentTypeRaw string
		want              schema.Seniority
	}{
		{"plain internship", "Software Engineering Intern", "", "", schema.SeniorityInternship},
		{"co-op", "Backend Co-op", "", "", schema.SeniorityInternship},
		{"new grad title", "New Grad Software Engineer", "", "", schema.SeniorityNewGrad},
		{"graduation year language", "Software Engineer", "must be graduating in 2027", "", schema.SeniorityNewGrad},
		{"Indian GET", "Graduate Engineer Trainee", "", "", schema.SeniorityNewGrad},
		{"Indian GET acronym as-written", "Software Engineer", "Apply now for our GET Program 2026", "", schema.SeniorityNewGrad},
		{
			// A real Cloudflare principal-level posting, found while
			// investigating why senior roles were classified new_grad: the
			// bare, case-insensitive \bget\b regex matched the ordinary
			// English word "get" here and misfired the Indian-GET-acronym
			// rule, overriding the correct senior-level title signal.
			"ordinary word 'get' does not trigger the Indian GET acronym rule",
			"Principal, GTM Strategic Initiatives Program Manager",
			"working across functions to get things done",
			"",
			schema.SeniorityStaff,
		},
		{"explicit 0-1 years", "Software Engineer", "0-1 years of experience", "", schema.SeniorityNewGrad},
		{"explicit years overrides title", "Associate Software Engineer", "5+ years of experience required", "", schema.SenioritySenior},
		{"junior title", "Junior Software Engineer", "", "", schema.SeniorityEntry},
		{"senior title", "Senior Software Engineer", "", "", schema.SenioritySenior},
		{"staff title", "Staff Software Engineer", "", "", schema.SeniorityStaff},
		{"unresolvable", "Software Engineer", "", "", schema.SeniorityUnknown},
		{
			// A real Affirm posting: no "senior"/"staff"/"principal" anywhere,
			// just the people-management title itself.
			"engineering manager title, no other level word",
			"Manager, Software Engineering (Resilience Engineering)",
			"",
			"",
			schema.SenioritySenior,
		},
		{
			"engineering manager title variant",
			"Engineering Manager – AI Engineering",
			"",
			"",
			schema.SenioritySenior,
		},
		{
			// "manager" is checked title-only and after seniorLevelPattern,
			// so a job whose description merely mentions a manager (extremely
			// common job-posting language) must not be pulled up to senior —
			// the same class of false positive the \bget\b fix addressed.
			"body text mentioning 'manager' does not promote an entry-level title",
			"Junior Software Engineer",
			"you will report to your engineering manager and receive regular mentorship",
			"",
			schema.SeniorityEntry,
		},
		{
			// "Principal" must still win over "Manager" appearing later in
			// the same title — this is the same fixture as the GET/get test
			// above, now also exercising the new manager rule against it.
			"principal beats manager in the same title",
			"Principal, GTM Strategic Initiatives Program Manager",
			"",
			"",
			schema.SeniorityStaff,
		},
		{"SDE II title", "Software Development Engineer II - Data", "", "", schema.SeniorityMid},
		{"SDE III title", "Software Development Engineer III Data", "", "", schema.SenioritySenior},
		{"SDE IV title", "Software Development Engineer IV Android", "", "", schema.SeniorityStaff},
		{"SDE-2 abbreviated", "SDE-2, Backend", "", "", schema.SeniorityMid},
		{"SDE 1 still resolves via the existing entry rule, unaffected by the new ladder patterns",
			"SDE-1, Backend", "", "", schema.SeniorityEntry},
		{
			// A real Stripe posting, found during P1 live verification: the
			// bare years-of-experience regex matched "2 years" here and
			// classified an internship as entry-level.
			"years of education, not experience, does not override internship",
			"Software Engineer, Intern",
			"Preferred qualifications: at least 2 years of university education, or equivalent work experience",
			"",
			schema.SeniorityInternship,
		},
		{
			// Lever categories.commitment / Ashby employmentType: a
			// structured field, not title regex — "Software Engineer" alone
			// resolves to unknown, but the platform-native field settles it.
			"structured employment type overrides an unresolvable title",
			"Software Engineer",
			"",
			"Intern",
			schema.SeniorityInternship,
		},
		{
			"explicit years overrides structured employment type",
			"Software Engineer",
			"5+ years of experience required",
			"Intern",
			schema.SenioritySenior,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveSeniority(c.title, c.requirementsText, c.employmentTypeRaw)
			if got != c.want {
				t.Errorf("ResolveSeniority(%q, %q, %q) = %q, want %q", c.title, c.requirementsText, c.employmentTypeRaw, got, c.want)
			}
		})
	}
}

func TestClassifyRole(t *testing.T) {
	families := taxonomy.LoadRolePatterns()
	skillOntology := taxonomy.LoadSkills()

	cases := []struct {
		name           string
		title          string
		description    string
		wantFamily     schema.RoleFamily
		wantIsSoftware bool
	}{
		{"backend intern", "backend engineer intern", "", schema.RoleSWEBackend, true},
		{"frontend intern", "frontend engineer intern", "", schema.RoleSWEFrontend, true},
		{"generic swe", "software engineer intern", "", schema.RoleSWEGeneral, true},
		{"sre", "site reliability engineer", "", schema.RoleSWEInfraSRE, true},
		{"unrelated title", "marketing manager", "", schema.RoleSWEOther, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyRole(c.title, c.description, families, skillOntology)
			if got.Family != c.wantFamily {
				t.Errorf("ClassifyRole(%q).Family = %q, want %q", c.title, got.Family, c.wantFamily)
			}
			if got.IsSoftware != c.wantIsSoftware {
				t.Errorf("ClassifyRole(%q).IsSoftware = %v, want %v", c.title, got.IsSoftware, c.wantIsSoftware)
			}
		})
	}
}

func TestClassifyRole_NegativeVeto(t *testing.T) {
	families := taxonomy.LoadRolePatterns()
	skillOntology := taxonomy.LoadSkills()
	got := ClassifyRole("backend team recruiter", "", families, skillOntology)
	if got.Family == schema.RoleSWEBackend {
		t.Error("negative pattern should veto swe.backend for a recruiter title")
	}
}

// TestClassifyRole_AdvocacyTechnicalEvidenceGate is docs/07 section 4's
// Hazard 1/2: an advocacy title is admitted only when the description
// carries a concrete technical signal; otherwise it escalates to Tier 2
// rather than being silently dropped or wrongly admitted.
func TestClassifyRole_AdvocacyTechnicalEvidenceGate(t *testing.T) {
	families := taxonomy.LoadRolePatterns()
	skillOntology := taxonomy.LoadSkills()

	t.Run("admitted with a named language", func(t *testing.T) {
		got := ClassifyRole(
			"developer advocate",
			"You'll write sample code in Python and Go, give conference talks, and build demos using our API.",
			families, skillOntology,
		)
		if got.Family != schema.RoleAdvocacyDevRel {
			t.Errorf("Family = %q, want advocacy.devrel", got.Family)
		}
		if !got.IsSoftware {
			t.Error("IsSoftware should be true once the technical-evidence gate passes")
		}
		if got.NeedsTier2Escalation {
			t.Error("a passing gate should not need Tier 2 escalation")
		}
	})

	t.Run("admitted with write-code phrasing", func(t *testing.T) {
		got := ClassifyRole(
			"developer relations engineer",
			"In this role you will write code, ship sample applications, and engage with our developer community.",
			families, skillOntology,
		)
		if got.Family != schema.RoleAdvocacyDevRel {
			t.Errorf("Family = %q, want advocacy.devrel", got.Family)
		}
	})

	t.Run("escalates to tier2 when no technical evidence", func(t *testing.T) {
		got := ClassifyRole(
			"developer advocate",
			"You'll build relationships with our community, plan events, and manage our social presence.",
			families, skillOntology,
		)
		if got.Family != schema.RoleSWEOther {
			t.Errorf("Family = %q, want swe.other (not admitted without technical evidence)", got.Family)
		}
		if got.IsSoftware {
			t.Error("IsSoftware should be false when the gate fails")
		}
		if !got.NeedsTier2Escalation {
			t.Error("expected NeedsTier2Escalation = true for a title match with no technical evidence")
		}
	})

	t.Run("solutions engineer gated the same way", func(t *testing.T) {
		withEvidence := ClassifyRole(
			"solutions engineer",
			"You'll build integration prototypes in Python and deploy customer proof-of-concepts using our REST API.",
			families, skillOntology,
		)
		if withEvidence.Family != schema.RoleAdvocacySolutions {
			t.Errorf("Family = %q, want advocacy.solutions", withEvidence.Family)
		}

		withoutEvidence := ClassifyRole(
			"solutions engineer",
			"You'll manage a sales pipeline and hit quarterly quota targets.",
			families, skillOntology,
		)
		if withoutEvidence.NeedsTier2Escalation != true {
			t.Error("a quota-focused description with no technical signal should escalate, not be admitted")
		}
	})

	t.Run("marketing hazard vetoed by negative pattern before the gate even runs", func(t *testing.T) {
		got := ClassifyRole(
			"developer marketing intern",
			"You'll write code samples in Python for our blog.",
			families, skillOntology,
		)
		if got.Family == schema.RoleAdvocacyDevRel {
			t.Error("developer marketing should be vetoed by the negative pattern, never reach advocacy.devrel")
		}
	})
}
