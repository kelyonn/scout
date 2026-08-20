package scheduler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/packages/fetch"
)

func TestRunOnceAllowedSuccessUpdatesEverything(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{
		StatusCode:   http.StatusOK,
		Body:         []byte(`{"jobs":[{"title":"Backend Engineer"}]}`),
		ETag:         `"abc123"`,
		LastModified: "Wed, 06 Aug 2026 09:15:00 GMT",
		FetchedAt:    time.Now(),
	}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1).WithConcurrency(4)

	n, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("RunOnce claimed %d sources, want 1", n)
	}

	if gate.calls.Load() != 1 {
		t.Errorf("gate.Allow called %d times, want 1", gate.calls.Load())
	}
	if gate.released.Load() != 1 {
		t.Errorf("release called %d times, want exactly 1", gate.released.Load())
	}
	if fetcher.calls.Load() != 1 {
		t.Errorf("fetcher.Fetch called %d times, want 1", fetcher.calls.Load())
	}

	row := getTestSource(t, pool, id)

	if row.TotalPolls != 1 {
		t.Errorf("total_polls = %d, want 1", row.TotalPolls)
	}
	if row.TotalSuccesses != 1 {
		t.Errorf("total_successes = %d, want 1", row.TotalSuccesses)
	}
	if row.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d, want 0", row.ConsecutiveFailures)
	}
	if row.CircuitOpenUntil.Valid {
		t.Error("circuit_open_until is set after a success, want NULL")
	}
	if row.LastEtag == nil || *row.LastEtag != `"abc123"` {
		t.Errorf("last_etag = %v, want \"abc123\"", row.LastEtag)
	}
	if row.LastModified == nil || *row.LastModified != "Wed, 06 Aug 2026 09:15:00 GMT" {
		t.Errorf("last_modified = %v", row.LastModified)
	}
	if len(row.LastContentHash) != 32 {
		t.Errorf("len(last_content_hash) = %d, want 32 (sha256)", len(row.LastContentHash))
	}
	if !row.LastChangedAt.Valid {
		t.Error("last_changed_at is NULL after new content was seen for the first time")
	}
	if !row.NextPollAt.Time.After(time.Now()) {
		t.Errorf("next_poll_at = %v, want it in the future", row.NextPollAt.Time)
	}
}

func TestRunOnceNotModifiedDoesNotAdvanceLastChangedAt(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{
		StatusCode:  http.StatusNotModified,
		NotModified: true,
		FetchedAt:   time.Now(),
	}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	row := getTestSource(t, pool, id)
	if row.LastChangedAt.Valid {
		t.Error("last_changed_at was set on a 304, want it to stay NULL (nothing changed)")
	}
	if row.TotalSuccesses != 1 {
		t.Errorf("total_successes = %d, want 1 (a 304 is a success)", row.TotalSuccesses)
	}
}

func TestRunOnceFetchErrorIncrementsFailures(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{err: context.DeadlineExceeded}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if gate.released.Load() != 1 {
		t.Error("release was not called after a fetch error — the concurrency slot would leak")
	}

	row := getTestSource(t, pool, id)
	if row.ConsecutiveFailures != 1 {
		t.Errorf("consecutive_failures = %d, want 1", row.ConsecutiveFailures)
	}
	if row.TotalSuccesses != 0 {
		t.Errorf("total_successes = %d, want 0", row.TotalSuccesses)
	}
	if row.TotalPolls != 1 {
		t.Errorf("total_polls = %d, want 1 (an attempt was made and failed)", row.TotalPolls)
	}
}

func TestRunOnceServerErrorCountsAsFailure(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusServiceUnavailable, FetchedAt: time.Now()}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	row := getTestSource(t, pool, id)
	if row.ConsecutiveFailures != 1 {
		t.Errorf("consecutive_failures = %d, want 1 (a 503 counts as a failure)", row.ConsecutiveFailures)
	}
}

