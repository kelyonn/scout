package jobs

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
)

// validTransitions implements docs/04-api-design.md section 4.1's state
// diagram:
//
//	new ──▶ viewed ──┬──▶ saved ────▶ applied ──▶ screening ──▶ interviewing
//	                 │                    │                          │
//	                 └──▶ dismissed       └──▶ rejected     ┌─────────┼─────────┐
//	                                                        ▼         ▼         ▼
//	                                                     offer   rejected  withdrawn
//	                                                        │
//	                                                        ▼
//	                                                    accepted
//
// plus two additions the diagram itself doesn't draw but the enum
// (application_state) and docs/12-frontend-ux.md section 4.2's "dismissed
// ... with undo" card state both imply: `withdrawn` is reachable from
// every active, non-terminal state (a user can withdraw at any point, not
// only from interviewing, where the diagram happens to draw it), and
// `dismissed` is undoable back to `saved`. `expired` has no entry here on
// either side — nothing transitions a job to `expired` through this
// user-facing endpoint; it is a system-driven state for when job.status
// itself expires, not implemented by this pass (see HANDOFF.md).
// accepted/rejected/withdrawn are terminal.
var validTransitions = map[db.ApplicationState]map[db.ApplicationState]bool{
	db.ApplicationStateNew: {
		db.ApplicationStateViewed:    true,
		db.ApplicationStateDismissed: true,
	},
	db.ApplicationStateViewed: {
		db.ApplicationStateSaved:     true,
		db.ApplicationStateDismissed: true,
	},
	db.ApplicationStateSaved: {
		db.ApplicationStateApplied:   true,
		db.ApplicationStateDismissed: true,
		db.ApplicationStateWithdrawn: true,
	},
	db.ApplicationStateDismissed: {
		db.ApplicationStateSaved: true,
	},
	db.ApplicationStateApplied: {
		db.ApplicationStateScreening: true,
		db.ApplicationStateRejected:  true,
		db.ApplicationStateWithdrawn: true,
	},
	db.ApplicationStateScreening: {
		db.ApplicationStateInterviewing: true,
		db.ApplicationStateRejected:     true,
		db.ApplicationStateWithdrawn:    true,
	},
	db.ApplicationStateInterviewing: {
		db.ApplicationStateOffer:     true,
		db.ApplicationStateRejected:  true,
		db.ApplicationStateWithdrawn: true,
	},
	db.ApplicationStateOffer: {
		db.ApplicationStateAccepted:  true,
		db.ApplicationStateRejected:  true,
		db.ApplicationStateWithdrawn: true,
	},
}

var validStates = func() map[string]db.ApplicationState {
	m := map[string]db.ApplicationState{}
	for _, s := range []db.ApplicationState{
		db.ApplicationStateNew, db.ApplicationStateViewed, db.ApplicationStateSaved,
		db.ApplicationStateDismissed, db.ApplicationStateApplied, db.ApplicationStateScreening,
		db.ApplicationStateInterviewing, db.ApplicationStateOffer, db.ApplicationStateAccepted,
		db.ApplicationStateRejected, db.ApplicationStateWithdrawn, db.ApplicationStateExpired,
	} {
		m[string(s)] = s
	}
	return m
}()

type stateTransitionRequest struct {
	State               string  `json:"state"`
	FoundElsewhereFirst *bool   `json:"found_elsewhere_first,omitempty"`
	Notes               *string `json:"notes,omitempty"`
	Rating              *int16  `json:"rating,omitempty"`
}

type stateResponse struct {
	JobGroupID          string  `json:"job_group_id"`
	State               string  `json:"state"`
	StateChangedAt      string  `json:"state_changed_at"`
	FoundElsewhereFirst bool    `json:"found_elsewhere_first"`
	Notes               *string `json:"notes,omitempty"`
	Rating              *int16  `json:"rating,omitempty"`
}

// State handles POST /v1/jobs/{group_id}/state — docs/04-api-design.md
// section 4.1. "State transitions are validated server-side against an
// explicit state machine; an invalid transition is a 409, not a silent
// accept."
func (h *Handler) State(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	var groupID pgtype.UUID
	if err := groupID.Scan(r.PathValue("group_id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid job group id")
		return
	}

	var req stateTransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	target, ok := validStates[req.State]
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "unknown state: "+req.State)
		return
	}

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("jobs state: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	current, err := q.GetUserJobState(ctx, db.GetUserJobStateParams{UserID: user.ID, JobGroupID: groupID})
	var currentState db.ApplicationState
	var hasExistingRow bool
	switch {
	case err == nil:
		currentState = current.State
		hasExistingRow = true
	case errors.Is(err, pgx.ErrNoRows):
		currentState = db.ApplicationStateNew
	default:
		h.log.Error("jobs state: get current state failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if target != currentState && !validTransitions[currentState][target] {
		writeError(w, http.StatusConflict, "invalid transition: "+string(currentState)+" -> "+string(target))
		return
	}

	foundElsewhereFirst := false
	if hasExistingRow {
		foundElsewhereFirst = current.FoundElsewhereFirst
	}
	if req.FoundElsewhereFirst != nil {
		foundElsewhereFirst = foundElsewhereFirst || *req.FoundElsewhereFirst
	}

	var appliedAt pgtype.Timestamptz
	if target == db.ApplicationStateApplied {
		_ = appliedAt.Scan(time.Now().UTC())
	}

	updated, err := q.UpsertUserJobState(ctx, db.UpsertUserJobStateParams{
		UserID: user.ID, JobGroupID: groupID, State: target,
		FoundElsewhereFirst: foundElsewhereFirst,
		Notes:               req.Notes, Rating: req.Rating, AppliedAt: appliedAt,
	})
	if err != nil {
		h.log.Error("jobs state: upsert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if target != currentState {
		var fromState *db.ApplicationState
		if hasExistingRow {
			fromState = &currentState
		}
		if err := q.InsertUserJobStateEvent(ctx, db.InsertUserJobStateEventParams{
			UserID: user.ID, JobGroupID: groupID, FromState: fromState, ToState: target,
		}); err != nil {
			// Not fatal to the response — the state itself is already
			// committed; a missing history row loses "days in current
			// state" accuracy, not the state itself.
			h.log.Warn("jobs state: insert history event failed", "err", err)
		}
	}

	writeJSON(w, http.StatusOK, stateResponse{
		JobGroupID:          groupID.String(),
		State:               string(updated.State),
		StateChangedAt:      updated.StateChangedAt.Time.UTC().Format(time.RFC3339),
		FoundElsewhereFirst: updated.FoundElsewhereFirst,
		Notes:               updated.Notes,
		Rating:              updated.Rating,
	})
}
