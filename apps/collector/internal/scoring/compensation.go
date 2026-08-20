// compensation — docs/09-ranking-scoring.md section 3.4, SCOUT-RANK-005.
// Percentile against comparables, not absolute value, since ₹80,000/month
// is excellent in India and poor in San Francisco. The percentile itself
// is computed in SQL (packages/db/queries/score.sql's
// SelectCompensationPercentile, using Postgres's percent_rank()) since it
// needs to rank against every other comparable job in the table — this
// function is the pure "what does the percentile mean" half.
package scoring

import "math"

// compensationMinComparables is docs/09's own floor: "A minimum of 20
// comparables is required; below that we fall back to a
// country-and-seniority prior rather than computing a percentile from 3
// data points."
const compensationMinComparables = 20

// computeCompensation implements docs/09 section 3.4's percentile-to-score
// mapping and its "if unknown" branch. hasComp is false when the job
// itself has no comp_normalized_inr_month (the common case per docs/07
// section 7 — most ATS postings state no compensation at all), in which
// case there is nothing to rank and the comparable counts are irrelevant.
// comparableCount/atOrBelowCount come from
// packages/db/queries/score.sql's SelectCompensationPercentileExact/Broad
// — the percentile itself (atOrBelowCount/comparableCount) is computed
// here rather than in SQL, since sqlc's type inference on that division
// doesn't resolve reliably and the arithmetic is trivial.
func computeCompensation(hasComp bool, comparableCount, atOrBelowCount int) (int, map[string]any) {
	if !hasComp {
		return placeholderScore, map[string]any{"confidence_low": true, "reason": "job has no normalized compensation"}
	}
	if comparableCount < compensationMinComparables {
		return placeholderScore, map[string]any{
			"confidence_low": true, "reason": "fewer than 20 comparables", "comparable_count": comparableCount,
		}
	}
	percentile := float64(atOrBelowCount) / float64(comparableCount)
	return int(math.Round(100 * percentile)), map[string]any{
		"confidence_low": false, "percentile": percentile, "comparable_count": comparableCount,
	}
}
