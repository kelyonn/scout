package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/apps/notifier/internal/telegram"
)

// --- render() — pure, no DB ---

func TestRender_IncludesOvernightJobs(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	comp := int64(80000)
	d := data{
		OvernightCount: 2, WeekCount: 10,
		OvernightTop: []overnightJob{
			{Company: "Razorpay", Title: "Backend Intern", LocationCity: "Bengaluru", Priority: 87, CompINRMonth: &comp},
		},
	}
	got := render(time.Date(2026, 8, 6, 8, 0, 0, 0, loc), d)

	for _, want := range []string{"Overnight: 2 new", "This week: 10 new", "Razorpay", "Backend Intern", "Bengaluru", "87", "₹80000/mo"} {
		if !strings.Contains(got, want) {
			t.Errorf("render() missing %q in:\n%s", want, got)
		}
	}
}

func TestRender_OmitsSectionsWithNoData(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	got := render(time.Date(2026, 8, 6, 8, 0, 0, 0, loc), data{})

	if strings.Contains(got, "New while you slept") {
		t.Error("expected the overnight section to be omitted when there are no overnight jobs")
	}
	if strings.Contains(got, "Closing soon") {
		t.Error("expected the closing-soon section to be omitted when nothing is closing soon")
	}
	if !strings.Contains(got, "Your week") {
		t.Error("expected the weekly-stats section to always render")
	}
}

func TestRender_ClosingSoonSingularDay(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	nowIST := time.Date(2026, 8, 6, 8, 0, 0, 0, loc)
	// DeadlineAt relative to nowIST, not time.Now(): this test used to pass
	// by accident — render() internally used time.Until (real wall clock)
	// rather than the nowIST it was given, so a deadline built from real
	// time and a fixed nowIST parameter happened to agree only because
	// nowIST was never actually being used for this calculation. Now that
	// render() genuinely computes DeadlineAt.Sub(nowIST), the fixture has
	// to be genuinely relative to nowIST too.
	d := data{ClosingSoon: []closingSoonJob{
		{Company: "Stripe", Title: "SWE Intern", DeadlineAt: nowIST.Add(20 * time.Hour)},
	}}
	got := render(nowIST, d)
	if !strings.Contains(got, "1 day left") {
		t.Errorf("render() = %q, want singular \"1 day left\"", got)
	}
}

// --- Run() — real Postgres + fake Telegram ---

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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping digest tests")
	return nil
}

