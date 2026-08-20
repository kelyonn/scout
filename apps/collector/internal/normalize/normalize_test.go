package normalize

import (
	"testing"

	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/taxonomy"
)

func TestStripHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ordinary HTML with real tags",
			in:   "<h2><strong>Who we are</strong></h2> <p>We build things.</p>",
			want: "Who we are We build things.",
		},
		{
			name: "entity-escaped HTML (observed live from Greenhouse's board API)",
			in:   "&lt;h2&gt;&lt;strong&gt;Who we are&lt;/strong&gt;&lt;/h2&gt; &lt;p&gt;We build things.&lt;/p&gt;",
			want: "Who we are We build things.",
		},
		{
			name: "an ordinary entity that is not part of a tag survives",
			in:   "<p>Compensation: &#8377;80,000 &amp; benefits</p>",
			want: "Compensation: ₹80,000 & benefits",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripHTML(c.in); got != c.want {
				t.Errorf("StripHTML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCanonicalizeURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "lowercases scheme and host, preserves path case",
			in:   "HTTP://Boards.Greenhouse.IO/Acme/Jobs/123",
			want: "https://boards.greenhouse.io/Acme/Jobs/123",
		},
		{
			name: "strips default https port",
			in:   "https://boards.greenhouse.io:443/acme/jobs/123",
			want: "https://boards.greenhouse.io/acme/jobs/123",
		},
		{
			name: "strips tracking params, sorts the rest",
			in:   "https://boards.greenhouse.io/acme/jobs/123?utm_source=x&b=2&a=1&gclid=y",
			want: "https://boards.greenhouse.io/acme/jobs/123?a=1&b=2",
		},
		{
			name: "strips session params",
			in:   "https://example.com/jobs/123?sessionid=abc&role=swe",
			want: "https://example.com/jobs/123?role=swe",
		},
		{
			name: "drops empty params",
			in:   "https://example.com/jobs/123?role=&team=eng",
			want: "https://example.com/jobs/123?team=eng",
		},
		{
			name: "drops the fragment on a non-hash-routing host",
			in:   "https://example.com/jobs/123#apply",
			want: "https://example.com/jobs/123",
		},
		{
			name: "keeps the fragment on a Workday host",
			in:   "https://acme.myworkdayjobs.com/en-US/External#/job/123",
			want: "https://acme.myworkdayjobs.com/en-US/External#/job/123",
		},
		{
			name: "drops a trailing slash",
			in:   "https://example.com/jobs/123/",
			want: "https://example.com/jobs/123",
		},
		{
			name: "keeps a bare root slash",
			in:   "https://example.com/",
			want: "https://example.com/",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := CanonicalizeURL(c.in)
			if err != nil {
				t.Fatalf("CanonicalizeURL(%q) error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("CanonicalizeURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCanonicalizeURL_HashStable(t *testing.T) {
	a, hashA, _ := CanonicalizeURL("https://boards.greenhouse.io/acme/jobs/123?utm_source=x")
	b, hashB, _ := CanonicalizeURL("https://boards.greenhouse.io/acme/jobs/123")
	if a != b {
		t.Fatalf("expected equal canonical URLs, got %q and %q", a, b)
	}
	if hashA != hashB {
		t.Error("expected equal hashes for equal canonical URLs")
	}
}

func TestNormalizeTitle(t *testing.T) {
	got := NormalizeTitle("Software Engineering Intern, Summer 2027 (Bengaluru) - REQ12345")
	if got.Normalized != "software engineering intern, (bengaluru) -" {
		t.Errorf("Normalized = %q", got.Normalized)
	}
	if got.Season != "summer" || got.Year != "2027" {
		t.Errorf("Season/Year = %q/%q, want summer/2027", got.Season, got.Year)
	}

	abbrev := NormalizeTitle("SWE Intern")
	if abbrev.Normalized != "software engineer intern" {
		t.Errorf("abbreviation expansion: got %q", abbrev.Normalized)
	}

	sre := NormalizeTitle("SRE Intern")
	if sre.Normalized != "site reliability engineer intern" {
		t.Errorf("SRE expansion: got %q", sre.Normalized)
	}
}

func TestNormalizeLocation(t *testing.T) {
	gaz := taxonomy.LoadGazetteer()

	cases := []struct {
		name       string
		raw        string
		remoteHint bool
		wantTier   int16
		wantMode   schema.WorkMode
	}{
		{"Bengaluru onsite", "Bengaluru, India", false, 1, schema.WorkOnsite},
		{"Bangalore alias", "Bangalore", false, 1, schema.WorkOnsite},
		{"Bengaluru locality", "Whitefield, Bengaluru", false, 1, schema.WorkOnsite},
		{"other Indian city", "Mumbai", false, 2, schema.WorkOnsite},
		{"India remote", "Remote - India", false, 3, schema.WorkRemote},
		{"fully remote, no city", "Remote", false, 3, schema.WorkRemote},
		{"remote via structured hint", "Anywhere", true, 3, schema.WorkRemote},
		{"international onsite", "San Francisco", false, 4, schema.WorkOnsite},
		{"international remote treated as remote", "San Francisco (Remote)", false, 3, schema.WorkRemote},
		{"multi-candidate picks best tier", "Hybrid: Bangalore/Hyderabad", false, 1, schema.WorkHybrid},
		{"unresolved", "Nowhereville", false, 0, schema.WorkOnsite},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeLocation(c.raw, c.remoteHint, gaz)
			if got.Tier != c.wantTier {
				t.Errorf("Tier = %d, want %d", got.Tier, c.wantTier)
			}
			if got.WorkMode != c.wantMode {
				t.Errorf("WorkMode = %q, want %q", got.WorkMode, c.wantMode)
			}
		})
	}
}

func TestParseCompensation_IndianNumbering(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		wantMin     float64
		wantMonthly float64
	}{
		{"lakh with Indian grouping", "₹1,00,000 per month", 100000, 100000},
		{"LPA converts to annual then monthly", "8 LPA", 800000, 800000.0 / 12},
		{"K suffix", "₹50K/month", 50000, 50000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseCompensation(c.text)
			if got.Min == nil {
				t.Fatalf("Min is nil for %q", c.text)
			}
			if *got.Min != c.wantMin {
				t.Errorf("Min = %v, want %v", *got.Min, c.wantMin)
			}
			if got.NormalizedINRMonth == nil {
				t.Fatalf("NormalizedINRMonth is nil for %q", c.text)
			}
			if diff := *got.NormalizedINRMonth - c.wantMonthly; diff > 0.01 || diff < -0.01 {
				t.Errorf("NormalizedINRMonth = %v, want %v", *got.NormalizedINRMonth, c.wantMonthly)
			}
			if got.Paid != schema.PaidYes {
				t.Errorf("Paid = %q, want paid", got.Paid)
			}
		})
	}
}

func TestParseCompensation_Range(t *testing.T) {
	got := ParseCompensation("₹80,000 - ₹1,00,000 per month")
	if got.Min == nil || got.Max == nil {
		t.Fatal("expected both Min and Max")
	}
	if *got.Min != 80000 || *got.Max != 100000 {
		t.Errorf("Min/Max = %v/%v, want 80000/100000", *got.Min, *got.Max)
	}
	if got.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", got.Confidence)
	}
}

func TestParseCompensation_VagueAndUnpaidAndUnknown(t *testing.T) {
	if got := ParseCompensation("This is a paid internship"); got.Paid != schema.PaidYes || got.Confidence != 0.50 {
		t.Errorf("vague positive: Paid=%q Confidence=%v", got.Paid, got.Confidence)
	}
	if got := ParseCompensation("This is an unpaid internship for academic credit"); got.Paid != schema.PaidNo {
		t.Errorf("unpaid: Paid=%q, want unpaid", got.Paid)
	}
	if got := ParseCompensation("Join our amazing team!"); got.Paid != schema.PaidUnknown {
		t.Errorf("nothing stated: Paid=%q, want unknown", got.Paid)
	}
}
