package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/notifier/internal/telegram"
	"github.com/kelyon/scout/apps/notifier/internal/trigger"
)

func TestInQuietHours(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}

	// The schema's own default: 00:00-07:30 IST.
	quietStart := pgtype.Time{Microseconds: 0, Valid: true}
	quietEnd := pgtype.Time{Microseconds: (7*3600 + 30*60) * 1_000_000, Valid: true}

	cases := []struct {
		name   string
		hour   int
		minute int
		want   bool
	}{
		{"03:00 IST is in quiet hours", 3, 0, true},
		{"00:00 IST boundary is in quiet hours", 0, 0, true},
		{"07:29 IST is in quiet hours", 7, 29, true},
		{"07:30 IST boundary is NOT in quiet hours", 7, 30, false},
		{"12:00 IST is not in quiet hours", 12, 0, false},
		{"23:59 IST is not in quiet hours", 23, 59, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := time.Date(2026, 8, 11, c.hour, c.minute, 0, 0, loc)
			got := inQuietHours(now, db.GetUserProfileRow{QuietHoursStart: quietStart, QuietHoursEnd: quietEnd})
			if got != c.want {
				t.Errorf("inQuietHours(%02d:%02d) = %v, want %v", c.hour, c.minute, got, c.want)
			}
		})
	}
}

func TestRenderDeadlineMessage(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		deadline time.Time
		wantSub  string
	}{
		{"20 hours away rounds up to 1 day left, not 0", now.Add(20 * time.Hour), "Closing tomorrow — 1 day left"},
		{"exactly on the deadline reads as closing today", now, "Closing today"},
		{"past the deadline still reads as closing today, not negative days", now.Add(-time.Hour), "Closing today"},
		{"60 hours away rounds up to 3 days", now.Add(60 * time.Hour), "Closing in 3 days"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deadline := c.deadline
			p := trigger.Payload{Title: "Backend Intern", CompanyName: "Acme", ApplyURL: "https://example.test/apply", DeadlineAt: &deadline}
			msg := renderDeadlineMessage(p, now)
			if !strings.Contains(msg, c.wantSub) {
				t.Errorf("renderDeadlineMessage = %q, want it to contain %q", msg, c.wantSub)
			}
			if !strings.Contains(msg, "Acme — Backend Intern") {
				t.Errorf("renderDeadlineMessage = %q, want company — title", msg)
			}
		})
	}
}

// --- DB-backed tests below ---

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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping deliver tests")
	return nil
}

