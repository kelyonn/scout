package normalize

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kelyon/scout/packages/schema"
)

// NormalizedComp is compensation parsing's output — docs/07 section 7.
type NormalizedComp struct {
	Min                *float64
	Max                *float64
	Currency           string // ISO 4217, empty if unknown
	Period             string // hour|month|year|stipend_total
	Confidence         float32
	NormalizedINRMonth *float64
	Paid               schema.PaidSignal
}

// Indian numbering multipliers, docs/07 section 7. Checked in this order —
// "lakh"/"crore" before the bare "k" suffix, since "8 lakh" must not also
// match a generic thousands pattern.
const (
	lakh  = 100_000
	crore = 10_000_000
)

var (
	// A number with optional Indian-style grouping (1,00,000) or Western
	// grouping (100,000), optional decimal.
	numberPattern = `[\d,]+(?:\.\d+)?`

	// Currency prefixes are capturing (unlike an earlier version of this
	// pattern) so hasCompensationSignal can check them locally — see its
	// own comment for why a bare, unmarked number pair must never match.
	// m/mn/million/b/bn/billion are recognized suffixes too, but only so
	// parseIndianNumber can explicitly reject them — see its own comment.
	compSuffix = `lpa|lakh|lakhs|lac|crore|cr|k|million|mn|m|billion|bn|b`
	// The separator class includes an em dash (—, U+2014) alongside the
	// hyphen and en dash it already had — found missing live 2026-08-27: a
	// real "Base Compensation Range $150,000 — $187,500 USD" never matched
	// rangePattern at all without it, silently falling through to
	// singleValuePattern matching each end as its own lone figure instead.
	rangePattern = regexp.MustCompile(
		`(?i)(₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(` + compSuffix + `)?\s*[-–—to]+\s*(₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(` + compSuffix + `)?`,
	)
	// The /|per separator before the period word is its own capturing group
	// (group 4) — see hasCompensationSignal's own comment for why "a period
	// word matched somewhere nearby" isn't proof of a salary on its own.
	singleValuePattern = regexp.MustCompile(
		`(?i)(₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(` + compSuffix + `)?\s*(/|per\s+)?\s*(hour|hr|month|mo|year|yr|annum|week)?`,
	)
	vaguePositivePattern = regexp.MustCompile(`(?i)competitive (?:stipend|salary|pay|compensation)|\bpaid internship\b|stipend provided`)
	unpaidPattern        = regexp.MustCompile(`(?i)\bunpaid\b|no stipend|for academic credit only|\bvolunteer\b`)
	currencySymbolINR    = regexp.MustCompile(`₹|\brs\.?\b|\binr\b`)
	currencySymbolUSD    = regexp.MustCompile(`\$|\busd\b`)
	monthlyWord          = regexp.MustCompile(`(?i)\b(month|mo|monthly)\b`)
	yearlyWord           = regexp.MustCompile(`(?i)\b(year|yr|annum|annual|lpa)\b`)
	hourlyWord           = regexp.MustCompile(`(?i)\b(hour|hr|hourly)\b`)
	weeklyWord           = regexp.MustCompile(`(?i)\b(week|weekly)\b`)
)

// ParseCompensation implements docs/07 section 7's extraction ladder. text
// is the compensation-relevant snippet — normally
// packages/schema.Posting.CompensationRawText, falling back to the full
// description when the adapter doesn't separate it out.
func ParseCompensation(text string) NormalizedComp {
	text = strings.TrimSpace(text)

	if c, ok := tryRangeMatches(text); ok {
		return c
	}
	if c, ok := trySingleValueMatches(text); ok {
		return c
	}

	// Explicit unpaid language wins over an accidental match on the vague
	// positive pattern (e.g. "paid internship" as a substring of "unpaid
	// internship") — checked first for that reason.
	if unpaidPattern.MatchString(text) {
		return NormalizedComp{Confidence: 0, Paid: schema.PaidNo}
	}

	if vaguePositivePattern.MatchString(text) {
		return finalize(NormalizedComp{Confidence: 0.50, Paid: schema.PaidYes})
	}

	return NormalizedComp{Confidence: 0, Paid: schema.PaidUnknown}
}

