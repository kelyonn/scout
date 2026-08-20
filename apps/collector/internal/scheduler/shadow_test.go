package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDecideShadowPromotion(t *testing.T) {
	tests := []struct {
		name                                       string
		totalPolls, totalSuccesses, totalJobsFound int64
		yieldRatio                                 float32
		wantPromote                                bool
		wantTier                                   int16
	}{
		{
			name:           "zero successful polls stays pending, not promoted",
			totalPolls:     6,
			totalSuccesses: 0,
			totalJobsFound: 0,
			wantPromote:    false,
		},
		{
			name:           "successful polls but zero jobs found stays pending",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: 0,
			wantPromote:    false,
		},
		{
			name:           "implausibly high job count stays pending",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: maxPlausibleJobs + 1,
			wantPromote:    false,
		},
		{
			name:           "exactly at the sanity ceiling is still plausible",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: maxPlausibleJobs,
			yieldRatio:     0.01,
			wantPromote:    true,
			wantTier:       5,
		},
		{
			name:           "healthy low-yield source promotes to the lowest auto tier",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: 3,
			yieldRatio:     0.01,
			wantPromote:    true,
			wantTier:       5,
		},
		{
			name:           "healthy medium-yield source promotes to tier 4",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: 5,
			yieldRatio:     0.1,
			wantPromote:    true,
			wantTier:       4,
		},
		{
			name:           "healthy default-yield source promotes to tier 3",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: 20,
			yieldRatio:     0.3,
			wantPromote:    true,
			wantTier:       3,
		},
		{
			name:           "high-yield source promotes to tier 2, never tier 1",
			totalPolls:     10,
			totalSuccesses: 10,
			totalJobsFound: 50,
			yieldRatio:     0.9,
			wantPromote:    true,
			wantTier:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideShadowPromotion(tt.totalPolls, tt.totalSuccesses, tt.totalJobsFound, tt.yieldRatio)
			if got.Promote != tt.wantPromote {
				t.Fatalf("Promote = %v, want %v (reason: %q)", got.Promote, tt.wantPromote, got.Reason)
			}
			if tt.wantPromote {
				if got.Tier != tt.wantTier {
					t.Fatalf("Tier = %d, want %d", got.Tier, tt.wantTier)
				}
				if got.Tier == 1 {
					t.Fatalf("decideShadowPromotion must never assign tier 1 automatically")
				}
			} else if got.Reason == "" {
				t.Fatalf("a flagged (non-promoted) decision must carry a reason")
			}
		})
	}
}

// shadowTestSourceOpts configures insertShadowTestSource.
type shadowTestSourceOpts struct {
	createdAt                                  time.Time
	shadowReviewedAt                           *time.Time
	totalPolls, totalSuccesses, totalJobsFound int64
	yieldRatio                                 float32
}

// insertShadowTestSource inserts a pending_review source with counters and a
// created_at set directly, the way insertTestSource does for active sources
// — raw INSERT because this is fixture setup, and because total_polls/
// total_jobs_found/yield_ratio are ordinarily only ever written by
// UpdateSourceAfterPoll, never by a query this package would use directly.
func insertShadowTestSource(t *testing.T, pool *pgxpool.Pool, o shadowTestSourceOpts) pgtype.UUID {
	t.Helper()

	url := fmt.Sprintf("https://example.test/%d/shadow.xml", time.Now().UnixNano())

	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		insert into source (kind, status, legal_posture, url, url_hash, created_at,
		                     shadow_reviewed_at, total_polls, total_successes, total_jobs_found, yield_ratio)
		values ('rss', 'pending_review', 'permitted', $1, digest($1, 'sha256'), $2, $3, $4, $5, $6, $7)
		returning id
	`, url, o.createdAt, o.shadowReviewedAt, o.totalPolls, o.totalSuccesses, o.totalJobsFound, o.yieldRatio).
		Scan(&id)
	if err != nil {
		t.Fatalf("insert shadow test source: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from source where id = $1::uuid`, id)
	})

	return id
}

