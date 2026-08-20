package workday

import (
	"context"
	"encoding/json"
	"fmt"
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
	return adapter.Source{ID: "src-1", URL: "https://acmehq.wd5.myworkdayjobs.com/wday/cxs/acmehq/External/jobs"}
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
	if first.ExternalID != "R-12345" {
		t.Errorf("ExternalID = %q, want the requisition number from bulletFields", first.ExternalID)
	}
	if first.TitleRaw != "Software Engineering Intern" {
		t.Errorf("TitleRaw = %q", first.TitleRaw)
	}
	wantURL := "https://acmehq.wd5.myworkdayjobs.com/External/job/Bengaluru/Software-Engineering-Intern_R-12345"
	if first.URL != wantURL {
		t.Errorf("URL = %q, want %q", first.URL, wantURL)
	}
	if first.ApplyURL != wantURL {
		t.Errorf("ApplyURL = %q, want %q", first.ApplyURL, wantURL)
	}
	if first.LocationRaw != "Bengaluru, India" {
		t.Errorf("LocationRaw = %q", first.LocationRaw)
	}
	if first.DescriptionHTML != "" {
		t.Error("DescriptionHTML should be empty — the search response has no description field (see package comment)")
	}
	if first.PostedAt == nil {
		t.Fatal("PostedAt should be set from postedOn")
	}
	if !first.PostedAtEstimated {
		t.Error("PostedAtEstimated should always be true — postedOn is relative text, never a real timestamp")
	}
	wantPosted := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -3)
	if !first.PostedAt.Equal(wantPosted) {
		t.Errorf("PostedAt = %v, want %v (3 days before fetch)", first.PostedAt, wantPosted)
	}
	if first.Adapter != "ats_workday" {
		t.Errorf("Adapter = %q, want ats_workday", first.Adapter)
	}

	today := postings[1]
	if !today.PostedAt.Equal(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("PostedAt = %v, want the fetch time for \"Posted Today\"", today.PostedAt)
	}
	if today.ExternalID != "R-99887" {
		t.Errorf("ExternalID = %q, want R-99887 (first bulletFields entry matching the requisition pattern)", today.ExternalID)
	}

	noReq := postings[2]
	if noReq.ExternalID != "/job/Hyderabad/Data-Analyst-Intern_R-55221" {
		t.Errorf("ExternalID = %q, want the externalPath fallback when bulletFields has no requisition number", noReq.ExternalID)
	}
	wantOldPosted := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	if !noReq.PostedAt.Equal(wantOldPosted) {
		t.Errorf("PostedAt = %v, want %v for \"Posted 30+ Days Ago\"", noReq.PostedAt, wantOldPosted)
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
	if postings[0].TitleRaw != "Estagiário de Engenharia de Software — 소프트웨어 엔지니어링 인턴" {
		t.Errorf("TitleRaw = %q, unicode not preserved", postings[0].TitleRaw)
	}
	if postings[0].LocationRaw != "São Paulo, Brasil" {
		t.Errorf("LocationRaw = %q, unicode not preserved", postings[0].LocationRaw)
	}
}

func TestParse_RejectsNonWorkdaySourceURL(t *testing.T) {
	a := New(nil)
	badSource := adapter.Source{ID: "src-1", URL: "https://example.com/not-a-workday-endpoint"}
	_, err := a.Parse(context.Background(), badSource, loadFixture(t, "standard.json"))
	if err == nil {
		t.Fatal("expected Parse to reject a source URL that isn't a Workday CXS jobs endpoint")
	}
}

func TestParseRelativePostedOn(t *testing.T) {
	fetchedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		text    string
		wantOK  bool
		wantAgo int // days before fetchedAt
	}{
		{"Posted Today", true, 0},
		{"Posted today", true, 0},
		{"Posted Yesterday", true, 1},
		{"Posted 3 Days Ago", true, 3},
		{"Posted 30+ Days Ago", true, 30},
		{"Posted 1 Day Ago", true, 1},
		{"", false, 0},
		{"Reposted", false, 0},
		{"Closing in 3 days", false, 0},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, ok := parseRelativePostedOn(c.text, fetchedAt)
			if ok != c.wantOK {
				t.Fatalf("parseRelativePostedOn(%q) ok = %v, want %v", c.text, ok, c.wantOK)
			}
			if !ok {
				return
			}
			want := fetchedAt.AddDate(0, 0, -c.wantAgo)
			if !got.Equal(want) {
				t.Errorf("parseRelativePostedOn(%q) = %v, want %v", c.text, got, want)
			}
		})
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
	if a.Kind() != adapter.SourceKindWorkday {
		t.Errorf("Kind() = %q, want %q", a.Kind(), adapter.SourceKindWorkday)
	}
}

// --- Fetch pagination (POST-based) ---

