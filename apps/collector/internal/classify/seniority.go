// Package classify implements Tier 0 classification only — docs/07
// section 4's pattern-rule tier and section 5's seniority resolution. Tier
// 1 (embedding nearest-neighbor) and Tier 2 (LLM) are P2
// (docs/19-roadmap.md), so a title this package can't place lands at
// swe.other / unknown with zero confidence rather than escalating anywhere
// — exactly the "still unresolvable -> unknown, included at reduced score"
// behavior docs/07 section 5 rule 5 specifies for the case that would
// otherwise escalate.
package classify

import (
	"regexp"

	"github.com/kelyon/scout/packages/schema"
)

// Signals verbatim from docs/07 section 5's table, plus the three
// resolution-rule patterns (years-of-experience, graduation-year, Indian
// GET/trainee language) from the ordered rules below the table.
var (
	// yearsPattern requires "experience" to follow within a few words —
	// "2 years of university education" (a real Stripe internship posting,
	// found during P1 live verification) must NOT match, or rule 1 below
	// overrides an internship classification on the strength of a phrase
	// about the candidate's degree, not their work history. A bare
	// "\d+\s*years?" without this constraint matched that phrase and every
	// other "N years" mention in a job description regardless of context.
	yearsPattern    = regexp.MustCompile(`(?i)(\d+)\+?\s*years?(?:\s+of)?(?:\s+[a-z]+){0,3}?\s+experience`)
	gradYearPattern = regexp.MustCompile(
		`(?i)\bgraduat(?:ing|e)\s+(?:in\s+)?20\d\d\b|\bclass of 20\d\d\b|\bbatch of 20\d\d\b`,
	)
	indianTraineePattern = regexp.MustCompile(
		`(?i)graduate engineer trainee|management trainee\s*\(technology\)`,
	)
	// indianGETAcronymPattern is deliberately case-sensitive (no (?i)):
	// "GET" is the Indian campus-hiring acronym ("Graduate Engineer
	// Trainee") only when it appears as-written, all-caps. Case-insensitive
	// \bget\b matched the ordinary English word "get" in any posting that
	// happened to say something like "get things done" — a real Cloudflare
	// principal-level posting, found while checking why senior roles were
	// classified new_grad — silently reclassifying it away from senior.
	indianGETAcronymPattern = regexp.MustCompile(`\bGET\b`)

	internshipPattern = regexp.MustCompile(
		`(?i)\bintern(?:ship)?\b|\bco-?op\b|\btrainee\b|summer analyst|industrial trainee|\bapprentice\b`,
	)
	newGradPattern = regexp.MustCompile(
		`(?i)new grad(?:uate)?|university grad|campus hire|graduate engineer|entry level`,
	)
	entryPattern       = regexp.MustCompile(`(?i)\bjunior\b|\bassociate\b|sde[\s-]?1\b|\bl3\b`)
	seniorLevelPattern = regexp.MustCompile(
		`(?i)\bsenior\b|\bsr\.?\b|\bstaff\b|\bprincipal\b|\bmid[\s-]level\b`,
	)

	// SDE ladder-level and "manager" title signals — matched against the
	// title alone, never the combined title+requirements text the patterns
	// above use. indianGETAcronymPattern's bug (this same file) was exactly
	// this failure mode: a level word that is unremarkable in body text
	// ("your manager", "reports to a senior engineer") got matched anyway
	// because the pattern searched the whole description, not just the
	// title. "manager" is common in job-description prose in a way "senior"
	// title language mostly isn't, so it gets the stricter, title-only
	// treatment here rather than joining seniorLevelPattern above.
	sdeLevel2Pattern = regexp.MustCompile(`(?i)\b(?:software development engineer|sde)[\s-]*(?:2|ii)\b`)
	sdeLevel3Pattern = regexp.MustCompile(`(?i)\b(?:software development engineer|sde)[\s-]*(?:3|iii)\b`)
	sdeLevel4Pattern = regexp.MustCompile(`(?i)\b(?:software development engineer|sde)[\s-]*(?:4|iv|5|v|6|vi)\b`)
	managerPattern   = regexp.MustCompile(`(?i)\bmanager\b`)
)

