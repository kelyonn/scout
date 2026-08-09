package heartbeat

import (
	"fmt"
	"os"
	"time"
)

// DefaultLivenessMaxAge is how stale the liveness file may be before the
// container is considered unhealthy.
//
// It is deliberately longer than the ping interval, so one slow cycle does not
// restart a working collector, and shorter than the dead-man's switch grace
// period, so Docker gets a chance to fix a stuck process before healthchecks.io
// wakes anyone up.
const DefaultLivenessMaxAge = 7 * time.Minute

// Touch records that a cycle completed, by writing the current time to path.
//
// This exists because the service images have no shell and no curl — a
// distroless container cannot run the usual `CMD-SHELL curl -f localhost/health`
// healthcheck. A file the process keeps touching, checked by the same binary via
// its `healthcheck` subcommand, gives Docker something true to test on a process
// that serves no HTTP.
//
// It is not the heartbeat table from docs/15 section 2. That check —
// "heartbeat row age < 60s" — needs a database client and arrives with polling,
// at which point it supersedes this: a row written by the poll loop proves the
// collector is *polling*, where this file proves only that it is ticking.
func Touch(path string) error {
	if path == "" {
		return nil
	}
	// Truncate-and-write rather than os.Chtimes: the file may not exist yet on
	// the first cycle after a restart, and a fresh tmpfs is the normal case.
	if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return fmt.Errorf("touch liveness file: %w", err)
	}
	return nil
}

// Fresh reports an error if path is missing or older than maxAge.
func Fresh(path string, maxAge time.Duration) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat liveness file: %w", err)
	}

	if age := time.Since(info.ModTime()); age > maxAge {
		return fmt.Errorf("liveness file is %s old, limit %s", age.Round(time.Second), maxAge)
	}
	return nil
}
