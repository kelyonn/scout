// Package search implements GET /v1/search — docs/04-api-design.md
// section 4.2. Keyword mode only: `mode=semantic` and `mode=hybrid` need a
// query embedding, which apps/brain (Python) computes, and ADR-001
// forbids Go calling Python synchronously. Rather than 501 on the two
// documented modes this pass can't serve, a semantic/hybrid request
// degrades to keyword and says so in the response (`mode_served`) — the
// same "degrade and say so" posture AGENTS.md rule 6 requires for AI
// calls generally, applied here to a mode this pass never had AI wired up
// for at all.
package search

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

const (
	defaultLimit  = 25
	maxLimit      = 50
	weightVersion = "v1-hand-tuned-2026-08"
)

// Handler serves GET /v1/search.
type Handler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New returns a search Handler.
func New(pool *pgxpool.Pool, log *slog.Logger) *Handler {
	return &Handler{pool: pool, log: log}
}

type searchResponse struct {
	Data       []searchResultItem `json:"data"`
	ModeServed string             `json:"mode_served"`
}

type searchResultItem struct {
	JobGroupID   string       `json:"job_group_id"`
	Title        string       `json:"title"`
	Company      companyInfo  `json:"company"`
	Location     locationInfo `json:"location"`
	Compensation *compInfo    `json:"compensation,omitempty"`
	RoleFamily   string       `json:"role_family"`
	Seniority    string       `json:"seniority"`
	Skills       []string     `json:"skills"`
	PostedAt     *string      `json:"posted_at,omitempty"`
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

// Search handles GET /v1/search?q=...&mode=....
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	limit := int32(defaultLimit)
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			if n > maxLimit {
				n = maxLimit
			}
			limit = int32(n) //nolint:gosec // n is clamped to maxLimit (50) immediately above, so this conversion cannot overflow
		}
	}

	if mode := r.URL.Query().Get("mode"); mode != "" && mode != "keyword" {
		h.log.Debug("search: mode requested but not served, degrading to keyword", "mode_requested", mode)
	}

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("search: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	rows, err := q.SearchJobs(ctx, db.SearchJobsParams{
		Query: query, UserID: user.ID, WeightVersion: weightVersion, Limit: limit,
	})
	if err != nil {
		h.log.Error("search: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]searchResultItem, len(rows))
	for i, row := range rows {
		items[i] = toSearchResultItem(row)
	}

	// keyword is the only mode this pass actually serves — see the
	// package comment. mode_served tells the caller so silently, without
	// erroring on a documented mode this pass can't yet honor.
	writeJSON(w, http.StatusOK, searchResponse{Data: items, ModeServed: "keyword"})
}

func toSearchResultItem(row db.SearchJobsRow) searchResultItem {
	item := searchResultItem{
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
