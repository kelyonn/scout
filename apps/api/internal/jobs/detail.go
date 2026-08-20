package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/queue"
)

// Detail handles GET /v1/jobs/{group_id} — docs/12-frontend-ux.md section
// 4.3's rule made concrete: "All thirteen scores are shown, always. A
// composite number without its components is unfalsifiable." Every
// job_score subscore this pass computes is in the response, not just
// priority.
func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	var groupID pgtype.UUID
	if err := groupID.Scan(r.PathValue("group_id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid job group id")
		return
	}

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("jobs detail: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	row, err := q.SelectJobDetail(ctx, db.SelectJobDetailParams{
		JobGroupID:    groupID,
		UserID:        user.ID,
		WeightVersion: weightVersion,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "job not found (or no longer open)")
			return
		}
		h.log.Error("jobs detail: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := toDetailResponse(row)

	// The AI summary (job.ai_summary) is generated on demand, the first
	// time a job is actually viewed — most ingested jobs are never opened,
	// so summarizing everything at ingest time would be mostly wasted
	// local-LLM compute (see apps/brain/scout_brain/summarize.py's own
	// comment). A null summary here means either "never requested yet" or
	// "still generating" — resp.SummaryPending distinguishes those for the
	// frontend (poll vs. show nothing), and h.enqueueSummaryIfNeeded only
	// enqueues once per outstanding request rather than once per view.
	if resp.AISummary == nil {
		resp.SummaryPending = true
		if err := h.enqueueSummaryIfNeeded(ctx, row.JobID); err != nil {
			// Not fatal to the response — the detail page still renders
			// without a summary, and the next view retries the enqueue.
			h.log.Warn("jobs detail: enqueue summarize failed", "job_id", row.JobID.String(), "err", err)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// enqueueSummaryIfNeeded enqueues a TaskSummarize job for jobID unless one
// is already outstanding (available, running, or scheduled for retry) —
// without this check, refreshing the detail page repeatedly while a slow
// local LLM call is still in flight would queue a redundant summarize job
// per refresh. A plain existence query rather than River's UniqueOpts:
// packages/riverpy (apps/brain's hand-rolled Python consumer) is minimal
// by design, and this avoids depending on schema/behavior beyond what it
// explicitly implements.
func (h *Handler) enqueueSummaryIfNeeded(ctx context.Context, jobID pgtype.UUID) error {
	var alreadyQueued bool
	err := h.pool.QueryRow(ctx, `
		select exists(
			select 1 from river_job
			where kind = 'brain_deep'
				and args->>'task' = 'summarize'
				and args->>'job_id' = $1
				and state in ('available', 'running', 'retryable', 'scheduled')
		)
	`, jobID.String()).Scan(&alreadyQueued)
	if err != nil {
		return fmt.Errorf("check outstanding summarize job: %w", err)
	}
	if alreadyQueued {
		return nil
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := h.queue.EnqueueBrainDeep(ctx, tx, jobID.String(), queue.TaskSummarize); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// scoreDetail is one of the thirteen subscores plus its weighted
// multiplier context — nil when P2's scoring pass left it an unpopulated
// placeholder for this job (docs/09's own "omit, don't substitute 50"
// rule for a subscore missing real input data, e.g. resume_match before a
// resume existed).
type detailResponse struct {
	JobGroupID          string        `json:"job_group_id"`
	Title               string        `json:"title"`
	Description         *string       `json:"description_text,omitempty"`
	DescriptionHTML     *string       `json:"description_html,omitempty"`
	AISummary           *string       `json:"ai_summary,omitempty"`
	SummaryPending      bool          `json:"summary_pending"`
	Requirements        *string       `json:"requirements_text,omitempty"`
	ApplyURL            string        `json:"apply_url"`
	CanonicalURL        string        `json:"canonical_url"`
	RoleFamily          string        `json:"role_family"`
	Seniority           string        `json:"seniority"`
	Location            locationInfo  `json:"location"`
	VisaSponsorship     *bool         `json:"visa_sponsorship,omitempty"`
	Compensation        *compDetail   `json:"compensation,omitempty"`
	Skills              []string      `json:"skills"`
	TechStack           []string      `json:"tech_stack"`
	PostedAt            *string       `json:"posted_at,omitempty"`
	DeadlineAt          *string       `json:"deadline_at,omitempty"`
	Company             companyDetail `json:"company"`
	Scores              *scoresDetail `json:"scores,omitempty"`
	SourceCount         int32         `json:"source_count"`
	FirstSeenAt         string        `json:"first_seen_at"`
	State               string        `json:"state"`
	FoundElsewhereFirst bool          `json:"found_elsewhere_first"`
	Notes               *string       `json:"notes,omitempty"`
	Rating              *int16        `json:"rating,omitempty"`
}

type compDetail struct {
	Min                *string `json:"min,omitempty"`
	Max                *string `json:"max,omitempty"`
	Currency           *string `json:"currency,omitempty"`
	NormalizedINRMonth *string `json:"normalized_inr_month,omitempty"`
	Paid               string  `json:"paid"`
}

type companyDetail struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	WebsiteURL  *string  `json:"website_url,omitempty"`
	SizeBucket  *string  `json:"size_bucket,omitempty"`
	Stage       *string  `json:"stage,omitempty"`
	Industries  []string `json:"industries"`
}

// scoresDetail is every column job_score has except the internal
// score_inputs jsonb blob (packages/db/queries/score.sql's own detailed
// per-subscore reasoning — a richer surface than this first pass exposes;
// explanation/explanation_model already covers the human-readable case).
type scoresDetail struct {
	Priority             int16   `json:"priority"`
	OverallMatch         *int16  `json:"overall_match,omitempty"`
	SkillMatch           *int16  `json:"skill_match,omitempty"`
	ResumeMatch          *int16  `json:"resume_match,omitempty"`
	CompanyQuality       *int16  `json:"company_quality,omitempty"`
	Compensation         *int16  `json:"compensation,omitempty"`
	LearningOpportunity  *int16  `json:"learning_opportunity,omitempty"`
	EngineeringCulture   *int16  `json:"engineering_culture,omitempty"`
	GrowthPotential      *int16  `json:"growth_potential,omitempty"`
	InterviewProbability *int16  `json:"interview_probability,omitempty"`
	CompetitionEstimate  *int16  `json:"competition_estimate,omitempty"`
	EaseOfApplying       *int16  `json:"ease_of_applying,omitempty"`
	DeadlineUrgency      *int16  `json:"deadline_urgency,omitempty"`
	LocationMultiplier   float32 `json:"location_multiplier"`
	FreshnessMultiplier  float32 `json:"freshness_multiplier"`
	Explanation          *string `json:"explanation,omitempty"`
}

func toDetailResponse(row db.SelectJobDetailRow) detailResponse {
	resp := detailResponse{
		JobGroupID:      row.JobGroupID.String(),
		Title:           row.Title,
		Description:     row.DescriptionText,
		DescriptionHTML: row.DescriptionHtml,
		AISummary:       row.AiSummary,
		Requirements:    row.RequirementsText,
		ApplyURL:        row.ApplyUrl,
		CanonicalURL:    row.CanonicalUrl,
		RoleFamily:      row.RoleFamily,
		Seniority:       row.Seniority,
		VisaSponsorship: row.VisaSponsorship,
		Skills:          row.Skills,
		TechStack:       row.TechStack,
		SourceCount:     row.MemberCount,
		Location: locationInfo{
			City:     row.LocationCity,
			Country:  row.LocationCountry,
			Tier:     row.LocationTier,
			WorkMode: row.WorkMode,
		},
		Company: companyDetail{
			ID:          row.CompanyID.String(),
			Name:        row.CompanyName,
			Description: row.CompanyDescription,
			WebsiteURL:  row.CompanyWebsiteUrl,
			SizeBucket:  row.CompanySizeBucket,
			Stage:       row.CompanyStage,
			Industries:  row.CompanyIndustries,
		},
		State:               row.State,
		FoundElsewhereFirst: row.FoundElsewhereFirst != nil && *row.FoundElsewhereFirst,
		Notes:               row.Notes,
		Rating:              row.Rating,
	}

	if row.GroupFirstSeenAt.Valid {
		resp.FirstSeenAt = row.GroupFirstSeenAt.Time.UTC().Format(time.RFC3339)
	}
	if row.PostedAt.Valid {
		s := row.PostedAt.Time.UTC().Format(time.RFC3339)
		resp.PostedAt = &s
	}
	if row.DeadlineAt.Valid {
		s := row.DeadlineAt.Time.UTC().Format(time.RFC3339)
		resp.DeadlineAt = &s
	}

	if row.CompNormalizedInrMonth.Valid || row.CompMin.Valid || row.CompMax.Valid {
		resp.Compensation = &compDetail{
			Min:                numericToString(row.CompMin),
			Max:                numericToString(row.CompMax),
			Currency:           row.CompCurrency,
			NormalizedINRMonth: numericToString(row.CompNormalizedInrMonth),
			Paid:               row.Paid,
		}
	}

	// A left-joined job_score can genuinely be absent (no score has ever
	// been computed for this job/user/weight_version combination yet) —
	// Priority is the join's own NOT NULL sentinel column in
	// SelectJobFeedRow but here it's *int16 from job_score directly, so
	// OverallMatch's nilness is what actually distinguishes "no score
	// row" from "a real priority of 0."
	if row.OverallMatch != nil || row.Priority != nil {
		var priority int16
		if row.Priority != nil {
			priority = *row.Priority
		}
		resp.Scores = &scoresDetail{
			Priority:             priority,
			OverallMatch:         row.OverallMatch,
			SkillMatch:           row.SkillMatch,
			ResumeMatch:          row.ResumeMatch,
			CompanyQuality:       row.CompanyQuality,
			Compensation:         row.Compensation,
			LearningOpportunity:  row.LearningOpportunity,
			EngineeringCulture:   row.EngineeringCulture,
			GrowthPotential:      row.GrowthPotential,
			InterviewProbability: row.InterviewProbability,
			CompetitionEstimate:  row.CompetitionEstimate,
			EaseOfApplying:       row.EaseOfApplying,
			DeadlineUrgency:      row.DeadlineUrgency,
			LocationMultiplier:   deref32(row.LocationMultiplier, 1.0),
			FreshnessMultiplier:  deref32(row.FreshnessMultiplier, 1.0),
			Explanation:          row.Explanation,
		}
	}

	return resp
}

// deref32 returns *f, or fallback when the LEFT JOIN found no job_score
// row at all — fallback matches the column's own schema default (1.0 for
// both multiplier columns), the same "no score yet" default a job would
// have before scoring.go ever ran.
func deref32(f *float32, fallback float32) float32 {
	if f == nil {
		return fallback
	}
	return *f
}

func numericToString(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	s := strconv.FormatFloat(f.Float64, 'f', 2, 64)
	return &s
}
