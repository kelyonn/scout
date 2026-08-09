package scheduler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"
)

func TestToSource(t *testing.T) {
	crawlDelay := float32(3.5)
	openUntil := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	row := db.SelectDueSourcesRow{
		ID:                pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true},
		Kind:              db.SourceKindAtsGreenhouse,
		Url:               "https://boards.greenhouse.io/example",
		MaxRps:            0.5,
		MaxConcurrency:    2,
		RobotsCrawlDelayS: &crawlDelay,
		CircuitOpenUntil:  pgtype.Timestamptz{Time: openUntil, Valid: true},
		LegalPosture:      db.LegalPosturePermitted,
	}

	src := toSource(row)

	if src.Kind != string(db.SourceKindAtsGreenhouse) {
		t.Errorf("Kind = %q, want %q", src.Kind, db.SourceKindAtsGreenhouse)
	}
	if src.URL != row.Url {
		t.Errorf("URL = %q, want %q", src.URL, row.Url)
	}
	if src.MaxRPS != 0.5 {
		t.Errorf("MaxRPS = %v, want 0.5", src.MaxRPS)
	}
	if src.MaxConcurrency != 2 {
		t.Errorf("MaxConcurrency = %v, want 2", src.MaxConcurrency)
	}
	if src.RobotsCrawlDelayS == nil || *src.RobotsCrawlDelayS != 3.5 {
		t.Errorf("RobotsCrawlDelayS = %v, want 3.5", src.RobotsCrawlDelayS)
	}
	if src.CircuitOpenUntil == nil || !src.CircuitOpenUntil.Equal(openUntil) {
		t.Errorf("CircuitOpenUntil = %v, want %v", src.CircuitOpenUntil, openUntil)
	}
	if string(src.LegalPosture) != string(db.LegalPosturePermitted) {
		t.Errorf("LegalPosture = %q, want %q", src.LegalPosture, db.LegalPosturePermitted)
	}
}

func TestToSourceHandlesNulls(t *testing.T) {
	// A source with no Crawl-delay and a closed circuit — the common case for
	// a healthy, freshly-registered source.
	row := db.SelectDueSourcesRow{
		ID:                pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		Kind:              db.SourceKindRss,
		Url:               "https://example.com/feed.xml",
		RobotsCrawlDelayS: nil,
		CircuitOpenUntil:  pgtype.Timestamptz{}, // not valid: NULL
	}

	src := toSource(row)

	if src.RobotsCrawlDelayS != nil {
		t.Errorf("RobotsCrawlDelayS = %v, want nil", src.RobotsCrawlDelayS)
	}
	if src.CircuitOpenUntil != nil {
		t.Errorf("CircuitOpenUntil = %v, want nil", src.CircuitOpenUntil)
	}
}

func TestDerefAndNullableStrRoundTrip(t *testing.T) {
	t.Run("nil pointer derefs to empty string", func(t *testing.T) {
		if got := derefStr(nil); got != "" {
			t.Errorf("derefStr(nil) = %q, want empty", got)
		}
	})

	t.Run("empty string becomes nil, not a pointer to empty", func(t *testing.T) {
		// This distinction matters: nullableStr("") -> nil writes SQL NULL via
		// UpdateSourceAfterPoll's sqlc.narg columns, not an empty string. The
		// two mean different things for last_etag: NULL is "no ETag ever
		// seen"; "" would be a (nonsensical) empty ETag value.
		got := nullableStr("")
		if got != nil {
			t.Errorf("nullableStr(\"\") = %v, want nil", got)
		}
	})

	t.Run("a non-empty string round-trips through both directions", func(t *testing.T) {
		want := `"abc123"`
		ptr := nullableStr(want)
		if ptr == nil {
			t.Fatal("nullableStr returned nil for a non-empty string")
		}
		if got := derefStr(ptr); got != want {
			t.Errorf("round trip = %q, want %q", got, want)
		}
	})
}

func TestTimestamptzRoundTrip(t *testing.T) {
	t.Run("nil round-trips to an invalid Timestamptz and back to nil", func(t *testing.T) {
		tz := toTimestamptz(nil)
		if tz.Valid {
			t.Error("toTimestamptz(nil).Valid = true, want false (SQL NULL)")
		}
		if got := fromTimestamptz(tz); got != nil {
			t.Errorf("fromTimestamptz(toTimestamptz(nil)) = %v, want nil", got)
		}
	})

	t.Run("a real time round-trips exactly", func(t *testing.T) {
		want := time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)
		got := fromTimestamptz(toTimestamptz(&want))
		if got == nil || !got.Equal(want) {
			t.Errorf("round trip = %v, want %v", got, want)
		}
	})
}

func TestDaysSinceLastActivity(t *testing.T) {
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	t.Run("no last_changed_at yet is treated as maximally recent", func(t *testing.T) {
		got := daysSinceLastActivity(pgtype.Timestamptz{}, now)
		if got != 0 {
			t.Errorf("daysSinceLastActivity(NULL) = %v, want 0", got)
		}
	})

	t.Run("a real timestamp computes the elapsed days", func(t *testing.T) {
		then := pgtype.Timestamptz{Time: now.Add(-72 * time.Hour), Valid: true}
		got := daysSinceLastActivity(then, now)
		if got < 2.99 || got > 3.01 {
			t.Errorf("daysSinceLastActivity(72h ago) = %v, want ~3", got)
		}
	})

	t.Run("a future timestamp (clock skew) clamps to zero rather than going negative", func(t *testing.T) {
		future := pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}
		got := daysSinceLastActivity(future, now)
		if got != 0 {
			t.Errorf("daysSinceLastActivity(future) = %v, want 0", got)
		}
	})
}

func TestRngForProducesDistinctSequencesPerSource(t *testing.T) {
	idA := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	idB := pgtype.UUID{Bytes: [16]byte{4, 5, 6}, Valid: true}

	rngA := rngFor(idA)
	rngB := rngFor(idB)

	// Not a statistical test — just confirms two different sources do not draw
	// from an accidentally-shared or trivially-correlated generator.
	same := true
	for i := 0; i < 10; i++ {
		if rngA.Uint64() != rngB.Uint64() {
			same = false
			break
		}
	}
	if same {
		t.Error("rngFor(idA) and rngFor(idB) produced identical sequences")
	}
}

func TestRngForIsSafeForConcurrentCalls(t *testing.T) {
	// The property that actually matters for RunOnce's worker pool: calling
	// rngFor concurrently from many goroutines must not race, because each
	// call constructs its own generator rather than sharing one.
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(n int) {
			id := pgtype.UUID{Bytes: [16]byte{byte(n)}, Valid: true}
			r := rngFor(id)
			_ = r.Uint64()
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}
