package jobs

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func doApplications(t *testing.T, h *Handler, rawQuery string) applicationsResponse {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/applications?"+rawQuery, nil)
	w := httptest.NewRecorder()
	h.Applications(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp applicationsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func containsGroupIDApp(items []applicationItem, groupID string) bool {
	for _, it := range items {
		if it.JobGroupID == groupID {
			return true
		}
	}
	return false
}

func TestApplications_OnlyReturnsJobsWithTrackedState(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	tracked := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	untracked := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, tracked.groupID, map[string]any{"state": "viewed"})
	doState(t, h, tracked.groupID, map[string]any{"state": "saved"})

	resp := doApplications(t, h, "")

	if !containsGroupIDApp(resp.Data, tracked.groupID) {
		t.Error("expected the saved job in /applications with no state filter")
	}
	if containsGroupIDApp(resp.Data, untracked.groupID) {
		t.Error("did not expect a job with no user_job_state row in /applications")
	}
}

func TestApplications_FiltersByState(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	saved := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	applied := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, saved.groupID, map[string]any{"state": "viewed"})
	doState(t, h, saved.groupID, map[string]any{"state": "saved"})

	doState(t, h, applied.groupID, map[string]any{"state": "viewed"})
	doState(t, h, applied.groupID, map[string]any{"state": "saved"})
	doState(t, h, applied.groupID, map[string]any{"state": "applied"})

	resp := doApplications(t, h, "state=applied")

	if !containsGroupIDApp(resp.Data, applied.groupID) {
		t.Error("expected the applied job when filtering state=applied")
	}
	if containsGroupIDApp(resp.Data, saved.groupID) {
		t.Error("did not expect the merely-saved job when filtering state=applied")
	}
}

func TestApplications_RepeatedStateParamIsOred(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	saved := insertJobFixture(t, pool, fixtureOpts{priority: 50})
	applied := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, saved.groupID, map[string]any{"state": "viewed"})
	doState(t, h, saved.groupID, map[string]any{"state": "saved"})

	doState(t, h, applied.groupID, map[string]any{"state": "viewed"})
	doState(t, h, applied.groupID, map[string]any{"state": "saved"})
	doState(t, h, applied.groupID, map[string]any{"state": "applied"})

	resp := doApplications(t, h, "state=saved&state=applied")

	if !containsGroupIDApp(resp.Data, saved.groupID) || !containsGroupIDApp(resp.Data, applied.groupID) {
		t.Errorf("expected both saved and applied jobs with state=saved&state=applied, got %d items", len(resp.Data))
	}
}

// TestApplications_SurvivesJobClosing is the one behavior that actually
// distinguishes this endpoint from GET /v1/jobs: an in-flight or
// historical application must not disappear from the Pipeline/Archive
// view just because the underlying job listing closed.
func TestApplications_SurvivesJobClosing(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)
	fx := insertJobFixture(t, pool, fixtureOpts{priority: 50})

	doState(t, h, fx.groupID, map[string]any{"state": "viewed"})
	doState(t, h, fx.groupID, map[string]any{"state": "saved"})
	doState(t, h, fx.groupID, map[string]any{"state": "applied"})

	if _, err := pool.Exec(context.Background(), `update job set status = 'closed' where id = $1::uuid`, fx.jobID); err != nil {
		t.Fatalf("close job: %v", err)
	}

	resp := doApplications(t, h, "")
	if !containsGroupIDApp(resp.Data, fx.groupID) {
		t.Error("expected an applied job to remain in /applications after the listing closed")
	}

	// The job detail page must also still resolve — an archived
	// application shouldn't 404 just because the posting closed.
	w := doDetail(t, h, fx.groupID)
	if w.Code != 200 {
		t.Errorf("detail for a closed-but-tracked job: status = %d, want 200", w.Code)
	}
}
