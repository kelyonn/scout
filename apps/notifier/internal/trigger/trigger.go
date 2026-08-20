// Package trigger implements docs/11-notifications.md section 2's trigger
// evaluation for five of the seven per-job triggers: bengaluru_match,
// high_score, remote_high_quality, newgrad_match (all in Evaluator), and
// deadline_approaching (DeadlineSweeper, in deadline.go — a data source
// and evaluation shape different enough from the other four that it isn't
// a fifth SelectUnnotifiedJobGroups case; see that type's own doc
// comment). deadline_approaching is actually two trigger values,
// deadline_t72h/deadline_t24h — docs/11 wants it to fire twice per job
// (T-72h and T-24h) and notification_dedup_idx allows exactly one
// notification per (job_group, trigger) ever, so a single trigger value
// can't express both firings without weakening that guarantee; see
// infra/migrations/000016's own comment. The remaining two
// (watchlist_hiring, prestige_opening) need data this pass doesn't have —
// a user-managed watchlist and a curated prestige-company registry,
// genuinely new features rather than gaps in existing data. `digest` is
// not evaluated as a per-job trigger at all — see
// apps/notifier/internal/digest, which runs once daily rather than per
// scored job.
package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"
)

// Thresholds are docs/11 section 2's table, verbatim — bengaluru_match's
// lower bar than high_score is deliberate, not a bug: the location
// multiplier already boosts a Bengaluru job's score, and the lower trigger
// threshold compounds that, which is the documented intent for a stated
// hard preference.
const (
	bengaluruMatchThreshold    int16 = 78
	highScoreThreshold         int16 = 88
	remoteHighQualityThreshold int16 = 82
	newgradMatchThreshold      int16 = 80
	bengaluruTier1                   = 1
	remoteTier3                      = 3

	batchLimit = 200
)

// Payload is what a notification's rendering needs — deliver.go's Telegram
// formatter reads this back out. Kept intentionally small; docs/11 section
// 6.1's richer message (skills matched, missing skills, time-to-apply
// estimate) needs data this pass doesn't compute yet.
type Payload struct {
	Title                  string     `json:"title"`
	CompanyName            string     `json:"company_name,omitempty"`
	ApplyURL               string     `json:"apply_url"`
	LocationCity           string     `json:"location_city,omitempty"`
	CompNormalizedINRMonth *int64     `json:"comp_normalized_inr_month,omitempty"`
	DeadlineAt             *time.Time `json:"deadline_at,omitempty"`
}

// Evaluator runs docs/11 section 8 steps 2-4 (eligibility, trigger
// evaluation, dedup insert) for one user.
type Evaluator struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	now  func() time.Time // overridable for tests; defaults to time.Now
}

// New returns an Evaluator.
func New(pool *pgxpool.Pool, log *slog.Logger) *Evaluator {
	return &Evaluator{pool: pool, log: log, now: time.Now}
}

