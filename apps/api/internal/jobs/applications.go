package jobs

import (
	"net/http"
	"strconv"
	"time"

	db "github.com/kelyon/scout/packages/db/gen"
)

// Applications handles GET /v1/applications — docs/04-api-design.md
// section 4.6, and the data source for docs/12-frontend-ux.md section
// 4.5's Pipeline view (Saved/Applied/Screening/Interviewing/Offer columns
// plus a collapsed Archive). `?state=` is repeatable, matching docs/04
// section 3's filter convention (repeated params OR within a field); no
// `state` means every tracked job, across every state.
func (h *Handler) Applications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("applications: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var states []string
	for _, s := range r.URL.Query()["state"] {
		if _, ok := validStates[s]; ok {
			states = append(states, s)
		}
	}

	params := db.SelectApplicationsParams{UserID: user.ID, WeightVersion: weightVersion}
	if len(states) > 0 {
		params.States = states
	}

	rows, err := q.SelectApplications(ctx, params)
	if err != nil {
		h.log.Error("applications: query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]applicationItem, len(rows))
	for i, row := range rows {
		items[i] = toApplicationItem(row)
	}

	writeJSON(w, http.StatusOK, applicationsResponse{Data: items})
}

type applicationsResponse struct {
	Data []applicationItem `json:"data"`
}

type applicationItem struct {
	JobGroupID          string       `json:"job_group_id"`
	Title               string       `json:"title"`
	Company             companyInfo  `json:"company"`
	Location            locationInfo `json:"location"`
	Compensation        *compInfo    `json:"compensation,omitempty"`
	RoleFamily          string       `json:"role_family"`
	Seniority           string       `json:"seniority"`
	Skills              []string     `json:"skills"`
	PostedAt            *string      `json:"posted_at,omitempty"`
	DeadlineAt          *string      `json:"deadline_at,omitempty"`
	Priority            int16        `json:"priority"`
	ApplyURL            string       `json:"apply_url"`
	JobStatus           string       `json:"job_status"`
	State               string       `json:"state"`
	StateChangedAt      string       `json:"state_changed_at"`
	FoundElsewhereFirst bool         `json:"found_elsewhere_first"`
	Notes               *string      `json:"notes,omitempty"`
	Rating              *int16       `json:"rating,omitempty"`
	AppliedAt           *string      `json:"applied_at,omitempty"`
}

func toApplicationItem(row db.SelectApplicationsRow) applicationItem {
	item := applicationItem{
		JobGroupID: row.JobGroupID.String(),
		Title:      row.Title,
		Company:    companyInfo{ID: row.CompanyID.String(), Name: row.CompanyName},
		Location: locationInfo{
			City:     row.LocationCity,
			Country:  row.LocationCountry,
			Tier:     row.LocationTier,
			WorkMode: row.WorkMode,
		},
		RoleFamily:          row.RoleFamily,
		Seniority:           row.Seniority,
		Skills:              row.Skills,
		Priority:            row.Priority,
		ApplyURL:            row.ApplyUrl,
		JobStatus:           row.JobStatus,
		State:               row.State,
		FoundElsewhereFirst: row.FoundElsewhereFirst,
		Notes:               row.Notes,
		Rating:              row.Rating,
	}
	if row.StateChangedAt.Valid {
		item.StateChangedAt = row.StateChangedAt.Time.UTC().Format(time.RFC3339)
	}
	if row.PostedAt.Valid {
		s := row.PostedAt.Time.UTC().Format(time.RFC3339)
		item.PostedAt = &s
	}
	if row.DeadlineAt.Valid {
		s := row.DeadlineAt.Time.UTC().Format(time.RFC3339)
		item.DeadlineAt = &s
	}
	if row.AppliedAt.Valid {
		s := row.AppliedAt.Time.UTC().Format(time.RFC3339)
		item.AppliedAt = &s
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
