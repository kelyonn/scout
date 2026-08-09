package interval

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"
)

func TestYieldFactor(t *testing.T) {
	tests := []struct {
		name       string
		yieldRatio float64
		want       float64
	}{
		// The range's stated maximum, reached at zero yield — "no evidence
		// yet" and "genuinely nothing found" both poll relatively often.
		{"zero yield is the range maximum", 0, 4.0},
		// The formula's asymptotic floor as yield grows large — the range's
		// stated minimum.
		{"very high yield approaches the range minimum", 100, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := YieldFactor(tt.yieldRatio)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("YieldFactor(%v) = %v, want ~%v", tt.yieldRatio, got, tt.want)
			}
		})
	}

	t.Run("monotonically decreasing", func(t *testing.T) {
		prev := YieldFactor(0)
		for _, r := range []float64{0.5, 1, 2, 5, 10} {
			cur := YieldFactor(r)
			if cur > prev {
				t.Fatalf("YieldFactor not monotonically decreasing at ratio=%v: %v > %v", r, cur, prev)
			}
			prev = cur
		}
	})

	t.Run("stays within the documented range 0.3-4.0", func(t *testing.T) {
		for _, r := range []float64{0, 0.1, 0.5, 1, 2, 5, 10, 50} {
			got := YieldFactor(r)
			if got < 0.3 || got > 4.0 {
				t.Errorf("YieldFactor(%v) = %v, out of the documented [0.3, 4.0] range", r, got)
			}
		}
	})
}

func TestRecencyFactor(t *testing.T) {
	tests := []struct {
		name string
		days float64
		want float64
	}{
		{"exactly the recent boundary", 7, 0.5},
		{"well within recent", 1, 0.5},
		{"exactly the stale boundary", 90, 2.0},
		{"well past stale", 365, 2.0},
		{"midpoint interpolates between the two anchors", 48.5, 1.25}, // (7+90)/2
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecencyFactor(tt.days)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("RecencyFactor(%v) = %v, want %v", tt.days, got, tt.want)
			}
		})
	}

	t.Run("never leaves the documented range 0.5-2.0", func(t *testing.T) {
		for _, d := range []float64{0, 7, 8, 50, 89, 90, 91, 1000} {
			got := RecencyFactor(d)
			if got < 0.5 || got > 2.0 {
				t.Errorf("RecencyFactor(%v) = %v, out of the documented [0.5, 2.0] range", d, got)
			}
		}
	})
}

func TestSeasonFactor(t *testing.T) {
	tests := []struct {
		month time.Month
		want  float64
	}{
		{time.August, 0.6},
		{time.September, 0.6},
		{time.October, 0.6},
		{time.January, 0.6},
		{time.February, 0.6},
		{time.March, 0.6},
		// Off-season: docs/06's own worked example is set in June and uses
		// 1.2, so that is the one non-peak value this package can verify
		// exactly rather than guess at.
		{time.April, 1.2},
		{time.May, 1.2},
		{time.June, 1.2},
		{time.July, 1.2},
		{time.November, 1.2},
		{time.December, 1.2},
	}
	for _, tt := range tests {
		t.Run(tt.month.String(), func(t *testing.T) {
			if got := SeasonFactor(tt.month); got != tt.want {
				t.Errorf("SeasonFactor(%s) = %v, want %v", tt.month, got, tt.want)
			}
		})
	}
}

func TestFailureFactor(t *testing.T) {
	tests := []struct {
		failures int
		want     float64
	}{
		// Both worked examples use failure_factor = 1.0 for a healthy source.
		{0, 1.0},
		{-1, 1.0}, // defensive: a negative count should never occur, but must not invert the formula
		{1, 2.0},
		{2, 4.0},
		// The range's stated ceiling, reached and held rather than approached.
		{3, 8.0},
		{4, 8.0},
		{10, 8.0},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.failures)+"_failures", func(t *testing.T) {
			if got := FailureFactor(tt.failures); got != tt.want {
				t.Errorf("FailureFactor(%d) = %v, want %v", tt.failures, got, tt.want)
			}
		})
	}
}

// TestWorkedExampleOneClampsToMinimum reproduces docs/06 section 3.1's first
// worked example: "A Greenhouse board with base = 900s that posted a job
// yesterday, during peak season, with a good yield ratio ... → clamped to
// min_interval (300s) = 5 minutes." yieldRatio is chosen so YieldFactor lands
// near the example's stated 0.45 — the exact value is not load-bearing here,
// because 900 × (anything under about 0.6) × 0.5 × 0.6 already falls well
// under the 300s floor, which is the property this test actually checks.
func TestWorkedExampleOneClampsToMinimum(t *testing.T) {
	got := Compute(Inputs{
		BaseSeconds:           900,
		MinSeconds:            300,
		MaxSeconds:            86400,
		YieldRatio:            1.6,                                                    // YieldFactor(1.6) ≈ 0.45, matching the example
		DaysSinceLastActivity: 1,                                                      // "posted a job yesterday" → RecencyFactor 0.5
		Now:                   time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC), // peak season
		ConsecutiveFailures:   0,
	})

	if got != 300*time.Second {
		t.Errorf("Compute(...) = %v, want the 300s floor (matching the example's stated clamp)", got)
	}
}

