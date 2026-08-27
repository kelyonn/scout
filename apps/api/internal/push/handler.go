// Package push implements ADR-012's device registration endpoint —
// POST /v1/push/register-device, the write side of
// notification_channel(kind = 'native_push'). Registration itself needs
// no FCM credentials: this endpoint only ever writes a device token to
// Postgres, never calls Firebase, so it works (and is fully testable)
// whether or not a Firebase project exists yet — apps/notifier/internal/fcm
// is what actually needs those credentials, at send time.
package push

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

// Handler serves the push registration endpoint.
type Handler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New constructs a Handler.
func New(pool *pgxpool.Pool, log *slog.Logger) *Handler {
	return &Handler{pool: pool, log: log}
}

// registerDeviceRequest is the Capacitor shell's own registration call,
// made once on launch and again whenever FCM hands the app a refreshed
// token (docs/adr/ADR-012's own "token_refreshed_at" column exists for
// exactly that second case).
type registerDeviceRequest struct {
	Platform    string  `json:"platform"` // "android" or "ios" — device_platform's own values
	DeviceToken string  `json:"device_token"`
	DeviceLabel *string `json:"device_label,omitempty"` // "Pixel 8", shown in Settings
	AppVersion  *string `json:"app_version,omitempty"`
}

// RegisterDevice handles POST /v1/push/register-device. ADR-015's single
// user means there's no per-request user to disambiguate — every
// registration attaches to GetSoleUser, the same pattern
// apps/api/internal/googlecalendar's own handlers already use.
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req registerDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	if req.DeviceToken == "" {
		writeError(w, http.StatusBadRequest, "device_token is required")
		return
	}
	platform, ok := parsePlatform(req.Platform)
	if !ok {
		writeError(w, http.StatusBadRequest, "platform must be \"android\" or \"ios\"")
		return
	}

	ctx := r.Context()
	q := db.New(h.pool)
	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("register device: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	channel, err := q.UpsertNativePushChannel(ctx, db.UpsertNativePushChannelParams{
		UserID:      user.ID,
		Platform:    platform,
		DeviceToken: req.DeviceToken,
		DeviceLabel: req.DeviceLabel,
		AppVersion:  req.AppVersion,
	})
	if err != nil {
		h.log.Error("register device: upsert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"channel_id": channel.ID.String()})
}

// parsePlatform validates against device_platform's own enum values
// (infra/migrations/000002_enums.up.sql) rather than trusting the request
// body to already be well-formed — this is the one place outside a
// migration where that enum's valid values need restating, since sqlc's
// generated DevicePlatform type has no exported "parse from string"
// helper of its own.
func parsePlatform(s string) (db.DevicePlatform, bool) {
	switch db.DevicePlatform(s) {
	case db.DevicePlatformAndroid, db.DevicePlatformIos:
		return db.DevicePlatform(s), true
	default:
		return "", false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
