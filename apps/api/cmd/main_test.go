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
)

const testToken = "test-token-0123456789abcdef0123456789abcdef"

func TestRoutes(t *testing.T) {
	a, err := auth.New(testToken, true)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	h := routes(slog.New(slog.NewTextHandler(io.Discard, nil)), a)

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
	routes(slog.New(slog.NewTextHandler(io.Discard, nil)), a).
		ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil))

	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("/health response contains the auth token")
	}
}
