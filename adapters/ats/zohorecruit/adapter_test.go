package zohorecruit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/adapter"
)

func loadFixture(t *testing.T, name string) *adapter.RawResponse {
	t.Helper()
	body, err := os.ReadFile("fixtures/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return &adapter.RawResponse{
		Body:       body,
		StatusCode: 200,
		FetchedAt:  time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func testSource() adapter.Source {
	return adapter.Source{ID: "src-1", URL: "https://acmehq.zohorecruit.com/jobs/Careers"}
}

func TestParse_Standard(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 3 {
		t.Fatalf("got %d postings, want 3", len(postings))
	}

	first := postings[0]
	if first.ExternalID != "603317000013238048" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://acmehq.zohorecruit.com/jobs/Careers/603317000013238048?source=CareerSite" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bangalore South, Karnataka, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.RequirementsText != "0-1" {
		t.Errorf("RequirementsText = %q, want the raw Work_Experience field", first.RequirementsText)
	}
	if first.DescriptionHTML == "" {
		t.Error("DescriptionHTML should be populated from Job_Description, including its embedded HTML tags")
	}
	if first.PostedAt == nil || !first.PostedAt.Equal(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PostedAt = %v, want 2026-08-20", first.PostedAt)
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — Date_Opened is Zoho's own stated open date")
	}
	if first.Adapter != "ats_zohorecruit" {
		t.Errorf("Adapter = %q, want ats_zohorecruit", first.Adapter)
	}

	// Second posting: a tenant field configuration that omits
	// Job_Description entirely — the real increff.zohorecruit.com shape —
	// must not error, just leave DescriptionHTML honestly empty.
	noDesc := postings[1]
	if noDesc.DescriptionHTML != "" {
		t.Errorf("DescriptionHTML = %q, want empty when Job_Description is blank", noDesc.DescriptionHTML)
	}

	// Third posting: no City/State (a remote-only posting, a real shape —
	// not every posting lists a city) and RemoteHint set.
	remote := postings[2]
	if remote.LocationRaw != "India" {
		t.Errorf("LocationRaw = %q, want just the country when city/state are blank", remote.LocationRaw)
	}
	if !remote.RemoteHint {
		t.Error("RemoteHint should be true when Remote_Job is true")
	}
}

func TestParse_Empty(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "empty.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}
}

func TestParse_Malformed(t *testing.T) {
	a := New(nil)
	_, err := a.Parse(context.Background(), testSource(), loadFixture(t, "malformed.html"))
	if err == nil {
		t.Fatal("expected an error parsing truncated JSON, got nil")
	}
}

func TestParse_NoJobsInput(t *testing.T) {
	a := New(nil)
	raw := &adapter.RawResponse{Body: []byte("<html><body>no jobs here</body></html>"), StatusCode: 200}
	_, err := a.Parse(context.Background(), testSource(), raw)
	if err == nil {
		t.Fatal("expected an error when the page has no id=\"jobs\" input, got nil")
	}
}

func TestParse_UnicodeHeavy(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "unicode-heavy.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].TitleRaw != "Estagiário de Engenharia de Software — エンジニアリングインターン" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "São Paulo, SP, Brazil" {
		t.Errorf("LocationRaw = %q, unicode not preserved", postings[0].LocationRaw)
	}
}

func TestValidate_RejectsEmptyTitle(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "shape-changed.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err == nil {
		t.Fatal("expected Validate to reject a posting with an empty title (renamed field)")
	}
}

func TestValidate_RejectsDuplicateExternalID(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "duplicate-ids.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err == nil {
		t.Fatal("expected Validate to reject duplicate external ids in one response")
	}
}

func TestValidate_AcceptsStandardResponse(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err != nil {
		t.Errorf("Validate should accept a well-formed response: %v", err)
	}
}

func TestKind(t *testing.T) {
	a := New(nil)
	if a.Kind() != adapter.SourceKindZohoRecruit {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindZohoRecruit)
	}
}

// TestCareerSiteBase_FallsBackOnUnparseableURL is the regression test for
// careerSiteBase's own documented fallback: a source seeded with a URL
// url.Parse can't make sense of must not panic — it should hand back the
// untouched URL, so Validate's own apply-URL check has something concrete
// to reject rather than a stack trace.
func TestCareerSiteBase_FallsBackOnUnparseableURL(t *testing.T) {
	got := careerSiteBase("not a url")
	if got != "not a url" {
		t.Errorf("careerSiteBase = %q, want the untouched input URL", got)
	}
}

func TestCareerSiteBase_StripsPath(t *testing.T) {
	got := careerSiteBase("https://acmehq.zohorecruit.com/jobs/Careers")
	if got != "https://acmehq.zohorecruit.com" {
		t.Errorf("careerSiteBase = %q, want scheme+host only", got)
	}
}
