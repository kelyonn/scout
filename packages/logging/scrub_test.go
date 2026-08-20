package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// newScrubbedLogger returns a logger backed by a scrubbing JSON handler
// writing to buf, so a test can assert on the exact bytes that reached
// the sink — the same property docs/16-observability.md section 6
// requires: "a test that seeds known PII patterns through the logger and
// asserts they never reach the sink."
func newScrubbedLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(Scrub(slog.NewJSONHandler(buf, nil)))
}

func TestScrub_RedactsEmailInMessage(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("failed to process resume for kalyan15122005@gmail.com")

	out := buf.String()
	if strings.Contains(out, "kalyan15122005@gmail.com") {
		t.Errorf("email reached the sink: %s", out)
	}
	if !strings.Contains(out, redacted) {
		t.Errorf("expected redaction marker in output: %s", out)
	}
}

func TestScrub_RedactsEmailInAttr(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("politeness gate contact", "operator_email", "ops@example.com")

	out := buf.String()
	if strings.Contains(out, "ops@example.com") {
		t.Errorf("email reached the sink via an attr: %s", out)
	}
}

func TestScrub_RedactsPhoneNumber(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("contact on file", "phone", "+91 8792894576")

	out := buf.String()
	if strings.Contains(out, "8792894576") {
		t.Errorf("phone number reached the sink: %s", out)
	}
}

func TestScrub_RedactsBearerToken(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("outbound request", "authorization", "Bearer sk-abcdefghijklmnopqrstuvwxyz123456")

	out := buf.String()
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz123456") {
		t.Errorf("bearer token reached the sink: %s", out)
	}
}

func TestScrub_RedactsInsideGroupedAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("event",
		slog.Group("request",
			slog.String("from", "sender@example.org"),
		),
	)

	out := buf.String()
	if strings.Contains(out, "sender@example.org") {
		t.Errorf("email inside a grouped attr reached the sink: %s", out)
	}
}

func TestScrub_RedactsAttrsAttachedViaWith(t *testing.T) {
	var buf bytes.Buffer
	base := newScrubbedLogger(&buf)
	log := base.With("contact_email", "attached@example.com")
	log.Info("some event")

	out := buf.String()
	if strings.Contains(out, "attached@example.com") {
		t.Errorf("email attached via With() reached the sink: %s", out)
	}
}

func TestScrub_LeavesOrdinaryFieldsAlone(t *testing.T) {
	var buf bytes.Buffer
	log := newScrubbedLogger(&buf)
	log.Info("source fetched",
		"source_id", "0192b7c4-1234-4abc-9def-000000000001",
		"source_kind", "ats_greenhouse",
		"http_status", 200,
		"items", 47,
		"duration_ms", 352,
	)

	out := buf.String()
	for _, want := range []string{"0192b7c4-1234-4abc-9def-000000000001", "ats_greenhouse", "\"http_status\":200", "\"items\":47", "\"duration_ms\":352"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q to survive scrubbing untouched, got: %s", want, out)
		}
	}
	if strings.Contains(out, redacted) {
		t.Errorf("no PII present, but a redaction marker appeared: %s", out)
	}
}

func TestScrub_EnabledDelegatesToNext(t *testing.T) {
	var buf bytes.Buffer
	handler := Scrub(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled should delegate to the wrapped handler's level filter")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Enabled(Warn) should be true when the wrapped handler is configured for Warn")
	}
}
