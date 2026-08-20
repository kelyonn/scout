package emailalert

import "testing"

// TestIndeed_Glassdoor_Handshake covers indeed.go/glassdoor.go/handshake.go
// — table-driven since all three are regexProvider instances differing
// only in sender/link patterns, and regexProvider's own extraction logic
// (title/company/location, malformed-HTML tolerance, unicode) is already
// covered exhaustively by linkedin_test.go. What's actually specific to
// each of these three is its sender match and its link pattern, which is
// what this test exercises.
func TestIndeed_Glassdoor_Handshake(t *testing.T) {
	cases := []struct {
		provider        regexProvider
		name            string
		matchingFrom    string
		nonMatchFrom    string
		standardFixture string
		emptyFixture    string
		wantTitle       string
		wantCompany     string
		wantLocation    string
	}{
		{
			provider: indeedProvider, name: "indeed",
			matchingFrom: "Indeed Alerts <alert@alert.indeed.com>", nonMatchFrom: "jobs-noreply@jobs-noreply.linkedin.com",
			standardFixture: "indeed_standard.html", emptyFixture: "indeed_empty.html",
			wantTitle: "Backend Engineering Intern", wantCompany: "Acme Corp", wantLocation: "Bengaluru, Karnataka",
		},
		{
			provider: glassdoorProvider, name: "glassdoor",
			matchingFrom: "Glassdoor <noreply@noreply.glassdoor.com>", nonMatchFrom: "alert@alert.indeed.com",
			standardFixture: "glassdoor_standard.html", emptyFixture: "glassdoor_empty.html",
			wantTitle: "Backend Engineering Intern", wantCompany: "Acme Corp", wantLocation: "Bengaluru, India",
		},
		{
			provider: handshakeProvider, name: "handshake",
			matchingFrom: "Handshake <notifications@notifications.joinhandshake.com>", nonMatchFrom: "noreply@noreply.glassdoor.com",
			standardFixture: "handshake_standard.html", emptyFixture: "handshake_empty.html",
			wantTitle: "Backend Engineering Intern", wantCompany: "Acme Corp", wantLocation: "Bengaluru, India",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.provider.Name() != c.name {
				t.Errorf("Name() = %q, want %q", c.provider.Name(), c.name)
			}
			if !c.provider.Matches(c.matchingFrom) {
				t.Errorf("Matches(%q) = false, want true", c.matchingFrom)
			}
			if c.provider.Matches(c.nonMatchFrom) {
				t.Errorf("Matches(%q) = true, want false (another provider's sender)", c.nonMatchFrom)
			}

			postings, err := c.provider.Parse(Message{HTML: loadHTMLFixture(t, c.standardFixture)})
			if err != nil {
				t.Fatalf("Parse(standard): %v", err)
			}
			if len(postings) == 0 {
				t.Fatal("Parse(standard) returned no postings")
			}
			if postings[0].TitleRaw != c.wantTitle {
				t.Errorf("TitleRaw = %q, want %q", postings[0].TitleRaw, c.wantTitle)
			}
			if postings[0].CompanyNameRaw != c.wantCompany {
				t.Errorf("CompanyNameRaw = %q, want %q", postings[0].CompanyNameRaw, c.wantCompany)
			}
			if postings[0].LocationRaw != c.wantLocation {
				t.Errorf("LocationRaw = %q, want %q", postings[0].LocationRaw, c.wantLocation)
			}
			if postings[0].TrackingURL == "" {
				t.Error("TrackingURL is empty")
			}

			empty, err := c.provider.Parse(Message{HTML: loadHTMLFixture(t, c.emptyFixture)})
			if err != nil {
				t.Fatalf("Parse(empty): %v", err)
			}
			if len(empty) != 0 {
				t.Errorf("Parse(empty) returned %d postings, want 0", len(empty))
			}
		})
	}
}

func TestMatchProvider(t *testing.T) {
	p, ok := MatchProvider("LinkedIn Job Alerts <jobalerts-noreply@jobalerts-noreply.linkedin.com>")
	if !ok || p.Name() != "linkedin" {
		t.Errorf("MatchProvider(linkedin sender) = %v, %v, want linkedin/true", p, ok)
	}

	_, ok = MatchProvider("someone@example.com")
	if ok {
		t.Error("MatchProvider(unknown sender) = true, want false")
	}
}