// tryRangeMatches and trySingleValueMatches both scan *every* regex match in
// the text, not just the first — a real gap found live 2026-08-27 in the
// same audit as the other fixes here: FindStringSubmatch only ever looks at
// the first match, so a description that mentions an unrelated number
// before the real compensation figure ("We do monthly 1:1s... This Staff
// role pays $180,000 per year" — "1:1s" contains a digit that matches
// first, fails hasLocalCompensationSignal, and previously made
// ParseCompensation give up on the entire text instead of continuing to the
// real figure that follows) silently masked a real, well-formed salary
// elsewhere in the same description. Every candidate is tried in order;
// the first one that passes its signal and plausibility checks wins.
func tryRangeMatches(text string) (NormalizedComp, bool) {
	for _, m := range rangePattern.FindAllStringSubmatch(text, -1) {
		if !hasCompensationSignal(m[1], m[3], m[4], m[6]) {
			continue
		}
		lo, hi, ok := parseRangeMatch(m)
		// lo > hi is a real, if rare, shape found live 2026-08-27: a
		// description with a malformed comma ("$15000,000 to $187500",
		// itself an upstream HTML-stripping artifact, not this parser's
		// concern to repair) parsed as lo=15,000,000, hi=187,500 — a
		// self-contradictory "range" that a plausibility check on hi alone
		// would have accepted, since 187,500 alone looks like a perfectly
		// normal salary. A low end above the high end is definitionally
		// invalid regardless of cause, so the whole match is rejected
		// rather than silently keeping the smaller, still-wrong number.
		if !ok || lo > hi {
			continue
		}
		currency := detectCurrency(text)
		period := detectPeriod(text)
		// Both ends checked, not just hi — the same malformed-comma case
		// would otherwise slip past a hi-only check whenever the mangled
		// number happened to land on lo instead.
		if !plausibleCompensation(lo, currency, period) || !plausibleCompensation(hi, currency, period) {
			continue
		}
		return finalize(NormalizedComp{
			Min: &lo, Max: &hi, Currency: currency, Period: period,
			Confidence: 0.95,
		}), true
	}
	return NormalizedComp{}, false
}

func trySingleValueMatches(text string) (NormalizedComp, bool) {
	for _, m := range singleValuePattern.FindAllStringSubmatch(text, -1) {
		if m[2] == "" || !hasLocalCompensationSignal(m[1], m[3], m[4], m[5]) {
			continue
		}
		val, ok := parseIndianNumber(m[2], m[3])
		if !ok {
			continue
		}
		currency := detectCurrency(text)
		// The locally-captured period (only trustworthy when it came with
		// its own /|per separator — a bare period word is exactly what
		// hasLocalCompensationSignal already refuses to accept on its own)
		// wins over detectPeriod's whole-text scan. Found live 2026-08-27:
		// a real "$180,000" annual salary was tagged comp_period='month'
		// because the surrounding paragraphs mentioned "monthly 1:1s" or
		// similar, nowhere near the actual figure — detectPeriod has no way
		// to know that mention wasn't about the salary. canonicalPeriod's
		// own empty-separator case returns "", so this only ever overrides
		// detectPeriod's result when the regex actually captured something
		// next to the number itself.
		period := canonicalPeriod(m[4], m[5])
		if period == "" {
			period = detectPeriod(text)
		}
		confidence := float32(0.90)
		if currency == "" {
			currency = "INR" // step 5: inferred from location (P1: default INR)
			confidence = 0.60
		}
		if period == "" {
			period = "month"
			confidence = 0.75
		}
		if !plausibleCompensation(val, currency, period) {
			continue
		}
		return finalize(NormalizedComp{
			Min: &val, Currency: currency, Period: period, Confidence: confidence,
		}), true
	}
	return NormalizedComp{}, false
}

