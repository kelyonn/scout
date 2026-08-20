// Package resume implements the resume upload endpoint — the "switch
// resume in the app itself" feature (P3's first slice, backend-only for
// now, per the user's own choice: an API endpoint before any frontend).
//
// Upload flow: multipart PDF -> extract plain text (ledongthuc/pdf) ->
// extract ontology skills (packages/skills.Extract, the
// same tool the ingestion pipeline uses on job postings, not a separate
// heuristic) -> write resume.raw_text + user_profile.skills/skill_levels,
// transactionally, then enqueue an embed_resume job in that same
// transaction so an embedding is only ever queued for a write that
// actually committed (packages/queue's own EnqueueEmbed/EnqueueBrainDeep
// reasoning, applied here).
package resume

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ledongthuc/pdf"

	db "github.com/kelyon/scout/packages/db/gen"
	"github.com/kelyon/scout/packages/queue"
	"github.com/kelyon/scout/packages/skills"
	"github.com/kelyon/scout/packages/taxonomy"
)

// maxUploadBytes bounds the multipart body — generous for a resume PDF
// (a handful of pages is a few hundred KB at most) while refusing to hold
// an unbounded request body in memory.
const maxUploadBytes = 10 << 20 // 10 MiB

// defaultNewSkillLevel is assigned to any ontology skill the new resume
// mentions that wasn't already in user_profile.skill_levels — Extract only
// reports presence, not proficiency, and 3 ("solid working knowledge") is
// the honest middle ground between "this must be a 5, they wrote it down"
// and "this must be a 0, we can't prove it," matching the same default
// used when this profile's skills were first hand-populated.
const defaultNewSkillLevel = 3

// Handler serves the resume upload/status endpoints.
type Handler struct {
	pool     *pgxpool.Pool
	queue    *queue.Client
	log      *slog.Logger
	skillSet []taxonomy.Skill
}

// New constructs a Handler. Taxonomy is loaded once at construction, same
// posture apps/collector/cmd's buildPipeline takes for the same data.
func New(pool *pgxpool.Pool, q *queue.Client, log *slog.Logger) *Handler {
	return &Handler{
		pool:     pool,
		queue:    q,
		log:      log,
		skillSet: taxonomy.LoadSkills(),
	}
}

type uploadResponse struct {
	OK               bool     `json:"ok"`
	Skills           []string `json:"skills"`
	SkillsAdded      []string `json:"skills_added"`
	SkillsRemoved    []string `json:"skills_removed"`
	RawTextLength    int      `json:"raw_text_length"`
	EmbeddingQueued  bool     `json:"embedding_queued"`
	ResumeUpdatedUTC string   `json:"resume_updated_at"`
}

