// Package companies implements GET /v1/companies, GET /v1/companies/{id},
// and GET/POST/DELETE /v1/me/watchlist — docs/04-api-design.md section
// 4.5. "Jobs at this company" needs no endpoint of its own: GET /v1/jobs
// already accepts ?company_id=, so the frontend calls that directly
// (apps/web/lib/api.ts's fetchJobs).
//
// /companies/trending isn't implemented — it needs a real hiring-velocity
// computation this pass doesn't build.
package companies

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

// defaultLimit/maxLimit mirror apps/api/internal/jobs/handler.go's own
// pair, sized down: the company list has no cursor to page through (see
// packages/db/queries/company.sql's comment on why), so this is the
// whole bound.
const (
	defaultLimit = 100
	maxLimit     = 200
)

// Handler serves the companies list and detail endpoints.
type Handler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New constructs a Handler.
func New(pool *pgxpool.Pool, log *slog.Logger) *Handler {
	return &Handler{pool: pool, log: log}
}

type companyListItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	LogoURL      *string  `json:"logo_url,omitempty"`
	CompanyType  string   `json:"company_type"`
	SizeBucket   *string  `json:"size_bucket,omitempty"`
	Stage        *string  `json:"stage,omitempty"`
	Industries   []string `json:"industries"`
	QualityScore *int16   `json:"quality_score,omitempty"`
	HQCity       *string  `json:"hq_city,omitempty"`
	HQCountry    *string  `json:"hq_country,omitempty"`
	OpenJobCount int64    `json:"open_job_count"`
}

type companyListResponse struct {
	Data []companyListItem `json:"data"`
}

func toListItem(row db.SelectCompaniesRow) companyListItem {
	return companyListItem{
		ID:           row.ID.String(),
		Name:         row.CanonicalName,
		LogoURL:      row.LogoUrl,
		CompanyType:  row.CompanyType,
		SizeBucket:   row.SizeBucket,
		Stage:        row.Stage,
		Industries:   row.Industries,
		QualityScore: row.QualityScore,
		HQCity:       row.HqCity,
		HQCountry:    row.HqCountry,
		OpenJobCount: row.OpenJobCount,
	}
}

// List handles GET /v1/companies. Only companies with at least one
// currently-open software job, ranked by how many they have — see
// packages/db/queries/company.sql's own comment.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	limit := defaultLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= maxLimit {
			limit = n
		}
	}

	params := db.SelectCompaniesParams{Limit: int32(limit)}
	if query := r.URL.Query().Get("q"); query != "" {
		params.Query = &query
	}

	rows, err := q.SelectCompanies(ctx, params)
	if err != nil {
		h.log.Error("companies list: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]companyListItem, len(rows))
	for i, row := range rows {
		items[i] = toListItem(row)
	}
	writeJSON(w, http.StatusOK, companyListResponse{Data: items})
}

type companyDetail struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	LegalName        *string  `json:"legal_name,omitempty"`
	Description      *string  `json:"description,omitempty"`
	LogoURL          *string  `json:"logo_url,omitempty"`
	WebsiteURL       *string  `json:"website_url,omitempty"`
	LinkedinURL      *string  `json:"linkedin_url,omitempty"`
	GithubOrg        *string  `json:"github_org,omitempty"`
	CompanyType      string   `json:"company_type"`
	SizeBucket       *string  `json:"size_bucket,omitempty"`
	FoundedYear      *int16   `json:"founded_year,omitempty"`
	Stage            *string  `json:"stage,omitempty"`
	Industries       []string `json:"industries"`
	TechStack        []string `json:"tech_stack"`
	HQCity           *string  `json:"hq_city,omitempty"`
	HQCountry        *string  `json:"hq_country,omitempty"`
	QualityScore     *int16   `json:"quality_score,omitempty"`
	EngineeringScore *int16   `json:"engineering_score,omitempty"`
	OpenJobCount     int64    `json:"open_job_count"`
}