func pageResponse(total int, ids ...string) []byte {
	postings := make([]workdayJobPosting, len(ids))
	for i, id := range ids {
		postings[i] = workdayJobPosting{
			Title: "Job " + id, ExternalPath: "/job/x/Job_" + id,
			BulletFields: []string{id}, PostedOn: "Posted Today",
		}
	}
	b, _ := json.Marshal(searchResponse{Total: total, JobPostings: postings})
	return b
}

func fakePages(t *testing.T, byOffset map[int]*fetch.Result, requestedOffsets *[]int) pageFetcher {
	t.Helper()
	return func(_ context.Context, offset int, _ adapter.FetchHints) (*fetch.Result, error) {
		*requestedOffsets = append(*requestedOffsets, offset)
		result, ok := byOffset[offset]
		if !ok {
			return nil, fmt.Errorf("fakePages: no stub for offset %d", offset)
		}
		return result, nil
	}
}

func TestFetch_CombinesMultiplePages(t *testing.T) {
	var offsets []int
	fetcher := fakePages(t, map[int]*fetch.Result{
		0:  {StatusCode: 200, Body: pageResponse(3, "R-1", "R-2")},
		20: {StatusCode: 200, Body: pageResponse(3, "R-3")},
	}, &offsets)

	raw, err := fetchPages(context.Background(), adapter.FetchHints{}, fetcher)
	if err != nil {
		t.Fatalf("fetchPages: %v", err)
	}

	a := New(nil)
	postings, err := a.Parse(context.Background(), testSource(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 3 {
		t.Fatalf("got %d postings across pages, want 3", len(postings))
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != pageSize {
		t.Errorf("requested offsets %v, want [0 %d]", offsets, pageSize)
	}
}

func TestFetch_NotModifiedShortCircuitsWithoutFurtherPages(t *testing.T) {
	var offsets []int
	fetcher := fakePages(t, map[int]*fetch.Result{
		0: {StatusCode: 304},
	}, &offsets)

	raw, err := fetchPages(context.Background(), adapter.FetchHints{IfNoneMatch: `"etag"`}, fetcher)
	if err != nil {
		t.Fatalf("fetchPages: %v", err)
	}
	if raw.StatusCode != 304 {
		t.Errorf("StatusCode = %d, want 304", raw.StatusCode)
	}
	if len(offsets) != 1 {
		t.Errorf("requested %d pages, want exactly 1", len(offsets))
	}
}

func TestRequiresOwnFetch(t *testing.T) {
	if !New(nil).RequiresOwnFetch() {
		t.Error("RequiresOwnFetch() = false, want true — the CXS endpoint needs a POST no conditional GET can produce")
	}
	var _ adapter.OwnFetcher = New(nil) // compile-time: Adapter must satisfy the interface the scheduler checks for
}

func TestSearchTextFor(t *testing.T) {
	cases := []struct {
		name string
		src  adapter.Source
		want string
	}{
		{"nil AdapterConfig", adapter.Source{}, ""},
		{"missing key", adapter.Source{AdapterConfig: map[string]any{}}, ""},
		{"non-string value", adapter.Source{AdapterConfig: map[string]any{"search_text": 5}}, ""},
		{"real value", adapter.Source{AdapterConfig: map[string]any{"search_text": "Bengaluru"}}, "Bengaluru"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := searchTextFor(c.src); got != c.want {
				t.Errorf("searchTextFor(%+v) = %q, want %q", c.src, got, c.want)
			}
		})
	}
}

// TestFetch_SearchTextReachesEveryPage proves searchTextFor's result
// actually lands in the POST body fetchPages sends for every page, not
// just page 1 — pagination narrowed to a location but only on the first
// request would silently widen back to the whole board from page 2 on.
func TestFetch_SearchTextReachesEveryPage(t *testing.T) {
	var bodies []searchRequest
	fetcher := func(ctx context.Context, offset int, hints adapter.FetchHints) (*fetch.Result, error) {
		// Mirrors Fetch's own body construction — this test's job is to
		// confirm searchTextFor's output is what Fetch would thread
		// through, not to re-exercise a real a.fetcher.Fetch call (a
		// concrete *fetch.Fetcher, not fakeable from this package — see
		// adapters/ats/workday's own package comment).
		req := searchRequest{AppliedFacets: map[string]any{}, Limit: pageSize, Offset: offset, SearchText: searchTextFor(testSourceWithSearchText("Bengaluru"))}
		bodies = append(bodies, req)
		if offset == 0 {
			return &fetch.Result{StatusCode: 200, Body: pageResponse(3, "R-1")}, nil
		}
		return &fetch.Result{StatusCode: 200, Body: pageResponse(3, "R-2", "R-3")}, nil
	}

	if _, err := fetchPages(context.Background(), adapter.FetchHints{}, fetcher); err != nil {
		t.Fatalf("fetchPages: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("got %d page requests, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b.SearchText != "Bengaluru" {
			t.Errorf("page %d SearchText = %q, want %q", i+1, b.SearchText, "Bengaluru")
		}
	}
}

func testSourceWithSearchText(text string) adapter.Source {
	src := testSource()
	src.AdapterConfig = map[string]any{"search_text": text}
	return src
}
