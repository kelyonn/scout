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

	rangePattern = regexp.MustCompile(
		`(?i)(?:₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(lpa|lakh|lakhs|lac|crore|cr|k)?\s*[-–to]+\s*(?:₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(lpa|lakh|lakhs|lac|crore|cr|k)?`,
	)
	singleValuePattern = regexp.MustCompile(
		`(?i)(₹|rs\.?|inr|\$|usd)?\s*(` + numberPattern + `)\s*(lpa|lakh|lakhs|lac|crore|cr|k)?\s*(?:/|per\s+)?\s*(hour|hr|month|mo|year|yr|annum|week)?`,
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

	if m := rangePattern.FindStringSubmatch(text); m != nil {
		if lo, hi, ok := parseRangeMatch(m); ok {
			currency := detectCurrency(text)
			period := detectPeriod(text)
			return finalize(NormalizedComp{
				Min: &lo, Max: &hi, Currency: currency, Period: period,
				Confidence: 0.95,
			})
		}
	}

	if m := singleValuePattern.FindStringSubmatch(text); m != nil && m[2] != "" {
		val, ok := parseIndianNumber(m[2], m[3])
		if ok {
			currency := detectCurrency(text)
			period := detectPeriod(text)
			confidence := float32(0.90)
			if currency == "" {
				currency = "INR" // step 5: inferred from location (P1: default INR)
				confidence = 0.60
			}
			if period == "" {
				period = "month"
				confidence = 0.75
			}
			return finalize(NormalizedComp{
				Min: &val, Currency: currency, Period: period, Confidence: confidence,
			})
		}
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

func parseRangeMatch(m []string) (lo, hi float64, ok bool) {
	lo, ok1 := parseIndianNumber(m[1], m[2])
	hi, ok2 := parseIndianNumber(m[3], m[4])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return lo, hi, true
}

// parseIndianNumber handles docs/07 section 7's explicit multiplier table:
// lakh/L/lac -> x100,000, crore/Cr -> x10,000,000, LPA -> lakhs/year (the
// multiplier is the same x100,000; the "/year" part is period, handled by
// detectPeriod), K -> x1,000. numStr may use Indian grouping (1,00,000) or
// Western (100,000) — both parse identically once commas are stripped.
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
	}
	return n, true
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
			monthly *= 160
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
