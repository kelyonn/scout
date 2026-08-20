package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/queue"
)

// testPool mirrors apps/notifier/internal/trigger's helper of the same
// name — skip rather than fail so `go test ./...` stays green without the
// local stack running.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	candidates := []string{
		os.Getenv("SCOUT_TEST_DATABASE_URL"),
		"postgres://scout:scout_local_dev_only@localhost:5433/scout?sslmode=disable",
		"postgres://scout:scout_ci@localhost:5432/scout?sslmode=disable",
	}
	for _, url := range candidates {
		if url == "" {
			continue
		}
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err = pool.Ping(ctx)
		cancel()
		if err != nil {
			pool.Close()
			continue
		}
		t.Cleanup(pool.Close)
		return pool
	}
	t.Skip("no reachable Postgres (set SCOUT_TEST_DATABASE_URL, or run `make dev-db`); skipping resume handler tests")
	return nil
}

// snapshotAndRestore protects the real, single seeded user's actual
// resume/skills (ADR-015: there is only ever one) from a test run that
// necessarily writes through this exact user, the same way
// apps/notifier/internal/trigger's testUser reuses that one real row
// rather than creating a throwaway one. Whatever is there before the test
// runs is put back afterward, regardless of pass or fail.
func snapshotAndRestore(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	var hadResume bool
	var rawText string
	err := pool.QueryRow(ctx, `select raw_text from resume where user_id = $1::uuid`, userID).Scan(&rawText)
	if err == nil {
		hadResume = true
	}

	var skills []string
	var skillLevels []byte
	if err := pool.QueryRow(ctx, `select skills, skill_levels from user_profile where user_id = $1::uuid`, userID).
		Scan(&skills, &skillLevels); err != nil {
		t.Fatalf("snapshot user_profile: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		if hadResume {
			_, err := pool.Exec(ctx, `
				update resume set raw_text = $1, embedding = NULL, embedding_version = NULL, updated_at = now()
				where user_id = $2::uuid
			`, rawText, userID)
			if err != nil {
				t.Errorf("restore resume: %v", err)
			}
		} else {
			_, _ = pool.Exec(ctx, `delete from resume where user_id = $1::uuid`, userID)
		}
		_, err := pool.Exec(ctx, `
			update user_profile set skills = $1, skill_levels = $2, updated_at = now() where user_id = $3::uuid
		`, skills, skillLevels, userID)
		if err != nil {
			t.Errorf("restore user_profile: %v", err)
		}
	})
}

func soleUserID(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	q := db.New(pool)
	user, err := q.GetSoleUser(context.Background())
	if err != nil {
		t.Skipf("no seeded app_user (run `make seed`); skipping: %v", err)
	}
	return user.ID
}

