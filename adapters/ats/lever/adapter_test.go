package lever

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
	return adapter.Source{ID: "src-1", URL: "https://api.lever.co/v0/postings/meesho?mode=json"}
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
	if first.ExternalID != "7d9af9b5-c1c7-48ec-bbb5-9b25e49f6596" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "AM/ Manager - Risk & Decision Science" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://jobs.lever.co/meesho/7d9af9b5-c1c7-48ec-bbb5-9b25e49f6596/apply" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bangalore, Karnataka" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.Department != "Financial Services" {
		t.Errorf("Department = %q", first.Department)
	}
	if first.EmploymentTypeRaw != "Full Time Employee" {
		t.Errorf("EmploymentTypeRaw = %q", first.EmploymentTypeRaw)
	}
	if first.RemoteHint {
		t.Error("RemoteHint should be false for workplaceType=onsite")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from createdAt")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — createdAt is Lever's own posting timestamp, not an edit proxy")
	}
	if first.Adapter != "ats_lever" {
		t.Errorf("Adapter = %q, want ats_lever", first.Adapter)
	}

	intern := postings[2]
	if intern.EmploymentTypeRaw != "Intern" {
		t.Errorf("EmploymentTypeRaw = %q, want Intern", intern.EmploymentTypeRaw)
	}
	if !intern.RemoteHint {
		t.Error("RemoteHint should be true for workplaceType=remote")
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
	if a.Kind() != adapter.SourceKindLever {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindLever)
	}
}
