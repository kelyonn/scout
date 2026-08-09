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

	backoff := time.Duration(float64(circuitBaseBackoff) * math.Pow(2, float64(failures-circuitOpenThreshold)))
	if backoff > circuitMaxBackoff {
		backoff = circuitMaxBackoff
	}

	until := now.Add(backoff)
	return failures, &until
}