func toDetail(row db.GetCompanyByIDRow) companyDetail {
	return companyDetail{
		ID:               row.ID.String(),
		Name:             row.CanonicalName,
		LegalName:        row.LegalName,
		Description:      row.Description,
		LogoURL:          row.LogoUrl,
		WebsiteURL:       row.WebsiteUrl,
		LinkedinURL:      row.LinkedinUrl,
		GithubOrg:        row.GithubOrg,
		CompanyType:      row.CompanyType,
		SizeBucket:       row.SizeBucket,
		FoundedYear:      row.FoundedYear,
		Stage:            row.Stage,
		Industries:       row.Industries,
		TechStack:        row.TechStack,
		HQCity:           row.HqCity,
		HQCountry:        row.HqCountry,
		QualityScore:     row.QualityScore,
		EngineeringScore: row.EngineeringScore,
		OpenJobCount:     row.OpenJobCount,
	}
}

// Detail handles GET /v1/companies/{id}.
func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	row, err := q.GetCompanyByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}

	writeJSON(w, http.StatusOK, toDetail(row))
}

type watchlistItem struct {
	CompanyID     string  `json:"company_id"`
	CompanyName   string  `json:"company_name"`
	LogoURL       *string `json:"logo_url,omitempty"`
	CompanyType   string  `json:"company_type"`
	NotifyAnyRole bool    `json:"notify_any_role"`
	AddedAt       string  `json:"added_at"`
	OpenJobCount  int64   `json:"open_job_count"`
}

type watchlistResponse struct {
	Data []watchlistItem `json:"data"`
}

func toWatchlistItem(row db.ListWatchlistRow) watchlistItem {
	return watchlistItem{
		CompanyID:     row.CompanyID.String(),
		CompanyName:   row.CompanyName,
		LogoURL:       row.LogoUrl,
		CompanyType:   row.CompanyType,
		NotifyAnyRole: row.NotifyAnyRole,
		AddedAt:       row.AddedAt.Time.UTC().Format(time.RFC3339),
		OpenJobCount:  row.OpenJobCount,
	}
}

// Watchlist handles GET /v1/me/watchlist — docs/04-api-design.md section 4.5.
func (h *Handler) Watchlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("watchlist: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	rows, err := q.ListWatchlist(ctx, user.ID)
	if err != nil {
		h.log.Error("watchlist: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]watchlistItem, len(rows))
	for i, row := range rows {
		items[i] = toWatchlistItem(row)
	}
	writeJSON(w, http.StatusOK, watchlistResponse{Data: items})
}

type addWatchlistRequest struct {
	CompanyID     string `json:"company_id"`
	NotifyAnyRole *bool  `json:"notify_any_role,omitempty"`
}

// AddToWatchlist handles POST /v1/me/watchlist. Idempotent: re-posting an
// already-watchlisted company_id updates notify_any_role rather than
// erroring — see packages/db/queries/watchlist.sql's UpsertWatchlistEntry
// comment.
func (h *Handler) AddToWatchlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	var req addWatchlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	var companyID pgtype.UUID
	if err := companyID.Scan(req.CompanyID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	notifyAnyRole := true
	if req.NotifyAnyRole != nil {
		notifyAnyRole = *req.NotifyAnyRole
	}

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("watchlist add: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if _, err := q.UpsertWatchlistEntry(ctx, db.UpsertWatchlistEntryParams{
		UserID: user.ID, CompanyID: companyID, NotifyAnyRole: notifyAnyRole,
	}); err != nil {
		// The only realistic failure beyond a genuine DB error is a
		// company_id that doesn't exist (foreign key violation) — both
		// collapse to the same 404, since the caller has no way to tell a
		// bad id from a real-but-wrong one without another round trip.
		writeError(w, http.StatusNotFound, "company not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveFromWatchlist handles DELETE /v1/me/watchlist/{company_id}.
// Unconditionally 204 whether or not a row existed — matching Save/Dismiss
// elsewhere in this API (docs/12 section 5: "undo over confirm"), removing
// something already absent isn't an error.
func (h *Handler) RemoveFromWatchlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	var companyID pgtype.UUID
	if err := companyID.Scan(r.PathValue("company_id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("watchlist remove: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if _, err := q.RemoveWatchlistEntry(ctx, db.RemoveWatchlistEntryParams{
		UserID: user.ID, CompanyID: companyID,
	}); err != nil {
		h.log.Error("watchlist remove: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
