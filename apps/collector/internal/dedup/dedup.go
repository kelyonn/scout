// Package dedup implements docs/08-dedup-identity.md's three-stage
// deduplication: Stage 1 exact-match (section 3.1, SCOUT-DEDUP-010,
// certainty 1.0), Stage 2 structural (section 3.2, stage2.go — SimHash,
// Jaro-Winkler title, location compatibility, certainty 0.95), and the
// Stage 2/Stage 3 handoff via the escalation this package returns for the
// caller to enqueue. Stage 3 itself (semantic, LLM adjudication) runs in
// apps/brain — Go and Python never call each other synchronously, so this
// package's only Stage 3 involvement is flagging a candidate pair.
package dedup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/schema"
)

// Result is dedup resolution's output: which job and job_group the posting
// belongs to, whether that job was just created, and (Stage 2 only) a
// candidate the caller should escalate to Stage 3.
type Result struct {
	JobID      pgtype.UUID
	JobGroupID pgtype.UUID
	IsNewJob   bool
	// Stage3CandidateJobID is set when Stage 2 found a plausible-but-
	// unconfirmed structural match (Gate 1+2 passed, Gate 3's SimHash
	// distance was 4-8 or >8) — see stage2.go's Stage2Result. The caller
	// (ingest.go) enqueues a brain_deep dedup_stage3 job against it.
	Stage3CandidateJobID *pgtype.UUID
}

// Resolve implements docs/08's full dedup flow for one posting. Stage 1
// (section 3.1) tries three exact-match keys in order: canonical_url_hash,
// then (ats_platform, ats_job_id), then content_hash. A match attaches to
// the existing job (last_seen_at and observation_count bumped, per
// docs/03 section 1's "jobs are derived, observations are immutable" —
// nothing about the existing row's content changes on a Stage 1 repeat).
//
// No Stage 1 match creates a new job_group and job — then Stage 2
// (section 3.2, stage2.go) runs against it before this call returns: it
// either merges that new group into an existing structurally-similar
// job's group (Gate 3 distance <= 3), or leaves it standing and flags a
// Stage 3 candidate for the caller to escalate asynchronously.
func Resolve(
	ctx context.Context, q db.Querier, normalized schema.NormalizedJob,
	companyID, sourceID pgtype.UUID, rawPayload []byte,
) (Result, error) {
	if existingID, existingGroupID, found, err := findExisting(ctx, q, normalized); err != nil {
		return Result{}, err
	} else if found {
		if err := q.TouchJob(ctx, existingID); err != nil {
			return Result{}, fmt.Errorf("dedup: touch job: %w", err)
		}
		if err := q.TouchJobGroup(ctx, existingGroupID); err != nil {
			return Result{}, fmt.Errorf("dedup: touch job_group: %w", err)
		}
		return Result{JobID: existingID, JobGroupID: existingGroupID, IsNewJob: false}, nil
	}

	group, err := q.InsertJobGroup(ctx, companyID)
	if err != nil {
		return Result{}, fmt.Errorf("dedup: insert job_group: %w", err)
	}

	descriptionStripped, simhash, err := stripAndHash(ctx, q, companyID, normalized.DescriptionText)
	if err != nil {
		return Result{}, fmt.Errorf("dedup: boilerplate strip: %w", err)
	}

	job, err := q.InsertJob(ctx, toInsertJobParams(normalized, group.ID, companyID, sourceID, rawPayload, descriptionStripped, simhash))
	if err != nil {
		return Result{}, fmt.Errorf("dedup: insert job: %w", err)
	}

	if repErr := q.SetJobGroupRepresentative(ctx, db.SetJobGroupRepresentativeParams{
		ID: group.ID, JobID: job.ID,
	}); repErr != nil {
		return Result{}, fmt.Errorf("dedup: set job_group representative: %w", repErr)
	}

	result := Result{JobID: job.ID, JobGroupID: group.ID, IsNewJob: true}

	var locationCity *string
	if normalized.LocationCity != "" {
		locationCity = &normalized.LocationCity
	}
	var locationTier *int16
	if normalized.LocationTier != 0 {
		locationTier = &normalized.LocationTier
	}
	stage2, err := Stage2Resolve(
		ctx, q, companyID, job.ID, group.ID,
		normalized.RoleFamily, normalized.Seniority, normalized.NormalizedTitle, simhash,
		locationCity, locationTier, normalized.WorkMode,
	)
	if err != nil {
		return Result{}, fmt.Errorf("dedup: stage2: %w", err)
	}
	if stage2.Merged {
		result.JobGroupID = stage2.JobGroupID
	}
	result.Stage3CandidateJobID = stage2.Stage3CandidateJobID

	return result, nil
}

// stripAndHash learns the company's boilerplate paragraph set from its
// recent postings (docs/08 section 3.3) and returns the new posting's
// stripped description plus its SimHash — both computed once here and
// reused by Stage 2 (candidateSimhash for other rows re-derives this same
// value from a stored description_stripped when needed) rather than
// recomputed.
func stripAndHash(ctx context.Context, q db.Querier, companyID pgtype.UUID, descriptionText string) (stripped string, simhash uint64, err error) {
	recent, err := q.SelectRecentDescriptionsForCompany(ctx, companyID)
	if err != nil {
		return "", 0, fmt.Errorf("select recent descriptions: %w", err)
	}
	descriptions := make([]string, 0, len(recent))
	for _, d := range recent {
		if d != nil {
			descriptions = append(descriptions, *d)
		}
	}
	learned := LearnBoilerplateHashes(descriptions)
	stripped = StripDescription(descriptionText, learned)
	return stripped, SimHash(stripped), nil
}