// Run evaluates every unnotified, eligible job_group for userID under
// weightVersion. suppressNotifications, when true, is AGENTS.md rule 3's
// guard: a trigger that would otherwise fire is skipped (job_group is
// still marked notified, so it is not re-evaluated forever), and no
// notification row is written. Nothing in this codebase sets
// suppressNotifications true yet — there is no backfill or rescore feature
// in P1 — but the guard itself must be provably correct before either
// feature exists, which is what this parameter and its dedicated tests are
// for.
func (e *Evaluator) Run(ctx context.Context, userID pgtype.UUID, weightVersion string, suppressNotifications bool) (evaluated, fired int, err error) {
	q := db.New(e.pool)

	rows, err := q.SelectUnnotifiedJobGroups(ctx, db.SelectUnnotifiedJobGroupsParams{
		UserID: userID, WeightVersion: weightVersion, Limit: batchLimit,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("trigger: select unnotified job groups: %w", err)
	}

	for _, row := range rows {
		evaluated++

		triggerName, ok := evaluateTrigger(row)
		if ok && !suppressNotifications {
			didFire, insertErr := e.insertNotification(ctx, q, userID, row, triggerName)
			if insertErr != nil {
				e.log.Warn("insert notification failed", "job_group_id", row.JobGroupID.String(), "err", insertErr)
			} else if didFire {
				fired++
			}
		}

		if err := q.MarkJobGroupNotified(ctx, row.JobGroupID); err != nil {
			e.log.Warn("mark job_group notified failed", "job_group_id", row.JobGroupID.String(), "err", err)
		}
	}

	return evaluated, fired, nil
}

// evaluateTrigger implements docs/11 section 2's trigger table, in the
// order listed there ("evaluated in order, first match wins") — a
// Bengaluru, remote-ineligible-by-definition role scoring 92 fires
// bengaluru_match, never high_score or anything after it.
func evaluateTrigger(row db.SelectUnnotifiedJobGroupsRow) (name string, ok bool) {
	isBengaluru := row.LocationTier != nil && *row.LocationTier == bengaluruTier1
	// docs/07 section 6's tiering table defines Tier 3 as exactly "remote,
	// India-eligible" (a US company hiring globally-remote is Tier 3, not
	// Tier 4) — location_tier == 3 already IS the eligibility check, not a
	// proxy for it, so no separate work_mode/country clause is needed here.
	isRemoteEligible := row.LocationTier != nil && *row.LocationTier == remoteTier3
	isNewGrad := row.Seniority == db.SeniorityNewGrad && slices.Contains(row.TargetSeniority, string(db.SeniorityNewGrad))

	switch {
	case isBengaluru && row.Priority >= bengaluruMatchThreshold:
		return "bengaluru_match", true
	case row.Priority >= highScoreThreshold:
		return "high_score", true
	case isRemoteEligible && row.Priority >= remoteHighQualityThreshold:
		return "remote_high_quality", true
	// watchlist_hiring, prestige_opening: no watchlist/prestige registry
	// exists yet — see package comment.
	case isNewGrad && row.Priority >= newgradMatchThreshold:
		return "newgrad_match", true
	default:
		return "", false
	}
}

// urgencyFor is docs/11 section 3's per-trigger urgency class. Every
// trigger this package evaluates is INSTANT except newgrad_match, which is
// BATCHED — "accumulated and delivered hourly on the hour" — the one
// urgency class below INSTANT that P1's Deliverer actually implements
// (apps/notifier/internal/deliver's own sendBatch); DIGEST belongs to
// apps/notifier/internal/digest, a separate daily process, not this table.
func urgencyFor(triggerName string) string {
	if triggerName == "newgrad_match" {
		return "batched"
	}
	return "instant"
}

// istLocation matches apps/notifier/internal/deliver and
// apps/notifier/internal/digest's own load of the same fixed frame of
// reference — docs/11's quiet hours and daily digest are both wall-clock
// IST, and "hourly on the hour" means the same IST wall clock, not UTC.
var istLocation = mustLoadIST()

func mustLoadIST() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// A missing tzdata entry is a container/build bug, not a runtime
		// condition — same posture apps/notifier/internal/deliver's own
		// identical load takes.
		panic("trigger: could not load Asia/Kolkata: " + err.Error())
	}
	return loc
}

// nextHourBoundary is docs/11 section 3's BATCHED delivery clock: a
// notification inserted at any point in an hour accumulates until that
// hour's close, then delivers with everything else queued in the same
// window. Ceiling rather than floor — a notification inserted exactly on
// an IST hour boundary is the only case that delivers on the same tick it
// was queued, everything else waits out its own hour.
//
// Computed against istLocation's wall clock rather than time.Time.Truncate:
// Truncate rounds against the Unix epoch in absolute time, which for a
// UTC+5:30 zone lands boundaries at :00 and :30 IST rather than on the
// actual IST hour — the same class of bug deliver.go's inQuietHours and
// digest.go's own IST arithmetic are already careful to avoid.
func nextHourBoundary(t time.Time) time.Time {
	ist := t.In(istLocation)
	floor := time.Date(ist.Year(), ist.Month(), ist.Day(), ist.Hour(), 0, 0, 0, istLocation)
	if floor.Equal(ist) {
		return t
	}
	return floor.Add(time.Hour)
}

// insertNotification is docs/11 section 8 step 4: INSERT, unique violation
// (here: zero rows returned by the ON CONFLICT DO NOTHING query) means
// already notified — not an error, just did not fire.
func (e *Evaluator) insertNotification(
	ctx context.Context, q *db.Queries, userID pgtype.UUID, row db.SelectUnnotifiedJobGroupsRow, triggerName string,
) (bool, error) {
	payload, err := json.Marshal(toPayload(row))
	if err != nil {
		return false, fmt.Errorf("marshal payload: %w", err)
	}

	urgency := urgencyFor(triggerName)
	var scheduledFor pgtype.Timestamptz
	if urgency == "batched" {
		scheduledFor = pgtype.Timestamptz{Time: nextHourBoundary(e.now()), Valid: true}
	}

	_, err = q.InsertNotification(ctx, db.InsertNotificationParams{
		UserID:         userID,
		JobGroupID:     row.JobGroupID,
		Trigger:        db.NotificationTrigger(triggerName),
		Urgency:        urgency,
		Payload:        payload,
		PriorityAtSend: row.Priority,
		ScheduledFor:   scheduledFor,
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("insert notification: %w", err)
	}
}

func toPayload(row db.SelectUnnotifiedJobGroupsRow) Payload {
	p := Payload{Title: row.Title, CompanyName: row.CompanyName, ApplyURL: row.ApplyUrl}
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
