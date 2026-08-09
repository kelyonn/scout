package changedetect

import (
	"strings"
	"testing"
)

func TestNormalizeStripsHTMLComments(t *testing.T) {
	in := `<div>jobs<!-- rendered at 2026-08-09 --></div>`
	got := string(Normalize([]byte(in)))
	if strings.Contains(got, "rendered") {
		t.Errorf("Normalize did not strip the comment: %q", got)
	}
}

func TestNormalizeStripsISODates(t *testing.T) {
	tests := []string{
		`"posted_at": "2026-08-09"`,
		`"posted_at": "2026-08-09T09:15:00Z"`,
		`"posted_at": "2026-08-09T09:15:00.123456Z"`,
		`"posted_at": "2026-08-09T09:15:00+05:30"`,
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			got := string(Normalize([]byte(in)))
			if strings.Contains(got, "2026") {
				t.Errorf("Normalize(%q) = %q, still contains the date", in, got)
			}
		})
	}
}

func TestNormalizeStripsRFC1123Dates(t *testing.T) {
	in := "Last-Modified: Wed, 06 Aug 2026 09:15:00 GMT"
	got := string(Normalize([]byte(in)))
	if strings.Contains(got, "2026") || strings.Contains(got, "GMT") {
		t.Errorf("Normalize did not strip the RFC1123 date: %q", got)
	}
}

func TestNormalizeStripsMonthNameDates(t *testing.T) {
	tests := []string{
		"Posted on August 9, 2026",
		"Posted on Aug 9, 2026",
		"Posted on 9 August 2026",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			got := string(Normalize([]byte(in)))
			if strings.Contains(got, "2026") {
				t.Errorf("Normalize(%q) = %q, still contains the date", in, got)
			}
		})
	}
}