func TestReviewShadowSources(t *testing.T) {
	pool := testPool(t)
	// WithShadowBatchLimit(2) plus deepPast below (long before any real
	// seed data's created_at) guarantees this pass looks at exactly this
	// test's two due fixtures and nothing from the 1,400+ real
	// pending_review sources already sitting in the shared local dev
	// database (many already past their own real 48h window — see
	// insertTestSource's comment in scheduler_test.go on why the shared-DB
	// test setup requires this). Without both guards, this test would
	// promote or flag real seeded sources as a side effect of `go test`.
	sched := New(pool, &fakeGate{}, &fakeFetcher{}, testLogger()).WithShadowBatchLimit(2)
	deepPast := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	tooRecent := insertShadowTestSource(t, pool, shadowTestSourceOpts{
		createdAt: time.Now().Add(-1 * time.Hour), // window not closed yet
	})
	healthy := insertShadowTestSource(t, pool, shadowTestSourceOpts{
		createdAt:      deepPast,
		totalPolls:     20,
		totalSuccesses: 20,
		totalJobsFound: 12,
		yieldRatio:     0.6,
	})
	anomalous := insertShadowTestSource(t, pool, shadowTestSourceOpts{
		createdAt:      deepPast,
		totalPolls:     20,
		totalSuccesses: 20,
		totalJobsFound: 0,
	})
	alreadyReviewed := time.Now().Add(-1 * time.Hour)
	skipped := insertShadowTestSource(t, pool, shadowTestSourceOpts{
		createdAt:        deepPast,
		shadowReviewedAt: &alreadyReviewed,
		totalPolls:       20,
		totalSuccesses:   20,
		totalJobsFound:   12,
		yieldRatio:       0.6,
	})

	n, err := sched.ReviewShadowSources(context.Background())
	if err != nil {
		t.Fatalf("ReviewShadowSources: %v", err)
	}
	// healthy and anomalous are both due and both older (deepPast) than any
	// real row, so a LIMIT-2 pass ordered by created_at ASC reaches exactly
	// these two and nothing else. tooRecent's window hasn't closed; skipped
	// was already reviewed.
	if n != 2 {
		t.Fatalf("reviewed = %d, want 2", n)
	}

	healthyRow := getTestSource(t, pool, healthy)
	if healthyRow.Status != "active" {
		t.Errorf("healthy source status = %q, want active", healthyRow.Status)
	}
	if healthyRow.PriorityTier != 2 {
		t.Errorf("healthy source priority_tier = %d, want 2", healthyRow.PriorityTier)
	}
	if !healthyRow.ShadowReviewedAt.Valid {
		t.Errorf("healthy source shadow_reviewed_at not set")
	}

	anomalousRow := getTestSource(t, pool, anomalous)
	if anomalousRow.Status != "pending_review" {
		t.Errorf("anomalous source status = %q, want still pending_review", anomalousRow.Status)
	}
	if !anomalousRow.ShadowReviewedAt.Valid {
		t.Errorf("anomalous source shadow_reviewed_at not set")
	}
	if anomalousRow.Notes == nil || *anomalousRow.Notes == "" {
		t.Errorf("anomalous source notes not populated with a reason")
	}

	tooRecentRow := getTestSource(t, pool, tooRecent)
	if tooRecentRow.Status != "pending_review" || tooRecentRow.ShadowReviewedAt.Valid {
		t.Errorf("tooRecent source should be untouched: status=%q reviewed=%v", tooRecentRow.Status, tooRecentRow.ShadowReviewedAt.Valid)
	}

	skippedRow := getTestSource(t, pool, skipped)
	if skippedRow.Status != "pending_review" {
		t.Errorf("already-reviewed source must not be re-promoted by a later pass: status=%q", skippedRow.Status)
	}

	// Idempotency (a resolved row must not be re-flagged or re-promoted on a
	// later pass — the migration's whole reason for shadow_reviewed_at
	// existing) is what the "skipped" fixture above already proves: it was
	// pre-set with shadow_reviewed_at and excluded from this very pass. A
	// literal second sched.ReviewShadowSources call isn't used to prove it
	// here, deliberately: with healthy and anomalous now resolved, a second
	// call's LIMIT-2 window would fall through to the next-oldest eligible
	// rows in the shared local dev database — real seeded sources — and
	// promote or flag them as a side effect of this test running.
}
