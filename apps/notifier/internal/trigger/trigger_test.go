package trigger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

const testWeightVersion = "trigger-test-weights"

// testPool mirrors apps/collector/internal/scheduler's helper of the same
// name — skip rather than fail so `go test ./...` stays green without the
// local stack running.
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
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping trigger tests")
	return nil
}

// testUser ensures a usable app_user + user_profile exist and returns the
// user's id — reuses the sole seeded user if `make seed` already ran,
// otherwise creates one, mirroring
// apps/collector/internal/scheduler.ensureSeeded's reasoning.
func testUser(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	ctx := context.Background()

	var id pgtype.UUID
	err := pool.QueryRow(ctx, `select id from app_user order by created_at asc limit 1`).Scan(&id)
	if err == nil {
		return id
	}

	err = pool.QueryRow(ctx, `
		insert into app_user (email, display_name) values ('trigger-test@scout.local', 'Trigger Test')
		returning id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	_, err = pool.Exec(ctx, `insert into user_profile (user_id) values ($1::uuid) on conflict do nothing`, id)
	if err != nil {
		t.Fatalf("insert test user_profile: %v", err)
	}
	return id
}

func insertTestWeightVersion(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		insert into weight_version (version, weights, source, active)
		values ($1, '{}'::jsonb, 'hand_tuned', false)
		on conflict (version) do nothing
	`, testWeightVersion)
	if err != nil {
		t.Fatalf("insert test weight_version: %v", err)
	}
}

func insertTestCompany(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	slug := fmt.Sprintf("trigger-test-co-%d", time.Now().UnixNano())
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::text, $1::text, $1::text, 'seed')
		returning id
	`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from company where id = $1::uuid`, id) })
	return id
}

