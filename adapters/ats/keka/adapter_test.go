package keka

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
		FetchedAt:  time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}
}

func testSource() adapter.Source {
	return adapter.Source{ID: "src-1", URL: "https://acmehq.keka.com/careers/api/jobs/default/active"}
}

func TestParse_Standard(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 3 {
		t.Fatalf("got %d postings, want 3", len(postings))
	}

	first := postings[0]
	if first.ExternalID != "138476" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://acmehq.keka.com/careers/jobdetails/138476" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bengaluru" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.RequirementsText != "0-1" {
		t.Errorf("RequirementsText = %q, want the raw experience field", first.RequirementsText)
	}
	if first.DescriptionHTML == "" {
		t.Error("DescriptionHTML should be populated from the description field")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from publishedOn")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — publishedOn is Keka's own posting timestamp")
	}
	if first.Adapter != "ats_keka" {
		t.Errorf("Adapter = %q, want ats_keka", first.Adapter)
	}

	// Second posting: multi-location join, and a populated compensation
	// string — real Keka behavior for both, not hypothetical.
	multi := postings[1]
	if multi.LocationRaw != "Mumbai, Bengaluru" {
		t.Errorf("LocationRaw = %q, want both locations joined", multi.LocationRaw)
	}
	if multi.CompensationRawText != "18,00,000 - 24,00,000 INR" {
		t.Errorf("CompensationRawText = %q", multi.CompensationRawText)
	}

	// Third posting: no locations at all (a real, valid shape — not every
	// posting lists one) and a fractional-second timestamp variant.
	noLoc := postings[2]
	if noLoc.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty when jobLocations is empty", noLoc.LocationRaw)
	}
	if noLoc.PostedAt == nil {
		t.Error("PostedAt should still parse a single-digit fractional second (\"...06:30:00.5Z\")")
	}
}

func TestParse_Empty(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "empty.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}
}

func TestParse_Malformed(t *testing.T) {
	a := New(nil)
	_, err := a.Parse(context.Background(), testSource(), loadFixture(t, "malformed.json"))
	if err == nil {
		t.Fatal("expected an error parsing truncated JSON, got nil")
	}
}

func TestParse_UnicodeHeavy(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "unicode-heavy.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].TitleRaw != "Estagiário de Engenharia de Software — エンジニアリングインターン" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "São Paulo" {
		t.Errorf("LocationRaw = %q, unicode not preserved", postings[0].LocationRaw)
	}
}

func TestValidate_RejectsEmptyTitle(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "shape-changed.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err == nil {
		t.Fatal("expected Validate to reject a posting with an empty title (renamed field)")
	}
}

func TestValidate_RejectsDuplicateExternalID(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "duplicate-ids.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err == nil {
		t.Fatal("expected Validate to reject duplicate external ids in one response")
	}
}

func TestValidate_AcceptsStandardResponse(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := a.Validate(context.Background(), testSource(), postings); err != nil {
		t.Errorf("Validate should accept a well-formed response: %v", err)
	}
}

func TestKind(t *testing.T) {
	a := New(nil)
	if a.Kind() != adapter.SourceKindKeka {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindKeka)
	}
}

// TestCareerPortalBase_FallsBackOnUnexpectedURLShape is the regression
// test for careerPortalBase's own documented fallback: a source seeded
// with a URL that doesn't end in the expected API suffix must not panic
// or silently drop the tenant — it should hand back the untouched URL, so
// Validate's own apply-URL check has something concrete to reject rather
// than a stack trace.
func TestCareerPortalBase_FallsBackOnUnexpectedURLShape(t *testing.T) {
	got := careerPortalBase("https://acmehq.keka.com/some/other/path")
	if got != "https://acmehq.keka.com/some/other/path" {
		t.Errorf("careerPortalBase = %q, want the untouched input URL", got)
	}
}
