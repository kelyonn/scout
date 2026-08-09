package scheduler

import (
	"testing"
	"time"
)

func TestNextCircuitStateSuccess(t *testing.T) {
	tests := []struct {
		name            string
		currentFailures int
	}{
		{"from closed", 0},
		{"from a few failures, not yet open", 3},
		{"from open", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures, openUntil := nextCircuitState(tt.currentFailures, true, time.Now())
			if failures != 0 {
				t.Errorf("failures = %d, want 0 after a success", failures)
			}
			if openUntil != nil {
				t.Errorf("openUntil = %v, want nil after a success", openUntil)
			}
		})
	}
}

func TestNextCircuitStateBelowThreshold(t *testing.T) {
	now := time.Now()
	for failures := 0; failures < circuitOpenThreshold-1; failures++ {
		got, openUntil := nextCircuitState(failures, false, now)
		if got != failures+1 {
			t.Errorf("nextCircuitState(%d, false) failures = %d, want %d", failures, got, failures+1)
		}
		if openUntil != nil {
			t.Errorf("nextCircuitState(%d, false) openUntil = %v, want nil (below threshold %d)",
				failures, openUntil, circuitOpenThreshold)
		}
	}
}

func TestNextCircuitStateOpensAtThreshold(t *testing.T) {
	now := time.Now()
	// docs/06 section 4: "5 consecutive failures → Open."
	failures, openUntil := nextCircuitState(circuitOpenThreshold-1, false, now)

	if failures != circuitOpenThreshold {
		t.Fatalf("failures = %d, want %d", failures, circuitOpenThreshold)
	}
	if openUntil == nil {
		t.Fatal("openUntil = nil, want the breaker to open at the threshold")
	}

	// backoff at the threshold is the base: 60s × 2^0 = 60s.
	wantBackoff := circuitBaseBackoff
	gotBackoff := openUntil.Sub(now)
	if gotBackoff < wantBackoff-time.Second || gotBackoff > wantBackoff+time.Second {
		t.Errorf("backoff = %v, want ~%v", gotBackoff, wantBackoff)
	}
}

func TestNextCircuitStateBackoffDoublesAndCaps(t *testing.T) {
	now := time.Now()

	tests := []struct {
		failures    int // failures BEFORE this poll
		wantBackoff time.Duration
	}{
		{circuitOpenThreshold, circuitBaseBackoff * 2},     // one failure past the threshold: doubles
		{circuitOpenThreshold + 1, circuitBaseBackoff * 4}, // doubles again
		{circuitOpenThreshold + 2, circuitBaseBackoff * 8},
	}

	for _, tt := range tests {
		_, openUntil := nextCircuitState(tt.failures, false, now)
		if openUntil == nil {
			t.Fatalf("failures=%d: openUntil = nil, want open", tt.failures)
		}
		got := openUntil.Sub(now)
		if got < tt.wantBackoff-time.Second || got > tt.wantBackoff+time.Second {
			t.Errorf("failures=%d: backoff = %v, want ~%v", tt.failures, got, tt.wantBackoff)
		}
	}

	t.Run("caps at 6 hours", func(t *testing.T) {
		_, openUntil := nextCircuitState(100, false, now)
		if openUntil == nil {
			t.Fatal("openUntil = nil, want open")
		}
		got := openUntil.Sub(now)
		if got != circuitMaxBackoff {
			t.Errorf("backoff = %v, want exactly the %v cap", got, circuitMaxBackoff)
		}
	})
}

func TestNextCircuitStateNeverGoesNegative(t *testing.T) {
	// Defensive: a negative starting count should never occur in practice
	// (consecutive_failures is a non-negative column), but must not produce
	// nonsense like a negative failure count or an inverted backoff.
	failures, openUntil := nextCircuitState(-1, false, time.Now())
	if failures < 0 {
		t.Errorf("failures = %d, want non-negative", failures)
	}
	if openUntil != nil {
		t.Errorf("openUntil = %v, want nil (still below threshold)", openUntil)
	}
}
