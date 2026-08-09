package scheduler

import (
	"math"
	"time"
)

// circuitOpenThreshold is "5 consecutive failures → Open" from docs/06
// section 4's circuit breaker state table.
const circuitOpenThreshold = 5

// circuitMaxBackoff is the table's stated cap: "A source down for a day is
// probed every 6 hours rather than every 5 minutes."
const circuitMaxBackoff = 6 * time.Hour

// circuitBaseBackoff is the table's formula: "min(60s × 2^(failures−5), 6
// hours)."
const circuitBaseBackoff = 60 * time.Second

// nextCircuitState computes the new consecutive-failure count and circuit
// breaker open-until time following one poll outcome, per docs/06 section 4:
//
//	Closed    Normal                 5 consecutive failures → Open
//	Open      No requests            After backoff, → Half-open
//	Half-open One probe request      Success → Closed. Failure → Open, backoff doubles
//
// There is no separate "half-open" representation in the data model — a
// half-open breaker is just a Closed-shaped row (CircuitOpenUntil in the
// past) that the politeness gate happened to let one request through for
// (apps/collector/internal/politeness), so this function does not need to
// know which case it is in. "Backoff doubles" on a half-open failure falls
// out of the same formula automatically: failures is one higher than last
// time, and 2^(n+1) is exactly double 2^n. A single formula, evaluated fresh
// every time, is what the state table's three transition rules collapse to.
func nextCircuitState(currentFailures int, success bool, now time.Time) (failures int, openUntil *time.Time) {
	if success {
		return 0, nil
	}

	failures = currentFailures + 1
	if failures < circuitOpenThreshold {
		return failures, nil
	}

	// Clamp in float64 space BEFORE converting to time.Duration (an int64
	// count of nanoseconds). 2^(failures-5) grows without bound as failures
	// grows, and converting an out-of-int64-range float64 to an integer type
	// is implementation-defined per the Go spec once it overflows — arm64 and
	// amd64 disagree about the result. At failures=100 this produced a huge
	// positive duration on arm64 (still under the cap check, so it slipped
	// through) and math.MinInt64 nanoseconds — a negative duration — on
	// amd64, which is what CI's linux/amd64 runner caught and a local arm64
	// Mac could not. Clamping the multiplier itself, before any conversion to
	// Duration, keeps the value always in int64 range, which makes the
	// conversion well-defined on every architecture instead of merely
	// well-behaved on the ones tested so far.
	backoffSeconds := math.Min(circuitBaseBackoff.Seconds()*math.Pow(2, float64(failures-circuitOpenThreshold)),
		circuitMaxBackoff.Seconds())
	backoff := time.Duration(backoffSeconds * float64(time.Second))

	until := now.Add(backoff)
	return failures, &until
}
