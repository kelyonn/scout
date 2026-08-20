package workable

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
	return adapter.Source{ID: "src-1", URL: "https://apply.workable.com/api/v1/widget/accounts/acme-robotics"}
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
	if first.ExternalID != "ABCD1234" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://apply.workable.com/acme-robotics/j/ABCD1234/apply/" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bengaluru, Karnataka, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.Department != "Engineering" {
		t.Errorf("Department = %q", first.Department)
	}
	if first.RemoteHint {
		t.Error("RemoteHint should be false — telecommute is false in the fixture")
	}
	if first.PostedAt == nil || first.PostedAt.Format("2006-01-02") != "2026-08-01" {
		t.Errorf("PostedAt = %v, want 2026-08-01", first.PostedAt)
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — published_on is Workable's own posting date")
	}
	if first.DescriptionHTML != "" {
		t.Error("DescriptionHTML should be empty — the widget endpoint has no description field, by design (see package comment)")
	}
	if first.Adapter != "ats_workable" {
		t.Errorf("Adapter = %q, want ats_workable", first.Adapter)
	}

	remote := postings[1]
	if !remote.RemoteHint {
		t.Error("RemoteHint should be true for the telecommute posting")
	}
	if remote.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty when city/state/country are all blank", remote.LocationRaw)
	}

	intern := postings[2]
	if intern.EmploymentTypeRaw != "Internship" {
		t.Errorf("EmploymentTypeRaw = %q, want Internship", intern.EmploymentTypeRaw)
	}
	if intern.LocationRaw != "Munich, Germany" {
		t.Errorf("LocationRaw = %q, want city+country with no state", intern.LocationRaw)
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
	if postings[0].TitleRaw != "Ingénieur Backend Stagiaire — 日本語対応" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "Ciudad de México, México" {
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
	if a.Kind() != adapter.SourceKindWorkable {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindWorkable)
	}
}