// parseRangeMatch also fixes a second bug found alongside the one
// hasCompensationSignal documents: "12-15 LPA" — a shared trailing suffix
// applying to both sides of the range, and the far more common phrasing in
// real Indian postings than the repeated "12 LPA-15 LPA" the two independent
// suffix groups otherwise assume — was parsing as lo=12 (no multiplier at
// all) and hi=1,500,000 (LPA applied only to the near number), a wildly
// inconsistent range silently written to the job row. If exactly one side
// has no suffix of its own, it borrows the other side's — but only when
// that side is a bare, un-grouped number ("12", plausibly meant in the same
// unit as its sibling), never when it already carries Indian- or
// Western-style comma grouping.
//
// That carve-out is itself a fix for a real regression the borrowing rule
// caused, found in the very same backfill pass that verified the rule:
// "loan offerings ranging from ₹50,000 to ₹5 Lakh" (prefr's own posting,
// nothing to do with the job's own compensation) borrowed "Lakh" onto
// "50,000" — already a complete, correctly-formatted absolute figure — and
// produced ₹5,000,000,000. A comma in the number string is a reliable
// signal that it doesn't need, and must not receive, a borrowed multiplier.
func parseRangeMatch(m []string) (lo, hi float64, ok bool) {
	loNum, hiNum := m[2], m[5]
	loSuffix, hiSuffix := m[3], m[6]
	if loSuffix == "" && hiSuffix != "" && !strings.Contains(loNum, ",") {
		loSuffix = hiSuffix
	} else if hiSuffix == "" && loSuffix != "" && !strings.Contains(hiNum, ",") {
		hiSuffix = loSuffix
	}

	lo, ok1 := parseIndianNumber(loNum, loSuffix)
	hi, ok2 := parseIndianNumber(hiNum, hiSuffix)
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return lo, hi, true
}

// hasCompensationSignal is the fix for a real bug found live 2026-08-27: a
// Waymo posting with no stated compensation at all showed "₹167/mo" because
// singleValuePattern matched the bare "2009" in "...Project in 2009,
// Waymo..." (a founding year) — every capture group in both patterns is
// optional, so before this check a plain number with no currency symbol, no
// lakh/crore/k suffix, and no adjacent period word matched just as
// confidently as a real "₹50,000/month". Requiring at least one of those
// three local markers is what "compensation-relevant" is supposed to mean;
// without it, ParseCompensation only ever needs a description to contain
// any 4-digit number to fabricate a confident, wrong salary.
func hasCompensationSignal(markers ...string) bool {
	for _, m := range markers {
		if m != "" {
			return true
		}
	}
	return false
}

// hasLocalCompensationSignal is singleValuePattern's own gate, stricter than
// hasCompensationSignal: a bare period word next to a number is not, on its
// own, proof of a salary — a real second bug found alongside the first
// (still live 2026-08-27, still in the same batch of Waymo-style false
// positives): "Senior Online Merchandiser - 6 Month FTC" matched the bare
// "6" as ₹6/year, because "Month" sat close enough to be captured as the
// period group even with no currency and no multiplier, and a fixed-term
// contract's *length* reads identically to a *rate* ("6 Month" vs "6/month")
// under a pattern that treats the /|per separator as optional. A period word
// only counts here when the separator that actually means "per" was present
// too — currency or a multiplier suffix stays sufficient on its own, since
// neither of those has a plausible non-compensation reading.
func hasLocalCompensationSignal(currency, suffix, separator, period string) bool {
	return currency != "" || suffix != "" || (separator != "" && period != "")
}

// parseIndianNumber handles docs/07 section 7's explicit multiplier table:
// lakh/L/lac -> x100,000, crore/Cr -> x10,000,000, LPA -> lakhs/year (the
// multiplier is the same x100,000; the "/year" part is period, handled by
// detectPeriod), K -> x1,000. numStr may use Indian grouping (1,00,000) or
// Western (100,000) — both parse identically once commas are stripped.
//
// m/mn/million/b/bn/billion are recognized suffixes purely to be rejected
// (ok=false): found live 2026-08-27 that "$50M in Series B funding" and
// "$170,000,000 in annual recurring revenue" both carry a real currency
// symbol next to a real number — a genuine per-posting compensation figure
// at this platform's target seniority never uses an M/B suffix at all, so
// recognizing the suffix and refusing it outright is simpler and more
// reliable than trying to distinguish "funding" from "salary" contextually.
func parseIndianNumber(numStr, suffix string) (float64, bool) {
	clean := strings.ReplaceAll(numStr, ",", "")
	n, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}

	switch strings.ToLower(suffix) {
	case "lpa", "lakh", "lakhs", "lac":
		n *= lakh
	case "crore", "cr":
		n *= crore
	case "k":
		n *= 1000
	case "m", "mn", "million", "b", "bn", "billion":
		return 0, false
	}
	return n, true
}