func testHandler(pool *pgxpool.Pool) *Handler {
	q, err := queue.New(pool)
	if err != nil {
		panic(err)
	}
	return New(pool, q, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func multipartResumeRequest(t *testing.T, pdfBytes []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("resume", "resume.pdf")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(pdfBytes); err != nil {
		t.Fatalf("write pdf bytes: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/resume", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestUpload_RealPDFEndToEnd(t *testing.T) {
	pool := testPool(t)
	userID := soleUserID(t, pool)
	snapshotAndRestore(t, pool, userID)

	pdfBytes, err := os.ReadFile("fixtures/sample_resume.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	h := testHandler(pool)
	w := httptest.NewRecorder()
	h.Upload(w, multipartResumeRequest(t, pdfBytes))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp uploadResponse
	if decodeErr := json.Unmarshal(w.Body.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if !resp.OK {
		t.Error("expected ok = true")
	}
	if !resp.EmbeddingQueued {
		t.Error("expected embedding_queued = true")
	}

	want := map[string]bool{"go": false, "python": false, "kubernetes": false, "docker": false}
	for _, s := range resp.Skills {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for skill, found := range want {
		if !found {
			t.Errorf("expected %q in extracted skills, got %v", skill, resp.Skills)
		}
	}

	// Verify the actual DB state, not just the response — the response
	// could be right while the write silently failed to land.
	var rawText string
	var embeddingIsNull bool
	err = pool.QueryRow(context.Background(),
		`select raw_text, embedding is null from resume where user_id = $1::uuid`, userID).
		Scan(&rawText, &embeddingIsNull)
	if err != nil {
		t.Fatalf("query resume: %v", err)
	}
	if rawText == "" || len(rawText) < 20 {
		t.Errorf("raw_text looks empty/truncated: %q", rawText)
	}
	if !embeddingIsNull {
		t.Error("embedding should be reset to NULL on upload, ready for embed_resume to recompute it")
	}

	var queuedCount int
	err = pool.QueryRow(context.Background(),
		`select count(*) from river_job where kind = 'embed_resume' and created_at >= now() - interval '1 minute'`).
		Scan(&queuedCount)
	if err != nil {
		t.Fatalf("query river_job: %v", err)
	}
	if queuedCount == 0 {
		t.Error("expected an embed_resume job to have been enqueued")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river_job where kind = 'embed_resume'`)
	})
}

func TestUpload_RejectsMissingField(t *testing.T) {
	pool := testPool(t)
	_ = soleUserID(t, pool) // still needs a seeded user for GetSoleUser not to be the first failure

	h := testHandler(pool)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.Close()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/resume", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	h.Upload(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpload_RejectsNonPDF(t *testing.T) {
	pool := testPool(t)
	_ = soleUserID(t, pool)

	h := testHandler(pool)
	w := httptest.NewRecorder()
	h.Upload(w, multipartResumeRequest(t, []byte("this is not a pdf, just plain text")))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestStatus_ReflectsCurrentResume(t *testing.T) {
	pool := testPool(t)
	userID := soleUserID(t, pool)
	snapshotAndRestore(t, pool, userID)

	pdfBytes, err := os.ReadFile("fixtures/sample_resume.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	h := testHandler(pool)
	uploadW := httptest.NewRecorder()
	h.Upload(uploadW, multipartResumeRequest(t, pdfBytes))
	if uploadW.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", uploadW.Code, uploadW.Body.String())
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river_job where kind = 'embed_resume'`)
	})

	statusW := httptest.NewRecorder()
	h.Status(statusW, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/resume", nil))

	if statusW.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", statusW.Code, statusW.Body.String())
	}
	var resp statusResponse
	if err := json.Unmarshal(statusW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasResume {
		t.Error("expected has_resume = true after a successful upload")
	}
	if resp.HasEmbedding {
		t.Error("expected has_embedding = false — the upload reset it, nothing has recomputed it yet")
	}
	if resp.RawTextLength == 0 {
		t.Error("expected a non-zero raw_text_length")
	}
}

func TestExtractPDFText_RealFixture(t *testing.T) {
	pdfBytes, err := os.ReadFile("fixtures/sample_resume.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	text, err := extractPDFText(pdfBytes)
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	for _, want := range []string{"Go", "Python", "Kubernetes", "Docker"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Errorf("expected extracted text to contain %q, got: %s", want, text)
		}
	}
}

// TestExtractPDFText_PreservesWordBoundariesOnDenseLayout is the
// regression test for a real bug: the library's own GetPlainText
// concatenated glyph runs with no inter-word spacing on this densely
// laid-out, justified two-column PDF, gluing e.g. "...same primitives
// Docker is built..." into "...primitivesDockerisbuilt...". That broke
// every \b-anchored packages/skills.Extract match on a word straddling a
// join — found live: 12 of 32 real skills silently missing from an actual
// upload's extracted skill list before extractPDFText moved to
// GetTextByRow + gap-based spacing. This fixture is the real PDF that
// exposed it, kept (with only the contact line's phone/email having no
// bearing on the assertions below) because a synthetic fixture generated
// by a different renderer does not reliably reproduce the same
// tight-kerning layout that caused the bug.
func TestExtractPDFText_PreservesWordBoundariesOnDenseLayout(t *testing.T) {
	pdfBytes, err := os.ReadFile("fixtures/real_resume_sample.pdf")
	if err != nil {
		t.Skipf("real_resume_sample.pdf fixture not present: %v", err)
	}
	text, err := extractPDFText(pdfBytes)
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}

	// Every one of these is a real skill from this resume that the old
	// GetPlainText-based extraction dropped because word boundaries were
	// glued away. \b-style check via strings.Fields: the word must appear
	// as its own token, not merely as a substring of some larger glued run.
	wantWords := map[string]bool{
		"Docker": false, "Kubernetes": false, "SQL": false, "Merkle": false,
		"Zero-Trust": false, "cgroups": false,
	}
	for _, field := range strings.Fields(text) {
		trimmed := strings.Trim(field, ".,;:()[]{}\"'|")
		if _, ok := wantWords[trimmed]; ok {
			wantWords[trimmed] = true
		}
	}
	for word, found := range wantWords {
		if !found {
			t.Errorf("expected %q to appear as its own word in extracted text (word-boundary regression) — got text:\n%s", word, text)
		}
	}
}

func TestExtractPDFText_RejectsGarbage(t *testing.T) {
	if _, err := extractPDFText([]byte("not a pdf at all")); err == nil {
		t.Error("expected an error for non-PDF bytes")
	}
}

func TestMergeSkillLevels_CarriesOverExistingLevels(t *testing.T) {
	oldSkills := []string{"go", "python"}
	oldLevels := []byte(`{"go": 5, "python": 4}`)
	newSkills := []string{"go", "python"}

	levels, added, removed := mergeSkillLevels(oldSkills, oldLevels, newSkills)

	if levels["go"] != 5 || levels["python"] != 4 {
		t.Errorf("expected existing levels carried over unchanged, got %v", levels)
	}
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("expected no additions/removals, got added=%v removed=%v", added, removed)
	}
}

func TestMergeSkillLevels_NewSkillGetsDefaultLevel(t *testing.T) {
	oldSkills := []string{"go"}
	oldLevels := []byte(`{"go": 5}`)
	newSkills := []string{"go", "rust"}

	levels, added, removed := mergeSkillLevels(oldSkills, oldLevels, newSkills)

	if levels["rust"] != defaultNewSkillLevel {
		t.Errorf("levels[rust] = %d, want default %d", levels["rust"], defaultNewSkillLevel)
	}
	if len(added) != 1 || added[0] != "rust" {
		t.Errorf("added = %v, want [rust]", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

func TestMergeSkillLevels_DroppedSkillIsReportedRemoved(t *testing.T) {
	oldSkills := []string{"go", "react", "webgl"}
	oldLevels := []byte(`{"go": 5, "react": 2, "webgl": 3}`)
	newSkills := []string{"go"}

	levels, added, removed := mergeSkillLevels(oldSkills, oldLevels, newSkills)

	if _, ok := levels["react"]; ok {
		t.Error("react should not appear in the new levels map — it's not in the new resume")
	}
	if _, ok := levels["webgl"]; ok {
		t.Error("webgl should not appear in the new levels map — it's not in the new resume")
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}
	wantRemoved := map[string]bool{"react": true, "webgl": true}
	if len(removed) != 2 {
		t.Fatalf("removed = %v, want 2 entries", removed)
	}
	for _, r := range removed {
		if !wantRemoved[r] {
			t.Errorf("unexpected entry in removed: %q", r)
		}
	}
}

func TestMergeSkillLevels_MalformedOldLevelsTreatsEverythingAsNew(t *testing.T) {
	levels, added, _ := mergeSkillLevels([]string{}, []byte(`not json`), []string{"go"})
	if levels["go"] != defaultNewSkillLevel {
		t.Errorf("levels[go] = %d, want default %d", levels["go"], defaultNewSkillLevel)
	}
	if len(added) != 1 {
		t.Errorf("added = %v, want [go]", added)
	}
}
