package emailalert

import (
	"os"
	"testing"
)

func loadHTMLFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("fixtures/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(body)
}

func TestLinkedIn_Matches(t *testing.T) {
	p := linkedinProvider
	cases := map[string]bool{
		"jobs-noreply@jobs-noreply.linkedin.com":                                 true,
		"LinkedIn Job Alerts <jobalerts-noreply@jobalerts-noreply.linkedin.com>": true,
		"alerts@indeed.com":   false,
		"noreply@example.com": false,
	}
	for from, want := range cases {
		if got := p.Matches(from); got != want {
			t.Errorf("Matches(%q) = %v, want %v", from, got, want)
		}
	}
}

func TestLinkedIn_ParseStandard(t *testing.T) {
	p := linkedinProvider
	postings, err := p.Parse(Message{HTML: loadHTMLFixture(t, "linkedin_standard.html")})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	first := postings[0]
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.CompanyNameRaw != "Acme Corp" {
		t.Errorf("CompanyNameRaw = %q", first.CompanyNameRaw)
	}
	if first.LocationRaw != "Bengaluru, Karnataka, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.TrackingURL != "https://www.linkedin.com/comm/jobs/view/1234567890/?trackingId=abcDEF123&refId=xyz" {
		t.Errorf("TrackingURL = %q", first.TrackingURL)
	}

	second := postings[1]
	if second.TitleRaw != "Remote Data Engineer" {
		t.Errorf("TitleRaw = %q", second.TitleRaw)
	}
	if second.LocationRaw != "Remote" {
		t.Errorf("LocationRaw = %q", second.LocationRaw)
	}
}

func TestLinkedIn_ParseEmpty(t *testing.T) {
	p := linkedinProvider
	postings, err := p.Parse(Message{HTML: loadHTMLFixture(t, "linkedin_empty.html")})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0 (the \"see all jobs\" link is not a job-view link)", len(postings))
	}
}

func TestLinkedIn_ParseMalformedDoesNotPanic(t *testing.T) {
	p := linkedinProvider
	// Unlike the JSON adapters, malformed HTML doesn't error — a
	// regex-based extractor degrades to "found nothing" or "found a
	// partial match" rather than failing outright, and this test's job is
	// to prove it doesn't panic on unclosed tags, not to assert a specific
	// error.
	postings, err := p.Parse(Message{HTML: loadHTMLFixture(t, "linkedin_malformed.html")})
	if err != nil {
		t.Fatalf("Parse returned an error rather than degrading: %v", err)
	}
	_ = postings
}

func TestLinkedIn_ParseUnicodeHeavy(t *testing.T) {
	p := linkedinProvider
	postings, err := p.Parse(Message{HTML: loadHTMLFixture(t, "linkedin_unicode.html")})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].TitleRaw != "Ingénieur Logiciel Stagiaire — 소프트웨어 엔지니어 인턴" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].CompanyNameRaw != "Société Générale & Cie" {
		t.Errorf("CompanyNameRaw = %q, want the &amp; entity decoded", postings[0].CompanyNameRaw)
	}
	if postings[0].LocationRaw != "Zürich, Schweiz" {
		t.Errorf("LocationRaw = %q, unicode not preserved", postings[0].LocationRaw)
	}
}
