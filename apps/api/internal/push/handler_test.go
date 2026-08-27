package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping push handler tests")
	return nil
}

func testHandler(pool *pgxpool.Pool) *Handler {
	return New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

var tokenCounter atomic.Uint64

// uniqueToken avoids colliding with a real device token or another test's,
// on the same shared dev database notification_channel_device_idx
// (user_id, device_token) enforces uniqueness against.
func uniqueToken(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	return fmt.Sprintf("test-device-%08x-%08x", h.Sum32(), tokenCounter.Add(1))
}

func cleanupChannel(t *testing.T, pool *pgxpool.Pool, deviceToken string) {
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from notification_channel where device_token = $1`, deviceToken)
	})
}

func TestRegisterDevice_CreatesChannel(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	token := uniqueToken(t)
	cleanupChannel(t, pool, token)

	body, _ := json.Marshal(registerDeviceRequest{
		Platform: "android", DeviceToken: token, DeviceLabel: strPtr("Pixel 8"), AppVersion: strPtr("1.0.0"),
	})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.RegisterDevice(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var kind, platform, label, appVersion string
	var enabled bool
	err := pool.QueryRow(context.Background(), `
		select kind::text, platform::text, device_label, app_version, enabled
		from notification_channel where device_token = $1
	`, token).Scan(&kind, &platform, &label, &appVersion, &enabled)
	if err != nil {
		t.Fatalf("query inserted channel: %v", err)
	}
	if kind != "native_push" {
		t.Errorf("kind = %q, want native_push", kind)
	}
	if platform != "android" {
		t.Errorf("platform = %q, want android", platform)
	}
	if label != "Pixel 8" {
		t.Errorf("device_label = %q", label)
	}
	if appVersion != "1.0.0" {
		t.Errorf("app_version = %q", appVersion)
	}
	if !enabled {
		t.Error("enabled = false, want true for a fresh registration")
	}
}

func TestRegisterDevice_ReRegisteringSameTokenUpdatesInPlace(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	token := uniqueToken(t)
	cleanupChannel(t, pool, token)

	first, _ := json.Marshal(registerDeviceRequest{Platform: "android", DeviceToken: token, DeviceLabel: strPtr("Old Label")})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader(first))
	w := httptest.NewRecorder()
	h.RegisterDevice(w, r)
	if w.Code != 200 {
		t.Fatalf("first registration status = %d, body = %s", w.Code, w.Body.String())
	}

	second, _ := json.Marshal(registerDeviceRequest{Platform: "android", DeviceToken: token, DeviceLabel: strPtr("New Label")})
	r2 := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader(second))
	w2 := httptest.NewRecorder()
	h.RegisterDevice(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("second registration status = %d, body = %s", w2.Code, w2.Body.String())
	}

	var count int
	if err := pool.QueryRow(context.Background(), `select count(*) from notification_channel where device_token = $1`, token).Scan(&count); err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("channel rows for this device = %d, want 1 — re-registering must update in place, not duplicate", count)
	}

	var label string
	if err := pool.QueryRow(context.Background(), `select device_label from notification_channel where device_token = $1`, token).Scan(&label); err != nil {
		t.Fatalf("query updated label: %v", err)
	}
	if label != "New Label" {
		t.Errorf("device_label = %q, want New Label after re-registration", label)
	}
}

func TestRegisterDevice_MissingDeviceTokenIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	body, _ := json.Marshal(registerDeviceRequest{Platform: "android"})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.RegisterDevice(w, r)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegisterDevice_InvalidPlatformIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	body, _ := json.Marshal(registerDeviceRequest{Platform: "windows-phone", DeviceToken: uniqueToken(t)})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.RegisterDevice(w, r)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegisterDevice_MalformedBodyIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/push/register-device", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	h.RegisterDevice(w, r)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestParsePlatform(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantVal db.DevicePlatform
	}{
		{"android", true, db.DevicePlatformAndroid},
		{"ios", true, db.DevicePlatformIos},
		{"", false, ""},
		{"Android", false, ""}, // case-sensitive — matches the enum's own literal values
		{"web", false, ""},
	}
	for _, c := range cases {
		got, ok := parsePlatform(c.in)
		if ok != c.wantOK || got != c.wantVal {
			t.Errorf("parsePlatform(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.wantVal, c.wantOK)
		}
	}
}

func strPtr(s string) *string { return &s }
