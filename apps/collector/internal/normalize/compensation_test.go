package normalize

import (
	"testing"

	"github.com/kelyon/scout/packages/schema"
)

// TestParseCompensation_BareYearMentionDoesNotMatch reproduces a real bug
// found live 2026-08-27: a Waymo posting
// ("Software Engineer, System-on-Chip (SoC) Testing") states no
// compensation at all, but ParseCompensation was called against the full
// description (CompensationRawText was empty, the ingest.go fallback) and
// matched the bare "2009" in "Since its start as the Google Self-Driving
// Car Project in 2009, Waymo has focused..." as a salary figure — no
// currency symbol, no lakh/crore/k suffix, no adjacent period word, just a
// number. It was normalized to comp_normalized_inr_month = 167.42 (2009/12)
// at confidence 0.9 and rendered as "₹167/mo" on a real job card.
func TestParseCompensation_BareYearMentionDoesNotMatch(t *testing.T) {
	text := `Let's get you there. Since its start as the Google Self-Driving Car
Project in 2009, Waymo has focused on building the Waymo Driver-The World's
Most Experienced Driver-to improve access to mobility while saving thousands
of lives now and in the future. The Waymo Driver powers Waymo One, a fully
autonomous ride-hailing service. This role is based in Bengaluru, India.`

	got := ParseCompensation(text)
	if got.Min != nil {
		t.Errorf("Min = %v, want nil — a bare year mention must not be treated as a salary", *got.Min)
	}
	if got.NormalizedINRMonth != nil {
		t.Errorf("NormalizedINRMonth = %v, want nil", *got.NormalizedINRMonth)
	}
	if got.Paid != schema.PaidUnknown {
		t.Errorf("Paid = %q, want %q — no compensation is actually stated here", got.Paid, schema.PaidUnknown)
	}
}

// TestParseCompensation_BareNumberRangeDoesNotMatch is the range-pattern
// counterpart of the bug above: "grew from 15 to 30 people" has the exact
// NUM-separator-NUM shape rangePattern looks for, with zero currency or
// suffix markers on either side.
// TestParseCompensation_ContractDurationDoesNotMatch reproduces a second
// real bug found alongside the bare-year one, in the same live audit: "6
// Month FTC" (a fixed-term-contract length, not a salary) matched
// singleValuePattern's bare "6" as a compensation figure, because the /|per
// separator that actually means "per month" was optional — the period word
// alone, with no separator and no currency, is not distinguishable from a
// duration by shape.
func TestParseCompensation_ContractDurationDoesNotMatch(t *testing.T) {
	cases := []string{
		"Senior Online Merchandiser - 6 Month FTC",
		"This is a 12 month contract role based in London.",
		"3 Year Development Programme for graduates.",
	}
	for _, text := range cases {
		got := ParseCompensation(text)
		if got.Min != nil {
			t.Errorf("text %q: Min = %v, want nil — a contract/programme duration is not a salary", text, *got.Min)
		}
	}
}

// TestParseCompensation_CompanyMetricsDoNotMatch reproduces a third real
// bug found in the same live audit: company boilerplate quoting real dollar
// figures for funding, revenue, or valuation — not the job's own
// compensation — passed every local-signal check a real salary would (a
// genuine "$" immediately before a genuine number), producing values off by
// orders of magnitude ($500,000,000/year, $170,000,000/hour, both observed
// live) that plausibleCompensation's ceiling now rejects.
func TestParseCompensation_CompanyMetricsDoNotMatch(t *testing.T) {
	cases := []string{
		"We've raised $50M in Series B funding to build the future of work.",
		"The company generates $170,000,000 in annual recurring revenue.",
		"Our platform is valued at $500,000,000 per the latest funding round.",
	}
	for _, text := range cases {
		got := ParseCompensation(text)
		if got.Min != nil {
			t.Errorf("text %q: Min = %v, want nil — a funding/revenue figure is not a salary", text, *got.Min)
		}
	}
}

func TestParseCompensation_HighButPlausibleSalaryStillMatches(t *testing.T) {
	// A real, very senior US salary must not be caught by the same ceiling
	// that rejects company-metrics figures — the ceiling is deliberately
	// far above this.
	got := ParseCompensation("This Staff Engineer role pays $450,000 per year.")
	if got.Min == nil || *got.Min != 450000 {
		t.Fatalf("Min = %v, want 450000 — a real high salary must still match", got.Min)
	}
}

// TestParseCompensation_RangeDoesNotBorrowSuffixOntoAGroupedNumber
// reproduces a regression the "12-15 LPA" shared-suffix fix itself caused,
// found live in the very backfill pass that verified that fix: a real
// prefr.com posting said "loan offerings ranging from ₹50,000 to ₹5 Lakh" —
// nothing to do with the job's own compensation — and the suffix-borrowing
// logic applied "Lakh" to "50,000" (already a complete, comma-grouped
// figure), producing ₹5,000,000,000. A comma in the number is proof it
// doesn't need a borrowed multiplier.
func TestParseCompensation_RangeDoesNotBorrowSuffixOntoAGroupedNumber(t *testing.T) {
	got := ParseCompensation("Loan offerings ranging from ₹50,000 to ₹5 Lakh for our borrowers.")
	if got.Min != nil && *got.Min > 1_000_000 {
		t.Errorf("Min = %v, want well under 1,000,000 — 50,000 must not be multiplied by Lakh", *got.Min)
	}
}

