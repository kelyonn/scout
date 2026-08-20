// Package realtime is the shared contract between the two sides of
// docs/04-api-design.md section 4.3's SSE stream: apps/collector publishes
// via Postgres NOTIFY (NotifyJobNew below), apps/api/internal/stream
// subscribes via LISTEN and fans out to connected clients. Neither side
// imports the other — this package exists only so the channel name and
// envelope shape aren't a magic string duplicated in two places.
package realtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Channel is the single Postgres NOTIFY channel every event type is
// published on, distinguished by Envelope.Event rather than one Postgres
// channel per event type — simpler for apps/api/internal/stream's Broker
// to LISTEN on one channel than to multiplex N.
const Channel = "scout_events"

// Envelope is the JSON payload passed to pg_notify — Postgres caps a
// NOTIFY payload at 8000 bytes, which every event this system emits stays
// comfortably under (job.new's data is a handful of scalar fields).
type Envelope struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// JobNew is job.new's data shape — docs/04-api-design.md section 4.3's
// literal example: {"job_group_id":"...","priority":91,"title":"..."}.
// job.score_updated and notification.sent (also documented there) have no
// write path yet — rescoring and the notifier's own send path — so this
// is the only event type actually published today; adding another is the
// same two-line shape plus a NotifyXxx function, not a new mechanism.
type JobNew struct {
	JobGroupID string `json:"job_group_id"`
	Priority   int16  `json:"priority"`
	Title      string `json:"title"`
}

// NotifyJobNew publishes a job.new event via pg_notify. tx is expected to
// be a transaction the caller commits itself — Postgres only delivers a
// NOTIFY sent inside a transaction once that transaction's outer commit
// succeeds, which is exactly the guarantee wanted here: a job the write
// then rolls back must never reach a connected client as "new."
func NotifyJobNew(ctx context.Context, tx pgx.Tx, data JobNew) error {
	raw, err := json.Marshal(Envelope{Event: "job.new", Data: data})
	if err != nil {
		return fmt.Errorf("realtime: marshal job.new: %w", err)
	}
	if _, err := tx.Exec(ctx, "select pg_notify($1, $2)", Channel, string(raw)); err != nil {
		return fmt.Errorf("realtime: notify job.new: %w", err)
	}
	return nil
}
