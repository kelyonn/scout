package ashby

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/adapter"
)

// loadFixture reads a recorded response from fixtures/ and wraps it exactly
// as adapter.RawResponse would be built from a real fetch.Result — see
// adapters/ats/greenhouse/adapter_test.go's own loadFixture for why this
// matters for fixture replay.
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
	return adapter.Source{ID: "src-1", URL: "https://api.ashbyhq.com/posting-api/job-board/ramp?includeCompensation=true"}
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
	if first.ExternalID != "34413f8d-26bf-4bbc-8ade-eb309a0e2245" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Security Engineer, Cloud" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://jobs.ashbyhq.com/ramp/34413f8d-26bf-4bbc-8ade-eb309a0e2245/application" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "New York, NY (HQ)" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.Department != "Engineering" {
		t.Errorf("Department = %q", first.Department)
	}
	if first.EmploymentTypeRaw != "FullTime" {
		t.Errorf("EmploymentTypeRaw = %q", first.EmploymentTypeRaw)
	}
	if !first.RemoteHint {
		t.Error("RemoteHint should be true — isRemote is true in the fixture")
	}
	if first.CompensationRawText != "$211.4K - $290.6K" {
		t.Errorf("CompensationRawText = %q", first.CompensationRawText)
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from publishedAt")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — publishedAt is Ashby's own posting timestamp, not an edit proxy")
	}
	if first.Adapter != "ats_ashby" {
		t.Errorf("Adapter = %q, want ats_ashby", first.Adapter)
	}

	second := postings[1]
	if second.CompensationRawText != "" {
		t.Errorf("CompensationRawText = %q, want empty when compensation is null", second.CompensationRawText)
	}

	intern := postings[2]
	if intern.EmploymentTypeRaw != "Intern" {
		t.Errorf("EmploymentTypeRaw = %q, want Intern", intern.EmploymentTypeRaw)
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
	if a.Kind() != adapter.SourceKindAshby {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindAshby)
	}
}