// TestWorkedExampleTwoIsInTheRightOrderOfMagnitude reproduces docs/06 section
// 3.1's second worked example: "The same board in June with no posting in 90
// days: 900 × 3.8 (no yield) × 2.0 (dormant) × 1.2 (off-season) × 1.0 = 8,208s
// → ~2.3 hours."
//
// This package's YieldFactor(0) is 4.0, not the example's 3.8 — see the
// package comment on interval.go for why: 0.3 + 3.7×e^0 = 4.0 exactly, which
// is also the range's own stated maximum, so 4.0 is what the documented
// formula actually produces at zero yield. The discrepancy is the spec
// example's, not this package's departure from the spec. The resulting
// interval (900×4.0×2.0×1.2×1.0 = 8,640s ≈ 2.4h) is checked against a
// tolerance around the example's 8,208s rather than for exact equality, which
// is what an honest implementation of the stated formula produces.
func TestWorkedExampleTwoIsInTheRightOrderOfMagnitude(t *testing.T) {
	got := Compute(Inputs{
		BaseSeconds:           900,
		MinSeconds:            300,
		MaxSeconds:            86400,
		YieldRatio:            0,  // "no yield"
		DaysSinceLastActivity: 90, // "nothing in 90 days"
		Now:                   time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC),
		ConsecutiveFailures:   0,
	})

	wantApprox := 8208 * time.Second
	tolerance := wantApprox / 10 // 10%

	diff := got - wantApprox
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		t.Errorf("Compute(...) = %v, want within 10%% of the example's %v (got a %v difference)",
			got, wantApprox, diff)
	}

	if got <= 300*time.Second || got >= 86400*time.Second {
		t.Errorf("Compute(...) = %v, expected it to land strictly between min and max (unclamped)", got)
	}
}

func TestComputeClampsToMinAndMax(t *testing.T) {
	t.Run("extreme factors clamp to the minimum", func(t *testing.T) {
		got := Compute(Inputs{
			BaseSeconds: 900, MinSeconds: 300, MaxSeconds: 86400,
			YieldRatio: 100, DaysSinceLastActivity: 0,
			Now: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		})
		if got != 300*time.Second {
			t.Errorf("Compute(...) = %v, want the 300s floor", got)
		}
	})

	t.Run("extreme factors clamp to the maximum", func(t *testing.T) {
		got := Compute(Inputs{
			// A large base so the product of near-maximal factors (yield 4.0 ×
			// recency 2.0 × season 1.2 × failure 8.0 ≈ 76.8×) comfortably
			// exceeds the 86400s ceiling rather than merely approaching it.
			BaseSeconds: 90000, MinSeconds: 300, MaxSeconds: 86400,
			YieldRatio: 0, DaysSinceLastActivity: 1000,
			Now:                 time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			ConsecutiveFailures: 10,
		})
		if got != 86400*time.Second {
			t.Errorf("Compute(...) = %v, want the 86400s ceiling", got)
		}
	})
}

func TestComputeCyclicalCap(t *testing.T) {
	// docs/06 section 3.1: cyclical sources cap at 4h regardless of
	// max_interval_s or measured yield, because a services firm or campus
	// platform that has been quiet for months can open a drive that fills
	// within days — a plain yield-based backoff would sleep through it.
	in := Inputs{
		// A large base so near-maximal factors comfortably exceed the 24h
		// ceiling — see the identical note in TestComputeClampsToMinAndMax.
		BaseSeconds: 90000, MinSeconds: 300, MaxSeconds: 86400, // 24h ceiling configured...
		YieldRatio: 0, DaysSinceLastActivity: 1000, // ...and factors that would otherwise reach it
		Now:                 time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		ConsecutiveFailures: 10,
	}

	continuous := Compute(in)
	if continuous != 86400*time.Second {
		t.Fatalf("continuous baseline = %v, want the 24h ceiling (test setup check)", continuous)
	}

	in.HiringPattern = HiringCyclical
	cyclical := Compute(in)
	if cyclical != 4*time.Hour {
		t.Errorf("cyclical Compute(...) = %v, want the 4h cap regardless of max_interval_s", cyclical)
	}
}

func TestComputeCyclicalCapDoesNotRaiseATighterMax(t *testing.T) {
	// A source configured with a max tighter than 4h must keep its own,
	// smaller ceiling — the cyclical override is a cap, not a floor.
	got := Compute(Inputs{
		BaseSeconds: 900, MinSeconds: 300, MaxSeconds: 1800, // 30 minutes, below the 4h cyclical cap
		YieldRatio: 0, DaysSinceLastActivity: 1000,
		Now:                 time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		ConsecutiveFailures: 10,
		HiringPattern:       HiringCyclical,
	})
	if got != 1800*time.Second {
		t.Errorf("Compute(...) = %v, want the source's own 1800s max to still apply", got)
	}
}

func TestJitterStaysWithinTenPercent(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := 900 * time.Second
	lower := time.Duration(float64(base) * 0.9)
	upper := time.Duration(float64(base) * 1.1)

	for i := 0; i < 10000; i++ {
		got := Jitter(base, rng)
		if got < lower || got > upper {
			t.Fatalf("Jitter(%v) = %v, outside the documented ±10%% band [%v, %v]", base, got, lower, upper)
		}
	}
}

func TestJitterIsNotConstant(t *testing.T) {
	// A jitter implementation that silently always returns the input (e.g. a
	// broken RNG wiring) would pass the bounds check above trivially — this
	// catches that failure mode specifically.
	rng := rand.New(rand.NewPCG(1, 2))
	base := 900 * time.Second

	seen := map[time.Duration]bool{}
	for i := 0; i < 100; i++ {
		seen[Jitter(base, rng)] = true
	}
	if len(seen) < 50 {
		t.Errorf("Jitter produced only %d distinct values across 100 draws, want meaningfully more", len(seen))
	}
}