func TestParseCompensation_LocalPeriodWinsOverUnrelatedMentionElsewhere(t *testing.T) {
	// "monthly" appears in the text but nowhere near the actual figure,
	// which is itself explicitly stated as annual — the local "per year"
	// next to the number must win over detectPeriod's whole-text scan.
	got := ParseCompensation("We do monthly 1:1s and monthly all-hands. This Staff role pays $180,000 per year.")
	if got.Period != "year" {
		t.Errorf("Period = %q, want year — the figure's own local \"per year\" must win", got.Period)
	}
	if got.Min == nil || *got.Min != 180000 {
		t.Fatalf("Min = %v, want 180000", got.Min)
	}
}

// TestParseCompensation_InvertedRangeFromMalformedCommaIsSkippedForALaterValidOne
// reproduces a real, live description verbatim: an upstream HTML-stripping
// artifact turned "$150,000 to $187,500" into "$15000,000 to $187500"
// earlier in the text, which parses as a self-contradictory lo=15,000,000,
// hi=187,500 — rejected by the lo>hi check — but the same description
// restates the real range correctly later ("Base Compensation Range
// $150,000 — $187,500 USD"). tryRangeMatches trying every candidate, not
// just the first, is what lets it fall through the malformed one and land
// on the real figure instead of giving up or keeping the wrong number.
func TestParseCompensation_InvertedRangeFromMalformedCommaIsSkippedForALaterValidOne(t *testing.T) {
	text := "We offer competitive salaries, base ranging from $15000,000 to $187500 plus bonus and equity based on your experience. " +
		"Base Compensation Range $150,000 — $187,500 USD Benefits & Perks: Competitive compensation."
	got := ParseCompensation(text)
	if got.Min == nil || *got.Min != 150000 {
		t.Fatalf("Min = %v, want 150000 — the malformed first mention must be skipped for the correct later one", got.Min)
	}
	if got.Max == nil || *got.Max != 187500 {
		t.Fatalf("Max = %v, want 187500", got.Max)
	}
}

func TestParseCompensation_BareNumberRangeDoesNotMatch(t *testing.T) {
	text := "Our team grew from 15 to 30 people over the last two years."
	got := ParseCompensation(text)
	if got.Min != nil || got.Max != nil {
		t.Errorf("Min/Max = %v/%v, want both nil — a headcount range is not a salary range", got.Min, got.Max)
	}
}

func TestParseCompensation_RupeeSymbolSingleValue(t *testing.T) {
	got := ParseCompensation("Stipend: ₹50,000 per month")
	if got.Min == nil || *got.Min != 50000 {
		t.Fatalf("Min = %v, want 50000", got.Min)
	}
	if got.Currency != "INR" {
		t.Errorf("Currency = %q, want INR", got.Currency)
	}
	if got.Period != "month" {
		t.Errorf("Period = %q, want month", got.Period)
	}
	if got.Paid != schema.PaidYes {
		t.Errorf("Paid = %q, want yes", got.Paid)
	}
}

func TestParseCompensation_LPASingleValue(t *testing.T) {
	got := ParseCompensation("Compensation: 8 LPA")
	if got.Min == nil || *got.Min != 800000 {
		t.Fatalf("Min = %v, want 800000 (8 lakh)", got.Min)
	}
	if got.Period != "year" {
		t.Errorf("Period = %q, want year", got.Period)
	}
	if got.NormalizedINRMonth == nil || *got.NormalizedINRMonth != 800000.0/12 {
		t.Errorf("NormalizedINRMonth = %v, want %v", got.NormalizedINRMonth, 800000.0/12)
	}
}

func TestParseCompensation_LakhRange(t *testing.T) {
	got := ParseCompensation("12-15 LPA depending on experience")
	if got.Min == nil || *got.Min != 1_200_000 {
		t.Fatalf("Min = %v, want 1200000", got.Min)
	}
	if got.Max == nil || *got.Max != 1_500_000 {
		t.Fatalf("Max = %v, want 1500000", got.Max)
	}
}

func TestParseCompensation_IndianGroupingRange(t *testing.T) {
	got := ParseCompensation("₹1,00,000 - ₹1,50,000 per month")
	if got.Min == nil || *got.Min != 100000 {
		t.Fatalf("Min = %v, want 100000 (one lakh)", got.Min)
	}
	if got.Max == nil || *got.Max != 150000 {
		t.Fatalf("Max = %v, want 150000", got.Max)
	}
}

func TestParseCompensation_VaguePositive(t *testing.T) {
	got := ParseCompensation("This is a paid internship with a competitive stipend.")
	if got.Paid != schema.PaidYes {
		t.Errorf("Paid = %q, want yes", got.Paid)
	}
	if got.Min != nil {
		t.Errorf("Min = %v, want nil — vague-positive language has no figure to extract", got.Min)
	}
}

func TestParseCompensation_Unpaid(t *testing.T) {
	got := ParseCompensation("This is an unpaid internship for academic credit only.")
	if got.Paid != schema.PaidNo {
		t.Errorf("Paid = %q, want no", got.Paid)
	}
}

func TestParseCompensation_NoSignalIsUnknown(t *testing.T) {
	got := ParseCompensation("We are looking for a backend engineer to join our growing team.")
	if got.Paid != schema.PaidUnknown {
		t.Errorf("Paid = %q, want unknown", got.Paid)
	}
	if got.Min != nil {
		t.Errorf("Min = %v, want nil", got.Min)
	}
}

func TestParseCompensation_UnpaidWinsOverPaidSubstring(t *testing.T) {
	// "unpaid internship" contains "paid internship" as a substring —
	// unpaidPattern must be checked first so this doesn't flip to PaidYes.
	got := ParseCompensation("This unpaid internship is for academic credit only.")
	if got.Paid != schema.PaidNo {
		t.Errorf("Paid = %q, want no", got.Paid)
	}
}
