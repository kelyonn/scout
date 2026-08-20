// Package jobs implements GET /v1/jobs and GET /v1/jobs/{group_id} —
// docs/04-api-design.md section 4.1's ranked feed and detail view. The
// query surface a real frontend needs to replace hand-run psql, which is
// what every prior verification pass in this project used instead.
//
// Deliberately a subset of the documented endpoint list: /similar,
// /score, /sources, /state, /feedback, /prep, /cover-letter all need
// either data this pass doesn't compute (AI prep questions, a semantic-
// neighbor index) or state this schema doesn't yet track (a job_state
// table for saved/applied/interviewing) — see this package's own tests
// for exactly what each response can and cannot honestly claim right now.
package jobs

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/queue"
)

// defaultLimit/maxLimit bound the page size a caller can request —
// docs/04's own pagination section doesn't state a cap, but an unbounded
// `limit` turns a cursor-paginated feed back into the offset-pagination
// failure mode it exists to avoid (a client asking for everything in one
// page defeats the cursor's purpose).
const (
	defaultLimit = 25
	maxLimit     = 100
)

// weightVersion is hardcoded because there is exactly one, always active,
// weight_version row in this single-user system (ADR-015) — see
// packages/db/queries/notification.sql's own SelectUnnotifiedJobGroups
// for the identical assumption.
const weightVersion = "v1-hand-tuned-2026-08"

// Handler serves the feed, job-detail, and job-summary endpoints
// (docs/04-api-design.md section 4.1).
type Handler struct {
	pool  *pgxpool.Pool
	queue *queue.Client
	log   *slog.Logger
}

// New constructs a Handler.
func New(pool *pgxpool.Pool, q *queue.Client, log *slog.Logger) *Handler {
	return &Handler{pool: pool, queue: q, log: log}
}

// cursor is the decoded shape of the opaque, base64url-encoded pagination
// token docs/04 section 3 describes: "encodes the sort key and a
// tiebreaker ID." Priority is a pointer so the zero page (no cursor yet)
// is distinguishable from "resume exactly at priority 0."
type cursor struct {
	Priority *int16 `json:"p"`
	JobID    string `json:"id"`
}

func encodeCursor(priority int16, jobID string) string {
	b, _ := json.Marshal(cursor{Priority: &priority, JobID: jobID})
	return base64.URLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, bool) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, false
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return cursor{}, false
	}
	return c, c.Priority != nil && c.JobID != ""
}

// List handles GET /v1/jobs.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("jobs list: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	params := db.SelectJobFeedParams{
		UserID:        user.ID,
		WeightVersion: weightVersion,
		Limit:         parseLimit(r.URL.Query().Get("limit")),
	}

	query := r.URL.Query()
	params.RoleFamilies = query["role_family"]
	params.Seniorities = query["seniority"]
	params.WorkModes = query["work_mode"]
	if tiers, ok := parseInt16List(query["location_tier"]); ok {
		params.LocationTiers = tiers
	}
	if ids, ok := parseUUIDList(query["company_id"]); ok {
		params.CompanyIds = ids
	}
	if mp, ok := parseInt16(query.Get("min_priority")); ok {
		params.MinPriority = &mp
	}

	if c := query.Get("cursor"); c != "" {
		decoded, ok := decodeCursor(c)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		var jobID pgtype.UUID
		if scanErr := jobID.Scan(decoded.JobID); scanErr != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		params.CursorPriority = decoded.Priority
		params.CursorJobID = jobID
	}

	rows, err := q.SelectJobFeed(ctx, params)
	if err != nil {
		h.log.Error("jobs list: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]feedItem, len(rows))
	for i, row := range rows {
		items[i] = toFeedItem(row)
	}

	page := pageInfo{HasMore: len(rows) == int(params.Limit)}
	if page.HasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = encodeCursor(last.Priority, last.JobID.String())
	}

	writeJSON(w, http.StatusOK, feedResponse{Data: items, Page: page})
}

