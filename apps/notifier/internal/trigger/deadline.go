package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

// deadlineT72hWindow/deadlineT24hWindow are docs/11 section 2's two
// reminder points for a tracked job's deadline. Both are checked on every
// sweep, independent of each other — a job already inside the T-24h window
// is also inside the T-72h window, so both inserts are attempted every
// time; notification_dedup_idx (via InsertNotification's ON CONFLICT DO
// NOTHING) is what turns "attempted every sweep" into "delivered exactly
// once per reminder," the same idempotency pattern the rest of this
// package already relies on.
const (
	deadlineT72hWindow = 72 * time.Hour
	deadlineT24hWindow = 24 * time.Hour
)

// DeadlineSweeper implements docs/11 section 2's deadline_approaching
// trigger. It is a separate type from Evaluator rather than a third
// SelectUnnotifiedJobGroups case because its data source is fundamentally
// different: Evaluator asks "is this newly-scored job_group eligible,"
// evaluated once per group ever; DeadlineSweeper asks "is any currently-
// tracked job's deadline close," evaluated fresh every sweep against a
// small, time-varying set (SelectDeadlineReminderCandidates), with no
// priority threshold at all — docs/11's own words are "any saved job."
type DeadlineSweeper struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	now  func() time.Time // overridable for tests; defaults to time.Now
}

// NewDeadlineSweeper returns a DeadlineSweeper.
func NewDeadlineSweeper(pool *pgxpool.Pool, log *slog.Logger) *DeadlineSweeper {
	return &DeadlineSweeper{pool: pool, log: log, now: time.Now}
}

// Run attempts both reminders for every tracked job with an approaching
// deadline. suppressNotifications mirrors Evaluator.Run's own guard
// (AGENTS.md rule 3) — a backfill or rescore must not fire deadline
// reminders either, even though nothing sets this true yet.
func (s *DeadlineSweeper) Run(ctx context.Context, userID pgtype.UUID, suppressNotifications bool) (evaluated, fired int, err error) {
	if suppressNotifications {
		return 0, 0, nil
	}

	q := db.New(s.pool)
	rows, err := q.SelectDeadlineReminderCandidates(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("deadline: select candidates: %w", err)
	}

	now := s.now()
	for _, row := range rows {
		evaluated++
		remaining := row.DeadlineAt.Time.Sub(now)

		if remaining <= deadlineT72hWindow {
			if s.insert(ctx, q, userID, row, db.NotificationTriggerDeadlineT72h) {
				fired++
			}
		}
		if remaining <= deadlineT24hWindow {
			if s.insert(ctx, q, userID, row, db.NotificationTriggerDeadlineT24h) {
				fired++
			}
		}
	}

	return evaluated, fired, nil
}

func (s *DeadlineSweeper) insert(
	ctx context.Context, q *db.Queries, userID pgtype.UUID, row db.SelectDeadlineReminderCandidatesRow, triggerName db.NotificationTrigger,
) bool {
	deadlineAt := row.DeadlineAt.Time
	payload, err := json.Marshal(toDeadlinePayload(row, deadlineAt))
	if err != nil {
		s.log.Warn("marshal deadline payload failed", "job_group_id", row.JobGroupID.String(), "err", err)
		return false
	}

	_, err = q.InsertNotification(ctx, db.InsertNotificationParams{
		UserID:     userID,
		JobGroupID: row.JobGroupID,
		Trigger:    triggerName,
		Urgency:    "instant",
		Payload:    payload,
		// docs/11's threshold for this trigger is "any saved job" — there
		// is no priority to report (SelectDeadlineReminderCandidates
		// doesn't join job_score), and 0 is inert everywhere priority_at_send
		// is actually read (deliver.go's bengaluruOverrideApplies only
		// checks trigger == "bengaluru_match").
		PriorityAtSend: 0,
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, pgx.ErrNoRows):
		return false
	default:
		s.log.Warn("insert deadline notification failed", "job_group_id", row.JobGroupID.String(), "trigger", triggerName, "err", err)
		return false
	}
}

func toDeadlinePayload(row db.SelectDeadlineReminderCandidatesRow, deadlineAt time.Time) Payload {
	p := Payload{Title: row.Title, CompanyName: row.CompanyName, ApplyURL: row.ApplyUrl, DeadlineAt: &deadlineAt}
	if row.LocationCity != nil {
		p.LocationCity = *row.LocationCity
	}
	if row.CompNormalizedInrMonth.Valid {
		if f, err := row.CompNormalizedInrMonth.Float64Value(); err == nil && f.Valid {
			v := int64(f.Float64)
			p.CompNormalizedINRMonth = &v
		}
	}
	return p
}
