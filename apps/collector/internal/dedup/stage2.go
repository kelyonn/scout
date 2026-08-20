// Dedup Stage 2 — structural matching. docs/08-dedup-identity.md section
// 3.2, SCOUT-DEDUP-011, and section 4's union-find grouping. Runs after
// Stage 1 finds no exact match and a new job/job_group has already been
// inserted: either merges that new group into an existing structurally-
// similar job's group, or leaves it standing and flags a plausible-but-
// unconfirmed candidate for Stage 3 (apps/brain, P2 Phase F) to check
// semantically.
package dedup

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/schema"
)

// gate1TitleThreshold and gate3MergeThreshold are docs/08 section 3.2's
// own literal thresholds.
const (
	gate1TitleThreshold = 0.85
	gate3MergeThreshold = 3
	// maxGroupMemberCount is docs/08 section 5's over-merge guard: "Above
	// 25, merging into it is blocked pending review." No review queue
	// exists yet to route a blocked merge into, so "blocked" here means
	// Stage 2 leaves the new job as its own group rather than merging —
	// the safe direction docs/08 itself calls out repeatedly.
	maxGroupMemberCount = 25
)

// Stage2Result is what Stage2Resolve found, folded back into the caller's
// dedup.Result.
type Stage2Result struct {
	// Merged is true if the new job's group was absorbed into (or
	// absorbed) an existing structurally-similar job's group.
	Merged bool
	// JobGroupID is the surviving group id when Merged is true.
	JobGroupID pgtype.UUID
	// Stage3CandidateJobID is set when Gate 1+2 passed but Gate 3's
	// SimHash distance was inconclusive (4-8 or >8) — docs/08's "escalate
	// to Stage 3." The caller (ingest.go) enqueues a brain_deep job
	// against it; Stage 2 itself makes no semantic judgment.
	Stage3CandidateJobID *pgtype.UUID
}

// Stage2Resolve runs docs/08 section 3.2's three gates against candidates
// in the same company + role_family + seniority, then either merges
// (Gate 3 distance <= 3) or flags for Stage 3 escalation (distance 4-8 or
// >8). newSimhash is the SimHash already computed for the new job's own
// stripped description — Resolve computes it once and reuses it both for
// the InsertJob write and this call, rather than recomputing.
func Stage2Resolve(
	ctx context.Context, q db.Querier,
	companyID, newJobID, newJobGroupID pgtype.UUID,
	roleFamily schema.RoleFamily, seniority schema.Seniority,
	normalizedTitle string, newSimhash uint64,
	locationCity *string, locationTier *int16, workMode schema.WorkMode,
) (Stage2Result, error) {
	if err := q.AdvisoryLockCompanyDedup(ctx, companyID.String()); err != nil {
		return Stage2Result{}, fmt.Errorf("dedup: stage2 advisory lock: %w", err)
	}

	candidates, err := q.SelectStage2Candidates(ctx, db.SelectStage2CandidatesParams{
		CompanyID:      companyID,
		RoleFamily:     db.RoleFamily(roleFamily),
		Seniority:      db.Seniority(seniority),
		ExcludingJobID: newJobID,
	})
	if err != nil {
		return Stage2Result{}, fmt.Errorf("dedup: stage2 select candidates: %w", err)
	}

	var best *stage2Candidate
	for _, c := range candidates {
		if JaroWinkler(normalizedTitle, c.NormalizedTitle) < gate1TitleThreshold {
			continue
		}
		if !locationCompatible(locationCity, locationTier, workMode, c.LocationCity, c.LocationTier, schema.WorkMode(c.WorkMode)) {
			continue
		}
		candSimhash := candidateSimhash(c)
		distance := HammingDistance(newSimhash, candSimhash)
		if best == nil || distance < best.distance {
			best = &stage2Candidate{row: c, distance: distance}
		}
	}

	if best == nil {
		return Stage2Result{}, nil
	}
	if best.distance > gate3MergeThreshold {
		return Stage2Result{Stage3CandidateJobID: &best.row.ID}, nil
	}

	survivingGroupID, merged, err := mergeJobGroups(
		ctx, q, newJobID, newJobGroupID, best.row.ID, best.row.JobGroupID,
		"structural", 0.95, map[string]any{"simhash_distance": best.distance},
	)
	if err != nil {
		return Stage2Result{}, fmt.Errorf("dedup: stage2 merge: %w", err)
	}
	if !merged {
		// Group-size cap hit — safe direction is to leave the new job
		// standing alone rather than force an over-sized merge.
		return Stage2Result{}, nil
	}
	return Stage2Result{Merged: true, JobGroupID: survivingGroupID}, nil
}

type stage2Candidate struct {
	row      db.SelectStage2CandidatesRow
	distance int
}

// candidateSimhash returns the candidate's stored simhash, or computes it
// on the fly from description_stripped for a job inserted before this
// phase existed (simhash NULL) — a one-time cost per such row, not a
// steady-state one, since InsertJob now always writes simhash going
// forward.
func candidateSimhash(c db.SelectStage2CandidatesRow) uint64 {
	if c.Simhash != nil {
		return uint64(*c.Simhash) //nolint:gosec // intentional bit-pattern round-trip, see simhashPtr's doc comment
	}
	if c.DescriptionStripped != nil {
		return SimHash(*c.DescriptionStripped)
	}
	return 0
}