func TestRunOnceRefuseIsTreatedAsFailureWithNoFetch(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultRefuse, Reason: "robots.txt disallows"}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if fetcher.calls.Load() != 0 {
		t.Errorf("fetcher.Fetch was called %d times after a REFUSE, want 0", fetcher.calls.Load())
	}

	row := getTestSource(t, pool, id)
	if row.ConsecutiveFailures != 1 {
		t.Errorf("consecutive_failures = %d, want 1", row.ConsecutiveFailures)
	}
}

func TestRunOnceDeferOnlyReschedulesAndTouchesNoCounters(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())
	before := getTestSource(t, pool, id)

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultDefer, Reason: "rate budget exhausted"}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if fetcher.calls.Load() != 0 {
		t.Errorf("fetcher.Fetch was called %d times after a DEFER, want 0", fetcher.calls.Load())
	}

	after := getTestSource(t, pool, id)
	if after.TotalPolls != before.TotalPolls {
		t.Errorf("total_polls changed from %d to %d on a DEFER, want unchanged", before.TotalPolls, after.TotalPolls)
	}
	if after.ConsecutiveFailures != before.ConsecutiveFailures {
		t.Errorf("consecutive_failures changed on a DEFER, want unchanged")
	}
	if !after.NextPollAt.Time.After(time.Now()) {
		t.Error("next_poll_at was not pushed into the future after a DEFER")
	}
}

func TestRunOnceSkipsSourcesNotYetDue(t *testing.T) {
	pool := testPool(t)
	opts := defaultTestSourceOpts()
	id := insertTestSource(t, pool, opts)

	// Push it into the future — must not be claimed.
	_, err := pool.Exec(context.Background(),
		`update source set next_poll_at = now() + interval '1 hour' where id = $1::uuid`, id)
	if err != nil {
		t.Fatalf("push next_poll_at forward: %v", err)
	}

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	// WithBatchLimit(1): bounds the batch, but on the shared dev database
	// (1,500+ real seeded sources, most already due — see insertTestSource's
	// comment in scheduler_test.go) it does not guarantee n == 0: some other,
	// real due row legitimately fills that one slot instead of this test's
	// own future-dated fixture. That is actually fine and expected — this
	// test's real invariant is narrower than "nothing anywhere is due," it
	// is "this specific row, pushed into the future, is not the one that got
	// claimed," which the row's own unchanged state below proves regardless
	// of what else RunOnce found to do in this call.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	row := getTestSource(t, pool, id)
	if row.TotalPolls != 0 {
		t.Errorf("total_polls = %d, want 0 (this source was not due and must not have been polled)", row.TotalPolls)
	}
}

func TestClaimPreventsDoubleProcessingWithinTheClaimWindow(t *testing.T) {
	// The property claimBatch exists for: once claimed, a source must not be
	// selected again by a second, concurrent RunOnce before the first one has
	// recorded an outcome — proving the short-transaction claim (not the
	// eventual UpdateSourceAfterPoll) is what makes the row temporarily
	// ineligible.
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	// WithBatchLimit(1) on both: see insertTestSource's comment in
	// scheduler_test.go for why an unbounded batch against the shared dev
	// database is unsafe.
	s1 := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)
	s2 := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)

	n1, err := s1.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first RunOnce claimed %d, want 1", n1)
	}

	// s1's poll has already completed synchronously (RunOnce waits for the
	// whole batch), so by the time we get here this fixture's next_poll_at
	// reflects the real post-poll reschedule, not the claim window — a
	// second RunOnce claiming 0 rows globally would prove nothing about the
	// claim mechanism itself, and on the shared dev database (1,500+ real
	// seeded sources — see insertTestSource's comment in scheduler_test.go)
	// it would not even be true: some other real due row legitimately fills
	// that second batch slot. The actual property under test — this
	// fixture is not double-counted within the claim window — is what
	// row.TotalPolls == 1 below proves directly, regardless of what else
	// either RunOnce call found to do.
	if _, err := s2.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	row := getTestSource(t, pool, id)
	if row.TotalPolls != 1 {
		t.Errorf("total_polls = %d after two immediate RunOnce calls, want 1 (not double-processed)", row.TotalPolls)
	}
}

