package scheduler

import (
	"context"
	"fmt"
	"time"

	db "github.com/kelyon/scout/packages/db/gen"
)

// ShadowReviewInterval governs how often Run checks for pending_review
// sources whose 48h shadow window (docs/05-source-catalog.md section 13)
// has closed. Cheap — SelectShadowSourcesDueForReview only ever returns
// rows for sources actually due — so a much shorter interval than the 48h
// window itself costs nothing and gets a healthy source promoted (and
// therefore eligible to notify) promptly rather than sitting an extra hour
// past its window for no reason.
const ShadowReviewInterval = 15 * time.Minute

// shadowReviewBatchLimit bounds one review pass the same way DefaultBatchLimit
// bounds one poll tick — nothing about shadow review needs to process more
// than a modest batch per pass; it runs again in ShadowReviewInterval either way.
const shadowReviewBatchLimit = 200

// Thresholds for decideShadowPromotion. Not from any spec — docs/05 section
// 13 says only "zero jobs, or a suspiciously high count" without numbers.
// minHealthyJobs=1 is the literal "zero jobs" case. maxPlausibleJobs is a
// judgment call: a single company's careers page listing more than this in
// 48 hours is far more likely to be an adapter matching too broadly (a
// selector that grabs every link on the page, a paginated endpoint fetched
// without its filter) than a company that genuinely has that many open
// roles. Revisit if a real GCC or large-tech source legitimately trips it.
const (
	maxPlausibleJobs int64 = 2000

	// Tier bucket floors, highest first — never tier 1, which stays reserved
	// for manual curation (docs/05 step 6 assigns a tier "by observed
	// yield," but is silent on tier 1 specifically; QuarantineSource
	// reserves 'quarantined' the same way, for a human to clear).
	tierYieldHigh   float32 = 0.5
	tierYieldMedium float32 = 0.2
	tierYieldLow    float32 = 0.05
)

// shadowDecision is decideShadowPromotion's verdict: promote to active at
// Tier, or flag with Reason and leave pending_review for a human to look.
type shadowDecision struct {
	Promote bool
	Tier    int16
	Reason  string
}

// decideShadowPromotion is the pure counterpart to UpdateSourceAfterPoll's
// yield_ratio math: given a shadow-run source's accumulated counters, decide
// promote-or-flag exactly the way interval.Compute and changedetect decide
// their own outcomes — as a plain function, tested without a database.
func decideShadowPromotion(totalPolls, totalSuccesses, totalJobsFound int64, yieldRatio float32) shadowDecision {
	if totalSuccesses == 0 {
		return shadowDecision{
			Reason: fmt.Sprintf("shadow review: 0 successful polls of %d attempted in 48h — adapter or URL likely broken", totalPolls),
		}
	}
	if totalJobsFound == 0 {
		return shadowDecision{
			Reason: fmt.Sprintf("shadow review: %d successful polls, 0 jobs found in 48h — check adapter_config against the live page", totalSuccesses),
		}
	}
	if totalJobsFound > maxPlausibleJobs {
		return shadowDecision{
			Reason: fmt.Sprintf("shadow review: %d jobs found in 48h, above the %d sanity ceiling — likely an adapter matching too broadly", totalJobsFound, maxPlausibleJobs),
		}
	}

	var tier int16
	switch {
	case yieldRatio >= tierYieldHigh:
		tier = 2
	case yieldRatio >= tierYieldMedium:
		tier = 3
	case yieldRatio >= tierYieldLow:
		tier = 4
	default:
		tier = 5
	}

	return shadowDecision{Promote: true, Tier: tier}
}

// ReviewShadowSources runs one shadow-review pass: every pending_review
// source whose 48h window has closed gets promoted or flagged, per
// decideShadowPromotion. Returns how many rows it looked at.
func (s *Scheduler) ReviewShadowSources(ctx context.Context) (int, error) {
	q := db.New(s.pool)

	rows, err := q.SelectShadowSourcesDueForReview(ctx, s.shadowBatchLimit)
	if err != nil {
		return 0, fmt.Errorf("scheduler: select shadow sources due for review: %w", err)
	}

	for _, row := range rows {
		decision := decideShadowPromotion(row.TotalPolls, row.TotalSuccesses, row.TotalJobsFound, row.YieldRatio)

		if decision.Promote {
			if err := q.PromoteShadowSource(ctx, db.PromoteShadowSourceParams{
				ID:           row.ID,
				PriorityTier: decision.Tier,
			}); err != nil {
				s.log.Warn("promote shadow source failed", "source_id", row.ID.String(), "err", err)
				continue
			}
			s.log.Info("shadow source promoted", "source_id", row.ID.String(), "tier", decision.Tier,
				"total_polls", row.TotalPolls, "total_jobs_found", row.TotalJobsFound, "yield_ratio", row.YieldRatio)
			continue
		}

		if err := q.FlagShadowSource(ctx, db.FlagShadowSourceParams{
			ID:     row.ID,
			Reason: decision.Reason,
		}); err != nil {
			s.log.Warn("flag shadow source failed", "source_id", row.ID.String(), "err", err)
			continue
		}
		// Warn, not Info: docs/05 section 13 calls this an alert — a shadow
		// run needing a human look, distinct from ordinary poll-tick noise.
		// url is a source registry entry (a public careers-page link), not
		// user data — AGENTS.md rule 7 is about resumes/emails/phones/job
		// descriptions, none of which this touches.
		s.log.Warn("shadow source flagged for review", "source_id", row.ID.String(), "url", row.Url, "reason", decision.Reason)
	}

	return len(rows), nil
}
