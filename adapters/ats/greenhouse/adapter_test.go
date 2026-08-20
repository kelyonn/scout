package greenhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/adapter"
)

// loadFixture reads a recorded response from fixtures/ and wraps it exactly
// as adapter.RawResponse would be built from a real fetch.Result — this is
// what makes Parse's fixture replay meaningful: the test exercises the same
// shape Fetch hands Parse in production, not a hand-built shortcut.
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
	return adapter.Source{ID: "src-1", URL: "https://boards-api.greenhouse.io/v1/boards/acme/jobs?content=true"}
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
	if first.ExternalID != "6123456" {
		t.Errorf("ExternalID = %q, want 6123456", first.ExternalID)
	}
	if first.TitleRaw != "Software Engineering Intern, Summer 2027" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://boards.greenhouse.io/acme/jobs/6123456" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bengaluru, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.Department != "Engineering" {
		t.Errorf("Department = %q", first.Department)
	}
	if first.DescriptionHTML == "" {
		t.Error("DescriptionHTML should not be empty")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from updated_at")
	}
	if !first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be true — Greenhouse only exposes updated_at, not a true creation date")
	}
	if first.Adapter != "ats_greenhouse" {
		t.Errorf("Adapter = %q, want ats_greenhouse", first.Adapter)
	}
}

// TestParse_CompanyName is schema.Posting.CompanyNameRaw's first real
// producer — a real board (bare numeric token, board is actually "Stitch
// Fix Annex") found while cleaning up discovery's bulk-added companies,
// where the token alone gives no readable name at all.
func TestParse_CompanyName(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "company_name.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].CompanyNameRaw != "Stitch Fix Annex" {
		t.Errorf("CompanyNameRaw = %q, want %q", postings[0].CompanyNameRaw, "Stitch Fix Annex")
	}
}

// TestParse_Standard's fixture has no company_name field at all — the
// common case, most boards' token already is a readable name — and
// CompanyNameRaw must stay empty rather than something fabricated.
func TestParse_CompanyNameAbsentStaysEmpty(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if postings[0].CompanyNameRaw != "" {
		t.Errorf("CompanyNameRaw = %q, want empty when the source has no company_name field", postings[0].CompanyNameRaw)
	}
}

// TestParse_EscapedContentIsUnescaped is the regression test for a real
// bug found while building the job detail page's HTML rendering:
// Greenhouse's own board API returns `content` HTML-entity-escaped a
// level too far ("&lt;p&gt;", not "<p>") — confirmed live against
// boards-api.greenhouse.io, not a hypothetical. Every job ingested via
// this adapter before the fix stored a description_html column that was
// unrenderable as HTML (a literal, ugly wall of "&lt;" and "&gt;" text)
// even though description_text (built by StripHTML, which already
// double-unescapes for its own unrelated reason) was unaffected.
func TestParse_EscapedContentIsUnescaped(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "escaped_content.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}

	got := postings[0].DescriptionHTML
	want := `<div class="content-intro"><p><strong>Who we are&nbsp;</strong></p>
<p>We ship things.</p></div>`
	if got != want {
		t.Errorf("DescriptionHTML =\n%q\nwant\n%q", got, want)
	}
}

// TestParse_Standard's fixture already stores content unescaped (the
// common case for Lever/Ashby, and apparently for some Greenhouse boards
// too) — UnescapeString on text with no entities must be a no-op, not
// something that mangles already-clean HTML.
func TestParse_AlreadyCleanContentIsUnchanged(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "standard.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "<p>Join our backend team building distributed systems.</p>"
	if postings[0].DescriptionHTML != want {
		t.Errorf("DescriptionHTML = %q, want %q (unchanged)", postings[0].DescriptionHTML, want)
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

func TestParse_SingleItem(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "single-item.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].ExternalID != "6200000" {
		t.Errorf("ExternalID = %q", postings[0].ExternalID)
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
	p := postings[0]
	if p.TitleRaw == "" {
		t.Error("TitleRaw should survive unicode content intact")
	}
	if p.LocationRaw != "Bengaluru, कर्नाटक, भारत" {
		t.Errorf("LocationRaw = %q, unicode should round-trip through JSON unmarshal unchanged", p.LocationRaw)
	}
}

func TestParse_MissingLocation(t *testing.T) {
	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), loadFixture(t, "missing-location.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty", postings[0].LocationRaw)
	}
	if postings[0].Department != "" {
		t.Errorf("Department = %q, want empty when departments is []", postings[0].Department)
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
	if a.Kind() != adapter.SourceKindGreenhouse {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindGreenhouse)
	}
}
