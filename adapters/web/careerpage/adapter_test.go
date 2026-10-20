package careerpage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
)

var fetchedAt = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("fixtures/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func raw(body []byte) *adapter.RawResponse {
	return &adapter.RawResponse{Body: body, StatusCode: 200, FetchedAt: fetchedAt}
}

func src(url string, cfg map[string]any) adapter.Source {
	return adapter.Source{ID: "src-1", URL: url, AdapterConfig: cfg}
}

const pageURL = "https://careers.acme.example/jobs/swe-intern-1042"

func TestParse_Standard(t *testing.T) {
	a := NewHTMLPage(nil)
	got, err := a.Parse(context.Background(), src(pageURL, nil), raw(fixture(t, "standard.html")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d postings, want 1 (the Organization block is not a job)", len(got))
	}
	p := got[0]
	checks := map[string][2]string{
		"ExternalID":          {p.ExternalID, "REQ-1042"},
		"URL":                 {p.URL, pageURL},
		"ApplyURL":            {p.ApplyURL, pageURL},
		"TitleRaw":            {p.TitleRaw, "Software Engineering Intern"},
		"DescriptionHTML":     {p.DescriptionHTML, "<p>Build payments infrastructure with our Bengaluru team.</p>"},
		"CompanyNameRaw":      {p.CompanyNameRaw, "Acme India"},
		"DomainHint":          {p.DomainHint, "acme.example"},
		"EmploymentTypeRaw":   {p.EmploymentTypeRaw, "INTERN, PART_TIME"},
		"LocationRaw":         {p.LocationRaw, "Bengaluru, Karnataka, IN"},
		"CompensationRawText": {p.CompensationRawText, "INR 800000 - 1200000 per year"},
		"Adapter":             {p.Adapter, "html_page"},
	}
	for field, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", field, c[0], c[1])
		}
	}
	if p.PostedAt == nil || p.PostedAt.Format("2006-01-02") != "2026-10-01" || p.PostedAtEstimated {
		t.Errorf("PostedAt = %v estimated=%v, want 2026-10-01 exact", p.PostedAt, p.PostedAtEstimated)
	}
	if p.DeadlineAt == nil || p.DeadlineAt.Format("2006-01-02") != "2026-12-31" {
		t.Errorf("DeadlineAt = %v", p.DeadlineAt)
	}
	if err := a.Validate(context.Background(), src(pageURL, nil), got); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestParse_GraphListingAndRemote(t *testing.T) {
	a := NewHTMLPage(nil)
	got, err := a.Parse(context.Background(), src("https://careers.acme.example/jobs", nil), raw(fixture(t, "listing.html")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d postings, want 2", len(got))
	}
	if !got[0].RemoteHint || got[0].CompensationRawText != "INR 35000 per month" || got[0].CompanyNameRaw != "Acme India" {
		t.Errorf("first posting = %+v", got[0])
	}
	if got[1].LocationRaw != "Hyderabad, India; Pune, India" {
		t.Errorf("multi-location = %q", got[1].LocationRaw)
	}
}

func TestParse_EmptyAndMalformed(t *testing.T) {
	a := NewHTMLPage(nil)
	tests := []struct {
		name    string
		fixture string
		want    int
		wantErr bool
	}{
		{"no job markup", "empty.html", 0, false},
		{"only broken job JSON", "malformed.html", 0, true},
		{"not HTML at all", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.fixture != "" {
				body = fixture(t, tt.fixture)
			}
			got, err := a.Parse(context.Background(), src(pageURL, nil), raw(body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.want {
				t.Errorf("got %d postings, want %d", len(got), tt.want)
			}
		})
	}
}

func TestParse_Unicode(t *testing.T) {
	a := NewHTMLPage(nil)
	got, err := a.Parse(context.Background(), src("https://careers.acme.example/jobs", map[string]any{"company_name": "Acme"}), raw(fixture(t, "unicode.html")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d postings, want 2 (second has raw newline/tab inside strings)", len(got))
	}
	if got[0].TitleRaw != "सॉफ़्टवेयर इंजीनियर इंटर्न — पुणे" || got[0].LocationRaw != "पुणे, IN" {
		t.Errorf("devanagari posting = %q / %q", got[0].TitleRaw, got[0].LocationRaw)
	}
	if got[0].CompensationRawText != "₹ 8 - 12 LPA" {
		t.Errorf("LPA text must pass through untouched, got %q", got[0].CompensationRawText)
	}
	if !strings.Contains(got[0].DescriptionHTML, "Café ☕") {
		t.Errorf("description mangled: %q", got[0].DescriptionHTML)
	}
	if got[1].TitleRaw != "தரவு ஆய்வாளர் Trainee" {
		t.Errorf("tamil title = %q", got[1].TitleRaw)
	}
	if got[1].CompensationRawText != "INR 1,00,000" || got[1].CompanyNameRaw != "Acme" {
		t.Errorf("second posting comp=%q company=%q", got[1].CompensationRawText, got[1].CompanyNameRaw)
	}
}

func TestCompensationText(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"LPA free text", "8-12 LPA", "8-12 LPA"},
		{"lakh free text", "₹5 lakh per annum", "₹5 lakh per annum"},
		{"entity-encoded rupee", "&#8377; 6 LPA", "₹ 6 LPA"},
		{"annual range", obj{"currency": "INR", "value": obj{"minValue": 800000.0, "maxValue": 1200000.0, "unitText": "YEAR"}}, "INR 800000 - 1200000 per year"},
		{"monthly stipend", obj{"currency": "INR", "value": obj{"value": 25000.0, "unitText": "MONTH"}}, "INR 25000 per month"},
		{"indian grouping string", obj{"currency": "INR", "value": "1,00,000"}, "INR 1,00,000"},
		{"flat value with unit", obj{"currency": "USD", "unitText": "HOUR", "value": 30.0}, "USD 30 per hour"},
		{"equal bounds collapse", obj{"currency": "INR", "value": obj{"minValue": 500000.0, "maxValue": 500000.0, "unitText": "YEAR"}}, "INR 500000 per year"},
		{"no numbers", obj{"currency": "INR", "value": obj{"unitText": "YEAR"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compensationText(tt.in); got != tt.want {
				t.Errorf("compensationText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSitemap(t *testing.T) {
	doc, err := parseSitemap(fixture(t, "sitemap.xml"))
	if err != nil {
		t.Fatalf("parseSitemap: %v", err)
	}
	if len(doc.urls) != 5 || doc.urls[1].lastmod != "2026-10-10T08:00:00Z" || doc.urls[0].lastmod != "2026-08-01T00:00:00Z" {
		t.Errorf("urls = %+v", doc.urls)
	}
	idx, err := parseSitemap(fixture(t, "sitemap-index.xml"))
	if err != nil || len(idx.sitemaps) != 2 || len(idx.urls) != 0 {
		t.Errorf("index = %+v err=%v", idx, err)
	}
	if _, err := parseSitemap([]byte("<html><body>nope</body></html>")); err == nil {
		t.Error("an HTML page must not parse as a sitemap")
	}
	if _, err := parseSitemap([]byte("<urlset><url><loc>x")); err == nil {
		// truncated XML is an error, not a silently empty sitemap
		t.Error("truncated sitemap should error")
	}
}

// fakeFetcher serves canned responses and records every URL requested.
type fakeFetcher struct {
	pages map[string]*fetch.Result
	calls []string
}

func (f *fakeFetcher) Fetch(_ context.Context, r fetch.Request) (*fetch.Result, error) {
	f.calls = append(f.calls, r.URL)
	if res, ok := f.pages[r.URL]; ok {
		cp := *res
		cp.FetchedAt = fetchedAt
		return &cp, nil
	}
	return &fetch.Result{StatusCode: 404, FetchedAt: fetchedAt}, nil
}

func ok(body []byte) *fetch.Result { return &fetch.Result{StatusCode: 200, Body: body, ETag: `"v1"`} }

// fakeGate records every URL cleared and refuses any in deny.
type fakeGate struct {
	allowed  []string
	deny     map[string]bool
	released int
}

func (g *fakeGate) Allow(_ context.Context, u string) (func(context.Context), error) {
	if g.deny[u] {
		return nil, fmt.Errorf("%w: robots", adapter.ErrRequestRefused)
	}
	g.allowed = append(g.allowed, u)
	return func(context.Context) { g.released++ }, nil
}

func jobPage(t *testing.T, path string) *fetch.Result {
	body := strings.ReplaceAll(string(fixture(t, "job-page.html")), "PATH", path)
	return ok([]byte(body))
}

func sitemapSetup(t *testing.T) (*fakeFetcher, *fakeGate) {
	f := &fakeFetcher{pages: map[string]*fetch.Result{
		"https://careers.acme.example/sitemap.xml":        ok(fixture(t, "sitemap.xml")),
		"https://careers.acme.example/jobs/old":           jobPage(t, "old"),
		"https://careers.acme.example/jobs/new":           jobPage(t, "new"),
		"https://careers.acme.example/jobs/nolastmod":     jobPage(t, "nolastmod"),
		"https://careers.acme.example/about":              ok([]byte("<html>about</html>")),
		"https://evil.example/jobs/offsite":               jobPage(t, "offsite"),
		"https://careers.acme.example/sitemap-jobs-1.xml": ok(fixture(t, "sitemap.xml")),
	}}
	return f, &fakeGate{deny: map[string]bool{}}
}

func TestFetch_SitemapGatesEveryFollowUpRequest(t *testing.T) {
	f, g := sitemapSetup(t)
	a := NewSitemap(f)
	s := src("https://careers.acme.example/sitemap.xml", map[string]any{"url_include": "/jobs/"})
	s.Gate = g

	resp, err := a.Fetch(context.Background(), s, adapter.FetchHints{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Rule 1: every request except the root (cleared by the scheduler) was
	// cleared by the gate first, and none went to the off-domain host.
	if len(f.calls) != 1+len(g.allowed) {
		t.Fatalf("requests=%v gate-cleared=%v: every follow-up needs exactly one clearance", f.calls, g.allowed)
	}
	for i, u := range g.allowed {
		if f.calls[i+1] != u {
			t.Errorf("request %d was %s, gate cleared %s", i+1, f.calls[i+1], u)
		}
		if strings.Contains(u, "evil.example") || strings.HasSuffix(u, "/about") {
			t.Errorf("must not visit %s", u)
		}
	}
	if g.released != len(g.allowed) {
		t.Errorf("released %d of %d gate slots", g.released, len(g.allowed))
	}
	// Newest lastmod first, undated last.
	want := []string{"https://careers.acme.example/jobs/new", "https://careers.acme.example/jobs/old", "https://careers.acme.example/jobs/nolastmod"}
	if strings.Join(g.allowed, ",") != strings.Join(want, ",") {
		t.Errorf("visit order = %v, want %v", g.allowed, want)
	}
	if resp.ETag != `"v1"` {
		t.Errorf("root validators not carried through: %q", resp.ETag)
	}

	postings, err := a.Parse(context.Background(), s, resp)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(postings) != 3 {
		t.Fatalf("got %d postings, want 3", len(postings))
	}
	// datePosted wins; none of the fixture pages omit it, so none are estimated.
	for _, p := range postings {
		if p.PostedAtEstimated {
			t.Errorf("%s: PostedAtEstimated with an explicit datePosted", p.ExternalID)
		}
	}
}

func TestFetch_NilGateFailsClosed(t *testing.T) {
	f, _ := sitemapSetup(t)
	a := NewSitemap(f)
	_, err := a.Fetch(context.Background(), src("https://careers.acme.example/sitemap.xml", nil), adapter.FetchHints{})
	if err == nil {
		t.Fatal("Fetch with no gate must error rather than fetch ungated pages")
	}
	if len(f.calls) != 1 {
		t.Errorf("requests = %v; only the scheduler-cleared root may be fetched", f.calls)
	}
}

func TestFetch_RefusedURLIsSkipped(t *testing.T) {
	f, g := sitemapSetup(t)
	g.deny["https://careers.acme.example/jobs/old"] = true
	a := NewSitemap(f)
	s := src("https://careers.acme.example/sitemap.xml", map[string]any{"url_include": "/jobs/"})
	s.Gate = g
	resp, err := a.Fetch(context.Background(), s, adapter.FetchHints{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, c := range f.calls {
		if strings.HasSuffix(c, "/jobs/old") {
			t.Errorf("a gate-refused URL was requested: %s", c)
		}
	}
	postings, _ := a.Parse(context.Background(), s, resp)
	if len(postings) != 2 {
		t.Errorf("got %d postings, want 2", len(postings))
	}
}

func TestFetch_MaxPagesAndIndex(t *testing.T) {
	f, g := sitemapSetup(t)
	f.pages["https://careers.acme.example/idx.xml"] = ok(fixture(t, "sitemap-index.xml"))
	a := NewSitemap(f)
	s := src("https://careers.acme.example/idx.xml", map[string]any{
		"url_include": "/jobs/", "sitemap_include": "sitemap-jobs", "max_pages": 1.0,
	})
	s.Gate = g
	if _, err := a.Fetch(context.Background(), s, adapter.FetchHints{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// 1 child sitemap + 1 job page; the blog sitemap is excluded by pattern.
	if len(g.allowed) != 2 || g.allowed[0] != "https://careers.acme.example/sitemap-jobs-1.xml" {
		t.Errorf("gate-cleared = %v", g.allowed)
	}
}

func TestFetch_RootNotModifiedAndRateLimited(t *testing.T) {
	for _, status := range []int{304, 429, 404} {
		f := &fakeFetcher{pages: map[string]*fetch.Result{"https://careers.acme.example/sitemap.xml": {StatusCode: status, RetryAfter: "30"}}}
		g := &fakeGate{}
		s := src("https://careers.acme.example/sitemap.xml", nil)
		s.Gate = g
		resp, err := NewSitemap(f).Fetch(context.Background(), s, adapter.FetchHints{IfNoneMatch: `"v1"`})
		if err != nil || resp.StatusCode != status || len(f.calls) != 1 || len(g.allowed) != 0 {
			t.Errorf("status %d: resp=%+v err=%v calls=%v", status, resp, err, f.calls)
		}
	}
}

func TestFetch_HTMLPageFollowsLinksOnlyWhenNoEmbeddedJobs(t *testing.T) {
	listing := `<html><body>
	  <a href="/jobs/one#apply">One</a><a href="/jobs/one">dup</a><a href="/blog/x">blog</a>
	  <a href="https://elsewhere.example/jobs/z">off-site</a></body></html>`
	f := &fakeFetcher{pages: map[string]*fetch.Result{
		"https://careers.acme.example/open":     ok([]byte(listing)),
		"https://careers.acme.example/jobs/one": jobPage(t, "one"),
		"https://careers.acme.example/single":   ok(fixture(t, "standard.html")),
	}}
	g := &fakeGate{}
	a := NewHTMLPage(f)

	s := src("https://careers.acme.example/open", map[string]any{"url_include": "/jobs/"})
	s.Gate = g
	resp, err := a.Fetch(context.Background(), s, adapter.FetchHints{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(g.allowed) != 1 || g.allowed[0] != "https://careers.acme.example/jobs/one" {
		t.Errorf("followed %v, want only /jobs/one once", g.allowed)
	}
	if ps, _ := a.Parse(context.Background(), s, resp); len(ps) != 1 {
		t.Errorf("got %d postings", len(ps))
	}

	// A page that embeds its own JobPosting is not crawled further.
	g2 := &fakeGate{}
	s2 := src("https://careers.acme.example/single", map[string]any{"url_include": "/jobs/"})
	s2.Gate = g2
	if _, err := a.Fetch(context.Background(), s2, adapter.FetchHints{}); err != nil || len(g2.allowed) != 0 {
		t.Errorf("err=%v followed=%v", err, g2.allowed)
	}
}

func TestParseConfig_BadRegexp(t *testing.T) {
	if _, err := parseConfig(map[string]any{"url_include": "("}); err == nil {
		t.Error("an invalid url_include must be rejected, not ignored")
	}
}

func TestSameSite(t *testing.T) {
	tests := []struct {
		other string
		want  bool
	}{
		{"https://careers.acme.example/x", true},
		{"https://jobs.acme.example/x", true},
		{"https://acme.example.evil.test/x", false},
		{"https://evil.example/x", false},
		{"ftp://careers.acme.example/x", false},
		{"javascript:alert(1)", false},
	}
	for _, tt := range tests {
		if got := sameSite("https://careers.acme.example/sitemap.xml", tt.other); got != tt.want {
			t.Errorf("sameSite(%q) = %v, want %v", tt.other, got, tt.want)
		}
	}
}

func TestFetch_ErrorsPropagate(t *testing.T) {
	a := NewSitemap(errFetcher{})
	if _, err := a.Fetch(context.Background(), src("https://careers.acme.example/sitemap.xml", nil), adapter.FetchHints{}); err == nil {
		t.Error("expected the network error")
	}
}

type errFetcher struct{}

func (errFetcher) Fetch(context.Context, fetch.Request) (*fetch.Result, error) {
	return nil, errors.New("boom")
}