func testUser(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var id pgtype.UUID
	if err := pool.QueryRow(ctx, `select id from app_user order by created_at asc limit 1`).Scan(&id); err == nil {
		return id
	}
	err := pool.QueryRow(ctx, `
		insert into app_user (email, display_name) values ('digest-test@scout.local', 'Digest Test')
		returning id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	_, _ = pool.Exec(ctx, `insert into user_profile (user_id) values ($1::uuid) on conflict do nothing`, id)
	return id
}

// insertTelegramChannel mirrors apps/notifier/internal/deliver's own
// helper of the same name — snapshot-and-restore rather than delete, since
// testUser can return a real app_user row from a dev database that
// already has a real linked Telegram channel.
func insertTelegramChannel(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, chatID int64) {
	t.Helper()
	ctx := context.Background()

	var existed bool
	var prevConfig []byte
	var prevEnabled bool
	err := pool.QueryRow(ctx, `
		select config, enabled from notification_channel where user_id = $1::uuid and kind = 'telegram'
	`, userID).Scan(&prevConfig, &prevEnabled)
	existed = err == nil

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
}

type fakeTelegramCapture struct {
	messages []string
}

// fakeTelegramServer stands in for the real Telegram Bot API.
// telegram.Client.SendMessage sends its parameters as a URL query string,
// not a JSON body, so the message text is read from there.
func fakeTelegramServer(t *testing.T, capture *fakeTelegramCapture) *telegram.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.messages = append(capture.messages, r.URL.Query().Get("text"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	t.Cleanup(srv.Close)
	return telegram.NewWithBaseURL("test-token", srv.URL)
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var slugCounter atomic.Uint64

func slug(t *testing.T) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	n := slugCounter.Add(1)
	return fmt.Sprintf("digest-test-%s-%08x-%08x", time.Now().UTC().Format("150405"), h.Sum32(), n)
}

// insertEligibleJob inserts a company/source/job_group/job tuple that
// clears SelectDigestOvernightJobs' eligibility bar (open, is_software,
// paid), with a controllable first_seen_at.
func insertEligibleJob(t *testing.T, pool *pgxpool.Pool, firstSeenAt time.Time, deadlineAt *time.Time) (groupID pgtype.UUID, jobID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	s := slug(t)

	var companyID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::citext, $1::text, $1::citext, 'seed') returning id
	`, s).Scan(&companyID); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	url := "https://example.test/" + s
	var sourceID pgtype.UUID
	// next_poll_at defaults to now(), which makes this row `select`able by
	// apps/collector/internal/scheduler's RunOnce (status=active,
	// legal_posture=permitted, next_poll_at<=now()) — a real source of
	// flaky cross-package failures there, since `go test ./...` runs
	// packages concurrently against the same shared test Postgres. Push it
	// out of range; this test doesn't care when the source is due.
	if err := pool.QueryRow(ctx, `
		insert into source (company_id, kind, status, legal_posture, url, url_hash, next_poll_at)
		values ($1::uuid, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'), now() + interval '10 years')
		returning id
	`, companyID, url).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	if err := pool.QueryRow(ctx, `insert into job_group (company_id, first_seen_at) values ($1::uuid, $2) returning id`,
		companyID, firstSeenAt).Scan(&groupID); err != nil {
		t.Fatalf("insert job_group: %v", err)
	}

	canonicalURL := url + "/job"
	err := pool.QueryRow(ctx, `
		insert into job (
			job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, apply_url, is_software, paid, status, deadline_at
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4, digest($4, 'sha256'),
			digest($4, 'sha256'), 'Backend Engineering Intern', 'backend engineering intern', $4, TRUE, 'paid', 'open', $5
		) returning id
	`, groupID, companyID, sourceID, canonicalURL, deadlineAt).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	if _, err := pool.Exec(ctx, `update job_group set representative_job_id = $1::uuid where id = $2::uuid`, jobID, groupID); err != nil {
		t.Fatalf("set representative: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from user_job_state where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from user_job_state_event where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `update job_group set representative_job_id = NULL where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from job_group where id = $1::uuid`, groupID)
		_, _ = pool.Exec(ctx, `delete from source where company_id = $1::uuid`, companyID)
		_, _ = pool.Exec(ctx, `delete from company where id = $1::uuid`, companyID)
	})

	return groupID, jobID
}