func insertTestSource(t *testing.T, pool *pgxpool.Pool, companyID pgtype.UUID) pgtype.UUID {
	t.Helper()
	url := fmt.Sprintf("https://example.test/%d/board", time.Now().UnixNano())
	var id pgtype.UUID
	// next_poll_at explicit and far in the future — the default (now()) makes
	// this row a candidate for apps/collector/internal/scheduler's RunOnce,
	// a real source of flaky cross-package failures there since `go test
	// ./...` runs packages concurrently against the same shared test DB.
	err := pool.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash, next_poll_at)
		values ($1, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'), now() + interval '10 years')
		returning id
	`, companyID, url).Scan(&id)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, id) })
	return id
}

// jobFixture is one seeded job_group + job + job_score, ready for
// SelectUnnotifiedJobGroups to pick up.
type jobFixture struct {
	JobGroupID pgtype.UUID
	JobID      pgtype.UUID
}

func insertTestJobFixture(
	t *testing.T, pool *pgxpool.Pool, companyID, sourceID, userID pgtype.UUID, locationTier *int16, priority int16,
) jobFixture {
	t.Helper()
	return insertTestJobFixtureWithPaid(t, pool, companyID, sourceID, userID, locationTier, priority, "paid")
}

func insertTestJobFixtureWithPaid(
	t *testing.T, pool *pgxpool.Pool, companyID, sourceID, userID pgtype.UUID, locationTier *int16, priority int16, paid string,
) jobFixture {
	t.Helper()
	ctx := context.Background()

	var groupID pgtype.UUID
	err := pool.QueryRow(ctx, `insert into job_group (company_id) values ($1::uuid) returning id`, companyID).Scan(&groupID)
	if err != nil {
		t.Fatalf("insert test job_group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `update job_group set representative_job_id = NULL where id = $1::uuid`, groupID)
		_, _ = pool.Exec(context.Background(), `delete from job where job_group_id = $1::uuid`, groupID)
		_, _ = pool.Exec(context.Background(), `delete from job_group where id = $1::uuid`, groupID)
	})

	canonicalURL := fmt.Sprintf("https://example.test/job/%d", time.Now().UnixNano())
	var jobID pgtype.UUID
	err = pool.QueryRow(ctx, `
		insert into job (
			job_group_id, company_id, primary_source_id, canonical_url, canonical_url_hash,
			content_hash, title, normalized_title, apply_url, is_software, paid, status, location_tier
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4::text, digest($4::text, 'sha256'),
			digest($4::text, 'sha256'), 'Test Job', 'test job', $4::text, TRUE, $6::paid_signal, 'open', $5::smallint
		)
		returning id
	`, groupID, companyID, sourceID, canonicalURL, locationTier, paid).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert test job: %v", err)
	}

	_, err = pool.Exec(ctx, `update job_group set representative_job_id = $1::uuid where id = $2::uuid`, jobID, groupID)
	if err != nil {
		t.Fatalf("set job_group representative: %v", err)
	}

	_, err = pool.Exec(ctx, `
		insert into job_score (
			job_id, user_id, weight_version, overall_match, skill_match, resume_match, company_quality,
			compensation, learning_opportunity, engineering_culture, growth_potential, interview_probability,
			competition_estimate, ease_of_applying, deadline_urgency, priority, location_multiplier,
			freshness_multiplier, score_inputs
		) values (
			$1::uuid, $2::uuid, $3::text, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, 50, $4::smallint, 1.0, 1.0, '{}'
		)
	`, jobID, userID, testWeightVersion, priority)
	if err != nil {
		t.Fatalf("insert test job_score: %v", err)
	}

	return jobFixture{JobGroupID: groupID, JobID: jobID}
}

func countNotifications(t *testing.T, pool *pgxpool.Pool, jobGroupID pgtype.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`select count(*) from notification where job_group_id = $1::uuid`, jobGroupID).Scan(&n)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

func testEvaluator(pool *pgxpool.Pool) *Evaluator {
	return New(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func tier1() *int16 { v := int16(1); return &v }
func tier2() *int16 { v := int16(2); return &v }
func tier3() *int16 { v := int16(3); return &v }
func tier4() *int16 { v := int16(4); return &v }

func TestRun_FiresBengaluruMatch(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier1(), bengaluruMatchThreshold)

	evaluated, fired, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if evaluated < 1 || fired < 1 {
		t.Fatalf("evaluated=%d fired=%d, want at least 1 each", evaluated, fired)
	}

	if countNotifications(t, pool, fixture.JobGroupID) != 1 {
		t.Error("expected exactly 1 notification row")
	}

	var trig string
	err = pool.QueryRow(context.Background(),
		`select trigger from notification where job_group_id = $1::uuid`, fixture.JobGroupID).Scan(&trig)
	if err != nil {
		t.Fatalf("query trigger: %v", err)
	}
	if trig != "bengaluru_match" {
		t.Errorf("trigger = %q, want bengaluru_match", trig)
	}
}

func TestRun_FiresHighScoreForNonBengaluru(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	// Below bengaluru_match's own threshold but above high_score's — proves
	// tier-2 doesn't get the lower bar.
	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), highScoreThreshold)

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var trig string
	err := pool.QueryRow(context.Background(),
		`select trigger from notification where job_group_id = $1::uuid`, fixture.JobGroupID).Scan(&trig)
	if err != nil {
		t.Fatalf("query trigger: %v", err)
	}
	if trig != "high_score" {
		t.Errorf("trigger = %q, want high_score", trig)
	}
}

func TestRun_NoFireBelowThreshold(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), 50)

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, fixture.JobGroupID) != 0 {
		t.Error("a below-threshold job should not produce a notification")
	}

	// Still marked notified, so it is not re-evaluated forever.
	var notifiedAt pgtype.Timestamptz
	err := pool.QueryRow(context.Background(),
		`select notified_at from job_group where id = $1::uuid`, fixture.JobGroupID).Scan(&notifiedAt)
	if err != nil {
		t.Fatalf("query notified_at: %v", err)
	}
	if !notifiedAt.Valid {
		t.Error("job_group.notified_at should be set even when no trigger fired")
	}
}

// TestRun_UnknownPaidFiresOnlyAboveEligibilityOverride is docs/07 section
// 7's eligibility rule for the common case — most ATS boards (Greenhouse
// among them) expose no compensation field, so paid = 'unknown' is the
// typical row, not the exception. SelectUnnotifiedJobGroups (packages/db/queries/notification.sql)
// admits it only when priority >= 92; anything paid='unknown' below that
// stays filtered out before a trigger is even evaluated, same as
// explicit 'unpaid'.
func TestRun_UnknownPaidFiresOnlyAboveEligibilityOverride(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	below := insertTestJobFixtureWithPaid(t, pool, companyID, sourceID, userID, tier1(), 91, "unknown")
	above := insertTestJobFixtureWithPaid(t, pool, companyID, sourceID, userID, tier1(), 92, "unknown")

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, below.JobGroupID) != 0 {
		t.Error("paid=unknown at priority 91 should not be eligible (below the 92 override)")
	}
	if countNotifications(t, pool, above.JobGroupID) != 1 {
		t.Error("paid=unknown at priority 92 should be eligible and fire bengaluru_match")
	}
}

// TestRun_SeniorityFilterExcludesNonTargetSeniority is
// packages/db/queries/notification.sql's seniority_filter_enabled clause
// (000012_seniority_filter): a senior/staff/director-level posting can
// still clear the high_score threshold on company_quality/deadline_urgency/
// freshness alone even though overall_match's seniority_fit term scored it
// 0, so the hard eligibility gate — not just that soft signal — is what
// actually keeps it out of a graduating-in-a-year user's notifications.
// user_profile.target_seniority defaults to {internship,new_grad}
// (testUser's fresh profile), so a 'senior' job must be excluded and an
// 'internship' job must still fire.
func TestRun_SeniorityFilterExcludesNonTargetSeniority(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	senior := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), highScoreThreshold)
	setJobSeniority(t, pool, senior.JobID, "senior")

	internship := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), highScoreThreshold)
	setJobSeniority(t, pool, internship.JobID, "internship")

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, senior.JobGroupID) != 0 {
		t.Error("a senior-level job should not fire a notification against target_seniority={internship,new_grad}")
	}
	if countNotifications(t, pool, internship.JobGroupID) != 1 {
		t.Error("an internship job should still fire a notification")
	}

	// Not marked notified — SelectUnnotifiedJobGroups excludes it before
	// evaluateTrigger ever runs, same as an excluded_companies row.
	var notifiedAt pgtype.Timestamptz
	err := pool.QueryRow(context.Background(),
		`select notified_at from job_group where id = $1::uuid`, senior.JobGroupID).Scan(&notifiedAt)
	if err != nil {
		t.Fatalf("query notified_at: %v", err)
	}
	if notifiedAt.Valid {
		t.Error("a seniority-filtered job_group should not be marked notified — it was never evaluated")
	}
}

// TestRun_SeniorityFilterUnknownAdmitted mirrors the paid clause's own
// admit-unknown-rather-than-hide rule (notification.sql's comment on both
// clauses): a job whose seniority classification found no signal must not
// read as "detected and excluded."
func TestRun_SeniorityFilterUnknownAdmitted(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	// insertTestJobFixture leaves seniority at its column default, 'unknown'.
	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), highScoreThreshold)

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, fixture.JobGroupID) != 1 {
		t.Error("seniority='unknown' should still be eligible, not silently excluded")
	}
}

// TestRun_SeniorityFilterDisabledAdmitsAnySeniority confirms the toggle
// itself: with seniority_filter_enabled turned off, a senior-level job
// reaches trigger evaluation exactly as it would have before 000012 —
// the escape hatch for once target_seniority widens past internship/
// new_grad.
func TestRun_SeniorityFilterDisabledAdmitsAnySeniority(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	_, err := pool.Exec(context.Background(),
		`update user_profile set seniority_filter_enabled = FALSE where user_id = $1::uuid`, userID)
	if err != nil {
		t.Fatalf("disable seniority filter: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`update user_profile set seniority_filter_enabled = TRUE where user_id = $1::uuid`, userID)
	})

	senior := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), highScoreThreshold)
	setJobSeniority(t, pool, senior.JobID, "senior")

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, senior.JobGroupID) != 1 {
		t.Error("with the filter disabled, a senior-level job should fire like any other eligible job")
	}
}

// TestRun_FiresRemoteHighQuality is docs/07 section 6's tiering table taken
// literally: location_tier == 3 already means "remote, India-eligible," so
// a tier-3 job at the trigger's own threshold should fire remote_high_quality
// without any separate work_mode or country check.
func TestRun_FiresRemoteHighQuality(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier3(), remoteHighQualityThreshold)

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var trig string
	err := pool.QueryRow(context.Background(),
		`select trigger from notification where job_group_id = $1::uuid`, fixture.JobGroupID).Scan(&trig)
	if err != nil {
		t.Fatalf("query trigger: %v", err)
	}
	if trig != "remote_high_quality" {
		t.Errorf("trigger = %q, want remote_high_quality", trig)
	}
}

// TestRun_RemoteHighQualityRequiresTier3 confirms tier 4 (international,
// not remote-eligible) does not get the tier-3 threshold — the same
// distinction docs/07's own tiering table draws between "San Francisco,
// remote (global)" (tier 3) and "San Francisco, onsite" (tier 4).
func TestRun_RemoteHighQualityRequiresTier3(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier4(), remoteHighQualityThreshold)

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, fixture.JobGroupID) != 0 {
		t.Error("a tier-4 job at the tier-3 threshold should not fire remote_high_quality")
	}
}

// TestRun_FiresNewgradMatch also checks the urgency written is 'batched',
// not 'instant' — docs/11 section 2's table gives newgrad_match BATCHED
// urgency, and apps/notifier/internal/deliver's sendBatch is what actually
// groups it, not this package, but the row this package writes is what
// makes that possible.
func TestRun_FiresNewgradMatch(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	// testUser's fresh profile defaults target_seniority to
	// {internship,new_grad}, so a new_grad job matches without extra setup.
	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), newgradMatchThreshold)
	setJobSeniority(t, pool, fixture.JobID, "new_grad")

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var trig, urgency string
	err := pool.QueryRow(context.Background(),
		`select trigger, urgency from notification where job_group_id = $1::uuid`, fixture.JobGroupID).Scan(&trig, &urgency)
	if err != nil {
		t.Fatalf("query trigger/urgency: %v", err)
	}
	if trig != "newgrad_match" {
		t.Errorf("trigger = %q, want newgrad_match", trig)
	}
	if urgency != "batched" {
		t.Errorf("urgency = %q, want batched", urgency)
	}
}

// TestRun_NewgradMatchRequiresTargetSeniorityMatch: a new_grad-classified
// job for a user who has narrowed target_seniority to exclude new_grad
// must not fire — "matches target profile" is docs/11's own phrase for
// this trigger, not just "is new_grad."
func TestRun_NewgradMatchRequiresTargetSeniorityMatch(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	_, err := pool.Exec(context.Background(),
		`update user_profile set target_seniority = '{internship}' where user_id = $1::uuid`, userID)
	if err != nil {
		t.Fatalf("narrow target_seniority: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`update user_profile set target_seniority = '{internship,new_grad}' where user_id = $1::uuid`, userID)
	})

	// seniority_filter_enabled defaults TRUE, so a job whose seniority
	// isn't in target_seniority never reaches SelectUnnotifiedJobGroups at
	// all — this proves the gate closes here too, same as
	// TestRun_SeniorityFilterExcludesNonTargetSeniority's senior-role case.
	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier2(), newgradMatchThreshold)
	setJobSeniority(t, pool, fixture.JobID, "new_grad")

	if _, _, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if countNotifications(t, pool, fixture.JobGroupID) != 0 {
		t.Error("a new_grad job should not fire newgrad_match once target_seniority no longer includes new_grad")
	}
}

// TestNextHourBoundary is the pure ceiling function BATCHED delivery relies
// on — exercised directly, no database needed.
func TestNextHourBoundary(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{
			"mid-hour rounds up to the next hour",
			time.Date(2026, 8, 11, 14, 23, 5, 0, loc),
			time.Date(2026, 8, 11, 15, 0, 0, 0, loc),
		},
		{
			"exactly on the hour stays put",
			time.Date(2026, 8, 11, 14, 0, 0, 0, loc),
			time.Date(2026, 8, 11, 14, 0, 0, 0, loc),
		},
		{
			"one second past the hour rounds up a full hour",
			time.Date(2026, 8, 11, 14, 0, 1, 0, loc),
			time.Date(2026, 8, 11, 15, 0, 0, 0, loc),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextHourBoundary(c.in)
			if !got.Equal(c.want) {
				t.Errorf("nextHourBoundary(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func setJobSeniority(t *testing.T, pool *pgxpool.Pool, jobID pgtype.UUID, seniority string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`update job set seniority = $1::seniority where id = $2::uuid`, seniority, jobID)
	if err != nil {
		t.Fatalf("set job seniority: %v", err)
	}
}

// TestRun_SuppressNotifications is docs/17's backfill/rescore suppression
// property (docs/11 section 12: "Full replay of 10,000 observations -> 0
// notifications"), exercised directly against the guard itself — AGENTS.md
// rule 3 requires it be provably correct even though no backfill or
// rescore feature calls it yet.
func TestRun_SuppressNotifications(t *testing.T) {
	pool := testPool(t)
	insertTestWeightVersion(t, pool)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)

	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier1(), 99)

	evaluated, fired, err := testEvaluator(pool).Run(context.Background(), userID, testWeightVersion, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if evaluated < 1 {
		t.Fatalf("evaluated=%d, want at least 1", evaluated)
	}
	if fired != 0 {
		t.Errorf("fired=%d, want 0 with suppressNotifications=true", fired)
	}
	if countNotifications(t, pool, fixture.JobGroupID) != 0 {
		t.Error("suppressNotifications=true must not write a notification row, even for a would-be-obvious match")
	}
}

// TestRun_UniquenessUnderConcurrency is docs/11 section 12's literal test:
// "100 parallel notify attempts for one group -> exactly 1 row." Exercised
// directly against InsertNotification rather than through Evaluator.Run,
// since Run's SELECT-then-mark-notified shape means only one goroutine
// would ever see an unnotified group in the first place — the property
// under test is the unique index itself, which InsertNotification's ON
// CONFLICT DO NOTHING is the code path for.
func TestRun_UniquenessUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	userID := testUser(t, pool)
	companyID := insertTestCompany(t, pool)
	sourceID := insertTestSource(t, pool, companyID)
	fixture := insertTestJobFixture(t, pool, companyID, sourceID, userID, tier1(), 90)

	q := db.New(pool)
	const attempts = 100
	var wg sync.WaitGroup
	var successes, conflicts int64
	var mu sync.Mutex

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := q.InsertNotification(context.Background(), db.InsertNotificationParams{
				UserID: userID, JobGroupID: fixture.JobGroupID, Trigger: "bengaluru_match",
				Urgency: "instant", Payload: []byte(`{}`), PriorityAtSend: 90,
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil && id.Valid {
				successes++
			} else {
				conflicts++
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1", successes)
	}
	if conflicts != attempts-1 {
		t.Errorf("conflicts = %d, want %d", conflicts, attempts-1)
	}
	if countNotifications(t, pool, fixture.JobGroupID) != 1 {
		t.Error("expected exactly 1 notification row after 100 concurrent attempts")
	}
}