func parseLimit(s string) int32 {
	if s == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return int32(n) //nolint:gosec // n is bounded to (0, maxLimit] by the checks above
}

func parseInt16(s string) (int16, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < math.MinInt16 || n > math.MaxInt16 {
		return 0, false
	}
	return int16(n), true //nolint:gosec // n is bounded to [math.MinInt16, math.MaxInt16] by the check above
}

func parseInt16List(ss []string) ([]int16, bool) {
	if len(ss) == 0 {
		return nil, false
	}
	out := make([]int16, 0, len(ss))
	for _, s := range ss {
		n, ok := parseInt16(s)
		if ok {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func parseUUIDList(ss []string) ([]pgtype.UUID, bool) {
	if len(ss) == 0 {
		return nil, false
	}
	out := make([]pgtype.UUID, 0, len(ss))
	for _, s := range ss {
		var id pgtype.UUID
		if err := id.Scan(s); err == nil {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

type feedResponse struct {
	Data []feedItem `json:"data"`
	Page pageInfo   `json:"page"`
}

type pageInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// feedItem is docs/04 section 4.1's documented shape, trimmed to fields
// this pass actually computes — no company logo_url/stage (no company
// registry beyond docs/07's competition-estimate metadata), no AI
// explanation (job_score.explanation exists per-user via the detail
// endpoint; the list feed doesn't join it to keep this query a single
// index scan rather than a second per-row lookup).
type feedItem struct {
	JobGroupID   string       `json:"job_group_id"`
	Title        string       `json:"title"`
	Company      companyInfo  `json:"company"`
	Location     locationInfo `json:"location"`
	Compensation *compInfo    `json:"compensation,omitempty"`
	RoleFamily   string       `json:"role_family"`
	Seniority    string       `json:"seniority"`
	Skills       []string     `json:"skills"`
	PostedAt     *string      `json:"posted_at,omitempty"`
	DeadlineAt   *string      `json:"deadline_at,omitempty"`
	FirstSeenAt  *string      `json:"first_seen_at,omitempty"`
	Priority     int16        `json:"priority"`
	ApplyURL     string       `json:"apply_url"`
	State        string       `json:"state"`
}

type companyInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type locationInfo struct {
	City     *string `json:"city,omitempty"`
	Country  *string `json:"country,omitempty"`
	Tier     *int16  `json:"tier,omitempty"`
	WorkMode string  `json:"work_mode"`
}

type compInfo struct {
	NormalizedINRMonth string `json:"normalized_inr_month"`
	Paid               string `json:"paid"`
}

func toFeedItem(row db.SelectJobFeedRow) feedItem {
	item := feedItem{
		JobGroupID: row.JobGroupID.String(),
		Title:      row.Title,
		Company:    companyInfo{ID: row.CompanyID.String(), Name: row.CompanyName},
		Location: locationInfo{
			City:     row.LocationCity,
			Country:  row.LocationCountry,
			Tier:     row.LocationTier,
			WorkMode: row.WorkMode,
		},
		RoleFamily: row.RoleFamily,
		Seniority:  row.Seniority,
		Skills:     row.Skills,
		Priority:   row.Priority,
		ApplyURL:   row.ApplyUrl,
		State:      row.State,
	}
	if row.PostedAt.Valid {
		s := row.PostedAt.Time.UTC().Format(time.RFC3339)
		item.PostedAt = &s
	}
	if row.DeadlineAt.Valid {
		s := row.DeadlineAt.Time.UTC().Format(time.RFC3339)
		item.DeadlineAt = &s
	}
	if row.FirstSeenAt.Valid {
		s := row.FirstSeenAt.Time.UTC().Format(time.RFC3339)
		item.FirstSeenAt = &s
	}
	if row.CompNormalizedInrMonth.Valid {
		f, err := row.CompNormalizedInrMonth.Float64Value()
		if err == nil && f.Valid {
			item.Compensation = &compInfo{
				NormalizedINRMonth: strconv.FormatFloat(f.Float64, 'f', 2, 64),
				Paid:               row.Paid,
			}
		}
	}
	return item
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
