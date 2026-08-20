package trigger

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDeadlineSweeper(pool *pgxpool.Pool) *DeadlineSweeper {
	return NewDeadlineSweeper(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func setJobDeadline(t *testing.T, pool *pgxpool.Pool, jobID pgtype.UUID, deadline time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`update job set deadline_at = $1::timestamptz where id = $2::uuid`, deadline, jobID)
	if err != nil {
		t.Fatalf("set job deadline: %v", err)
	}
}

func insertUserJobState(t *testing.T, pool *pgxpool.Pool, userID, jobGroupID pgtype.UUID, state string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		insert into user_job_state (user_id, job_group_id, state)
		values ($1::uuid, $2::uuid, $3::application_state)
		on conflict (user_id, job_group_id) do update set state = excluded.state
	`, userID, jobGroupID, state)
	if err != nil {
		t.Fatalf("insert user_job_state: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from user_job_state where user_id = $1::uuid and job_group_id = $2::uuid`, userID, jobGroupID)
	})
}

// countDeadlineNotifications returns trigger names sorted lexically in Go
// (not `order by trigger` in SQL, which sorts a Postgres enum by its
// declared ordinal rather than alphabetically) — the ordering tests below
// care about which two triggers exist, not the order Postgres happens to
// return them in.
func countDeadlineNotifications(t *testing.T, pool *pgxpool.Pool, jobGroupID pgtype.UUID) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`select trigger from notification where job_group_id = $1::uuid`, jobGroupID)
	if err != nil {
		t.Fatalf("query deadline notifications: %v", err)
	}
	defer rows.Close()
	var triggers []string
	for rows.Next() {
		var trig string
		if err := rows.Scan(&trig); err != nil {
			t.Fatalf("scan trigger: %v", err)
		}
		triggers = append(triggers, trig)
	}
	sort.Strings(triggers)
	return triggers
}

// TestDeadlineSweeper_FiresT72hOnlyOutsideT24hWindow: a tracked job 48h from
// its deadline is inside the T-72h window but not the T-24h one — only one
// reminder should exist.
func TestDeadlineSweeper_FiresT72hOnlyOutsideT24hWindow(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)
	now := time.Now()
	setJobDeadline(t, pool, fixture.JobID, now.Add(48*time.Hour))
	insertUserJobState(t, pool, userID, fixture.JobGroupID, "saved")

	sweeper := testDeadlineSweeper(pool)
	sweeper.now = func() time.Time { return now }

	// evaluated/fired are aggregated across every tracked job the shared
	// test user has (testUser reuses the sole real app_user row, same as
	// every other package's tests) — go test ./... runs packages
	// concurrently, so another package's own deadline-adjacent fixture can
	// genuinely be in flight here too. Only a loose lower bound on
	// evaluated is safe; the real assertion is countDeadlineNotifications
	// scoped to this test's own job_group_id below.
	evaluated, _, err := sweeper.Run(context.Background(), userID, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if evaluated < 1 {
		t.Fatalf("evaluated=%d, want at least 1", evaluated)
	}

	triggers := countDeadlineNotifications(t, pool, fixture.JobGroupID)
	if len(triggers) != 1 || triggers[0] != "deadline_t72h" {
		t.Errorf("triggers = %v, want exactly [deadline_t72h]", triggers)
	}
}

// TestDeadlineSweeper_FiresBothWindowsInsideT24h: a tracked job 12h from its
// deadline is inside both windows — both reminders should exist (dedup
// index proves they're independent notification rows, not one collapsing
// into the other).
func TestDeadlineSweeper_FiresBothWindowsInsideT24h(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)
	now := time.Now()
	setJobDeadline(t, pool, fixture.JobID, now.Add(12*time.Hour))
	insertUserJobState(t, pool, userID, fixture.JobGroupID, "applied")

	sweeper := testDeadlineSweeper(pool)
	sweeper.now = func() time.Time { return now }

	// fired is a cross-test-shared-user aggregate (see the comment on
	// TestDeadlineSweeper_FiresT72hOnlyOutsideT24hWindow) — the real
	// assertion is the job_group-scoped trigger list below.
	if _, _, err := sweeper.Run(context.Background(), userID, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	triggers := countDeadlineNotifications(t, pool, fixture.JobGroupID)
	if len(triggers) != 2 || triggers[0] != "deadline_t24h" || triggers[1] != "deadline_t72h" {
		t.Errorf("triggers = %v, want [deadline_t24h deadline_t72h]", triggers)
	}
}

// TestDeadlineSweeper_RepeatedSweepDoesNotDuplicate proves the dedup index
// is what actually enforces "fires once per reminder," not any in-memory
// bookkeeping in DeadlineSweeper itself.
func TestDeadlineSweeper_RepeatedSweepDoesNotDuplicate(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)
	now := time.Now()
	setJobDeadline(t, pool, fixture.JobID, now.Add(48*time.Hour))
	insertUserJobState(t, pool, userID, fixture.JobGroupID, "saved")

	sweeper := testDeadlineSweeper(pool)
	sweeper.now = func() time.Time { return now }

	if _, _, err := sweeper.Run(context.Background(), userID, false); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// fired on the second Run is a cross-test-shared-user aggregate (see
	// TestDeadlineSweeper_FiresT72hOnlyOutsideT24hWindow's comment) —
	// the real proof that THIS job_group's reminder didn't duplicate is
	// still exactly one notification row for it below.
	if _, _, err := sweeper.Run(context.Background(), userID, false); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(countDeadlineNotifications(t, pool, fixture.JobGroupID)) != 1 {
		t.Error("expected exactly 1 notification row after two sweeps")
	}
}

// TestDeadlineSweeper_ExcludesTerminalStates mirrors
// packages/db/queries/digest.sql's SelectDigestClosingSoon filter — a
// dismissed job's approaching deadline is not something to remind about.
func TestDeadlineSweeper_ExcludesTerminalStates(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)
	now := time.Now()
	setJobDeadline(t, pool, fixture.JobID, now.Add(48*time.Hour))
	insertUserJobState(t, pool, userID, fixture.JobGroupID, "dismissed")

	sweeper := testDeadlineSweeper(pool)
	sweeper.now = func() time.Time { return now }

	if _, _, err := sweeper.Run(context.Background(), userID, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(countDeadlineNotifications(t, pool, fixture.JobGroupID)) != 0 {
		t.Error("a dismissed job's approaching deadline should not fire a reminder")
	}
}

// TestDeadlineSweeper_SuppressNotifications is AGENTS.md rule 3's guard,
// exercised the same way TestRun_SuppressNotifications proves it for
// Evaluator.
func TestDeadlineSweeper_SuppressNotifications(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)
	now := time.Now()
	setJobDeadline(t, pool, fixture.JobID, now.Add(12*time.Hour))
	insertUserJobState(t, pool, userID, fixture.JobGroupID, "saved")

	sweeper := testDeadlineSweeper(pool)
	sweeper.now = func() time.Time { return now }

	if _, fired, err := sweeper.Run(context.Background(), userID, true); err != nil {
		t.Fatalf("Run: %v", err)
	} else if fired != 0 {
		t.Errorf("fired=%d, want 0 with suppressNotifications=true", fired)
	}
	if len(countDeadlineNotifications(t, pool, fixture.JobGroupID)) != 0 {
		t.Error("suppressNotifications=true must not write any notification row")
	}
}
