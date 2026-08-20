package dedup

import (
	"context"
	"fmt"
	"testing"
	"time"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/schema"
)

// testPostingFull extends the base package's testPosting with the fields
// Stage 2 actually reads: description text (Gate 3 input) and a shared
// location (Gate 2 needs a city match, remote+tier match, or exactly one
// side omitted — two postings with both cities nil satisfies none of
// docs/08's four listed Gate 2 clauses literally, so tests that want Gate 2
// to pass set a matching city explicitly rather than relying on both being
// empty).
func testPostingFull(canonicalURL, title, normalizedTitle, description string) schema.NormalizedJob {
	p := testPosting(canonicalURL)
	p.Title = title
	p.NormalizedTitle = normalizedTitle
	p.DescriptionText = description
	p.LocationCity = "Bengaluru"
	p.LocationTier = 1
	// SelectStage2Candidates filters posted_at > now() - 45 days — a NULL
	// posted_at (the zero value here) never satisfies that, silently
	// excluding the row from candidate generation regardless of how
	// similar it is. Real postings almost always have at least an
	// estimated posted_at by the time they reach dedup, so this is a
	// realistic fixture, not a workaround for a fixture-only gap.
	now := time.Now()
	p.PostedAt = &now
	return p
}

const sharedJobDescription = `We are hiring a Software Engineering Intern for our backend team this summer.

You will work directly with senior engineers on real production systems, writing Go and Python code that serves millions of requests per day. You'll participate in code reviews, on-call rotations, and design discussions alongside the rest of the team.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.`

const unrelatedJobDescription = `We are hiring a Backend Engineer Intern to help us build our new payments ledger from scratch.

This is a foundational systems project: you'll design the schema, implement double-entry accounting invariants, and work closely with finance on reconciliation. Deep interest in distributed systems and correctness is a must.

Acme Corp is an equal opportunity employer and does not discriminate based on race, color, religion, sex, or national origin.`

// TestStage2Resolve_MergesStructurallySimilarPosting is docs/08 section
// 3.2's core claim: two postings with similar titles, compatible location,
// and near-identical descriptions (Gate 1+2+3 all pass, Gate 3 <= 3) merge
// into one job_group even though Stage 1's exact-match found nothing (two
// different canonical URLs, two different content hashes).
func TestStage2Resolve_MergesStructurallySimilarPosting(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	companyID := insertTestCompany(t, tx)
	sourceID := insertTestSource(t, tx, companyID)

	base := time.Now().UnixNano()
	first := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-a", base),
		"Software Engineering Intern", "software engineering intern",
		sharedJobDescription,
	)
	second := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-b", base),
		"Software Engineering Intern - Summer 2027", "software engineering intern - summer 2027",
		sharedJobDescription,
	)

	firstResult, err := Resolve(ctx, q, first, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	secondResult, err := Resolve(ctx, q, second, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	if !secondResult.IsNewJob {
		t.Error("second posting is a genuinely different job row, IsNewJob should be true")
	}
	if secondResult.JobID == firstResult.JobID {
		t.Error("the two postings are different job rows even though they share a job_group")
	}
	// firstResult.JobGroupID is a snapshot from *before* the second
	// Resolve's merge ran — Stage 2 reassigns the first job's row to the
	// surviving group afterward, but that struct value was already
	// returned and does not retroactively update. secondResult.JobGroupID
	// is authoritative (it's read after the merge completed), so the
	// database is what actually confirms both jobs ended up together.
	firstJob, err := q.GetJobByID(ctx, firstResult.JobID)
	if err != nil {
		t.Fatalf("GetJobByID(first): %v", err)
	}
	if firstJob.JobGroupID != secondResult.JobGroupID {
		t.Errorf("first job's current job_group_id (%v) should equal the surviving group from Stage 2's merge (%v)",
			firstJob.JobGroupID, secondResult.JobGroupID)
	}

	group, err := q.SelectJobGroupForMerge(ctx, secondResult.JobGroupID)
	if err != nil {
		t.Fatalf("SelectJobGroupForMerge: %v", err)
	}
	if group.MemberCount != 2 {
		t.Errorf("merged group member_count = %d, want 2", group.MemberCount)
	}
}

// TestStage2Resolve_DissimilarDescriptionEscalatesToStage3 checks the
// other branch: same title pattern and location (Gate 1+2 pass), but a
// substantively different description (Gate 3 distance > 3) — no merge,
// but the candidate is flagged for Stage 3.
func TestStage2Resolve_DissimilarDescriptionEscalatesToStage3(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	companyID := insertTestCompany(t, tx)
	sourceID := insertTestSource(t, tx, companyID)

	base := time.Now().UnixNano()
	first := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-a", base),
		"Backend Engineer Intern", "backend engineer intern",
		sharedJobDescription,
	)
	second := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-b", base),
		"Backend Engineer Intern", "backend engineer intern",
		unrelatedJobDescription,
	)

	firstResult, err := Resolve(ctx, q, first, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	secondResult, err := Resolve(ctx, q, second, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	if secondResult.JobGroupID == firstResult.JobGroupID {
		t.Error("dissimilar-description postings should not be auto-merged by Stage 2")
	}
	if secondResult.Stage3CandidateJobID == nil {
		t.Fatal("expected Stage3CandidateJobID to be set for a Gate1+2-pass, Gate3-inconclusive pair")
	}
	if *secondResult.Stage3CandidateJobID != firstResult.JobID {
		t.Errorf("Stage3CandidateJobID = %v, want the first posting's job id %v",
			*secondResult.Stage3CandidateJobID, firstResult.JobID)
	}
}

// TestStage2Resolve_DifferentRoleFamilyNeverMerges is docs/08 section 5's
// over-merge guard: "Cross-role-family: Never merged automatically."
// SelectStage2Candidates filters by role_family in its WHERE clause, so a
// different-family posting should never even appear as a candidate.
func TestStage2Resolve_DifferentRoleFamilyNeverMerges(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	companyID := insertTestCompany(t, tx)
	sourceID := insertTestSource(t, tx, companyID)

	base := time.Now().UnixNano()
	first := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-a", base),
		"Software Engineering Intern", "software engineering intern",
		sharedJobDescription,
	)
	first.RoleFamily = schema.RoleSWEBackend

	second := testPostingFull(
		fmt.Sprintf("https://example.test/job/%d-b", base),
		"Software Engineering Intern", "software engineering intern",
		sharedJobDescription,
	)
	second.RoleFamily = schema.RoleSWEFrontend

	firstResult, err := Resolve(ctx, q, first, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	secondResult, err := Resolve(ctx, q, second, companyID, sourceID, []byte(`{}`))
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	if secondResult.JobGroupID == firstResult.JobGroupID {
		t.Error("different role_family postings must never be merged, even with identical title/description")
	}
	if secondResult.Stage3CandidateJobID != nil {
		t.Error("different role_family should not even surface as a Stage 3 candidate — filtered at the SQL level")
	}
}
