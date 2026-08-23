package googlecalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
)

// AuthURLHandler handles GET /v1/integrations/google/auth-url?state=...
// — returns the Google consent-screen URL for the caller (the web app's
// own /api/integrations/google/connect route) to redirect the browser
// to. Doesn't redirect itself: this is called as a JSON fetch from a
// Next.js route handler, which then issues the real 302 to the browser
// from its own origin — see that route's own comment for why.
func (h *Handler) AuthURLHandler(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "google calendar integration not configured")
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		writeError(w, http.StatusBadRequest, "missing state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": h.AuthURL(state)})
}

// Status handles GET /v1/integrations/google/status.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "connected": false})
		return
	}

	ctx := r.Context()
	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("google calendar status: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cred, err := q.GetGoogleCalendarCredential(ctx, user.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "connected": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":      true,
		"connected":    true,
		"calendar_id":  cred.CalendarID,
		"connected_at": cred.ConnectedAt.Time.UTC().Format(time.RFC3339),
	})
}

type callbackRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// Callback handles POST /v1/integrations/google/callback — called
// server-to-server by the web app's own callback route
// (apps/web/app/api/integrations/google/callback/route.ts) after Google
// redirects the browser back with an authorization code. The code
// exchange (and the client secret it requires) happens here, not in the
// web app, so the secret never needs to exist outside the Go API's
// environment.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "google calendar integration not configured")
		return
	}

	var req callbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	ctx := r.Context()

	// The redirect_uri used in the token exchange must exactly match the
	// one used to build the original consent URL — Google rejects a
	// mismatch. The web app's own callback route is the one that knows
	// its real origin (different in local dev vs production), so it's
	// passed through here rather than assumed.
	cfg := h.cfg.oauthConfig()
	if req.RedirectURI != "" {
		cfg.RedirectURL = req.RedirectURI
	}

	tok, err := cfg.Exchange(ctx, req.Code)
	if err != nil {
		h.log.Error("google calendar: code exchange failed", "err", err)
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}

	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("google calendar callback: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	_, err = q.UpsertGoogleCalendarCredential(ctx, db.UpsertGoogleCalendarCredentialParams{
		UserID:       user.ID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenExpiry:  pgtype.Timestamptz{Time: tok.Expiry, Valid: true},
		CalendarID:   "primary",
	})
	if err != nil {
		h.log.Error("google calendar: store credential failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

// Disconnect handles POST /v1/integrations/google/disconnect. Clears the
// stored credential and the local event-ID mapping — deliberately
// doesn't delete the events themselves from the user's real calendar;
// disconnecting the integration isn't the same action as "delete
// everything Scout ever put on my calendar," and doing that silently
// would be a surprising, hard-to-reverse side effect of what reads as a
// settings toggle.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("google calendar disconnect: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := q.DeleteGoogleCalendarEventsForUser(ctx, user.ID); err != nil {
		h.log.Error("google calendar disconnect: clear event mapping failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := q.DeleteGoogleCalendarCredential(ctx, user.ID); err != nil {
		h.log.Error("google calendar disconnect: delete credential failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

type syncResult struct {
	Pushed int      `json:"pushed"`
	Errors []string `json:"errors,omitempty"`
}

// Sync handles POST /v1/integrations/google/sync — the push direction.
// Upserts one all-day event per tracked (saved-or-beyond, not
// dismissed/rejected/withdrawn) job with a deadline. On-demand, not a
// background job: the user connects, then presses "Sync now" — no new
// scheduler infrastructure for a feature whose data changes on the
// order of "a few times a week," not continuously.
func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "google calendar integration not configured")
		return
	}

	ctx := r.Context()
	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("google calendar sync: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cred, err := q.GetGoogleCalendarCredential(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, "not connected")
		return
	}

	client, err := h.httpClientForUser(ctx, user.ID)
	if err != nil {
		h.log.Error("google calendar sync: auth failed", "err", err)
		writeError(w, http.StatusBadGateway, "google auth failed — try reconnecting")
		return
	}

	rows, err := q.SelectApplicationsWithDeadlines(ctx, user.ID)
	if err != nil {
		h.log.Error("google calendar sync: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	result := syncResult{}
	for _, row := range rows {
		if pushErr := h.pushOne(ctx, client, cred.CalendarID, q, user.ID, row); pushErr != nil {
			h.log.Error("google calendar sync: push failed", "job_group_id", row.JobGroupID, "err", pushErr)
			result.Errors = append(result.Errors, row.Title+": "+pushErr.Error())
			continue
		}
		result.Pushed++
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) pushOne(
	ctx context.Context, client *http.Client, calendarID string, q *db.Queries,
	userID pgtype.UUID, row db.SelectApplicationsWithDeadlinesRow,
) error {
	deadline := row.DeadlineAt.Time
	ev := calendarEvent{
		Summary:     "Deadline: " + row.Title + " @ " + row.CompanyName,
		Description: "Tracked in Scout. Apply: " + row.ApplyUrl,
		Start:       eventDate{Date: deadline.Format("2006-01-02")},
		End:         eventDate{Date: deadline.AddDate(0, 0, 1).Format("2006-01-02")},
	}

	existing, err := q.GetGoogleCalendarEvent(ctx, row.JobGroupID)
	if err == nil {
		// Already pushed once — update in place rather than creating a
		// second event for the same job every time Sync is pressed.
		if updErr := updateEvent(ctx, client, calendarID, existing.GoogleEventID, ev); updErr != nil {
			return updErr
		}
		if upsertErr := q.UpsertGoogleCalendarEvent(ctx, db.UpsertGoogleCalendarEventParams{
			JobGroupID:    row.JobGroupID,
			UserID:        userID,
			GoogleEventID: existing.GoogleEventID,
		}); upsertErr != nil {
			return fmt.Errorf("record pushed event: %w", upsertErr)
		}
		return nil
	}

	eventID, err := insertEvent(ctx, client, calendarID, ev)
	if err != nil {
		return err
	}
	if upsertErr := q.UpsertGoogleCalendarEvent(ctx, db.UpsertGoogleCalendarEventParams{
		JobGroupID:    row.JobGroupID,
		UserID:        userID,
		GoogleEventID: eventID,
	}); upsertErr != nil {
		return fmt.Errorf("record pushed event: %w", upsertErr)
	}
	return nil
}

type pulledEvent struct {
	Summary string `json:"summary"`
	Date    string `json:"date"`
}

// Events handles GET /v1/integrations/google/events — the pull
// direction. Read-only, not persisted anywhere: fetched live on every
// call, the same "no durable copy of external state" posture
// AGENTS.md rule 8 already takes with Redis, applied here to a table
// that doesn't exist at all rather than one that's allowed to lose data.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "google calendar integration not configured")
		return
	}

	ctx := r.Context()
	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("google calendar events: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cred, err := q.GetGoogleCalendarCredential(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, "not connected")
		return
	}

	client, err := h.httpClientForUser(ctx, user.ID)
	if err != nil {
		h.log.Error("google calendar events: auth failed", "err", err)
		writeError(w, http.StatusBadGateway, "google auth failed — try reconnecting")
		return
	}

	now := time.Now().UTC()
	items, err := listEvents(ctx, client, cred.CalendarID, now, now.AddDate(0, 0, 30))
	if err != nil {
		h.log.Error("google calendar events: list failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to fetch events")
		return
	}

	out := make([]pulledEvent, 0, len(items))
	for _, ev := range items {
		date := ev.Start.Date
		if date == "" {
			continue // timed event with no all-day date field; skip rather than guess a display date from dateTime
		}
		out = append(out, pulledEvent{Summary: ev.Summary, Date: date})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}
