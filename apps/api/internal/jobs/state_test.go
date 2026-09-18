package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func doState(t *testing.T, h *Handler, groupID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/jobs/"+groupID+"/state", bytes.NewReader(raw))
	r.SetPathValue("group_id", groupID)
	w := httptest.NewRecorder()
	h.State(w, r)
	return w
}

func TestState_ValidTransitionChainSucceeds(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	for _, target := range []string{"viewed", "saved", "applied", "screening", "interviewing", "offer", "accepted"} {
		w := doState(t, h, fx.groupID, map[string]any{"state": target})
		if w.Code != 200 {
			t.Fatalf("transition to %q: status = %d, body = %s", target, w.Code, w.Body.String())
		}
		var resp stateResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.State != target {
			t.Errorf("State = %q, want %q", resp.State, target)
		}
	}
}

func TestState_InvalidTransitionIsConflict(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	// new -> offer skips the entire chain; the diagram (docs/04 section
	// 4.1) has no such edge.
	w := doState(t, h, fx.groupID, map[string]any{"state": "offer"})
	if w.Code != 409 {
		t.Errorf("status = %d, want 409, body = %s", w.Code, w.Body.String())
	}
}

func TestState_UnknownStateIsUnprocessable(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	w := doState(t, h, fx.groupID, map[string]any{"state": "not_a_real_state"})
	if w.Code != 422 {
		t.Errorf("status = %d, want 422, body = %s", w.Code, w.Body.String())
	}
}

func TestState_InvalidGroupIDIsBadRequest(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	w := doState(t, h, "not-a-uuid", map[string]any{"state": "viewed"})
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestState_RepostingSameStateIsIdempotent(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	w := doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	if w.Code != 200 {
		t.Errorf("re-posting the current state: status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

func TestState_DismissedCanBeUndoneBackToSaved(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "dismissed"})
	w := doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	if w.Code != 200 {
		t.Errorf("dismissed -> saved (undo): status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

func TestState_AppliedCanBeUndoneBackToSaved(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	doState(t, h, fx.groupID, map[string]any{"state": "applied"})
	w := doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	if w.Code != 200 {
		t.Errorf("applied -> saved (undo): status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

func TestState_WithdrawnReachableFromAppliedNotJustInterviewing(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	doState(t, h, fx.groupID, map[string]any{"state": "applied"})
	w := doState(t, h, fx.groupID, map[string]any{"state": "withdrawn"})
	if w.Code != 200 {
		t.Errorf("applied -> withdrawn: status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

// TestState_FoundElsewhereFirstIsCapturedAndSticky is docs/16-observability.md
// section 2.1's "one tap ... is the whole instrument" made concrete: once
// set true for a (user, job) pair, a later transition that doesn't
// explicitly repeat the flag must not silently clear it back to false.
func TestState_FoundElsewhereFirstIsCapturedAndSticky(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	w := doState(t, h, fx.groupID, map[string]any{"state": "applied", "found_elsewhere_first": true})
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp stateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.FoundElsewhereFirst {
		t.Fatal("expected found_elsewhere_first = true after setting it on the applied transition")
	}

	w2 := doState(t, h, fx.groupID, map[string]any{"state": "screening"})
	var resp2 stateResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp2.FoundElsewhereFirst {
		t.Error("found_elsewhere_first must stay true on a later transition that doesn't set it")
	}
}

func TestState_RealTransitionInsertsHistoryEvent(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})

	var count int
	err := pool.QueryRow(context.Background(), `
		select count(*) from user_job_state_event where job_group_id = $1::uuid
	`, fx.groupID).Scan(&count)
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	if count != 2 {
		t.Errorf("history event count = %d, want 2 (new->viewed, viewed->saved)", count)
	}
}

func TestState_FeedAndDetailReflectCurrentState(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 60})

	w := doDetail(t, h, fx.groupID)
	var before detailResponse
	_ = json.Unmarshal(w.Body.Bytes(), &before)
	if before.State != "new" {
		t.Errorf("initial State = %q, want %q", before.State, "new")
	}

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})

	w2 := doDetail(t, h, fx.groupID)
	var after detailResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &after)
	if after.State != "saved" {
		t.Errorf("State after save = %q, want %q", after.State, "saved")
	}

	resp := doList(t, h, "min_priority=1&limit=200&company_id="+fx.companyID)
	found := false
	for _, item := range resp.Data {
		if item.JobGroupID == fx.groupID {
			found = true
			if item.State != "saved" {
				t.Errorf("feed item State = %q, want %q", item.State, "saved")
			}
		}
	}
	if !found {
		t.Fatal("fixture job not found in feed")
	}
}

// TestState_RatingOutOfRangeIsUnprocessable: the schema's CHECK
// constraint used to be the only guard, which turned a rating of 9 into
// a 500 "internal error" — the caller learned nothing about what was
// wrong. Both bounds, plus the in-range case to prove the check isn't
// simply rejecting every rating.
func TestState_RatingOutOfRangeIsUnprocessable(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	for _, tc := range []struct {
		rating int
		want   int
	}{
		{rating: 0, want: 422},
		{rating: 6, want: 422},
		{rating: 3, want: 200},
	} {
		w := doState(t, h, fx.groupID, map[string]any{"state": "viewed", "rating": tc.rating})
		if w.Code != tc.want {
			t.Errorf("rating %d: status = %d, want %d, body = %s", tc.rating, w.Code, tc.want, w.Body.String())
		}
	}
}

// TestState_NotesTooLongIsUnprocessable bounds the one free-text field
// the API accepts — and checks that notes at the limit still round-trip,
// and that a follow-up transition without a notes field keeps them
// (the upsert's coalesce), which is what lets the detail page's editor
// and the Save/Apply buttons coexist on one row.
func TestState_NotesTooLongIsUnprocessable(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	tooLong := strings.Repeat("x", maxNotesBytes+1)
	if w := doState(t, h, fx.groupID, map[string]any{"state": "viewed", "notes": tooLong}); w.Code != 422 {
		t.Errorf("over-limit notes: status = %d, want 422, body = %s", w.Code, w.Body.String())
	}

	atLimit := strings.Repeat("y", maxNotesBytes)
	w := doState(t, h, fx.groupID, map[string]any{"state": "viewed", "notes": atLimit})
	if w.Code != 200 {
		t.Fatalf("at-limit notes: status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	// A later transition with no notes field must not clear them.
	w = doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	if w.Code != 200 {
		t.Fatalf("transition after notes: status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp stateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Notes == nil || *resp.Notes != atLimit {
		t.Errorf("notes were not preserved across a transition that omitted them")
	}
}
