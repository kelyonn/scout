// Package interval implements the scheduler's adaptive polling interval:
// docs/06-ingestion-pipeline.md section 3.1.
//
// The formula is:
//
//	interval = clamp(base × yield_factor × recency_factor × season_factor ×
//	  failure_factor, min_interval, max_interval)
//
// # What is exact and what is interpreted
//
// yield_factor has an explicit closed-form formula in the spec table
// (0.3 + 3.7 × e^(−2 × yield_ratio)) and is implemented exactly as given.
//
// recency_factor, season_factor, and failure_factor are given only as a range
// plus one or two named anchor points, not a full formula — docs/06 gives
// "posted in the last 7 days → 0.5, nothing in 90 days → 2.0" for recency,
// "Aug–Oct and Jan–Mar → 0.6" for season (with the one worked non-peak
// example implying 1.2 for the rest of the year), and "exponential backoff"
// with no base for failure. Each of those three is this package's own
// interpretation of the named points, documented on its function, and each
// one is chosen to reproduce the two fully worked examples in docs/06 section
// 3.1 exactly — see interval_test.go's TestWorkedExamples.
//
// # yield_ratio and days-since-activity have no data source yet
//
// yield_ratio is "new jobs / 100 polls" — a per-posting signal that needs the
// job table and normalization pipeline neither of which exist yet (P2). Until
// then, callers pass 0, which yield_factor correctly reads as "no signal yet,
// poll cautiously often" (4.0, the top of its range) — the same behavior a
// genuinely low-yield source would get, which is the right default for a
// source nobody has evidence about.
//
// The spec's recency_factor input is "days since a job was last posted,"
// which has the same problem. The nearest signal that already exists is
// source.last_changed_at — the timestamp of the last Layer-2 content-hash
// change (apps/collector/internal/changedetect). It is not the same thing
// (an ATS re-rendering its page with new marketing copy would count as
// "activity" here and would not once job-level tracking exists), but it is
// the closest available proxy and it fails in the safe direction: a source
// that looks recently active gets polled more, not less. Callers are
// expected to compute "days since last_changed_at" and pass that in as
// DaysSinceLastActivity; the field is named for what it actually measures
// today rather than for the field it will eventually replace.
package interval

import (
	"math"
	"math/rand/v2"
	"time"
)

// HiringPattern mirrors source.hiring_pattern
// (infra/migrations/000004_source.up.sql).
type HiringPattern string

const (
	// HiringContinuous is the default: no cap beyond the source's own
	// max_interval_s.
	HiringContinuous HiringPattern = "continuous"
	// HiringCyclical caps the effective max at cyclicalMaxSeconds regardless
	// of the source's configured max_interval_s or measured yield.
	HiringCyclical HiringPattern = "cyclical"
)

// cyclicalMaxSeconds is the hard ceiling docs/06 section 3.1 sets for
// cyclical sources regardless of measured yield: "Sources with
// hiring_pattern = 'cyclical' therefore use max_interval = 4h regardless of
// recent yield." Services firms and campus platforms post nothing for weeks
// and then open a drive that fills within days; left to yield-based backoff
// alone they would drift to a 24h interval and Scout would find the drive on
// day two.
const cyclicalMaxSeconds = 4 * 60 * 60

// jitterFraction is the ±10% docs/06 section 3.3 specifies: "Every computed
// next_poll_at gets ±10% random jitter. Without it, sources registered in the
// same batch synchronize forever and produce periodic thundering herds."
const jitterFraction = 0.10

// Inputs bundles everything the formula needs for one source.
type Inputs struct {
	BaseSeconds int
	MinSeconds  int
	MaxSeconds  int

	HiringPattern HiringPattern

	// YieldRatio is new jobs per 100 polls. 0 until the job table exists —
	// see the package comment.
	YieldRatio float64

	// DaysSinceLastActivity — see the package comment on what this measures
	// today versus what the spec ultimately intends it to measure.
	DaysSinceLastActivity float64

	// ConsecutiveFailures is source.consecutive_failures.
	ConsecutiveFailures int

	// Now selects which season is in effect. Passed in rather than read via
	// time.Now() so the two worked examples (one in peak season, one in June)
	// are reproducible as ordinary table-driven tests.
	Now time.Time
}