// employmentTypePattern matches a source's own structured employment-type
// field (Lever categories.commitment, Ashby employmentType) when it says
// "Intern"/"Internship" outright. Not one of docs/07 section 5's five
// title/requirements-text rules — those predate P1 having any adapter that
// supplies this field — but a strict superset of the same signal: a
// platform-native field beats regex over free text for exactly the reason
// rule 1 beats title language, so it is checked in that same slot, after
// years-of-experience and before the text-pattern rules.
var employmentTypePattern = regexp.MustCompile(`(?i)\bintern(?:ship)?\b`)

// ResolveSeniority implements docs/07 section 5's five resolution rules, in
// order, against a job's title and requirements text. employmentTypeRaw is
// the source's own structured employment-type field, when it has one; pass
// "" for adapters that don't supply it.
func ResolveSeniority(title, requirementsText, employmentTypeRaw string) schema.Seniority {
	combined := title + " " + requirementsText

	// Rule 1: explicit years-of-experience beats title language.
	if m := yearsPattern.FindStringSubmatch(requirementsText); m != nil {
		years := atoiSafe(m[1])
		switch {
		case years <= 1:
			return schema.SeniorityNewGrad // table's explicit "0-1 years" signal
		case years < 3:
			return schema.SeniorityEntry
		case years < 5:
			return schema.SeniorityMid
		case years < 8:
			return schema.SenioritySenior
		default:
			return schema.SeniorityStaff
		}
	}

	if employmentTypePattern.MatchString(employmentTypeRaw) {
		return schema.SeniorityInternship
	}

	// Rule 2: explicit graduation-year language is decisive for new_grad.
	if gradYearPattern.MatchString(combined) {
		return schema.SeniorityNewGrad
	}

	// Rule 3: Indian GET / trainee language maps to new_grad.
	if indianTraineePattern.MatchString(combined) || indianGETAcronymPattern.MatchString(combined) {
		return schema.SeniorityNewGrad
	}

	// Title-language signals, checked internship-first: an internship
	// posting that happens to also say "new grad program" in its
	// description should still be classified as an internship, since that
	// is the harder and more consequential filter for this system.
	switch {
	case internshipPattern.MatchString(combined):
		return schema.SeniorityInternship
	case newGradPattern.MatchString(combined):
		return schema.SeniorityNewGrad
	// SDE ladder levels, title-only and checked before the general senior/
	// entry rules: "Software Development Engineer II" must not fall to
	// entryPattern on some unrelated signal, and level 3/4+ must map to
	// senior/staff rather than the bare seniorLevelPattern's mid default.
	case sdeLevel4Pattern.MatchString(title):
		return schema.SeniorityStaff
	case sdeLevel3Pattern.MatchString(title):
		return schema.SenioritySenior
	case sdeLevel2Pattern.MatchString(title):
		return schema.SeniorityMid
	case seniorLevelPattern.MatchString(combined):
		// Checked before "entry" so "Senior Associate" doesn't fall through
		// to entry on the "associate" signal.
		return classifySeniorLevel(combined)
	case managerPattern.MatchString(title):
		// Below seniorLevelPattern deliberately: "Principal, ... Program
		// Manager" must resolve via "principal" -> staff, not short-circuit
		// here to senior on the weaker "manager" signal alone. This case
		// only fires when nothing stronger already matched — i.e. a bare
		// "Manager, Software Engineering" with no other level word.
		return schema.SenioritySenior
	case entryPattern.MatchString(combined):
		return schema.SeniorityEntry
	}

	// Rules 4-5: Tier 2 LLM escalation is out of scope (P2); still
	// unresolvable falls straight to unknown, included at reduced score
	// rather than excluded.
	return schema.SeniorityUnknown
}

func classifySeniorLevel(text string) schema.Seniority {
	switch {
	case regexp.MustCompile(`(?i)\bstaff\b|\bprincipal\b`).MatchString(text):
		return schema.SeniorityStaff
	case regexp.MustCompile(`(?i)\bsenior\b|\bsr\.?\b`).MatchString(text):
		return schema.SenioritySenior
	default:
		return schema.SeniorityMid
	}
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
