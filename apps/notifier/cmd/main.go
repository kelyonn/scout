// Command notifier delivers notifications across channels.
//
// Placeholder: does nothing yet. Delivery lands in M1 (Telegram) and M3
// (native push, Web Push, email) — see docs/11-notifications.md.
//
// Two guarantees live in this binary and must not be weakened: the unique index
// on (user_id, job_group_id, trigger), and the suppress_notifications guard for
// backfills, which belongs here at the notifier rather than at the caller.
package main

import (
	"log/slog"
	"os"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log.Info("notifier placeholder: not implemented until M1")
}