// findExisting tries the three Stage 1 exact-match keys in order, returning
// as soon as one hits. Each query returns its own sqlc-generated row type
// (not the shared db.Job) because an explicit column list — required to
// exclude the tsvector search_vector column pgx can't scan, see job.sql's
// header comment — makes every query's result shape distinct in sqlc's
// eyes even though the columns are identical; only ID and JobGroupID are
// pulled out here since that's all a match needs.
func findExisting(
	ctx context.Context, q db.Querier, normalized schema.NormalizedJob,
) (id, groupID pgtype.UUID, found bool, err error) {
	byURL, err := q.FindJobByCanonicalURLHash(ctx, normalized.CanonicalURLHash)
	switch {
	case err == nil:
		return byURL.ID, byURL.JobGroupID, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("dedup: find by canonical url hash: %w", err)
	}

	if normalized.ATSPlatform != "" && normalized.ATSJobID != "" {
		byATS, atsErr := q.FindJobByATSID(ctx, db.FindJobByATSIDParams{
			AtsPlatform: normalized.ATSPlatform,
			AtsJobID:    normalized.ATSJobID,
		})
		switch {
		case atsErr == nil:
			return byATS.ID, byATS.JobGroupID, true, nil
		case !errors.Is(atsErr, pgx.ErrNoRows):
			return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("dedup: find by ats id: %w", atsErr)
		}
	}

	byContent, err := q.FindJobByContentHash(ctx, normalized.ContentHash)
	switch {
	case err == nil:
		return byContent.ID, byContent.JobGroupID, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("dedup: find by content hash: %w", err)
	}

	return pgtype.UUID{}, pgtype.UUID{}, false, nil
}

func toInsertJobParams(
	n schema.NormalizedJob, groupID, companyID, sourceID pgtype.UUID, rawPayload []byte,
	descriptionStripped string, simhash uint64,
) db.InsertJobParams {
	return db.InsertJobParams{
		JobGroupID:             groupID,
		CompanyID:              companyID,
		PrimarySourceID:        sourceID,
		CanonicalUrl:           n.CanonicalURL,
		CanonicalUrlHash:       n.CanonicalURLHash,
		AtsPlatform:            nilIfEmpty(n.ATSPlatform),
		AtsJobID:               nilIfEmpty(n.ATSJobID),
		ContentHash:            n.ContentHash,
		Title:                  n.Title,
		NormalizedTitle:        n.NormalizedTitle,
		DescriptionHtml:        nilIfEmpty(n.DescriptionHTML),
		DescriptionText:        nilIfEmpty(n.DescriptionText),
		DescriptionStripped:    nilIfEmpty(descriptionStripped),
		Simhash:                simhashPtr(simhash),
		RequirementsText:       nilIfEmpty(n.RequirementsText),
		ApplyUrl:               n.ApplyURL,
		RoleFamily:             db.RoleFamily(n.RoleFamily),
		RoleConfidence:         n.RoleConfidence,
		Seniority:              db.Seniority(n.Seniority),
		IsSoftware:             n.IsSoftware,
		Skills:                 emptyIfNil(n.Skills),
		LocationRaw:            nilIfEmpty(n.LocationRaw),
		LocationCity:           nilIfEmpty(n.LocationCity),
		LocationRegion:         nilIfEmpty(n.LocationRegion),
		LocationCountry:        interfaceIfNonEmpty(n.LocationCountry),
		LocationTier:           tierPtr(n.LocationTier),
		WorkMode:               db.WorkMode(n.WorkMode),
		VisaSponsorship:        n.VisaSponsorship,
		Paid:                   db.PaidSignal(n.Paid),
		CompMin:                numericFromFloat(n.CompMin),
		CompMax:                numericFromFloat(n.CompMax),
		CompCurrency:           interfaceIfNonEmpty(n.CompCurrency),
		CompPeriod:             nilIfEmpty(n.CompPeriod),
		CompNormalizedInrMonth: numericFromFloat(n.CompNormalizedINRMonth),
		CompConfidence:         n.CompConfidence,
		PostedAt:               toTimestamptz(n.PostedAt),
		PostedAtEstimated:      n.PostedAtEstimated,
		DeadlineAt:             toTimestamptz(n.DeadlineAt),
		RawPayload:             rawPayload,
	}
}

// emptyIfNil turns a nil skills slice into an empty one — pgx sends a nil
// Go slice as SQL NULL, which the NOT NULL DEFAULT '{}' column rejects
// outright since an explicit value (even NULL) bypasses the column default.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// interfaceIfNonEmpty exists because sqlc generates `interface{}` params for
// nullable bpchar columns (location_country, comp_currency) rather than
// *string — a codegen quirk of the pgx/v5 driver for that type, not a
// deliberate schema choice. A bare string (or nil) is what pgx expects to
// encode into a CHAR(n) column here.
func interfaceIfNonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func tierPtr(tier int16) *int16 {
	if tier == 0 {
		return nil
	}
	return &tier
}

// simhashPtr reinterprets a uint64 SimHash as job.simhash's signed BIGINT
// column — the bit pattern round-trips exactly through this conversion in
// both directions (candidateSimhash converts back with uint64(*int64)),
// which is all XOR/popcount-based Hamming distance needs; the sign bit
// carries no meaning here.
func simhashPtr(h uint64) *int64 {
	v := int64(h) //nolint:gosec // intentional bit-pattern round-trip, see doc comment above
	return &v
}

func toTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func numericFromFloat(v *float64) pgtype.Numeric {
	if v == nil {
		return pgtype.Numeric{}
	}
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(*v, 'f', -1, 64)); err != nil {
		return pgtype.Numeric{}
	}
	return n
}
