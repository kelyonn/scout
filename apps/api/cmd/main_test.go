package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kelyon/scout/apps/api/internal/auth"
	"github.com/kelyon/scout/apps/api/internal/jobs"
	"github.com/kelyon/scout/apps/api/internal/resume"
	"github.com/kelyon/scout/apps/api/internal/search"
	"github.com/kelyon/scout/apps/api/internal/stream"
)

const testToken = "test-token-0123456789abcdef0123456789abcdef"

// testResumeHandler and testJobsHandler build handlers with no real
// pool/queue — TestRoutes and TestHealthDoesNotLeakTheToken never actually
// invoke either (no test case here hits /v1/resume or /v1/jobs); they only
// need to exist to satisfy routes' signature. Real behavior is covered by
// apps/api/internal/resume and apps/api/internal/jobs' own tests, against
// a real database.
func testResumeHandler() *resume.Handler {
	return resume.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testJobsHandler() *jobs.Handler {
	return jobs.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testSearchHandler() *search.Handler {
	return search.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testStreamHandler() *stream.Handler {
	return stream.New(stream.NewBroker())
}

func TestRoutes(t *testing.T) {
	a, err := auth.New(testToken, true)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	h := routes(slog.New(slog.NewTextHandler(io.Discard, nil)), a, testResumeHandler(), testJobsHandler(), testSearchHandler(), testStreamHandler())

	tests := []struct {
		name       string
		method     string
		path       string
		withToken  bool
		wantStatus int
	}{
		{
			name:       "health is reachable without a credential",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
		},
		{
			name:       "the token exchange is reachable without a cookie",
			method:     http.MethodPost,
			path:       "/auth/session",
			withToken:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:   "an unknown path is 401, not 404, when unauthenticated",
			method: http.MethodGet,
			path:   "/v1/does-not-exist",
			// The distinction matters: 404 would tell an unauthenticated caller
			// which routes exist.
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "an unknown path is 404 once authenticated",
			method:     http.MethodGet,
			path:       "/v1/does-not-exist",
			withToken:  true,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "the root path is authenticated",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			if tt.withToken {
				r.Header.Set("Authorization", "Bearer "+testToken)
			}
			w := httptest.NewRecorder()

			h.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.path, w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHealthDoesNotLeakTheToken(t *testing.T) {
	// AGENTS.md rule 7. The health endpoint is the one thing an unauthenticated
	// caller can reach, so it is the one place a careless status payload would
	// be readable by anything on the tailnet.
	a, err := auth.New(testToken, true)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	w := httptest.NewRecorder()
	routes(slog.New(slog.NewTextHandler(io.Discard, nil)), a, testResumeHandler(), testJobsHandler(), testSearchHandler(), testStreamHandler()).
		ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil))

	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("/health response contains the auth token")
	}
}

func TestMetricsIsReachableWithoutACredential(t *testing.T) {
	a, err := auth.New(testToken, true)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	h := routes(slog.New(slog.NewTextHandler(io.Discard, nil)), a, testResumeHandler(), testJobsHandler(), testSearchHandler(), testStreamHandler())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "go_goroutines") {
		t.Error("response doesn't look like a Prometheus exposition (no go_goroutines line)")
	}
}

// flusherRecorder is httptest.NewRecorder's ResponseWriter with its own
// Flush() call counted — httptest.ResponseRecorder already implements
// http.Flusher as a no-op, which would make a broken passthrough in
// statusRecorder invisible to a test using it directly. Wrapping it lets
// this test actually observe whether the call reached the underlying
// writer.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flusherRecorder) Flush() { f.flushes++ }

// TestStatusRecorderForwardsFlush is the regression test for a real risk
// this middleware introduced: GET /v1/stream's SSE handler
// (apps/api/internal/stream/handler.go) type-asserts its ResponseWriter
// to http.Flusher and silently stops flushing — breaking the live feed
// with no error anywhere — if that capability doesn't survive being
// wrapped by observeRequests' statusRecorder.
func TestStatusRecorderForwardsFlush(t *testing.T) {
	inner := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	flusher, ok := http.ResponseWriter(rec).(http.Flusher)
	if !ok {
		t.Fatal("statusRecorder does not implement http.Flusher")
	}
	flusher.Flush()
	flusher.Flush()

	if inner.flushes != 2 {
		t.Errorf("inner.flushes = %d, want 2 — Flush() is not reaching the underlying ResponseWriter", inner.flushes)
	}
}
