package smartrecruiters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
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
	return adapter.Source{ID: "src-1", URL: "https://api.smartrecruiters.com/v1/companies/AcmeHQ/postings"}
}

// --- Parse (operates on Fetch's already-combined body) ---

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
	if first.ExternalID != "post-001" {
		t.Errorf("ExternalID = %q", first.ExternalID)
	}
	if first.TitleRaw != "Backend Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	if first.ApplyURL != "https://jobs.smartrecruiters.com/AcmeHQ/post-001-backend-engineering-intern" {
		t.Errorf("ApplyURL = %q", first.ApplyURL)
	}
	if first.LocationRaw != "Bengaluru, Karnataka, in" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.RemoteHint {
		t.Error("RemoteHint should be false")
	}
	if first.DescriptionHTML != "" {
		t.Error("DescriptionHTML should be empty — the postings list has no description field (see package comment)")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from releasedDate")
	}
	if first.PostedAtEstimated {
		t.Error("PostedAtEstimated should be false — releasedDate is SmartRecruiters' own posting timestamp")
	}
	if first.Adapter != "ats_smartrecruiters" {
		t.Errorf("Adapter = %q, want ats_smartrecruiters", first.Adapter)
	}

	remote := postings[1]
	if !remote.RemoteHint {
		t.Error("RemoteHint should be true for the remote posting")
	}
	if remote.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty when city/region/country are all blank", remote.LocationRaw)
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
	if postings[0].LocationRaw != "São Paulo, São Paulo, br" {
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
	if a.Kind() != adapter.SourceKindSmartRecruiters {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindSmartRecruiters)
	}
}

// --- Fetch pagination — the one thing genuinely specific to this adapter ---

func pageResponse(totalFound int, ids ...string) []byte {
	content := make([]smartRecruitersPosting, len(ids))
	for i, id := range ids {
		content[i] = smartRecruitersPosting{ID: id, Name: "Job " + id, PostingURL: "https://jobs.smartrecruiters.com/AcmeHQ/" + id}
	}
	b, _ := json.Marshal(smartRecruitersResponse{TotalFound: totalFound, Content: content})
	return b
}

// fakePages returns a pageFetcher backed by an in-memory offset->response
// map — see fetchPages' own comment for why this stands in for a real
// HTTP round trip in these tests rather than an httptest.Server.
func fakePages(t *testing.T, byOffset map[string]*fetch.Result, requested *[]string) pageFetcher {
	t.Helper()
	return func(_ context.Context, reqURL string, _ adapter.FetchHints) (*fetch.Result, error) {
		u, err := url.Parse(reqURL)
		if err != nil {
			t.Fatalf("parse fake request url %q: %v", reqURL, err)
		}
		offset := u.Query().Get("offset")
		*requested = append(*requested, offset)
		result, ok := byOffset[offset]
		if !ok {
			return nil, fmt.Errorf("fakePages: no stub for offset %q", offset)
		}
		return result, nil
	}
}

func TestFetch_CombinesMultiplePages(t *testing.T) {
	var requested []string
	fetcher := fakePages(t, map[string]*fetch.Result{
		"0":   {StatusCode: 200, Body: pageResponse(3, "a", "b")},
		"100": {StatusCode: 200, Body: pageResponse(3, "c")},
	}, &requested)

	raw, err := fetchPages(context.Background(), adapter.Source{ID: "src-1", URL: "https://api.smartrecruiters.com/v1/companies/AcmeHQ/postings"}, adapter.FetchHints{}, fetcher)
	if err != nil {
		t.Fatalf("fetchPages: %v", err)
	}

	a := New(nil)
	postings, err := a.Parse(context.Background(), adapter.Source{}, raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 3 {
		t.Fatalf("got %d postings across pages, want 3 (a, b, c combined)", len(postings))
	}
	if len(requested) != 2 {
		t.Errorf("requested %d pages, want exactly 2 (offsets 0 and %d)", len(requested), pageSize)
	}
}

func TestFetch_StopsOnEmptyPage(t *testing.T) {
	var requested []string
	// totalFound (500) overstates what the fake server will ever actually
	// return — a real-world inconsistency this test proves doesn't hang
	// the loop: page 2 comes back empty, and that ends pagination.
	fetcher := fakePages(t, map[string]*fetch.Result{
		"0":   {StatusCode: 200, Body: pageResponse(500, "a")},
		"100": {StatusCode: 200, Body: pageResponse(500)},
	}, &requested)

	raw, err := fetchPages(context.Background(), adapter.Source{ID: "src-1", URL: "https://api.smartrecruiters.com/v1/companies/AcmeHQ/postings"}, adapter.FetchHints{}, fetcher)
	if err != nil {
		t.Fatalf("fetchPages: %v", err)
	}
	a := New(nil)
	postings, err := a.Parse(context.Background(), adapter.Source{}, raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 1 {
		t.Errorf("got %d postings, want 1 (pagination should stop at the first empty page)", len(postings))
	}
	if len(requested) != 2 {
		t.Errorf("requested %d pages, want exactly 2 (page 1 with data, page 2 empty, then stop)", len(requested))
	}
}

func TestFetch_NotModifiedShortCircuitsWithoutFurtherPages(t *testing.T) {
	var requested []string
	fetcher := fakePages(t, map[string]*fetch.Result{
		"0": {StatusCode: 304},
	}, &requested)

	raw, err := fetchPages(context.Background(), adapter.Source{ID: "src-1", URL: "https://api.smartrecruiters.com/v1/companies/AcmeHQ/postings"},
		adapter.FetchHints{IfNoneMatch: `"some-etag"`}, fetcher)
	if err != nil {
		t.Fatalf("fetchPages: %v", err)
	}
	if raw.StatusCode != 304 {
		t.Errorf("StatusCode = %d, want 304", raw.StatusCode)
	}
	if len(requested) != 1 {
		t.Errorf("made %d requests, want exactly 1 (a 304 on page 1 must not trigger further pages)", len(requested))
	}
}