func cleanupDigestNotifications(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID) {
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			delete from notification_delivery where notification_id in (
				select id from notification where user_id = $1::uuid and trigger = 'digest'
			)
		`, userID)
		_, _ = pool.Exec(context.Background(), `delete from notification where user_id = $1::uuid and trigger = 'digest'`, userID)
	})
}

// istTime anchors to the real current date, not a fixed historical one —
// SelectDigestClosingSoon's `after`/`before` window and
// SelectDigestOvernightJobs' `since` are both computed from g.now() in
// Go, but the dev/CI database this runs against holds real rows from
// real ingestion with real timestamps near actual wall-clock time. A
// hardcoded past date made every real row in the database look like it
// fell inside the digest's lookback windows, which is what made the
// data-accuracy tests below flaky before this fix.
// istTime builds a fixed-date IST timestamp for tests — deliberately NOT
// derived from time.Now(). It used to be ("today" at the given
// hour/minute), which broke in a real, reproduced way: two istTime calls
// within the same test (simulating "later the same day") land on
// different calendar dates if the real wall clock crosses midnight
// between them, which a long-running test session eventually does. The
// reference date below is otherwise arbitrary — nothing in this package
// asserts a specific weekday or date string.
func istTime(hour, minute int) time.Time {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return time.Date(2026, time.August, 19, hour, minute, 0, 0, loc)
}

func TestRun_NotDueBeforeEightAMIST(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	insertTelegramChannel(t, pool, userID, 999)
	cleanupDigestNotifications(t, pool, userID)
	capture := &fakeTelegramCapture{}
	tg := fakeTelegramServer(t, capture)

	g := New(pool, tg, testLogger())
	g.now = func() time.Time { return istTime(7, 59) }

	sent, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sent {
		t.Error("expected sent=false before 08:00 IST")
	}
	if len(capture.messages) != 0 {
		t.Error("expected no Telegram message sent before 08:00 IST")
	}
}

func TestRun_SendsOnceAtOrAfterEightAMIST(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	insertTelegramChannel(t, pool, userID, 999)
	cleanupDigestNotifications(t, pool, userID)
	capture := &fakeTelegramCapture{}
	tg := fakeTelegramServer(t, capture)

	g := New(pool, tg, testLogger())
	g.now = func() time.Time { return istTime(8, 0) }

	sent, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sent {
		t.Fatal("expected sent=true at 08:00 IST")
	}
	if len(capture.messages) != 1 {
		t.Fatalf("expected exactly 1 Telegram message, got %d", len(capture.messages))
	}
	if !strings.Contains(capture.messages[0], "Good morning") {
		t.Errorf("message = %q, want it to start with the digest greeting", capture.messages[0])
	}
}

func TestRun_SecondCallSameDayIsANoOp(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	insertTelegramChannel(t, pool, userID, 999)
	cleanupDigestNotifications(t, pool, userID)
	capture := &fakeTelegramCapture{}
	tg := fakeTelegramServer(t, capture)

	g := New(pool, tg, testLogger())
	g.now = func() time.Time { return istTime(8, 0) }

	if _, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08"); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	g.now = func() time.Time { return istTime(9, 30) } // later the same day
	sent, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08")
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if sent {
		t.Error("expected sent=false on the second call the same day")
	}
	if len(capture.messages) != 1 {
		t.Errorf("expected exactly 1 Telegram message total across both calls, got %d", len(capture.messages))
	}
}

func TestRun_IncludesRealOvernightJobAndClosingSoonFixture(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	insertTelegramChannel(t, pool, userID, 999)
	cleanupDigestNotifications(t, pool, userID)
	capture := &fakeTelegramCapture{}
	tg := fakeTelegramServer(t, capture)

	now := istTime(8, 0)
	overnightGroupID, _ := insertEligibleJob(t, pool, now.Add(-2*time.Hour), nil)

	deadline := now.Add(2 * 24 * time.Hour)
	closingGroupID, _ := insertEligibleJob(t, pool, now.Add(-10*24*time.Hour), &deadline)
	if _, err := pool.Exec(context.Background(), `
		insert into user_job_state (user_id, job_group_id, state) values ($1::uuid, $2::uuid, 'saved')
	`, userID, closingGroupID); err != nil {
		t.Fatalf("insert user_job_state: %v", err)
	}

	g := New(pool, tg, testLogger())
	g.now = func() time.Time { return now }

	sent, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sent {
		t.Fatal("expected sent=true")
	}

	msg := capture.messages[0]
	if !strings.Contains(msg, "New while you slept") {
		t.Errorf("message missing overnight section:\n%s", msg)
	}
	if !strings.Contains(msg, "Closing soon") {
		t.Errorf("message missing closing-soon section:\n%s", msg)
	}
	if !strings.Contains(msg, "2 day") {
		t.Errorf("message = %q, want a 2-day countdown for the closing-soon fixture", msg)
	}
	_ = overnightGroupID
}

// TestRun_AppliedThisWeekReflectsRealStateEvent runs against a shared
// dev/CI database that may already hold real 'applied' transitions for
// this user within the last 7 days (this session's own browser
// verification of the state machine produced some) — it asserts the
// count increases by exactly one after adding a fixture event, not that
// it equals 1, which would be a false failure against real prior data.
func TestRun_AppliedThisWeekReflectsRealStateEvent(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	insertTelegramChannel(t, pool, userID, 999)
	cleanupDigestNotifications(t, pool, userID)
	capture := &fakeTelegramCapture{}
	tg := fakeTelegramServer(t, capture)

	weekAgo := istTime(8, 0).Add(-7 * 24 * time.Hour)
	var baseline int64
	if err := pool.QueryRow(context.Background(), `
		select count(*) from user_job_state_event
		where user_id = $1::uuid and to_state = 'applied' and occurred_at >= $2
	`, userID, weekAgo).Scan(&baseline); err != nil {
		t.Fatalf("query baseline applied count: %v", err)
	}

	groupID, _ := insertEligibleJob(t, pool, istTime(8, 0).Add(-72*time.Hour), nil)
	if _, err := pool.Exec(context.Background(), `
		insert into user_job_state_event (user_id, job_group_id, from_state, to_state, occurred_at)
		values ($1::uuid, $2::uuid, 'saved', 'applied', now())
	`, userID, groupID); err != nil {
		t.Fatalf("insert state event: %v", err)
	}

	g := New(pool, tg, testLogger())
	g.now = func() time.Time { return istTime(8, 0) }

	if _, err := g.Run(context.Background(), userID, "v1-hand-tuned-2026-08"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msg := capture.messages[0]
	want := fmt.Sprintf("Applied %d", baseline+1)
	if !strings.Contains(msg, want) {
		t.Errorf("message = %q, want %q reflecting baseline+1", msg, want)
	}
}