// locationCompatible is docs/08 section 3.2 Gate 2, made symmetric — the
// doc's own formula reads one-directionally ("a.work_mode == 'remote' AND
// b.tier == a.tier") but which side is "new" vs "candidate" is arbitrary,
// so both directions are checked.
func locationCompatible(
	aCity *string, aTier *int16, aWorkMode schema.WorkMode,
	bCity *string, bTier *int16, bWorkMode schema.WorkMode,
) bool {
	if aCity != nil && bCity != nil && *aCity == *bCity {
		return true
	}
	if aWorkMode == schema.WorkRemote && bWorkMode == schema.WorkRemote {
		return true
	}
	if aWorkMode == schema.WorkRemote && aTier != nil && bTier != nil && *aTier == *bTier {
		return true
	}
	if bWorkMode == schema.WorkRemote && aTier != nil && bTier != nil && *aTier == *bTier {
		return true
	}
	if (aCity == nil) != (bCity == nil) {
		return true
	}
	return false
}

// mergeJobGroups implements docs/08 section 4's union-find merge: the
// older group always wins (preserves first_seen_at, which notification
// dedup and freshness scoring both key on), the newer group's jobs move
// over, and the representative is recomputed. Returns merged=false without
// changing anything if the group-size cap (maxGroupMemberCount) would be
// exceeded — docs/08 section 5's over-merge guard.
func mergeJobGroups(
	ctx context.Context, q db.Querier,
	newJobID, newGroupID, matchedJobID, matchedGroupID pgtype.UUID,
	stage string, certainty float32, signal map[string]any,
) (survivingGroupID pgtype.UUID, merged bool, err error) {
	newGroup, err := q.SelectJobGroupForMerge(ctx, newGroupID)
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("select new job_group: %w", err)
	}
	matchedGroup, err := q.SelectJobGroupForMerge(ctx, matchedGroupID)
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("select matched job_group: %w", err)
	}

	if newGroup.MemberCount+matchedGroup.MemberCount > maxGroupMemberCount {
		return pgtype.UUID{}, false, nil
	}

	keep, absorb := newGroup, matchedGroup
	if matchedGroup.FirstSeenAt.Time.Before(newGroup.FirstSeenAt.Time) {
		keep, absorb = matchedGroup, newGroup
	}

	if reassignErr := q.ReassignJobsToGroup(ctx, db.ReassignJobsToGroupParams{
		KeepGroupID: keep.ID, AbsorbGroupID: absorb.ID,
	}); reassignErr != nil {
		return pgtype.UUID{}, false, fmt.Errorf("reassign jobs: %w", reassignErr)
	}
	if updateErr := q.UpdateJobGroupAfterMerge(ctx, db.UpdateJobGroupAfterMergeParams{
		ID: keep.ID, AbsorbedMemberCount: absorb.MemberCount, AbsorbedFirstSeenAt: absorb.FirstSeenAt,
	}); updateErr != nil {
		return pgtype.UUID{}, false, fmt.Errorf("update kept group: %w", updateErr)
	}
	if deleteErr := q.DeleteJobGroup(ctx, absorb.ID); deleteErr != nil {
		return pgtype.UUID{}, false, fmt.Errorf("delete absorbed group: %w", deleteErr)
	}

	signalJSON, err := json.Marshal(signal)
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("marshal merge signal: %w", err)
	}
	if err := q.InsertJobMergeEvent(ctx, db.InsertJobMergeEventParams{
		JobID: newJobID, MatchedJobID: matchedJobID,
		FromGroupID: newGroupID, IntoGroupID: keep.ID,
		Stage: stage, Certainty: certainty, Signal: signalJSON,
	}); err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("insert merge event: %w", err)
	}

	if err := recomputeRepresentative(ctx, q, keep.ID); err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("recompute representative: %w", err)
	}

	return keep.ID, true, nil
}

// recomputeRepresentative implements docs/08 section 4's representative
// scoring. Every current source is a Tier 1/2 direct ATS integration (no
// email-alert or community source exists yet), so the +40/-20 terms for
// that signal are constants across every job scored here today — real
// differentiation currently comes from description length, structured
// compensation/location, posted_at reliability, and recency.
func recomputeRepresentative(ctx context.Context, q db.Querier, groupID pgtype.UUID) error {
	rows, err := q.SelectJobsForRepresentativeScoring(ctx, groupID)
	if err != nil {
		return fmt.Errorf("select jobs for representative scoring: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	var mostRecent pgtype.UUID
	var mostRecentAt pgtype.Timestamptz
	for _, r := range rows {
		if !mostRecentAt.Valid || r.LastSeenAt.Time.After(mostRecentAt.Time) {
			mostRecent = r.ID
			mostRecentAt = r.LastSeenAt
		}
	}

	var best pgtype.UUID
	var bestScore float64 = -1
	for _, r := range rows {
		score := representativeScore(r, r.ID == mostRecent)
		if score > bestScore {
			best = r.ID
			bestScore = score
		}
	}

	if err := q.SetJobGroupRepresentative(ctx, db.SetJobGroupRepresentativeParams{ID: groupID, JobID: best}); err != nil {
		return fmt.Errorf("set job group representative: %w", err)
	}
	return nil
}

func representativeScore(r db.SelectJobsForRepresentativeScoringRow, isMostRecent bool) float64 {
	score := 40.0 // every current source is a Tier 1/2 direct ATS integration
	if r.DescriptionLength >= 1000 {
		score += 25
	}
	if boolOrFalse(r.HasStructuredComp) {
		score += 15
	}
	if boolOrFalse(r.HasStructuredLocation) {
		score += 10
	}
	if boolOrFalse(r.HasReliablePostedAt) {
		score += 10
	}
	if isMostRecent {
		score += 5
	}
	return score
}

// boolOrFalse handles both SelectJobsForRepresentativeScoringRow's *bool
// fields and its one interface{} field (has_structured_location) — a
// sqlc/pgx codegen quirk for a computed boolean expression over a nullable
// column, the same class of thing packages/db's other codegen-quirk
// comments already document, not a deliberate schema choice.
func boolOrFalse(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case *bool:
		return b != nil && *b
	default:
		return false
	}
}
