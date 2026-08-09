package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchAndFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alive")

	t.Run("a missing file is not fresh", func(t *testing.T) {
		// The state right after a restart, and the one that must read as
		// unhealthy rather than as an error the healthcheck swallows.
		if err := Fresh(path, time.Minute); err == nil {
			t.Fatal("Fresh returned nil for a file that does not exist")
		}
	})

	t.Run("a just-touched file is fresh", func(t *testing.T) {
		if err := Touch(path); err != nil {
			t.Fatalf("Touch: %v", err)
		}
		if err := Fresh(path, time.Minute); err != nil {
			t.Fatalf("Fresh: %v", err)
		}
	})

	t.Run("a stale file is not fresh", func(t *testing.T) {
		old := time.Now().Add(-30 * time.Minute)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
		if err := Fresh(path, time.Minute); err == nil {
			t.Fatal("Fresh returned nil for a 30-minute-old file")
		}
	})

	t.Run("touching again refreshes it", func(t *testing.T) {
		if err := Touch(path); err != nil {
			t.Fatalf("Touch: %v", err)
		}
		if err := Fresh(path, time.Minute); err != nil {
			t.Fatalf("Fresh after re-touch: %v", err)
		}
	})
}

func TestTouchWithNoPathIsANoOp(t *testing.T) {
	// The local case: no liveness file configured, and nothing should be
	// written to the working directory as a side effect.
	if err := Touch(""); err != nil {
		t.Fatalf("Touch(\"\"): %v", err)
	}
}

func TestTouchReportsAnUnwritablePath(t *testing.T) {
	// The container healthcheck reads this file, so a silent write failure would
	// present as a healthy collector that Docker can never restart.
	path := filepath.Join(t.TempDir(), "no-such-dir", "alive")
	if err := Touch(path); err == nil {
		t.Fatal("Touch returned nil for an unwritable path")
	}
}
