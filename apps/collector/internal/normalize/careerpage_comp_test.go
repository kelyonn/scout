package normalize

import (
	"context"
	"testing"

	"github.com/kelyon/scout/adapters/web/careerpage"
	"github.com/kelyon/scout/packages/adapter"
)

// TestParseCompensation_CareerPageJSONLD pins the contract between the
// career-page adapter's CompensationRawText and ParseCompensation: salaries
// published as JSON-LD baseSalary, in the Indian shapes (LPA, lakh, grouped
// digits), must come out as the right absolute INR amounts.
func TestParseCompensation_CareerPageJSONLD(t *testing.T) {
	tests := []struct {
		name       string
		baseSalary string // the JSON value of "baseSalary"
		wantMin    float64
		wantMax    float64 // 0 = single value
		period     string
	}{
		{"LPA range as text", `"8-12 LPA"`, 800_000, 1_200_000, "year"},
		{"rupee symbol and spaces", `"₹ 8 - 12 LPA"`, 800_000, 1_200_000, "year"},
		{"single LPA", `"6 LPA"`, 600_000, 0, "year"},
		{"lakh per annum", `"₹5 lakh per annum"`, 500_000, 0, "year"},
		{"Indian grouping monthly", `"₹ 1,00,000 - 1,50,000 per month"`, 100_000, 150_000, "month"},
		{"MonetaryAmount yearly range", `{"@type":"MonetaryAmount","currency":"INR","value":{"minValue":800000,"maxValue":1200000,"unitText":"YEAR"}}`, 800_000, 1_200_000, "year"},
		{"MonetaryAmount monthly stipend", `{"@type":"MonetaryAmount","currency":"INR","value":{"value":25000,"unitText":"MONTH"}}`, 25_000, 0, "month"},
	}
	a := careerpage.NewHTMLPage(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := `<script type="application/ld+json">{"@type":"JobPosting","title":"Intern","identifier":"1","baseSalary":` + tt.baseSalary + `}</script>`
			ps, err := a.Parse(context.Background(), adapter.Source{URL: "https://x.example/j/1"}, &adapter.RawResponse{Body: []byte(page)})
			if err != nil || len(ps) != 1 {
				t.Fatalf("Parse: %v (%d postings)", err, len(ps))
			}
			got := ParseCompensation(ps[0].CompensationRawText)
			if got.Min == nil || *got.Min != tt.wantMin {
				t.Fatalf("text %q: Min = %v, want %v", ps[0].CompensationRawText, got.Min, tt.wantMin)
			}
			if tt.wantMax != 0 && (got.Max == nil || *got.Max != tt.wantMax) {
				t.Errorf("Max = %v, want %v", got.Max, tt.wantMax)
			}
			if got.Currency != "INR" || got.Period != tt.period {
				t.Errorf("currency=%q period=%q, want INR/%s", got.Currency, got.Period, tt.period)
			}
		})
	}
}