func TestNormalizeStripsCSRFAndSessionTokens(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"csrf attribute", `<input name="csrf_token" value="a1b2c3d4e5">`},
		{"bare csrf", `csrf: "a1b2c3d4e5"`},
		{"nonce", `<script nonce="a1b2c3d4e5">`},
		{"underscore token", `_token=a1b2c3d4e5`},
		{"session id", `sessionid=a1b2c3d4e5abc`},
		{"session_id with underscore", `session_id: "a1b2c3d4e5"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Normalize([]byte(tt.in)))
			if strings.Contains(got, "a1b2c3d4e5") {
				t.Errorf("Normalize(%q) = %q, still contains the token", tt.in, got)
			}
		})
	}
}

func TestNormalizeDoesNotStripLegitimateContentMentioningTokens(t *testing.T) {
	// docs/06 scopes the strip rules to csrf/nonce/_token/session identifiers
	// specifically, not any occurrence of the word "token" — a job posting
	// for an API platform role is exactly the kind of content that would be
	// wrongly gutted by an overbroad pattern.
	in := "We are hiring an engineer to build our API token service."
	got := string(Normalize([]byte(in)))
	if !strings.Contains(got, "API token service") {
		t.Errorf("Normalize incorrectly stripped legitimate content: %q", got)
	}
}

func TestNormalizeStripsRelativeTimeStrings(t *testing.T) {
	tests := []string{
		"posted 2 hours ago",
		"posted 3 minutes ago",
		"posted 1 day ago",
		"posted yesterday",
		"posted today",
		"posted just now",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			got := string(Normalize([]byte(in)))
			if got == in || strings.Contains(got, "ago") ||
				strings.Contains(got, "yesterday") || strings.Contains(got, "today") {
				t.Errorf("Normalize(%q) = %q, relative time not stripped", in, got)
			}
		})
	}
}

func TestNormalizeCollapsesWhitespace(t *testing.T) {
	in := "12   jobs\n\nfound"
	got := string(Normalize([]byte(in)))
	if got != "12 jobs found" {
		t.Errorf("Normalize whitespace collapse = %q, want %q", got, "12 jobs found")
	}
}

// TestNormalizeTheDocumentedExample is the exact case docs/06 section 6 names:
// "many career pages embed... a '12 jobs found · updated 3 minutes ago'
// string. Without stripping those, every single poll produces a different
// hash and Layer 2 never fires."
func TestNormalizeTheDocumentedExample(t *testing.T) {
	first := `<div>12 jobs found · updated 3 minutes ago</div><ul><li>Backend Engineer</li></ul>`
	second := `<div>12 jobs found · updated 7 minutes ago</div><ul><li>Backend Engineer</li></ul>`

	normFirst, normSecond := string(Normalize([]byte(first))), string(Normalize([]byte(second)))
	if normFirst != normSecond {
		t.Fatalf("normalized forms differ:\n  first:  %q\n  second: %q", normFirst, normSecond)
	}
}

func TestHashIsStableAcrossVolatileNoise(t *testing.T) {
	// The actual property Layer 2 exists to provide: two fetches of a page
	// whose only differences are rendering-timestamp noise must hash equal.
	first := []byte(`{"updated":"2026-08-09T09:15:00Z","jobs":[{"title":"Backend Engineer"}]}`)
	second := []byte(`{"updated":"2026-08-09T09:20:00Z","jobs":[{"title":"Backend Engineer"}]}`)

	h1, h2 := Hash(first), Hash(second)
	if string(h1) != string(h2) {
		t.Errorf("hashes differ despite only volatile content changing")
	}
}

func TestHashChangesWithRealContentChanges(t *testing.T) {
	// The complementary property: Layer 2 must not become so aggressive at
	// stripping noise that it stops detecting an actual new posting.
	first := []byte(`{"jobs":[{"title":"Backend Engineer"}]}`)
	second := []byte(`{"jobs":[{"title":"Backend Engineer"},{"title":"Frontend Engineer"}]}`)

	h1, h2 := Hash(first), Hash(second)
	if string(h1) == string(h2) {
		t.Error("hashes are equal despite a real content change")
	}
}

func TestHashLength(t *testing.T) {
	// Must match source.last_content_hash's BYTEA storage with no encoding
	// step — 32 raw bytes, the sha256 digest size.
	h := Hash([]byte("anything"))
	if len(h) != 32 {
		t.Errorf("len(Hash(...)) = %d, want 32", len(h))
	}
}

func TestChanged(t *testing.T) {
	body := []byte(`{"jobs":[{"title":"Backend Engineer"}]}`)
	otherBody := []byte(`{"jobs":[{"title":"Frontend Engineer"}]}`)

	t.Run("empty last hash always reports changed", func(t *testing.T) {
		if !Changed(body, nil) {
			t.Error("Changed with a nil lastHash = false, want true (first poll ever)")
		}
		if !Changed(body, []byte{}) {
			t.Error("Changed with an empty lastHash = false, want true")
		}
	})

	t.Run("matching hash reports unchanged", func(t *testing.T) {
		if Changed(body, Hash(body)) {
			t.Error("Changed = true for an identical body")
		}
	})

	t.Run("differing content reports changed", func(t *testing.T) {
		if !Changed(otherBody, Hash(body)) {
			t.Error("Changed = false for genuinely different content")
		}
	})

	t.Run("volatile-only differences report unchanged", func(t *testing.T) {
		withTimestampV1 := []byte(`{"updated":"2026-08-09T09:15:00Z","jobs":[{"title":"Backend Engineer"}]}`)
		withTimestampV2 := []byte(`{"updated":"2026-08-09T09:20:00Z","jobs":[{"title":"Backend Engineer"}]}`)
		if Changed(withTimestampV2, Hash(withTimestampV1)) {
			t.Error("Changed = true despite only a timestamp differing")
		}
	})
}

func TestNormalizeNeverPanics(t *testing.T) {
	inputs := []string{
		"",
		"\x00\x01\xff\xfe binary garbage",
		strings.Repeat("<!-- unterminated comment", 3),
		strings.Repeat("2026-08-09 ", 1000),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Normalize(%q) panicked: %v", in, r)
				}
			}()
			Normalize([]byte(in))
		}()
	}
}