// Compute returns the clamped interval for one source. It does not apply
// jitter — see [Jitter], applied separately so a caller can log or test the
// pre-jitter value.
func Compute(in Inputs) time.Duration {
	factor := YieldFactor(in.YieldRatio) *
		RecencyFactor(in.DaysSinceLastActivity) *
		SeasonFactor(in.Now.Month()) *
		FailureFactor(in.ConsecutiveFailures)

	seconds := float64(in.BaseSeconds) * factor

	maxSeconds := in.MaxSeconds
	if in.HiringPattern == HiringCyclical && maxSeconds > cyclicalMaxSeconds {
		maxSeconds = cyclicalMaxSeconds
	}

	seconds = math.Max(float64(in.MinSeconds), math.Min(float64(maxSeconds), seconds))
	return time.Duration(seconds) * time.Second
}

// YieldFactor implements the spec's exact formula: "0.3 + 3.7 × e^(−2 ×
// yield_ratio)". At yield_ratio = 0 (no signal, or genuinely nothing found)
// this evaluates to the range's stated maximum, 4.0 — poll relatively often
// when there is no evidence either way. High yield decays the factor toward
// 0.3, the range's stated minimum.
func YieldFactor(yieldRatio float64) float64 {
	return 0.3 + 3.7*math.Exp(-2*yieldRatio)
}

// RecencyFactor interpolates linearly between the two anchor points docs/06
// gives — "posted in the last 7 days → 0.5" and "nothing in 90 days → 2.0" —
// clamping outside that window rather than extrapolating past the range the
// spec actually bounds (0.5–2.0). The spec states two points, not a curve;
// linear interpolation is the simplest function connecting them and avoids
// the sharp step a piecewise-constant version would put at exactly 7 days,
// with no evidence from the spec that a step is what is intended either.
func RecencyFactor(daysSinceLastActivity float64) float64 {
	const (
		recentDays = 7.0
		recentVal  = 0.5
		staleDays  = 90.0
		staleVal   = 2.0
	)

	switch {
	case daysSinceLastActivity <= recentDays:
		return recentVal
	case daysSinceLastActivity >= staleDays:
		return staleVal
	default:
		t := (daysSinceLastActivity - recentDays) / (staleDays - recentDays)
		return recentVal + t*(staleVal-recentVal)
	}
}

// SeasonFactor implements the two data points docs/06 section 3.1 actually
// gives: peak internship season (August–October and January–March) is 0.6,
// and the one non-peak worked example (June) is 1.2. The stated range is
// 0.6–1.5, wider than these two points cover; nothing in the spec describes a
// gradient within the "off-season" half of the year, so this returns exactly
// one of the two documented values rather than inventing intermediate ones.
func SeasonFactor(month time.Month) float64 {
	switch month {
	case time.August, time.September, time.October,
		time.January, time.February, time.March:
		return 0.6
	default:
		return 1.2
	}
}

// FailureFactor is "exponential backoff on consecutive failures," range
// 1.0–8.0, with no base given. 2^failures, capped at the range's stated
// maximum, is the simplest function that is exactly 1.0 at zero failures
// (both worked examples use failure_factor = 1.0 for a healthy source) and
// exactly reaches the stated ceiling (2^3 = 8) rather than approaching it
// asymptotically.
func FailureFactor(consecutiveFailures int) float64 {
	if consecutiveFailures <= 0 {
		return 1.0
	}
	return math.Min(8.0, math.Pow(2, float64(consecutiveFailures)))
}

// Jitter applies docs/06 section 3.3's ±10% random jitter to d, using rng as
// the source of randomness so callers (and tests) control determinism
// explicitly rather than reaching for the global generator.
func Jitter(d time.Duration, rng *rand.Rand) time.Duration {
	// rng.Float64() is in [0, 1); shifting to [-1, 1) and scaling by
	// jitterFraction gives a uniform draw from [-10%, +10%).
	factor := 1 + (rng.Float64()*2-1)*jitterFraction
	return time.Duration(float64(d) * factor)
}
