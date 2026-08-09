package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A token long enough to satisfy MinTokenLength. Test-only, obviously.
const testToken = "test-token-0123456789abcdef0123456789abcdef"

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mustAuth(t *testing.T) *Authenticator {
	t.Helper()
	a, err := New(testToken, true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNewRejectsUnusableTokens(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"empty fails closed", "", ErrNoToken},
		{"short token rejected", "hunter2", ErrTokenTooShort},
		{"one below the minimum rejected", strings.Repeat("a", MinTokenLength-1), ErrTokenTooShort},
		{"exactly the minimum accepted", strings.Repeat("a", MinTokenLength), nil},
		{"long token accepted", testToken, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := New(tt.token, true)
			if !errors.Is(err, tt.want) {
				t.Fatalf("New(%q) error = %v, want %v", tt.name, err, tt.want)
			}
			if tt.want == nil && a == nil {
				t.Fatal("New returned nil Authenticator without an error")
			}
			if tt.want != nil && a != nil {
				// An Authenticator returned alongside an error is the shape that
				// leads to a caller using it anyway.
				t.Fatal("New returned an Authenticator alongside an error")
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		cookie     *http.Cookie
		wantStatus int
	}{
		{
			name:       "no credential",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct bearer token",
			header:     "Bearer " + testToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "bearer scheme is case-insensitive",
			header:     "bearer " + testToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong token",
			header:     "Bearer " + strings.Repeat("b", len(testToken)),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "token that is a prefix of the real one",
			// Guards the constant-time comparison: a prefix must not pass, and
			// must not be distinguishable from any other wrong answer.
			header:     "Bearer " + testToken[:len(testToken)-1],
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token with trailing whitespace is trimmed and accepted",
			header:     "Bearer " + testToken + "  ",
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong scheme",
			header:     "Basic " + testToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "raw token without a scheme",
			header:     testToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid session cookie",
			cookie:     &http.Cookie{Name: SessionCookie, Value: testToken},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong session cookie",
			cookie:     &http.Cookie{Name: SessionCookie, Value: "nope"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unrelated cookie",
			cookie:     &http.Cookie{Name: "other", Value: testToken},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "a stale cookie does not rescue a bad header",
			// The header wins outright. If it is present and wrong, the request
			// fails even when a valid cookie is also attached.
			header:     "Bearer wrong",
			cookie:     &http.Cookie{Name: SessionCookie, Value: testToken},
			wantStatus: http.StatusUnauthorized,
		},
	}

	a := mustAuth(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/jobs", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}

			w := httptest.NewRecorder()
			a.Middleware(testLogger(), next).ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if w.Code == http.StatusUnauthorized {
				if got := w.Header().Get("WWW-Authenticate"); got == "" {
					t.Error("401 without a WWW-Authenticate header")
				}
			}
		})
	}
}

func TestSessionHandler(t *testing.T) {
	a := mustAuth(t)

	t.Run("valid token sets a hardened cookie", func(t *testing.T) {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/session", nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()

		a.SessionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want 1", len(cookies))
		}
		c := cookies[0]

		switch {
		case c.Name != SessionCookie:
			t.Errorf("cookie name = %q, want %q", c.Name, SessionCookie)
		case !c.HttpOnly:
			t.Error("cookie is not HttpOnly: script can read the token")
		case !c.Secure:
			t.Error("cookie is not Secure: the token can cross plaintext")
		case c.SameSite != http.SameSiteStrictMode:
			t.Errorf("cookie SameSite = %v, want Strict", c.SameSite)
		case c.MaxAge <= 0:
			t.Errorf("cookie MaxAge = %d, want a long-lived cookie", c.MaxAge)
		}
	})

	t.Run("cookie alone cannot mint a new cookie", func(t *testing.T) {
		// The exchange is for a caller holding the actual token. Letting a
		// session refresh itself is a flow Scout does not have.
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		w := httptest.NewRecorder()

		a.SessionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("wrong token issues no cookie", func(t *testing.T) {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/session", nil)
		r.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()

		a.SessionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
		if cookies := w.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("got %d cookies on a rejected exchange, want 0", len(cookies))
		}
	})
}

func TestLogoutClearsTheCookie(t *testing.T) {
	a := mustAuth(t)
	w := httptest.NewRecorder()

	a.LogoutHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the browser drops it", cookies[0].MaxAge)
	}
	if cookies[0].Value != "" {
		t.Error("logout left the token in the cookie value")
	}
}

func TestIdentity(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string // "" means no identity; "stable" means non-empty
	}{
		{"absent header yields no identity", "", ""},
		{"present header yields a fingerprint", "someone@example.com", "stable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Tailscale-User-Login", tt.header)
			}

			got := Identity(r)

			if tt.want == "" {
				if got != "" {
					t.Fatalf("Identity = %q, want empty", got)
				}
				return
			}
			if got == "" {
				t.Fatal("Identity is empty, want a fingerprint")
			}
			// Rule 7: the address itself must not survive into anything
			// loggable.
			if strings.Contains(got, "@") || strings.Contains(got, "example.com") {
				t.Fatalf("Identity = %q, which leaks the address", got)
			}
		})
	}

	t.Run("fingerprint is stable and case-insensitive", func(t *testing.T) {
		lower := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		lower.Header.Set("Tailscale-User-Login", "someone@example.com")
		upper := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		upper.Header.Set("Tailscale-User-Login", "SomeOne@Example.COM")

		if Identity(lower) != Identity(upper) {
			t.Error("the same login produced two fingerprints")
		}
	})

	t.Run("different logins produce different fingerprints", func(t *testing.T) {
		a := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		a.Header.Set("Tailscale-User-Login", "laptop@example.com")
		b := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		b.Header.Set("Tailscale-User-Login", "phone@example.com")

		if Identity(a) == Identity(b) {
			t.Error("two logins collided; the fingerprint cannot distinguish devices")
		}
	})

	t.Run("identity alone does not authenticate", func(t *testing.T) {
		// The failure this guards against is treating a proxy-supplied header as
		// proof. It is evidence about an already-authenticated request, never a
		// grant.
		auth := mustAuth(t)
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/jobs", nil)
		r.Header.Set("Tailscale-User-Login", "someone@example.com")
		w := httptest.NewRecorder()

		auth.Middleware(testLogger(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: a Tailscale header granted access", w.Code, http.StatusUnauthorized)
		}
	})
}
