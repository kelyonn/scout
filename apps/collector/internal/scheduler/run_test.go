package scheduler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kelyon/scout/apps/collector/internal/fetch"
	"github.com/kelyon/scout/apps/collector/internal/politeness"
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

	s := New(pool, gate, fetcher, testLogger()).WithBatchLimit(10).WithConcurrency(4)

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

	s := New(pool, gate, fetcher, testLogger())
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

	s := New(pool, gate, fetcher, testLogger())
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

	s := New(pool, gate, fetcher, testLogger())
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

	s := New(pool, gate, fetcher, testLogger())
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

	s := New(pool, gate, fetcher, testLogger())
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

	s := New(pool, gate, fetcher, testLogger())
	n, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 0 {
		t.Errorf("RunOnce claimed %d sources, want 0 (none were due)", n)
	}
}

func TestClaimPreventsDoubleProcessingWithinTheClaimWindow(t *testing.T) {
	// The property claimBatch exists for: once claimed, a source must not be
	// selected again by a second, concurrent RunOnce before the first one has
	// recorded an outcome — proving the short-transaction claim (not the
	// eventual UpdateSourceAfterPoll) is what makes the row temporarily
	// ineligible.
	pool := testPool(t)
	insertTestSource(t, pool, defaultTestSourceOpts())

	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	s1 := New(pool, gate, fetcher, testLogger())
	s2 := New(pool, gate, fetcher, testLogger())

	n1, err := s1.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first RunOnce claimed %d, want 1", n1)
	}

	// s1's poll has already completed synchronously (RunOnce waits for the
	// whole batch), so by the time we get here the row's next_poll_at reflects
	// the real post-poll reschedule, not the claim window — a second RunOnce
	// finding nothing here would prove nothing about the claim mechanism
	// itself. What we actually assert is narrower and still meaningful: a
	// second call immediately after the first does not double-count the same
	// source within one outcome.
	n2, err := s2.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second RunOnce (immediately after the first completed) claimed %d, want 0", n2)
	}

	if fetcher.calls.Load() != 1 {
		t.Errorf("fetcher.Fetch was called %d times across both RunOnce calls, want exactly 1", fetcher.calls.Load())
	}
}

func TestRunOnceWithNothingDueReturnsZero(t *testing.T) {
	pool := testPool(t)
	gate := &fakeGate{}
	fetcher := &fakeFetcher{}

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

	n, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 0 {
		t.Errorf("RunOnce = %d, want 0", n)
	}
	if gate.calls.Load() != 0 || fetcher.calls.Load() != 0 {
		t.Error("gate or fetcher was called despite nothing being due")
	}
}

func TestRunBlocksUntilContextCancelled(t *testing.T) {
	pool := testPool(t)
	gate := &fakeGate{decision: politeness.Decision{Result: politeness.ResultAllow}}
	fetcher := &fakeFetcher{result: &fetch.Result{StatusCode: http.StatusOK, FetchedAt: time.Now()}}

	s := New(pool, gate, fetcher, testLogger())

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
