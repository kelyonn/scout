// Ingestion: the part of a poll cycle docs/06-ingestion-pipeline.md sections
// 8-9 describe. normalize/classify Tier 0/dedup Stage 1/score stay wired
// directly into pollOne rather than through River — all deterministic,
// sub-millisecond Go, and moving already-fast code onto a queue for its own
// sake adds nothing. The one genuinely Python-bound step, embedding
// (packages/queue, apps/brain), goes through River: see processPosting's
// EnqueueEmbed call.
package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/classify"
	"github.com/kelyon/scout/apps/collector/internal/dedup"
	"github.com/kelyon/scout/apps/collector/internal/normalize"
	"github.com/kelyon/scout/apps/collector/internal/scoring"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
	"github.com/kelyon/scout/packages/metrics"
	"github.com/kelyon/scout/packages/queue"
	"github.com/kelyon/scout/packages/realtime"
	"github.com/kelyon/scout/packages/schema"
	"github.com/kelyon/scout/packages/skills"
	"github.com/kelyon/scout/packages/taxonomy"
)

// Pipeline bundles what normalize/classify/dedup/score need beyond what
// Scheduler already carries. A nil Pipeline (the zero value of Scheduler
// before WithPipeline is called) means "not configured" — pollOne then
// behaves exactly as it did before this phase: fetch and change-detect the
// source row, write nothing resembling a job. Same coarsening-is-safe
// posture as the rest of this package.
type Pipeline struct {
	Adapters  map[padapter.SourceKind]padapter.Adapter
	Gazetteer *taxonomy.Gazetteer
	Roles     []taxonomy.RoleFamily
	Skills    []taxonomy.Skill
}

// WithPipeline enables ingestion. Without it, this package's behavior is
// unchanged from before P1's adapters landed.
func (s *Scheduler) WithPipeline(p *Pipeline) *Scheduler { s.pipeline = p; return s }

// scoringContext is loaded once per ingestion call (see the comment at its
// call site for why re-querying per poll is fine at P1's scale) rather than
// cached across polls — the single user's profile and the active
// weight_version both change rarely enough that staleness isn't a real risk
// either way, so the simpler "always read fresh" option wins. resumeText and
// companies are likewise loaded once per call rather than per posting:
// resumeText changes only when the user updates their resume (make
// seed-resume), and companies is an embedded taxonomy file, not a query.
type scoringContext struct {
	userID        pgtype.UUID
	profile       scoring.UserProfile
	weights       scoring.Weights
	weightVersion string
	resumeText    string // "" if no resume row exists yet — resume_match's callers treat that as absent
	companies     map[string]taxonomy.Company
}

// loadScoringContext reads the sole user's profile and the active
// weight_version — ADR-015's single-user system, so "the user" needs no
// parameter.
func (s *Scheduler) loadScoringContext(ctx context.Context) (*scoringContext, error) {
	q := db.New(s.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get sole user: %w", err)
	}
	profile, err := q.GetUserProfile(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("get user profile: %w", err)
	}
	wv, err := q.GetActiveWeightVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active weight_version: %w", err)
	}
	weights, err := scoring.ParseWeights(wv.Weights)
	if err != nil {
		return nil, fmt.Errorf("parse active weight_version: %w", err)
	}

	targetRoles := make([]schema.RoleFamily, len(profile.TargetRoles))
	for i, r := range profile.TargetRoles {
		targetRoles[i] = schema.RoleFamily(r)
	}
	targetSeniority := make([]schema.Seniority, len(profile.TargetSeniority))
	for i, sv := range profile.TargetSeniority {
		targetSeniority[i] = schema.Seniority(sv)
	}
	var skillLevels map[string]int
	if len(profile.SkillLevels) > 0 {
		if unmarshalErr := json.Unmarshal(profile.SkillLevels, &skillLevels); unmarshalErr != nil {
			return nil, fmt.Errorf("parse user_profile.skill_levels: %w", unmarshalErr)
		}
	}

	// pgx.ErrNoRows here just means `make seed-resume` hasn't run yet — a
	// real, legitimate state (P1 never required a resume row at all), not
	// an ingestion failure. resume_match's own computeResumeMatch treats
	// an empty resumeText as "no keyword matches," the correct degraded
	// behavior for it.
	resumeText, err := q.GetResumeRawText(ctx, user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get resume raw_text: %w", err)
	}

	return &scoringContext{
		userID: user.ID,
		profile: scoring.UserProfile{
			TargetRoles:     targetRoles,
			TargetSeniority: targetSeniority,
			SkillLevels:     skillLevels,
		},
		weights:       weights,
		weightVersion: wv.Version,
		resumeText:    resumeText,
		companies:     taxonomy.LoadCompanies(),
	}, nil
}

