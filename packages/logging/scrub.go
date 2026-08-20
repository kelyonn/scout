// Package logging wraps an slog.Handler with the scrubbing middleware
// docs/16-observability.md section 6 requires: "Never logged: resume
// content, full job descriptions, email addresses, phone numbers, session
// tokens, API keys, notification channel credentials. A scrubbing
// middleware enforces this."
//
// This is a backstop, not the primary defense. The primary defense is
// AGENTS.md rule 7 itself — reference IDs instead of PII, enforced by
// review (see e.g. ADR-015's own note about logging a Tailscale identity
// header as a truncated SHA-256 fingerprint rather than the address it
// actually is). Full job descriptions and resume content have no reliable
// regex signature, so this package cannot catch a call site that logs
// job.description_text or resume.raw_text directly — nothing can, short of
// a type system that marks those fields unloggable. What it does catch:
// email addresses, phone numbers, and bearer-token/API-key-shaped strings,
// wherever they appear in a log line, including ones no reviewer noticed.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
)

const redacted = "[REDACTED]"

var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// Deliberately tuned toward AGENTS.md's own documented primary market
	// (Indian numbers, "8 LPA" territory) plus a generic international
	// fallback. Requires either a leading "+" or at least one internal
	// separator (space/dash/dot) between digit groups — a bare, unbroken
	// run of digits is deliberately NOT matched, because this codebase
	// logs plenty of those legitimately (UUIDs, content hashes) and they
	// are not phone numbers. A real phone number logged with no country
	// code and no separator at all (just ten bare digits) slips through;
	// that gap is accepted in exchange for not corrupting every source_id
	// and content_hash in production logs.
	phonePattern = regexp.MustCompile(`\+\d{1,3}[-.\s]?\d{2,5}(?:[-.\s]?\d{2,5}){1,3}\b|\b\d{2,5}[-.\s]\d{2,5}(?:[-.\s]\d{2,5})*\b`)

	// Bearer tokens and the common hosted-provider secret-key prefixes
	// this project's own LLM cascade uses (scout_brain/llm.py: Gemini,
	// Groq/OpenRouter's OpenAI-compatible key shape).
	tokenPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-]{10,}\b|\bsk-[A-Za-z0-9]{10,}\b|\bgh[opsu]_[A-Za-z0-9]{10,}\b`)
)

func scrubString(s string) string {
	s = emailPattern.ReplaceAllString(s, redacted)
	s = tokenPattern.ReplaceAllString(s, redacted)
	s = phonePattern.ReplaceAllString(s, redacted)
	return s
}

// scrubValue scrubs a slog.Value in place, recursing into groups (the
// shape slog.Group / WithGroup produce) so a PII string nested inside a
// grouped attr is caught the same as a top-level one.
func scrubValue(v slog.Value) slog.Value {
	switch v.Kind() {
	case slog.KindString:
		return slog.StringValue(scrubString(v.String()))
	case slog.KindGroup:
		attrs := v.Group()
		scrubbed := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			scrubbed[i] = slog.Attr{Key: a.Key, Value: scrubValue(a.Value)}
		}
		return slog.GroupValue(scrubbed...)
	default:
		// Numeric, bool, time, duration, and any other typed value passes
		// through untouched — scrubbing only ever applies to strings,
		// which is where an accidentally-logged email/phone/token would
		// actually appear.
		return v
	}
}

func scrubAttrs(attrs []slog.Attr) []slog.Attr {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = slog.Attr{Key: a.Key, Value: scrubValue(a.Value)}
	}
	return scrubbed
}

// scrubbingHandler wraps another slog.Handler, scrubbing the message and
// every attribute (both call-site attrs and ones attached via
// Logger.With) before they reach it.
type scrubbingHandler struct {
	next slog.Handler
}

// Scrub wraps handler with the PII-scrubbing middleware. Every Go
// service's slog.Logger construction should go through this — see
// apps/api, apps/collector, and apps/notifier's cmd/main.go.
func Scrub(handler slog.Handler) slog.Handler {
	return &scrubbingHandler{next: handler}
}

func (h *scrubbingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *scrubbingHandler) Handle(ctx context.Context, record slog.Record) error {
	scrubbed := slog.NewRecord(record.Time, record.Level, scrubString(record.Message), record.PC)
	record.Attrs(func(a slog.Attr) bool {
		scrubbed.AddAttrs(slog.Attr{Key: a.Key, Value: scrubValue(a.Value)})
		return true
	})
	if err := h.next.Handle(ctx, scrubbed); err != nil {
		return fmt.Errorf("logging: scrub: %w", err)
	}
	return nil
}

func (h *scrubbingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &scrubbingHandler{next: h.next.WithAttrs(scrubAttrs(attrs))}
}

func (h *scrubbingHandler) WithGroup(name string) slog.Handler {
	return &scrubbingHandler{next: h.next.WithGroup(name)}
}
