package companies

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWatchlist_AddThenAppearsInList(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	name := "Watchlist Add Co " + slug(t)
	companyID := insertCompanyWithJob(t, pool, name, "open")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from watchlist where company_id = $1::uuid`, companyID)
	})

	body, _ := json.Marshal(addWatchlistRequest{CompanyID: companyID})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/me/watchlist", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.AddToWatchlist(w, r)

	if w.Code != 204 {
		t.Fatalf("add status = %d, body = %s", w.Code, w.Body.String())
	}

	lr := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/me/watchlist", nil)
	lw := httptest.NewRecorder()
	h.Watchlist(lw, lr)

	if lw.Code != 200 {
		t.Fatalf("list status = %d, body = %s", lw.Code, lw.Body.String())
	}
	var resp watchlistResponse
	if err := json.Unmarshal(lw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var found *watchlistItem
	for i := range resp.Data {
		if resp.Data[i].CompanyID == companyID {
			found = &resp.Data[i]
		}
	}
	if found == nil {
		t.Fatal("added company does not appear in the watchlist")
	}
	if !found.NotifyAnyRole {
		t.Error("notify_any_role should default true")
	}
	if found.OpenJobCount != 1 {
		t.Errorf("open_job_count = %d, want 1", found.OpenJobCount)
	}
}

func TestWatchlist_AddIsIdempotentAndUpdatesNotifyAnyRole(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	name := "Watchlist Upsert Co " + slug(t)
	companyID := insertCompanyWithJob(t, pool, name, "open")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from watchlist where company_id = $1::uuid`, companyID)
	})

	post := func(notifyAnyRole bool) int {
		body, _ := json.Marshal(addWatchlistRequest{CompanyID: companyID, NotifyAnyRole: &notifyAnyRole})
		r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/me/watchlist", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.AddToWatchlist(w, r)
		return w.Code
	}

	if code := post(true); code != 204 {
		t.Fatalf("first add status = %d", code)
	}
	if code := post(false); code != 204 {
		t.Fatalf("second add (upsert) status = %d", code)
	}

	lr := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/me/watchlist", nil)
	lw := httptest.NewRecorder()
	h.Watchlist(lw, lr)
	var resp watchlistResponse
	if err := json.Unmarshal(lw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var rows int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from watchlist where company_id = $1::uuid`, companyID).Scan(&rows); err != nil {
		t.Fatalf("count watchlist rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("watchlist rows for company = %d, want exactly 1 (re-adding must not duplicate the PK)", rows)
	}

	for _, item := range resp.Data {
		if item.CompanyID == companyID && item.NotifyAnyRole {
			t.Error("second add with notify_any_role=false should have updated the existing row, not left it true")
		}
	}
}

func TestWatchlist_AddUnknownCompanyIs404(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	body, _ := json.Marshal(addWatchlistRequest{CompanyID: "00000000-0000-0000-0000-000000000000"})
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/me/watchlist", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.AddToWatchlist(w, r)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestWatchlist_RemoveRemovesEntry(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	name := "Watchlist Remove Co " + slug(t)
	companyID := insertCompanyWithJob(t, pool, name, "open")

	body, _ := json.Marshal(addWatchlistRequest{CompanyID: companyID})
	ar := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/me/watchlist", bytes.NewReader(body))
	aw := httptest.NewRecorder()
	h.AddToWatchlist(aw, ar)
	if aw.Code != 204 {
		t.Fatalf("add status = %d", aw.Code)
	}

	dr := httptest.NewRequestWithContext(context.Background(), "DELETE", "/v1/me/watchlist/"+companyID, nil)
	dr.SetPathValue("company_id", companyID)
	dw := httptest.NewRecorder()
	h.RemoveFromWatchlist(dw, dr)
	if dw.Code != 204 {
		t.Fatalf("remove status = %d, body = %s", dw.Code, dw.Body.String())
	}

	var rows int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from watchlist where company_id = $1::uuid`, companyID).Scan(&rows); err != nil {
		t.Fatalf("count watchlist rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("watchlist rows for company after remove = %d, want 0", rows)
	}
}

// TestWatchlist_RemoveAbsentEntryIsStill204 — docs/12 section 5's "undo
// over confirm" posture: removing something already absent isn't an
// error, same as Dismiss/Save elsewhere in this API.
func TestWatchlist_RemoveAbsentEntryIsStill204(t *testing.T) {
	pool := testPool(t)
	h := testHandler(pool)

	name := "Watchlist Never Added Co " + slug(t)
	companyID := insertCompanyWithJob(t, pool, name, "open")

	dr := httptest.NewRequestWithContext(context.Background(), "DELETE", "/v1/me/watchlist/"+companyID, nil)
	dr.SetPathValue("company_id", companyID)
	dw := httptest.NewRecorder()
	h.RemoveFromWatchlist(dw, dr)
	if dw.Code != 204 {
		t.Errorf("status = %d, want 204 even though the entry never existed", dw.Code)
	}
}
