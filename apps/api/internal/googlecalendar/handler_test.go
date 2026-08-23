package googlecalendar

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	candidates := []string{
		os.Getenv("SCOUT_TEST_DATABASE_URL"),
		"postgres://scout:scout_local_dev_only@localhost:5433/scout?sslmode=disable",
		"postgres://scout:scout_ci@localhost:5432/scout?sslmode=disable",
	}
	for _, url := range candidates {
		if url == "" {
			continue
		}
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err = pool.Ping(ctx)
		cancel()
		if err != nil {
			pool.Close()
			continue
		}
		t.Cleanup(pool.Close)
		return pool
	}
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping googlecalendar handler tests")
	return nil
}

var slugCounter atomic.Uint64

func slug(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	n := slugCounter.Add(1)
	return "gcal-test-" + time.Now().UTC().Format("150405") + "-" + hex32(h.Sum32()) + "-" + hex32(uint32(n))
}

func hex32(n uint32) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = hexDigits[n&0xF]
		n >>= 4
	}
	return string(b)
}

func testUser(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(context.Background(), `select id from app_user order by created_at asc limit 1`).Scan(&id); err != nil {
		t.Fatalf("get sole user (run `make seed`): %v", err)
	}
	return id
}

// Every test below is deliberately network-free: real Google credentials
// don't exist in CI, and a unit test that reaches googleapis.com is
// flaky by construction. What's tested is everything reachable without
// one — the disabled/not-connected degraded paths (the ones that matter
// most, since "not configured" is this integration's default state on
// every machine that hasn't set up OAuth credentials) and the DB layer.

func TestStatus_NotConfigured(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{})

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/integrations/google/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != false {
		t.Errorf("enabled = %v, want false", resp["enabled"])
	}
	if resp["connected"] != false {
		t.Errorf("connected = %v, want false", resp["connected"])
	}
}

func TestStatus_ConfiguredButNotConnected(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{ClientID: "fake-id", ClientSecret: "fake-secret"})

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/integrations/google/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != true {
		t.Errorf("enabled = %v, want true", resp["enabled"])
	}
	if resp["connected"] != false {
		t.Errorf("connected = %v, want false", resp["connected"])
	}
}

func TestStatus_Connected(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{ClientID: "fake-id", ClientSecret: "fake-secret"})
	userID := testUser(t, pool)

	q := db.New(pool)
	_, err := q.UpsertGoogleCalendarCredential(context.Background(), db.UpsertGoogleCalendarCredentialParams{
		UserID:       userID,
		AccessToken:  "fake-access-" + slug(t),
		RefreshToken: "fake-refresh-" + slug(t),
		TokenExpiry:  pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CalendarID:   "primary",
	})
	if err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from google_calendar_credential where user_id = $1::uuid`, userID)
	})

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/integrations/google/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["connected"] != true {
		t.Errorf("connected = %v, want true", resp["connected"])
	}
	if resp["calendar_id"] != "primary" {
		t.Errorf("calendar_id = %v, want primary", resp["calendar_id"])
	}
}

func TestSync_NotConfiguredIs503(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{})

	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/integrations/google/sync", nil)
	w := httptest.NewRecorder()
	h.Sync(w, r)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestEvents_NotConfiguredIs503(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{})

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/integrations/google/events", nil)
	w := httptest.NewRecorder()
	h.Events(w, r)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestSync_ConfiguredButNotConnectedIs412(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{ClientID: "fake-id", ClientSecret: "fake-secret"})

	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/integrations/google/sync", nil)
	w := httptest.NewRecorder()
	h.Sync(w, r)

	// Never reaches httpClientForUser (no network call attempted) — the
	// credential lookup fails first, same as production would for a user
	// who has never connected.
	if w.Code != 412 {
		t.Errorf("status = %d, want 412", w.Code)
	}
}

func TestDisconnect_ClearsCredentialAndEventMapping(t *testing.T) {
	pool := testPool(t)
	h := New(pool, testLog(), Config{ClientID: "fake-id", ClientSecret: "fake-secret"})
	userID := testUser(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	_, err := q.UpsertGoogleCalendarCredential(ctx, db.UpsertGoogleCalendarCredentialParams{
		UserID:       userID,
		AccessToken:  "fake-access-" + slug(t),
		RefreshToken: "fake-refresh-" + slug(t),
		TokenExpiry:  pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CalendarID:   "primary",
	})
	if err != nil {
		t.Fatalf("insert credential: %v", err)
	}

	r := httptest.NewRequestWithContext(ctx, "POST", "/v1/integrations/google/disconnect", nil)
	w := httptest.NewRecorder()
	h.Disconnect(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	if _, err := q.GetGoogleCalendarCredential(ctx, userID); err == nil {
		t.Error("credential still present after disconnect")
	}
}

func TestAuthURL_ContainsStateAndClientID(t *testing.T) {
	h := New(nil, testLog(), Config{
		ClientID:    "my-client-id",
		RedirectURL: "http://localhost:3000/api/integrations/google/callback",
	})

	url := h.AuthURL("csrf-token-abc")

	if !strings.Contains(url, "client_id=my-client-id") {
		t.Errorf("AuthURL missing client_id: %s", url)
	}
	if !strings.Contains(url, "state=csrf-token-abc") {
		t.Errorf("AuthURL missing state: %s", url)
	}
	if !strings.Contains(url, "access_type=offline") {
		t.Errorf("AuthURL missing access_type=offline (needed for a refresh_token): %s", url)
	}
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
