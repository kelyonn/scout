// Package googlecalendar implements Scout's two-way Google Calendar
// integration: push tracked deadlines out as real events, pull the
// user's existing events in for the Calendar page to show alongside
// them. Both directions were an explicit choice — the simpler
// read-only or write-only version was the recommendation, but the user
// asked for both.
//
// Degrades to "not configured" when SCOUT_GOOGLE_CLIENT_ID/
// SCOUT_GOOGLE_CLIENT_SECRET aren't set — the same pattern
// apps/brain's LLM cascade and apps/collector/internal/emailalert's
// IMAP poller already use: a missing optional integration must never
// block the rest of the app or fail startup.
//
// Deliberately calls the Calendar v3 REST API directly over net/http
// rather than depending on google.golang.org/api/calendar/v3 — that
// package pulls in the full generated Google API client machinery
// (gRPC-adjacent transports, dozens of transitive packages) for four
// REST calls this package hand-rolls in about as many functions.
// golang.org/x/oauth2 (+ its google sub-package, same module) is the one
// new dependency: OAuth token exchange and refresh are exactly what it
// exists for, and reimplementing that safely is not the kind of code
// worth hand-rolling the way the REST calls are.
package googlecalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	db "github.com/kelyon/scout/packages/db/gen"
)

const calendarScope = "https://www.googleapis.com/auth/calendar.events"

// Config holds the OAuth client credentials, read from
// SCOUT_GOOGLE_CLIENT_ID / SCOUT_GOOGLE_CLIENT_SECRET /
// SCOUT_GOOGLE_REDIRECT_URL. All-empty is a valid disabled state.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether the integration has real credentials
// configured. Checked before every handler does anything else.
func (c Config) Enabled() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

func (c Config) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Scopes:       []string{calendarScope},
		Endpoint:     google.Endpoint,
	}
}

// Handler serves the integration's endpoints.
type Handler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	cfg  Config
}

// New constructs a Handler. cfg may be the zero value — Enabled() is
// false in that case, and every handler responds accordingly rather
// than erroring.
func New(pool *pgxpool.Pool, log *slog.Logger, cfg Config) *Handler {
	return &Handler{pool: pool, log: log, cfg: cfg}
}

// AuthURL returns the Google consent-screen URL the frontend redirects
// the browser to. state is an opaque anti-CSRF token the caller
// generates and later verifies on the callback — this package doesn't
// generate or check it itself; see apps/web/app/api/integrations/
// google/connect/route.ts for where that happens.
func (h *Handler) AuthURL(state string) string {
	return h.cfg.oauthConfig().AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		// Forces Google to re-issue a refresh_token even for a user who
		// consented before — without this, a reconnect after a
		// disconnect would get an access_token only, and this
		// integration would silently stop working the moment that
		// access_token's one-hour lifetime expired.
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// httpClientForUser returns an *http.Client that authenticates Calendar
// API requests as the given user, refreshing the stored access token
// first if it has expired. A refreshed token is persisted back
// immediately — Google issues a new access_token on essentially every
// call once the old one expires, and losing that update would mean
// refreshing again (and hitting Google's refresh-token rate limit) on
// every subsequent request instead of once per hour.
func (h *Handler) httpClientForUser(ctx context.Context, userID pgtype.UUID) (*http.Client, error) {
	q := db.New(h.pool)
	cred, err := q.GetGoogleCalendarCredential(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("not connected: %w", err)
	}

	tok := &oauth2.Token{
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		Expiry:       cred.TokenExpiry.Time,
	}

	src := h.cfg.oauthConfig().TokenSource(ctx, tok)
	fresh, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	if fresh.AccessToken != tok.AccessToken {
		if updErr := q.UpdateGoogleCalendarTokens(ctx, db.UpdateGoogleCalendarTokensParams{
			UserID:      userID,
			AccessToken: fresh.AccessToken,
			TokenExpiry: pgtype.Timestamptz{Time: fresh.Expiry, Valid: true},
		}); updErr != nil {
			// Not fatal to this request — the fresh token still works for
			// the call about to happen. Logged because a persistent
			// failure here means every future call refreshes again
			// needlessly.
			h.log.Error("google calendar: persist refreshed token failed", "err", updErr)
		}
	}

	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(fresh)), nil
}
