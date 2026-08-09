// Package auth implements Scout's authentication: a single static bearer token
// behind the Tailscale network gate.
//
// The rationale is [ADR-015]. In short: the API is not reachable from the public
// internet, so authentication is a second factor behind network membership
// rather than the gate that keeps strangers out. That makes a static token the
// right size — it works identically for the browser, the Android WebView, curl,
// and any script, and its entire credential lifecycle is "change the variable
// and restart".
//
// Three details are load-bearing and are the classic ways this mechanism is got
// wrong:
//
//  1. Comparison is constant-time, over fixed-length digests rather than the raw
//     strings, so neither the token's contents nor its length leaks by timing.
//  2. The service refuses to start without a token. A missing secret must fail
//     closed; an API that silently serves unauthenticated because an environment
//     variable was misspelled is worse than no auth at all.
//  3. The token is never logged, in any form, at any level (AGENTS.md rule 7).
//
// [ADR-015]: ../../../../docs/adr/ADR-015-single-user-auth.md
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// SessionCookie is the name of the cookie the browser presents after exchanging
// the token once. Its value is the token itself: there is no session store,
// because a session store is state that would have to live somewhere durable and
// Redis holds nothing durable (AGENTS.md rule 8). For one user with one secret,
// a server-side session table buys nothing that rotating the variable does not.
const SessionCookie = "scout_session"

// MinTokenLength is the shortest token that will be accepted at startup.
//
// 32 characters is roughly 128 bits at base64, which is far past brute-forcible
// by an attacker who must already be on the tailnet to try. The check exists to
// catch a human typing a memorable password into the variable, not to defend a
// threshold.
const MinTokenLength = 32

// ErrNoToken is returned by New when SCOUT_AUTH_TOKEN is unset or empty.
var ErrNoToken = errors.New("auth: SCOUT_AUTH_TOKEN is not set")

// ErrTokenTooShort is returned by New when the configured token is shorter than
// [MinTokenLength].
var ErrTokenTooShort = errors.New("auth: SCOUT_AUTH_TOKEN is too short")

// Authenticator validates presented credentials against the configured token.
//
// It holds the SHA-256 digest of the token rather than the token itself. This is
// not meaningful protection — anything that can read process memory can read the
// environment too — but it does mean the plaintext is not sitting in a
// long-lived struct waiting to be caught by a panic dump or a careless %+v.
type Authenticator struct {
	want [sha256.Size]byte

	// secureCookie controls the Secure attribute on the session cookie.
	// Production is HTTPS via `tailscale cert`, so this is true there. It is
	// configurable only because a plain-HTTP localhost would otherwise never
	// receive the cookie it just set.
	secureCookie bool
}

// New returns an Authenticator for the given token.
//
// It returns an error rather than falling back to an open API: see the package
// comment. Callers should treat a failure here as fatal.
func New(token string, secureCookie bool) (*Authenticator, error) {
	if token == "" {
		return nil, ErrNoToken
	}
	if len(token) < MinTokenLength {
		// The length is not a secret; the token is. Reporting the minimum
		// without echoing the value is the useful half of the message.
		return nil, fmt.Errorf("%w: need at least %d characters", ErrTokenTooShort, MinTokenLength)
	}

	return &Authenticator{
		want:         sha256.Sum256([]byte(token)),
		secureCookie: secureCookie,
	}, nil
}

// valid reports whether presented matches the configured token.
//
// Both sides are hashed first so the comparison runs over two fixed-length
// 32-byte values. subtle.ConstantTimeCompare returns early when its arguments
// differ in length, so comparing the raw strings would leak the token's length
// through timing — a small leak, but a free one to close.
func (a *Authenticator) valid(presented string) bool {
	if presented == "" {
		return false
	}
	got := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(got[:], a.want[:]) == 1
}

// credential extracts the presented token from a request.
//
// The Authorization header wins over the cookie so that a caller holding a fresh
// token is not shadowed by a stale cookie from a previous rotation.
func credential(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		// Scheme match is case-insensitive per RFC 7235 section 2.1. Being
		// strict here rejects working clients for no security benefit.
		if len(h) >= 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return ""
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// Identity returns a stable, non-reversible fingerprint of the Tailscale
// identity behind a request, or "" when the ingress did not supply one.
//
// ADR-015 says the Tailscale-User-Login header is recorded in the audit log.
// That header is an email address, and AGENTS.md rule 7 says email addresses are
// never logged. Both are satisfied by recording a fingerprint: it still
// distinguishes the laptop from the phone across log lines, which is the entire
// value the identity has for a system with exactly one user, and it writes no
// address anywhere.
//
// It is never the basis for granting access — it is evidence about a request
// that has already been authenticated by token.
func Identity(r *http.Request) string {
	login := r.Header.Get("Tailscale-User-Login")
	if login == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(login)))
	return hex.EncodeToString(sum[:4])
}

// Middleware rejects any request that does not present the configured token.
//
// Exempt paths bypass it entirely; see [Authenticator.Handler] for what is
// exempt and why.
func (a *Authenticator) Middleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(credential(r)) {
			// No token, no partial token, no length, no source address beyond
			// what the access log already has. A failed auth line that quotes
			// the attempt is how a secret ends up in a log file.
			log.Warn("auth rejected", "path", r.URL.Path, "method", r.Method)
			unauthorized(w)
			return
		}

		if id := Identity(r); id != "" {
			log.Info("authenticated", "path", r.URL.Path, "tailscale_id", id)
		}

		next.ServeHTTP(w, r)
	})
}

// SessionHandler exchanges a valid bearer token for a long-lived cookie.
//
// The user types the token once per device, ever. The cookie is HttpOnly so
// script cannot read it, Secure so it never crosses plaintext, and
// SameSite=Strict because Scout has no cross-site flow of any kind and therefore
// nothing to lose by forbidding them all.
func (a *Authenticator) SessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Deliberately reads the Authorization header only. Accepting the cookie
		// here would let an existing session refresh itself indefinitely, which
		// is not a flow Scout has — the exchange is for a caller that holds the
		// actual token.
		h := r.Header.Get("Authorization")
		presented := ""
		if len(h) >= 7 && strings.EqualFold(h[:7], "bearer ") {
			presented = strings.TrimSpace(h[7:])
		}

		if !a.valid(presented) {
			unauthorized(w)
			return
		}

		// gosec G124 wants Secure to be an unconditional literal. It is
		// conditional here for exactly one reason: a browser will not store a
		// Secure cookie issued over plain HTTP, so a hard-coded true would make
		// local development appear to log in and then silently not. Production
		// is HTTPS via `tailscale cert` and passes true — see run() in
		// apps/api/cmd/main.go, where the only way to get false is
		// SCOUT_ENV=local.
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see above
			Name:     SessionCookie,
			Value:    presented,
			Path:     "/",
			HttpOnly: true,
			Secure:   a.secureCookie,
			SameSite: http.SameSiteStrictMode,
			// One year. Rotation is the revocation mechanism (ADR-015), not
			// expiry — a short lifetime would mean re-typing the token on three
			// devices on a schedule, which is friction that buys nothing when
			// the same secret is what gets re-typed.
			MaxAge: 365 * 24 * 60 * 60,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	}
}

// LogoutHandler clears the session cookie on this device.
func (a *Authenticator) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		// Same G124 reasoning as SessionHandler. The attributes must match the
		// cookie being cleared or the browser keeps the original.
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see SessionHandler
			Name:     SessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   a.secureCookie,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="scout"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
}