// canonicalPeriod maps singleValuePattern's own captured separator+period
// groups to the comp_period column's vocabulary. Empty unless separator is
// also non-empty — see the call site's own comment for why a period word
// with no separator isn't trusted here, the same reasoning
// hasLocalCompensationSignal already applies to whether the match is
// accepted at all.
func canonicalPeriod(separator, word string) string {
	if separator == "" {
		return ""
	}
	switch strings.ToLower(word) {
	case "year", "yr", "annum":
		return "year"
	case "hour", "hr":
		return "hour"
	case "week":
		return "month" // no distinct "week" column — same fold detectPeriod uses
	case "month", "mo":
		return "month"
	default:
		return ""
	}
}

func detectCurrency(text string) string {
	switch {
	case currencySymbolINR.MatchString(text):
		return "INR"
	case currencySymbolUSD.MatchString(text):
		return "USD"
	default:
		return ""
	}
}

func detectPeriod(text string) string {
	switch {
	case yearlyWord.MatchString(text):
		return "year"
	case hourlyWord.MatchString(text):
		return "hour"
	case weeklyWord.MatchString(text):
		return "month" // no distinct "week" column on comp_period; folded into month below
	case monthlyWord.MatchString(text):
		return "month"
	default:
		return ""
	}
}

// stipendDefaultMonths is docs/07 section 7's normalization default:
// "stipend_total / estimated_duration_months (default 3 for summer
// internships)".
const stipendDefaultMonths = 3

// hoursPerMonth converts an hourly rate to a monthly one (~40hr/week x 4
// weeks) — shared by finalize and plausibleCompensation so the two never
// drift onto different hour-to-month assumptions.
const hoursPerMonth = 160

// Plausibility ceilings for plausibleCompensation — deliberately generous
// (far above any real individual salary this project would ever score) so
// the only matches rejected are values off by orders of magnitude, not
// merely high ones.
const (
	maxPlausibleAnnualINR = 5_00_00_000 // ₹5 crore/year (~$600k)
	maxPlausibleAnnualUSD = 5_000_000   // $5M/year
)

// plausibleCompensation is a third fix alongside hasCompensationSignal and
// hasLocalCompensationSignal, for a false-positive class neither of those
// catches: a real currency symbol next to a real number that is still not a
// salary. Found live 2026-08-27 in the same audit — company boilerplate
// like "we've raised $50M in Series B funding" or "$170M in annual
// recurring revenue" has a genuine "$" immediately before a genuine number,
// so it passes every local-signal check a real salary would, and without
// this the resulting figure ($500,000,000/year, $170,000,000/hour, both
// observed) is off by so many orders of magnitude that no currency
// conversion or period mistake could explain it. Both ceilings are
// annualized-and-compared rather than compared directly, so an hourly or
// monthly rate under the ceiling isn't rejected just because its raw digits
// look large.
func plausibleCompensation(value float64, currency, period string) bool {
	annual := value
	switch period {
	case "hour":
		annual = value * hoursPerMonth * 12
	case "month", "":
		annual = value * 12
	case "year":
		annual = value
	}

	switch currency {
	case "INR":
		return annual <= maxPlausibleAnnualINR
	case "USD":
		return annual <= maxPlausibleAnnualUSD
	default:
		// A range match (unlike singleValuePattern) never defaults an
		// undetected currency to INR, so this case is real: a bare
		// number-to-number range with no currency symbol at all ("500-1000
		// employees", "30-50 million users") still needs a ceiling, or a
		// large enough bare range slips through with nothing to compare
		// it against. The more generous of the two known ceilings is the
		// safe universal fallback — no real compensation figure in any
		// currency this project would see normalizes above it.
		return annual <= maxPlausibleAnnualUSD
	}
}

// finalize computes comp_normalized_inr_month (docs/07 section 7,
// "Normalization") and the paid determination (section 7, "The paid
// determination"). FX conversion for non-INR currencies is out of scope —
// docs/07 calls for a cached, daily-refreshed monthly-average rate, which
// needs a live data source this pass doesn't have; USD amounts are left
// unconverted (NormalizedINRMonth stays nil) rather than silently wrong.
func finalize(c NormalizedComp) NormalizedComp {
	if c.Min != nil && c.Currency == "INR" {
		monthly := *c.Min
		switch c.Period {
		case "hour":
			monthly *= hoursPerMonth
		case "year":
			monthly /= 12
		case "stipend_total":
			monthly /= stipendDefaultMonths
		}
		c.NormalizedINRMonth = &monthly
	}

	if c.Confidence >= 0.50 {
		c.Paid = schema.PaidYes
	} else if c.Paid == "" {
		c.Paid = schema.PaidUnknown
	}
	return c
}