// freshTestUser always creates a brand-new app_user rather than reusing
// whatever sole row testPool's real local dev database already has
// (ADR-015's single-user architecture, which every package's own testUser
// helper mirrors elsewhere in this codebase). GetUndeliveredNotifications
// is correctly scoped by user_id (packages/db/queries/notification.sql),
// but that only isolates *other* users' rows — it can't distinguish "this
// deliver test's own fixture" from "apps/notifier/internal/trigger's own
// concurrently-running test," which reuses that identical shared sole row
// too, since go test ./... runs packages concurrently against the shared
// local Postgres. Any test here that asserts an exact sent/queued count
// needs a user nothing else can write into — this is that user.
// ON DELETE CASCADE (notification_channel/notification -> app_user) makes
// cleanup a single delete.
func freshTestUser(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("deliver-test-%d@scout.local", time.Now().UnixNano())
	var id pgtype.UUID
	err := pool.QueryRow(ctx, `
		insert into app_user (email, display_name) values ($1::text, 'Deliver Test (isolated)')
		returning id
	`, email).Scan(&id)
	if err != nil {
		t.Fatalf("insert fresh test user: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into user_profile (user_id) values ($1::uuid)`, id); err != nil {
		t.Fatalf("insert fresh test user_profile: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from app_user where id = $1::uuid`, id) })
	return id
}

// insertTelegramChannel upserts a test channel for userID. Every caller in
// this file now passes a freshTestUser id, which can never already have a
// channel, but the snapshot-and-restore shape is kept rather than a blind
// insert-then-delete-by-id: it is what makes this helper safe to call with
// a real, already-linked user too (exactly what happened once, running
// these tests against the real local dev database's own sole user before
// freshTestUser existed), and there is no upside to a second, narrower
// version that would break the moment anyone reused it that way again.
func insertTelegramChannel(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, chatID int64) pgtype.UUID {
	t.Helper()
	ctx := context.Background()

	var existed bool
	var prevConfig []byte
	var prevEnabled bool
	err := pool.QueryRow(ctx, `
		select config, enabled from notification_channel where user_id = $1::uuid and kind = 'telegram'
	`, userID).Scan(&prevConfig, &prevEnabled)
	switch {
	case err == nil:
		existed = true
	case err.Error() == "no rows in result set":
		existed = false
	default:
		t.Fatalf("check existing telegram channel: %v", err)
	}

	var id pgtype.UUID
	config := fmt.Sprintf(`{"chat_id": %d}`, chatID)
	err = pool.QueryRow(ctx, `
		insert into notification_channel (user_id, kind, config, enabled, verified_at)
		values ($1::uuid, 'telegram', $2::jsonb, TRUE, now())
		on conflict (user_id, kind) where kind in ('telegram','email','discord')
		do update set config = excluded.config, enabled = TRUE
		returning id
	`, userID, config).Scan(&id)
	if err != nil {
		t.Fatalf("insert telegram channel: %v", err)
	}

	t.Cleanup(func() {
		if existed {
			_, _ = pool.Exec(context.Background(), `
				update notification_channel set config = $2::jsonb, enabled = $3 where id = $1::uuid
			`, id, prevConfig, prevEnabled)
			return
		}
		_, _ = pool.Exec(context.Background(), `delete from notification_channel where id = $1::uuid`, id)
	})
	return id
}

func insertTestNotification(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, trig string, priority int16) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	payload, _ := json.Marshal(map[string]any{"title": "Test Job", "apply_url": "https://example.test/apply"})
	err := pool.QueryRow(context.Background(), `
		insert into notification (user_id, job_group_id, trigger, urgency, payload, priority_at_send)
		values ($1::uuid, NULL, $2::notification_trigger, 'instant', $3::jsonb, $4::smallint)
		returning id
	`, userID, trig, payload, priority).Scan(&id)
	if err != nil {
		t.Fatalf("insert test notification: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from notification where id = $1::uuid`, id) })
	return id
}

// insertTestBatchedNotification mirrors insertTestNotification but with
// urgency='batched' and an explicit scheduled_for, the two fields
// InsertNotification's own coalesce-to-now default doesn't exercise.
func insertTestBatchedNotification(
	t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, priority int16, scheduledFor time.Time, title, companyName string,
) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	payload, _ := json.Marshal(map[string]any{
		"title": title, "company_name": companyName, "apply_url": "https://example.test/apply",
	})
	err := pool.QueryRow(context.Background(), `
		insert into notification (user_id, job_group_id, trigger, urgency, payload, priority_at_send, scheduled_for)
		values ($1::uuid, NULL, 'newgrad_match', 'batched', $2::jsonb, $3::smallint, $4::timestamptz)
		returning id
	`, userID, payload, priority, scheduledFor).Scan(&id)
	if err != nil {
		t.Fatalf("insert test batched notification: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from notification where id = $1::uuid`, id) })
	return id
}

func fakeTelegramServer(t *testing.T) (*httptest.Server, *telegram.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, telegram.NewWithBaseURL("test-token", srv.URL)
}

// fakeTelegramServerCounting is fakeTelegramServer plus a request counter,
// for asserting a batch renders as exactly one Telegram call rather than
// one per notification.
func fakeTelegramServerCounting(t *testing.T) (*telegram.Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return telegram.NewWithBaseURL("test-token", srv.URL), &calls
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// deterministicDaytimeIST is a fixed, comfortably-outside-quiet-hours
// timestamp for tests that need d.now to land somewhere real and daytime
// without depending on the actual wall clock — using time.Now() here was a
// real, if rare, source of flakiness: the batched-delivery tests below
// failed for real once this session ran long enough to cross into IST
// quiet hours (00:00-07:30), the same class of bug TestRun_DeliversOutsideQuietHours
// already guards against with its own fixed date.
func deterministicDaytimeIST() time.Time {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic("deliver_test: could not load Asia/Kolkata: " + err.Error())
	}
	return time.Date(2026, 8, 11, 12, 0, 0, 0, loc)
}

func countDeliveries(t *testing.T, pool *pgxpool.Pool, notificationID pgtype.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`select count(*) from notification_delivery where notification_id = $1::uuid`, notificationID).Scan(&n)
	if err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	return n
}

func TestRun_DeliversOutsideQuietHours(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	insertTelegramChannel(t, pool, userID, 555)
	_, tg := fakeTelegramServer(t)

	notifID := insertTestNotification(t, pool, userID, "high_score", 90)

	d := New(pool, tg, testLogger())
	d.now = func() time.Time {
		loc, _ := time.LoadLocation("Asia/Kolkata")
		return time.Date(2026, 8, 11, 12, 0, 0, 0, loc) // well outside quiet hours
	}

	sent, queued, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 1 || queued != 0 {
		t.Fatalf("sent=%d queued=%d, want sent=1 queued=0", sent, queued)
	}
	if countDeliveries(t, pool, notifID) != 1 {
		t.Error("expected exactly 1 notification_delivery row")
	}
}

func TestRun_QueuesDuringQuietHours(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	insertTelegramChannel(t, pool, userID, 555)
	_, tg := fakeTelegramServer(t)

	notifID := insertTestNotification(t, pool, userID, "high_score", 90)

	d := New(pool, tg, testLogger())
	d.now = func() time.Time {
		loc, _ := time.LoadLocation("Asia/Kolkata")
		return time.Date(2026, 8, 11, 3, 0, 0, 0, loc) // 03:00 IST, inside default quiet hours
	}

	sent, queued, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 0 || queued != 1 {
		t.Fatalf("sent=%d queued=%d, want sent=0 queued=1", sent, queued)
	}
	if countDeliveries(t, pool, notifID) != 0 {
		t.Error("a quiet-hours-held notification must not get a delivery row")
	}
}

func TestRun_BengaluruOverrideBreaksThroughQuietHours(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	insertTelegramChannel(t, pool, userID, 555)
	_, tg := fakeTelegramServer(t)

	notifID := insertTestNotification(t, pool, userID, "bengaluru_match", 94)

	d := New(pool, tg, testLogger())
	d.now = func() time.Time {
		loc, _ := time.LoadLocation("Asia/Kolkata")
		return time.Date(2026, 8, 11, 3, 0, 0, 0, loc)
	}

	sent, _, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent=%d, want 1 — bengaluru_match >= 92 should break through quiet hours", sent)
	}
	if countDeliveries(t, pool, notifID) != 1 {
		t.Error("expected exactly 1 notification_delivery row")
	}
}

// TestRun_BatchesMultipleNewgradMatchesIntoOneMessage is docs/11 section
// 3's BATCHED behavior: several notifications due in the same hour render
// as one Telegram message, not one each, and every one of them still gets
// its own notification_delivery row (so budget accounting and per-item
// delivery health both still work).
func TestRun_BatchesMultipleNewgradMatchesIntoOneMessage(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	insertTelegramChannel(t, pool, userID, 555)
	tg, calls := fakeTelegramServerCounting(t)

	now := deterministicDaytimeIST()
	due := now.Add(-time.Minute) // already past its scheduled_for boundary
	n1 := insertTestBatchedNotification(t, pool, userID, 85, due, "Backend Intern", "Acme")
	n2 := insertTestBatchedNotification(t, pool, userID, 81, due, "Platform Intern", "Beta Co")

	d := New(pool, tg, testLogger())
	d.now = func() time.Time { return now }

	sent, queued, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 2 || queued != 0 {
		t.Fatalf("sent=%d queued=%d, want sent=2 queued=0", sent, queued)
	}
	if *calls != 1 {
		t.Errorf("telegram calls = %d, want exactly 1 for a 2-notification batch", *calls)
	}
	if countDeliveries(t, pool, n1) != 1 || countDeliveries(t, pool, n2) != 1 {
		t.Error("each batched notification should still get its own notification_delivery row")
	}
}

// TestRun_BatchedNotificationNotYetDueStaysQueued confirms
// GetUndeliveredNotifications' scheduled_for <= now() clause actually
// holds a batched notification back until its hour boundary — the whole
// point of computing scheduled_for at insert time. scheduled_for <= now()
// runs inside the SQL query against Postgres's own real clock, not
// Deliverer's injected d.now — so notDue has to be relative to the real
// wall clock (not deterministicDaytimeIST's fixed 2026-08-11, which by now
// is in the past and would make the row look overdue rather than
// not-yet-due). d.now itself doesn't matter here: the row is filtered out
// in SQL before Deliverer's Go-level loop ever sees it.
func TestRun_BatchedNotificationNotYetDueStaysQueued(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	insertTelegramChannel(t, pool, userID, 555)
	tg, calls := fakeTelegramServerCounting(t)

	now := time.Now()
	notDue := now.Add(45 * time.Minute)
	notifID := insertTestBatchedNotification(t, pool, userID, 85, notDue, "Backend Intern", "Acme")

	d := New(pool, tg, testLogger())
	d.now = func() time.Time { return now }

	sent, queued, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 0 || queued != 0 {
		t.Fatalf("sent=%d queued=%d, want sent=0 queued=0 — not yet fetched at all", sent, queued)
	}
	if *calls != 0 {
		t.Errorf("telegram calls = %d, want 0 before the batch's hour boundary", *calls)
	}
	if countDeliveries(t, pool, notifID) != 0 {
		t.Error("a not-yet-due batched notification must not get a delivery row")
	}
}

func TestRun_HourlyBudgetQueuesOverflow(t *testing.T) {
	pool := testPool(t)
	userID := freshTestUser(t, pool)
	channelID := insertTelegramChannel(t, pool, userID, 555)
	_, tg := fakeTelegramServer(t)

	now := deterministicDaytimeIST()
	// Fill the hourly budget with maxPerHour already-sent deliveries.
	for i := 0; i < maxPerHour; i++ {
		n := insertTestNotification(t, pool, userID, "high_score", 90)
		_, err := pool.Exec(context.Background(), `
			insert into notification_delivery (notification_id, channel_id, status, attempts, sent_at)
			values ($1::uuid, $2::uuid, 'sent', 1, $3::timestamptz)
		`, n, channelID, now)
		if err != nil {
			t.Fatalf("seed prior delivery: %v", err)
		}
	}

	notifID := insertTestNotification(t, pool, userID, "high_score", 90)

	d := New(pool, tg, testLogger())
	d.now = func() time.Time { return now }

	sent, queued, err := d.Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent != 0 || queued != 1 {
		t.Fatalf("sent=%d queued=%d, want sent=0 queued=1 once the hourly budget is exhausted", sent, queued)
	}
	if countDeliveries(t, pool, notifID) != 0 {
		t.Error("a budget-held notification must not get a delivery row")
	}
}