// Upload handles POST /v1/resume: a multipart/form-data request with the
// PDF in a field named "resume".
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	file, _, err := r.FormFile("resume")
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart file field named \"resume\"")
		return
	}
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read upload: "+err.Error())
		return
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "uploaded file is empty")
		return
	}

	text, err := extractPDFText(body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read this as a PDF: "+err.Error())
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		writeError(w, http.StatusUnprocessableEntity, "PDF parsed but contained no extractable text — likely a scanned image, not real text")
		return
	}

	ctx := r.Context()
	q := db.New(h.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("resume upload: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	profile, err := q.GetUserProfile(ctx, user.ID)
	if err != nil {
		h.log.Error("resume upload: get user profile failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	newSkills := skills.Extract(text, h.skillSet)
	newLevels, added, removed := mergeSkillLevels(profile.Skills, profile.SkillLevels, newSkills)

	levelsJSON, err := json.Marshal(newLevels)
	if err != nil {
		h.log.Error("resume upload: marshal skill levels failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.log.Error("resume upload: begin tx failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	updated, err := qtx.UpsertResume(ctx, db.UpsertResumeParams{UserID: user.ID, RawText: text})
	if err != nil {
		h.log.Error("resume upload: upsert resume failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := qtx.UpdateUserProfileSkills(ctx, db.UpdateUserProfileSkillsParams{
		UserID: user.ID, Skills: newSkills, SkillLevels: levelsJSON,
	}); err != nil {
		h.log.Error("resume upload: update profile skills failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := h.queue.EnqueueEmbedResume(ctx, tx); err != nil {
		h.log.Error("resume upload: enqueue embed_resume failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		h.log.Error("resume upload: commit failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.log.Info("resume upload: replaced resume", "skills_added", len(added), "skills_removed", len(removed))

	writeJSON(w, http.StatusOK, uploadResponse{
		OK:               true,
		Skills:           newSkills,
		SkillsAdded:      added,
		SkillsRemoved:    removed,
		RawTextLength:    len(text),
		EmbeddingQueued:  true,
		ResumeUpdatedUTC: updated.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

type statusResponse struct {
	HasResume     bool     `json:"has_resume"`
	HasEmbedding  bool     `json:"has_embedding"`
	Skills        []string `json:"skills"`
	RawTextLength int      `json:"raw_text_length"`
	UpdatedAtUTC  string   `json:"updated_at,omitempty"`
}

// Status handles GET /v1/resume: what's currently loaded, without dumping
// the full resume text back over the wire.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := db.New(h.pool)

	user, err := q.GetSoleUser(ctx)
	if err != nil {
		h.log.Error("resume status: get sole user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	profile, err := q.GetUserProfile(ctx, user.ID)
	if err != nil {
		h.log.Error("resume status: get user profile failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resumeRow, err := q.GetResume(ctx, user.ID)
	if err != nil {
		// No resume uploaded yet is a real, expected state — not an error.
		writeJSON(w, http.StatusOK, statusResponse{HasResume: false, Skills: profile.Skills})
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{
		HasResume:     true,
		HasEmbedding:  resumeRow.HasEmbedding,
		Skills:        profile.Skills,
		RawTextLength: len(resumeRow.RawText),
		UpdatedAtUTC:  resumeRow.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// extractPDFText reconstructs readable text from ledongthuc/pdf's lowest-
// level API, page.Content().Text — one entry per glyph, in content-stream
// order, each with its own X/Y/W from the actual text-positioning matrix
// at that point. Deliberately not GetPlainText or GetTextByRow: both
// concatenate glyph runs without reconstructing the spacing between them,
// and on a densely, justified two-column resume PDF that glues real prose
// into one unbroken run — "...same primitives Docker is built..." becomes
// "...primitivesDockerisbuilt...". That breaks every \b-anchored
// packages/skills.Extract match on a word straddling a join. Found live
// against a real upload: 12 of 32 real skills (Docker, Kubernetes-
// adjacent terms among them) silently missing from the extracted list.
// GetTextByRow turned out to have the identical problem — its own
// row-bucketing produced the same glued output on this PDF, which is why
// this function drops to Content() and does its own line/word
// reconstruction from raw glyph positions instead of trusting either.
func extractPDFText(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}

	var sb strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		writeGlyphsWithSpacing(&sb, page.Content().Text)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// yLineEpsilon is how much a glyph's Y can differ from the previous one
// before this function treats it as a new line rather than the same
// baseline — a few points absorbs ordinary sub-pixel jitter within one
// line of text without merging two visually distinct lines together.
const yLineEpsilon = 1.5

// wordGapFraction is the fraction of the current glyph's own width that
// the gap to the previous glyph's right edge must exceed before this
// function inserts a space — a proportional, font-size-relative threshold
// rather than a fixed point value, since a resume mixes body text and
// larger headings whose natural inter-letter spacing differs. Tuned
// against the real fixture this bug was found on
// (fixtures/real_resume_sample.pdf): low enough to still split "Docker"
// from "is" (a real word gap), high enough not to split kerned pairs
// within one word.
const wordGapFraction = 0.35

// writeGlyphsWithSpacing walks Content().Text in its original
// content-stream order (already left-to-right, top-to-bottom for ordinary
// typeset flow — the order text-showing operators were emitted in),
// starting a new output line on a Y jump and inserting a space wherever
// the horizontal gap between consecutive glyphs is wide relative to glyph
// size, the same "gap implies a word boundary" heuristic most PDF text
// extractors use when a document (as many LaTeX/typesetting-tool outputs
// do) never emits an explicit space glyph at all, relying purely on
// positioning for the visual gap.
func writeGlyphsWithSpacing(sb *strings.Builder, glyphs []pdf.Text) {
	var prevX, prevY, prevW float64
	have := false
	for _, g := range glyphs {
		s := filterGlyph(g.S)
		if s == "" {
			// A glyph that decoded to nothing meaningful (see filterGlyph)
			// still occupies its own space on the line — treat it exactly
			// like whitespace rather than dropping it silently, so the
			// gap it represents still separates the words on either side.
			s = " "
		}
		switch {
		case !have:
			// first glyph on the page, nothing to compare against
		case absFloat(g.Y-prevY) > yLineEpsilon:
			sb.WriteString("\n")
		case g.X-prevX > prevW+wordGapFraction*maxFloat(g.W, prevW):
			sb.WriteString(" ")
		}
		sb.WriteString(s)
		prevX, prevY, prevW = g.X, g.Y, g.W
		have = true
	}
}

// filterGlyph drops characters that are almost certainly font-encoding
// artifacts rather than real content — found live in
// fixtures/real_resume_sample.pdf: U+2126 (Ω) and U+2297 (⊗) appearing
// at line-ends and around "|" separators, exactly where an invisible
// LaTeX-template layout/alignment glyph (a tab stop, a rule) would sit.
// Neither codepoint has any plausible reason to appear in real resume
// prose, so rather than hand-list every such artifact this project might
// ever encounter, everything outside a positive allowlist — ASCII
// printable, common "smart" punctuation, and a handful of symbols
// ordinary prose actually uses — is treated the same way: as nothing,
// letting the caller substitute a space (a real separator character,
// which empirically is what these artifacts' position always turns out
// to represent).
func filterGlyph(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if isAllowedRune(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isAllowedRune(r rune) bool {
	switch {
	case r >= 0x20 && r <= 0x7E: // ASCII printable, including plain space
		return true
	case r == '\n' || r == '\t':
		return true
	}
	switch r {
	case '–', '—', // en dash, em dash
		'‘', '’', '“', '”', // smart single/double quotes
		'→', '←', '↔', // arrows, used in e.g. "pitch → lightness"
		'•', '·', '®', '™', '€', '£', '¥', '°', '×', '÷':
		return true
	}
	return false
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// mergeSkillLevels replaces oldSkills entirely with newSkills (see
// UpdateUserProfileSkills's own comment on why this is a replace, not a
// merge), carrying over the existing proficiency level for any skill
// present in both, and defaulting genuinely new skills to
// defaultNewSkillLevel. Returns the added/removed sets for the response.
func mergeSkillLevels(oldSkills []string, oldLevelsJSON []byte, newSkills []string) (map[string]int, []string, []string) {
	oldLevels := map[string]int{}
	_ = json.Unmarshal(oldLevelsJSON, &oldLevels) // malformed/absent -> empty map, every skill treated as new

	oldSet := make(map[string]bool, len(oldSkills))
	for _, s := range oldSkills {
		oldSet[s] = true
	}
	newSet := make(map[string]bool, len(newSkills))
	for _, s := range newSkills {
		newSet[s] = true
	}

	newLevels := make(map[string]int, len(newSkills))
	var added []string
	for _, s := range newSkills {
		if level, ok := oldLevels[s]; ok {
			newLevels[s] = level
		} else {
			newLevels[s] = defaultNewSkillLevel
			added = append(added, s)
		}
	}

	var removed []string
	for _, s := range oldSkills {
		if !newSet[s] {
			removed = append(removed, s)
		}
	}

	return newLevels, added, removed
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