// runIngestion is docs/06 sections 8-9 for one poll cycle: Parse, Validate,
// then normalize -> classify -> dedup -> score -> write for every posting,
// all in one transaction. Returns how many postings were parsed and how
// many turned out to be new jobs, for UpdateSourceAfterPoll's counters.
// Every failure path logs and returns zeros rather than propagating an
// error — ingestion failing must never stop pollOne from still recording
// the poll's own outcome (fetch succeeded, content changed, next interval),
// the same reasoning pollOne's own doc comment gives for not returning
// errors at all.
func (s *Scheduler) runIngestion(ctx context.Context, row db.SelectDueSourcesRow, result *fetch.Result) (jobsFound, newJobs int64) {
	if s.pipeline == nil {
		return 0, 0
	}
	ad, ok := s.pipeline.Adapters[padapter.SourceKind(row.Kind)]
	if !ok {
		// No adapter for this source_kind yet — every non-Greenhouse kind
		// today. Not an error: the source is still change-detected and
		// scheduled correctly, it just has nothing downstream of Layer 2
		// until its adapter lands.
		return 0, 0
	}
	if !row.CompanyID.Valid {
		s.log.Warn("skipping ingestion: source has no company_id", "source_id", row.ID.String())
		return 0, 0
	}

	src, err := toAdapterSource(row)
	if err != nil {
		s.log.Warn("skipping ingestion: could not build adapter source", "source_id", row.ID.String(), "err", err)
		return 0, 0
	}
	raw := padapter.NewRawResponse(result)

	postings, err := ad.Parse(ctx, src, raw)
	if err != nil {
		s.recordParseError(ctx, row.ID, row.Url, result, err)
		return 0, 0
	}

	if validateErr := ad.Validate(ctx, src, postings); validateErr != nil {
		// docs/06 section 10: "Validation failure: Suspected breakage. Store
		// raw, alert, keep the previous jobs live." Keeping previous jobs
		// live means not writing anything this round, not attempting to
		// salvage individual postings from a response Validate has already
		// judged implausible.
		s.log.Error("adapter validation failed; skipping this poll's postings", "source_id", row.ID.String(), "err", validateErr)
		return 0, 0
	}
	if len(postings) == 0 {
		return 0, 0
	}

	scoreCtx, err := s.loadScoringContext(ctx)
	if err != nil {
		s.log.Warn("skip scoring this poll: could not load scoring context", "source_id", row.ID.String(), "err", err)
		return 0, 0
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.log.Warn("ingestion: begin tx failed", "source_id", row.ID.String(), "err", err)
		return 0, 0
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now()
	for _, posting := range postings {
		isNew, ok := s.processPosting(ctx, tx, row, posting, scoreCtx, result.StatusCode, now)
		if !ok {
			continue
		}
		jobsFound++
		if isNew {
			newJobs++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		s.log.Warn("ingestion: commit tx failed", "source_id", row.ID.String(), "err", err)
		return 0, 0
	}

	return jobsFound, newJobs
}

// processPosting runs one posting's normalize -> classify -> dedup -> score
// -> write inside its own savepoint — a pgx nested transaction, opened via
// Begin on the poll's already-open tx — so a failure specific to this
// posting rolls back only its own writes rather than the whole poll.
//
// This matters concretely: raw_observation's (source_id, content_hash)
// unique index lives on each partition individually, not as a partitioned
// index on the parent (docs/03 section 10's own comment explains why — a
// parent-level constraint would have to include the partition key, which
// guarantees nothing). That means InsertObservation cannot use ON CONFLICT
// against it (Postgres has no arbiter index to validate against when the
// insert targets the parent table), so a genuine duplicate — found live
// against a real Greenhouse board during P1 verification, where a posting
// was re-observed with unchanged content — is a real constraint-violation
// error. Without a savepoint, that error poisons every statement after it
// in the shared per-poll transaction until rollback, silently dropping the
// rest of that poll's postings. With one, only this posting's job_score
// and observation writes roll back; the job and job_group rows dedup
// already resolved are unaffected since Stage 1 dedup only ever attaches
// to or creates them, never inside this posting's own savepoint scope in a
// way that a later rollback here could orphan.
func (s *Scheduler) processPosting(
	ctx context.Context, tx pgx.Tx, row db.SelectDueSourcesRow, posting schema.Posting,
	scoreCtx *scoringContext, statusCode int, now time.Time,
) (isNewJob, ok bool) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		s.log.Warn("posting: begin savepoint failed", "source_id", row.ID.String(), "err", err)
		return false, false
	}
	committed := false
	defer func() {
		if !committed {
			_ = savepoint.Rollback(ctx)
		}
	}()
	q := db.New(savepoint)

	normalized, needsTier2RoleEscalation, err := s.buildNormalizedJob(posting)
	if err != nil {
		s.log.Warn("skip posting: normalize failed", "source_id", row.ID.String(), "url", posting.URL, "err", err)
		return false, false
	}

	payload, err := json.Marshal(posting)
	if err != nil {
		s.log.Warn("skip posting: marshal payload failed", "source_id", row.ID.String(), "err", err)
		return false, false
	}

	dedupResult, err := dedup.Resolve(ctx, q, normalized, row.CompanyID, row.ID, payload)
	if err != nil {
		s.log.Warn("skip posting: dedup failed", "source_id", row.ID.String(), "url", posting.URL, "err", err)
		return false, false
	}

	ext, err := s.buildExternalInputs(ctx, q, scoreCtx, row.CompanyID, dedupResult.JobID, normalized)
	if err != nil {
		s.log.Warn("posting: build external scoring inputs failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
		return false, false
	}

	scoreResult := scoring.Compute(normalized, scoreCtx.profile, scoreCtx.weights, ext, now)
	scoreParams := toInsertJobScoreParams(dedupResult.JobID, scoreCtx.userID, scoreCtx.weightVersion, normalized, scoreResult)
	if _, err := q.InsertJobScore(ctx, scoreParams); err != nil {
		s.log.Warn("posting: job_score insert failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
		return false, false
	}

	obsParams := toInsertObservationParams(row.ID, posting, normalized, dedupResult.JobID, statusCode)
	if _, err := q.InsertObservation(ctx, obsParams); err != nil {
		s.log.Warn("posting: raw_observation insert failed (likely a genuine duplicate content_hash)", "source_id", row.ID.String(), "err", err)
		return false, false
	}

	// docs/04-api-design.md section 4.3's SSE stream — a connected client
	// (the Opportunities page, open in a browser) learns about a genuinely
	// new job within the same commit that created it, no polling. Only for
	// a new job: an existing job re-observed unchanged isn't "new" to
	// anyone already watching. Not fatal to the posting on failure — a
	// missed live-update is a UX gap, not a data-correctness one.
	if dedupResult.IsNewJob {
		if err := realtime.NotifyJobNew(ctx, savepoint, realtime.JobNew{
			JobGroupID: dedupResult.JobGroupID.String(), Priority: scoreResult.Priority, Title: normalized.Title,
		}); err != nil {
			s.log.Warn("posting: notify job.new failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
		}
	}

	// Only a genuinely new job needs a fresh embedding — an existing job
	// Stage 1 dedup just re-attached an observation to already has one (or
	// has one in flight), and re-embedding identical content on every
	// re-observation would be pure waste. Enqueued inside this posting's
	// own savepoint: the embed job exists if and only if this posting's
	// writes actually commit, per ADR-003's whole point.
	if s.queue != nil && dedupResult.IsNewJob {
		if err := s.queue.EnqueueEmbed(ctx, savepoint, dedupResult.JobID.String()); err != nil {
			s.log.Warn("posting: enqueue embed failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
			return false, false
		}
	}

	// Dedup Stage 2 found a plausible-but-unconfirmed structural match
	// (docs/08 section 3.2's Gate 1+2 passed, Gate 3 inconclusive) —
	// apps/brain's Stage 3 consumer (P2 Phase F) checks it semantically
	// once this job's own embed job above has run.
	if s.queue != nil && dedupResult.Stage3CandidateJobID != nil {
		candidateID := dedupResult.Stage3CandidateJobID.String()
		if err := s.queue.EnqueueDedupStage3(ctx, savepoint, dedupResult.JobID.String(), candidateID); err != nil {
			s.log.Warn("posting: enqueue dedup stage3 failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
			return false, false
		}
	}

	// Tier 0 hit an advocacy.* title whose description failed the
	// technical-evidence gate — classify.RoleResult.NeedsTier2Escalation's
	// own comment explains why this gets a second, smarter look rather
	// than being silently left at swe.other. Only for a genuinely new job:
	// an existing job's classification isn't re-decided on every
	// re-observation, same reasoning as the embed skip above.
	if s.queue != nil && dedupResult.IsNewJob && needsTier2RoleEscalation {
		if err := s.queue.EnqueueBrainDeep(ctx, savepoint, dedupResult.JobID.String(), queue.TaskClassifyTier2); err != nil {
			s.log.Warn("posting: enqueue classify tier2 failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
			return false, false
		}
	}

	// Upgrades scoreParams.Explanation's synchronous template to a
	// personalized, LLM-generated one (docs/09 section 6/7). Only for a
	// genuinely new job, same reasoning as the embed/classify enqueues
	// above — an existing job's job_score row is replaced on conflict on
	// every re-observation, and re-explaining identical content on each
	// one would be pure waste for a notification that already went out
	// with the template. A rescore under a new weight_version starts a
	// fresh row back at the template with no explain job of its own; that
	// is an accepted, documented gap (see HANDOFF.md), not an oversight.
	if s.queue != nil && dedupResult.IsNewJob {
		if err := s.queue.EnqueueBrainDeep(ctx, savepoint, dedupResult.JobID.String(), queue.TaskExplain); err != nil {
			s.log.Warn("posting: enqueue explain failed", "source_id", row.ID.String(), "job_id", dedupResult.JobID.String(), "err", err)
			return false, false
		}
	}

	if err := savepoint.Commit(ctx); err != nil {
		s.log.Warn("posting: commit savepoint failed", "source_id", row.ID.String(), "err", err)
		return false, false
	}
	committed = true

	if dedupResult.IsNewJob {
		// docs/16-observability.md section 3.1's aggregate discovery-rate
		// signal — incremented only after the commit above actually
		// succeeds, so this counter never counts a job that a later crash
		// or error rolled back.
		metrics.JobsDiscoveredTotal.WithLabelValues(
			string(normalized.RoleFamily), metrics.LocationTierLabel(int(normalized.LocationTier)), string(row.Kind),
		).Inc()
	}

	return dedupResult.IsNewJob, true
}

// buildExternalInputs gathers everything scoring.Compute needs that isn't
// already on schema.NormalizedJob/scoring.UserProfile — see
// scoring.ExternalInputs' own comment for why this I/O lives here rather
// than inside the (deliberately pure) scoring package.
func (s *Scheduler) buildExternalInputs(
	ctx context.Context, q db.Querier, scoreCtx *scoringContext,
	companyID, jobID pgtype.UUID, normalized schema.NormalizedJob,
) (scoring.ExternalInputs, error) {
	ext := scoring.ExternalInputs{
		SkillOntology: s.pipeline.Skills,
		ResumeText:    scoreCtx.resumeText,
	}

	cosine, err := q.SelectResumeJobCosine(ctx, db.SelectResumeJobCosineParams{UserID: scoreCtx.userID, JobID: jobID})
	switch {
	case err == nil:
		ext.HasResumeCosine = true
		ext.ResumeCosine = cosine
	case errors.Is(err, pgx.ErrNoRows):
		// Job or resume embedding doesn't exist yet (the embed queue job
		// hasn't run, or `make seed-resume` was never called) — the
		// documented "unknown" state resume_match's own formula handles,
		// not an ingestion error.
	default:
		return scoring.ExternalInputs{}, fmt.Errorf("select resume/job cosine: %w", err)
	}

	if normalized.CompNormalizedINRMonth != nil {
		thisComp := numericFromFloat(normalized.CompNormalizedINRMonth)
		exact, exactErr := q.SelectCompensationPercentileExact(ctx, db.SelectCompensationPercentileExactParams{
			ThisComp: thisComp, RoleFamily: db.RoleFamily(normalized.RoleFamily),
			Seniority: db.Seniority(normalized.Seniority), LocationCountry: interfaceIfNonEmpty(normalized.LocationCountry),
		})
		if exactErr != nil {
			return scoring.ExternalInputs{}, fmt.Errorf("select compensation percentile (exact): %w", exactErr)
		}
		count, atOrBelow := int(exact.ComparableCount), int(exact.AtOrBelowCount)
		if count < compensationMinComparablesForIngest {
			broad, broadErr := q.SelectCompensationPercentileBroad(ctx, db.SelectCompensationPercentileBroadParams{
				ThisComp: thisComp, Seniority: db.Seniority(normalized.Seniority),
				LocationCountry: interfaceIfNonEmpty(normalized.LocationCountry),
			})
			if broadErr != nil {
				return scoring.ExternalInputs{}, fmt.Errorf("select compensation percentile (broad): %w", broadErr)
			}
			count, atOrBelow = int(broad.ComparableCount), int(broad.AtOrBelowCount)
		}
		ext.JobHasCompensation = true
		ext.CompensationComparableCount = count
		ext.CompensationAtOrBelowCount = atOrBelow
	}

	slug, err := q.GetCompanySlug(ctx, companyID)
	switch {
	case err == nil:
		if c, ok := scoreCtx.companies[slug]; ok {
			wellKnown := c.WellKnown
			ext.CompanyWellKnown = &wellKnown
		}
	case errors.Is(err, pgx.ErrNoRows):
		// Company row was deleted between dedup and here — vanishingly
		// unlikely within one savepoint, and competition_estimate's own
		// nil-CompanyWellKnown handling covers it either way.
	default:
		return scoring.ExternalInputs{}, fmt.Errorf("get company slug: %w", err)
	}

	return ext, nil
}

// compensationMinComparablesForIngest mirrors
// apps/collector/internal/scoring's own compensationMinComparables (not
// exported — this package only needs the threshold to decide whether to
// try the broader query, not the full scoring logic behind it).
const compensationMinComparablesForIngest = 20

// numericFromFloat and interfaceIfNonEmpty mirror
// apps/collector/internal/dedup's own unexported helpers of the same name
// (that package's own comments explain the pgtype.Numeric conversion and
// the bpchar codegen quirk in full) — small enough, and used by different
// packages for different sqlc params, that duplicating them here is
// simpler than exporting cross-package plumbing for two one-line helpers.
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

func interfaceIfNonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// buildNormalizedJob runs one posting through URL canonicalization, title
// normalization, location tiering, compensation parsing, Tier 0
// classification, and skill extraction — docs/07-normalization-taxonomy.md
// sections 3-8. needsTier2RoleEscalation is true when Tier 0 hit an
// advocacy.* title whose description failed the technical-evidence gate —
// see classify.RoleResult.NeedsTier2Escalation's own comment for why the
// caller (processPosting) enqueues a Tier 2 job for it rather than treating
// it as a plain unresolved title.
func (s *Scheduler) buildNormalizedJob(p schema.Posting) (nj schema.NormalizedJob, needsTier2RoleEscalation bool, err error) {
	canonicalURL, urlHash, err := normalize.CanonicalizeURL(p.URL)
	if err != nil {
		return schema.NormalizedJob{}, false, fmt.Errorf("canonicalize url: %w", err)
	}

	titleNorm := normalize.NormalizeTitle(p.TitleRaw)
	loc := normalize.NormalizeLocation(p.LocationRaw, p.RemoteHint, s.pipeline.Gazetteer)

	descriptionText := p.DescriptionText
	if descriptionText == "" && p.DescriptionHTML != "" {
		descriptionText = normalize.StripHTML(p.DescriptionHTML)
	}
	requirementsText := p.RequirementsText
	if requirementsText == "" {
		requirementsText = descriptionText
	}

	compText := p.CompensationRawText
	if compText == "" {
		compText = descriptionText
	}
	comp := normalize.ParseCompensation(compText)

	role := classify.ClassifyRole(titleNorm.Normalized, descriptionText, s.pipeline.Roles, s.pipeline.Skills)
	seniority := classify.ResolveSeniority(p.TitleRaw, requirementsText, p.EmploymentTypeRaw)
	extractedSkills := skills.Extract(p.TitleRaw+" "+descriptionText, s.pipeline.Skills)

	content := contentHash(canonicalURL, titleNorm.Normalized, descriptionText)

	applyURL := p.ApplyURL
	if applyURL == "" {
		applyURL = p.URL
	}

	nj = schema.NormalizedJob{
		CanonicalURL:           canonicalURL,
		CanonicalURLHash:       urlHash[:],
		ATSPlatform:            p.Adapter,
		ATSJobID:               p.ExternalID,
		ContentHash:            content,
		Title:                  p.TitleRaw,
		NormalizedTitle:        titleNorm.Normalized,
		DescriptionHTML:        p.DescriptionHTML,
		DescriptionText:        descriptionText,
		RequirementsText:       requirementsText,
		ApplyURL:               applyURL,
		RoleFamily:             role.Family,
		RoleConfidence:         role.Confidence,
		Seniority:              seniority,
		IsSoftware:             role.IsSoftware,
		Skills:                 extractedSkills,
		LocationRaw:            p.LocationRaw,
		LocationCity:           loc.City,
		LocationRegion:         loc.Region,
		LocationCountry:        loc.Country,
		LocationTier:           loc.Tier,
		WorkMode:               loc.WorkMode,
		VisaSponsorship:        normalize.DetectVisaSponsorship(descriptionText),
		Paid:                   comp.Paid,
		CompMin:                comp.Min,
		CompMax:                comp.Max,
		CompCurrency:           comp.Currency,
		CompPeriod:             comp.Period,
		CompNormalizedINRMonth: comp.NormalizedINRMonth,
		CompConfidence:         compConfidencePtr(comp.Confidence),
		PostedAt:               p.PostedAt,
		PostedAtEstimated:      p.PostedAtEstimated,
		DeadlineAt:             p.DeadlineAt,
	}
	return nj, role.NeedsTier2Escalation, nil
}

func compConfidencePtr(v float32) *float32 {
	if v == 0 {
		return nil
	}
	return &v
}

// contentHash is Stage 1 dedup's third exact-match key (docs/08 section
// 3.1) — a fingerprint of the parts of a posting that identify it
// independent of URL, so a re-posted job under a new URL with identical
// content still resolves to the same job on this key.
func contentHash(canonicalURL, normalizedTitle, descriptionText string) []byte {
	sum := sha256.Sum256([]byte(canonicalURL + "|" + normalizedTitle + "|" + descriptionText))
	return sum[:]
}

func toInsertJobScoreParams(jobID, userID pgtype.UUID, weightVersion string, job schema.NormalizedJob, r scoring.Result) db.InsertJobScoreParams {
	// r.ScoreInputs is built entirely from marshalable primitives, so the
	// error is always nil in practice.
	inputs, _ := json.Marshal(r.ScoreInputs)
	return db.InsertJobScoreParams{
		JobID:                jobID,
		UserID:               userID,
		WeightVersion:        weightVersion,
		OverallMatch:         r.OverallMatch,
		SkillMatch:           r.SkillMatch,
		ResumeMatch:          r.ResumeMatch,
		CompanyQuality:       r.CompanyQuality,
		Compensation:         r.Compensation,
		LearningOpportunity:  r.LearningOpportunity,
		EngineeringCulture:   r.EngineeringCulture,
		GrowthPotential:      r.GrowthPotential,
		InterviewProbability: r.InterviewProbability,
		CompetitionEstimate:  r.CompetitionEstimate,
		EaseOfApplying:       r.EaseOfApplying,
		DeadlineUrgency:      r.DeadlineUrgency,
		Priority:             r.Priority,
		LocationMultiplier:   r.LocationMultiplier,
		FreshnessMultiplier:  r.FreshnessMultiplier,
		// docs/09 section 6/7: written synchronously so a notification is
		// never sent unexplained; packages/queue's TaskExplain upgrades it
		// to an LLM-generated explanation asynchronously.
		Explanation:      scoring.Explain(job, r),
		ExplanationModel: scoring.ExplanationModelTemplate,
		ScoreInputs:      inputs,
	}
}

func toInsertObservationParams(
	sourceID pgtype.UUID, p schema.Posting, n schema.NormalizedJob, jobID pgtype.UUID, statusCode int,
) db.InsertObservationParams {
	// schema.Posting is built entirely from marshalable primitives, so the
	// error is always nil in practice.
	payload, _ := json.Marshal(p)
	status := int16(statusCode) //nolint:gosec // HTTP status codes fit comfortably in int16

	return db.InsertObservationParams{
		SourceID:         sourceID,
		ExternalID:       nilIfEmptyStr(p.ExternalID),
		Url:              p.URL,
		CanonicalUrl:     n.CanonicalURL,
		CanonicalUrlHash: n.CanonicalURLHash,
		ContentHash:      n.ContentHash,
		Payload:          payload,
		HttpStatus:       &status,
		JobID:            jobID,
	}
}

func nilIfEmptyStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// recordParseError implements docs/06 section 10's "Parse error: Bug. Store
// raw, alert, do not retry — retrying a deterministic parse failure is
// pointless." There is no per-adapter-parse-error source status (adding one
// is a bigger schema change than this pass warrants — see the plan's scope
// notes), so "do not retry" is approximated by the same backoff pollOne
// already applies to a normal fetch failure, and "store raw, alert" is
// exact: this writes an observation row and logs at Error rather than Warn.
func (s *Scheduler) recordParseError(ctx context.Context, sourceID pgtype.UUID, url string, result *fetch.Result, parseErr error) {
	s.log.Error("adapter parse failed", "source_id", sourceID.String(), "err", parseErr)

	q := db.New(s.pool)
	statusCode := int16(0)
	if result != nil {
		statusCode = int16(result.StatusCode) //nolint:gosec // HTTP status codes fit comfortably in int16
	}
	err := q.InsertParseErrorObservation(ctx, db.InsertParseErrorObservationParams{
		SourceID:     sourceID,
		Url:          url,
		Payload:      fmt.Sprintf(`{"error": %q}`, parseErr.Error()),
		HttpStatus:   &statusCode,
		ProcessError: parseErr.Error(),
	})
	if err != nil {
		s.log.Warn("failed to record parse-error observation", "source_id", sourceID.String(), "err", err)
	}
}
