package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFetchBasicGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/board" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Wed, 06 Aug 2026 09:15:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/board"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if string(result.Body) != `{"jobs":[]}` {
		t.Errorf("Body = %q", result.Body)
	}
	if result.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", result.ETag, `"abc123"`)
	}
	if result.LastModified != "Wed, 06 Aug 2026 09:15:00 GMT" {
		t.Errorf("LastModified = %q", result.LastModified)
	}
	if result.NotModified {
		t.Error("NotModified = true for a 200")
	}
	if result.Truncated {
		t.Error("Truncated = true for a small body")
	}
	if result.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
}

func TestFetchIdentifiesItself(t *testing.T) {
	// SCOUT-LEGAL-003, same requirement the robots checker satisfies: a
	// descriptive User-Agent with a contact URL, and a From header with the
	// operator's email.
	var gotUA, gotFrom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotFrom = r.Header.Get("From")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	if _, err := f.Fetch(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if !strings.Contains(gotUA, "Scout") {
		t.Errorf("User-Agent = %q, want it to name Scout", gotUA)
	}
	if gotFrom != "operator@example.com" {
		t.Errorf("From = %q, want the operator email", gotFrom)
	}
}

func TestFetchSendsConditionalHeaders(t *testing.T) {
	var gotINM, gotIMS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotINM = r.Header.Get("If-None-Match")
		gotIMS = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{
		URL:             srv.URL,
		IfNoneMatch:     `"abc123"`,
		IfModifiedSince: "Wed, 06 Aug 2026 09:15:00 GMT",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotINM != `"abc123"` {
		t.Errorf("If-None-Match sent = %q, want %q", gotINM, `"abc123"`)
	}
	if gotIMS != "Wed, 06 Aug 2026 09:15:00 GMT" {
		t.Errorf("If-Modified-Since sent = %q", gotIMS)
	}
	if !result.NotModified {
		t.Error("NotModified = false for a 304")
	}
	if result.StatusCode != http.StatusNotModified {
		t.Errorf("StatusCode = %d, want 304", result.StatusCode)
	}
}

func TestFetchOmitsConditionalHeadersWhenNotSet(t *testing.T) {
	// The first poll of a source has no prior ETag or Last-Modified — the
	// headers must be absent, not sent empty, which some servers treat
	// differently from "not present at all."
	var sawINM, sawIMS bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawINM = r.Header["If-None-Match"]
		_, sawIMS = r.Header["If-Modified-Since"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	if _, err := f.Fetch(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if sawINM {
		t.Error("If-None-Match was sent with no prior ETag")
	}
	if sawIMS {
		t.Error("If-Modified-Since was sent with no prior Last-Modified")
	}
}

func TestFetchDecodesGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Error("request did not offer gzip")
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"jobs":[1,2,3]}`))
		_ = zw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if string(result.Body) != `{"jobs":[1,2,3]}` {
		t.Errorf("Body = %q, want the decompressed JSON", result.Body)
	}
}

func TestFetchTruncatesOversizedBodies(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), MaxBodyBytes+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(oversized)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if !result.Truncated {
		t.Error("Truncated = false for a body over MaxBodyBytes")
	}
	if len(result.Body) != MaxBodyBytes {
		t.Errorf("len(Body) = %d, want exactly MaxBodyBytes (%d)", len(result.Body), MaxBodyBytes)
	}
}

func TestFetchDoesNotFlagAnExactlySizedBody(t *testing.T) {
	exact := bytes.Repeat([]byte("a"), MaxBodyBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(exact)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if result.Truncated {
		t.Error("Truncated = true for a body exactly at MaxBodyBytes")
	}
	if len(result.Body) != MaxBodyBytes {
		t.Errorf("len(Body) = %d, want MaxBodyBytes (%d)", len(result.Body), MaxBodyBytes)
	}
}

func TestFetchFollowsRedirectsUpToTheLimit(t *testing.T) {
	var mux http.ServeMux
	srv := httptest.NewServer(&mux)
	defer srv.Close()

	// Exactly maxRedirects (3) hops, landing on a 200 — must succeed.
	mux.HandleFunc("/hop0", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/hop1", http.StatusFound)
	})
	mux.HandleFunc("/hop1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/hop2", http.StatusFound)
	})
	mux.HandleFunc("/hop2", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/hop0"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(result.Body) != "ok" {
		t.Errorf("Body = %q, want to have reached /final", result.Body)
	}
	if result.FinalURL != srv.URL+"/final" {
		t.Errorf("FinalURL = %q, want %q — apps/collector/internal/emailalert's tracking-redirect resolution depends on this", result.FinalURL, srv.URL+"/final")
	}
}

func TestFetchRefusesTooManyRedirects(t *testing.T) {
	var mux http.ServeMux
	srv := httptest.NewServer(&mux)
	defer srv.Close()

	for i := 0; i < 5; i++ {
		next := i + 1
		mux.HandleFunc("/hop"+strconv.Itoa(i), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, srv.URL+"/hop"+strconv.Itoa(next), http.StatusFound)
		})
	}

	f := newTestFetcher(plainDialContext)
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/hop0"})
	if err == nil {
		t.Fatal("Fetch followed more than maxRedirects hops without error")
	}
}

func TestFetchRefusesASchemeChangingRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect https-looking traffic to a plain-http URL. httptest servers
		// are plain HTTP, so this simulates the downgrade by redirecting to a
		// URL whose scheme literally differs from the request's.
		http.Redirect(w, r, "ftp://"+r.Host+"/x", http.StatusFound)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err == nil {
		t.Fatal("Fetch followed a scheme-changing redirect without error")
	}
}

func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	f := newTestFetcher(plainDialContext)
	_, err := f.Fetch(context.Background(), Request{URL: "ftp://example.com/board"})
	if err == nil {
		t.Fatal("Fetch accepted a non-http(s) scheme")
	}
}

// TestFetchRefusesLoopbackTarget uses the REAL New() constructor, not the
// test dialer — proving the production Fetcher, exactly as the collector will
// construct it, actually refuses to reach an internal address rather than
// merely that the underlying dial function would if called directly (which
// TestSafeDialContextRefusesLoopback in dial_test.go already covers).
func TestFetchRefusesLoopbackTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New("https://scout.example/bot", "operator@example.com")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := f.Fetch(ctx, Request{URL: srv.URL})
	if err == nil {
		t.Fatal("the production Fetcher reached an httptest.Server on 127.0.0.1, want a refusal")
	}
}

func TestFetchRespectsContextCancellation(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	// Ordered so close(block) unwinds before srv.Close() — see the identical
	// note in politeness/heartbeat's tests for why the order matters.
	defer srv.Close()
	defer close(block)

	f := newTestFetcher(plainDialContext)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := f.Fetch(ctx, Request{URL: srv.URL})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Fetch succeeded despite the context deadline")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Fetch took %v to fail after a 100ms deadline, want it to fail promptly", elapsed)
	}
}

func TestFetchReportsErrorStatusesAsResultsNotErrors(t *testing.T) {
	// A 4xx or 5xx is information the scheduler needs (docs/06 section 10's
	// error-handling table branches on exact status), not a Go error — the
	// request itself completed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch returned an error for a 503: %v", err)
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", result.StatusCode)
	}
}

// TestFetchPostSendsMethodBodyAndContentType covers the capability
// adapters/ats/workday needs — Workday's CXS search endpoint requires a
// POST with a JSON body, unlike every other adapter's plain GET.
func TestFetchPostSendsMethodBodyAndContentType(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"total":0,"jobPostings":[]}`))
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	result, err := f.Fetch(context.Background(), Request{
		URL: srv.URL, Method: http.MethodPost,
		Body: []byte(`{"limit":20,"offset":0}`), ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody != `{"limit":20,"offset":0}` {
		t.Errorf("body = %q, want the request body sent verbatim", gotBody)
	}
}

// TestFetchDefaultMethodIsStillGet is the regression guard for every
// other adapter: Request{} with no Method set must behave exactly as it
// did before Method/Body/ContentType existed.
func TestFetchDefaultMethodIsStillGet(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newTestFetcher(plainDialContext)
	if _, err := f.Fetch(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET (the zero-value default)", gotMethod)
	}
}
