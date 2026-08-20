package teamtailor

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
	return adapter.Source{ID: "src-1", URL: "https://acmehq.teamtailor.com/jobs.json"}
}

func TestParse_Standard(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	first := postings[0]
	if first.ExternalID != "555001" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://acmehq.teamtailor.com/jobs/555001-backend-engineering-intern" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.DescriptionHTML != "<p>Join our backend team building payments infrastructure.</p>" {
		t.Errorf("DescriptionHTML = %q", first.DescriptionHTML)
	}
	if first.LocationRaw != "Bengaluru, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.RemoteHint {
		t.Error("RemoteHint should be false — Bengaluru, India carries no remote signal")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from date_published")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — date_published is Teamtailor's own posting timestamp")
	}
	if first.Adapter != "ats_teamtailor" {
		t.Errorf("Adapter = %q, want ats_teamtailor", first.Adapter)
	}

	// Second posting lists two jobLocation entries (verified live —
	// Teamtailor postings commonly list several cities for one opening) —
	// one of them literally "Remote", which is what RemoteHint keys off.
	second := postings[1]
	if !second.RemoteHint {
		t.Error("RemoteHint should be true — one jobLocation entry is \"Remote\"")
	}
	if second.LocationRaw != "Remote; Berlin, Germany" {
		t.Errorf("LocationRaw = %q, want both jobLocation entries joined", second.LocationRaw)
	}
	if second.DescriptionHTML != "<p>Own our internal platform tooling across multiple regional teams.</p>" {
		t.Errorf("DescriptionHTML = %q", second.DescriptionHTML)
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
	if postings[0].TitleRaw != "Praktikant Softwareentwicklung — 소프트웨어 엔지니어 인턴" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "München, Deutschland" {
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
	if a.Kind() != adapter.SourceKindTeamtailor {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindTeamtailor)
	}
}