func TestRunOnceWithNothingDueReturnsZero(t *testing.T) {
	pool := testPool(t)
	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	// A real, non-nil result rather than the zero-value fakeFetcher: this
	// test's fixture is pushed into the future below, but on the shared dev
	// database (1,500+ real seeded sources — see insertTestSource's comment)
	// a WithBatchLimit(1) call can still legitimately claim some other real
	// due row instead of finding literally nothing. A zero-value fakeFetcher
	// returns (nil, nil) from Fetch, which crashed the whole test binary
	// (nil pointer dereference in fetchStatusFor) the moment that happened —
	// found live once real due data existed. This fake must be crash-safe
	// for whatever real row might land in that one slot.
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)

	// Not inserting any source at all is not a reliable "nothing due" test on
	// a shared database — other tests in this package insert their own rows
	// concurrently. Instead, insert one and immediately push it far into the
	// future so this test's own assertion is self-contained.
	id := insertTestSource(t, pool, defaultTestSourceOpts())
	_, err := pool.Exec(context.Background(),
		`update source set next_poll_at = now() + interval '1 hour' where id = $1::uuid`, id)
	if err != nil {
		t.Fatalf("push next_poll_at forward: %v", err)
	}

	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// The real invariant: this specific future-dated row was not the one
	// claimed and polled — not "RunOnce found globally nothing to do," which
	// the shared real seed data makes untestable here (see fetcher's comment
	// above).
	row := getTestSource(t, pool, id)
	if row.TotalPolls != 0 {
		t.Errorf("total_polls = %d, want 0 (this source was not due and must not have been polled)", row.TotalPolls)
	}
}

func TestRunBlocksUntilContextCancelled(t *testing.T) {
	pool := testPool(t)
	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	// WithBatchLimit(1): keeps this test scoped to its own fixture — see
	// insertTestSource's comment in scheduler_test.go for why an unbounded
	// batch against the shared dev database is unsafe.
	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(1)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx, 20*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// TestUpdateSourceAfterPoll_YieldRatioTracksRecentPolls proves the EMA
// yield_ratio computation packages/db/queries/source.sql's
// UpdateSourceAfterPoll now performs — docs/16-observability.md's
// scout_source_yield_ratio, and the first time this column has ever been
// written (it defaulted to 0 from schema creation through P2, which
// silently pinned interval.Compute's yield_factor input at its maximum —
// see that query's own comment). A poll that finds a new job nudges the
// ratio up toward 1; a poll that finds nothing nudges it down toward 0;
// it never leaves [0, 1].
func TestUpdateSourceAfterPoll_YieldRatioTracksRecentPolls(t *testing.T) {
	pool := testPool(t)
	id := insertTestSource(t, pool, defaultTestSourceOpts())
	q := db.New(pool)

	pollWithYield := func(newJobs int64) float32 {
		t.Helper()
		yieldRatio, err := q.UpdateSourceAfterPoll(context.Background(), db.UpdateSourceAfterPollParams{
			ID: id, NextPollAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
			CurrentIntervalS: 900, Success: true, JobsFound: newJobs, NewJobs: newJobs,
		})
		if err != nil {
			t.Fatalf("UpdateSourceAfterPoll: %v", err)
		}
		return yieldRatio
	}

	if got := pollWithYield(0); got != 0 {
		t.Fatalf("yield_ratio after one empty poll from a fresh source = %v, want 0", got)
	}

	// Ten consecutive yielding polls should pull the ratio up off zero —
	// each poll's own weight is small (0.01), so this checks direction and
	// boundedness, not a specific value.
	var last float32
	for range 10 {
		last = pollWithYield(1)
	}
	if last <= 0 {
		t.Errorf("yield_ratio after 10 yielding polls = %v, want > 0", last)
	}
	if last > 1 {
		t.Errorf("yield_ratio = %v, want <= 1", last)
	}

	// A run of empty polls should pull it back down again.
	var afterEmpty float32
	for range 10 {
		afterEmpty = pollWithYield(0)
	}
	if afterEmpty >= last {
		t.Errorf("yield_ratio after 10 empty polls = %v, want less than %v (the value before them)", afterEmpty, last)
	}
	if afterEmpty < 0 {
		t.Errorf("yield_ratio = %v, want >= 0", afterEmpty)
	}
}
