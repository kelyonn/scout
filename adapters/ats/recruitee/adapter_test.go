package recruitee

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
		FetchedAt:  time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}
}

func testSource() adapter.Source {
	return adapter.Source{ID: "src-1", URL: "https://acmehq.recruitee.com/api/offers/"}
}

func TestParse_Standard(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// 3 offers in the fixture, one status="draft" — Parse must skip it.
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2 (the draft offer must be filtered out)", len(postings))
	}

	first := postings[0]
	if first.ExternalID != "12345" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://acmehq.recruitee.com/o/backend-engineering-intern/c/new" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.DescriptionHTML != "<p>Join our backend team building payments infrastructure.</p>" {
		t.Errorf("DescriptionHTML = %q", first.DescriptionHTML)
	}
	if first.RequirementsText != "<p>Currently pursuing a CS degree.</p>" {
		t.Errorf("RequirementsText = %q", first.RequirementsText)
	}
	if first.LocationRaw != "Bengaluru, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.RemoteHint {
		t.Error("RemoteHint should be false")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from published_at")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — published_at is Recruitee's own posting timestamp")
	}
	if first.Adapter != "ats_recruitee" {
		t.Errorf("Adapter = %q, want ats_recruitee", first.Adapter)
	}

	remote := postings[1]
	if !remote.RemoteHint {
		t.Error("RemoteHint should be true for the remote posting")
	}
	if remote.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty when city/country are both blank", remote.LocationRaw)
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
	if postings[0].TitleRaw != "Ingénieur Logiciel Stagiaire — 软件工程实习生" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "Zürich, Schweiz" {
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
	if a.Kind() != adapter.SourceKindRecruitee {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindRecruitee)
	}
}
