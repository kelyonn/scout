// Package deliver implements docs/11-notifications.md section 8 steps 5-9
// for Telegram: budget check, quiet hours, render, send, record, plus
// section 3's BATCHED urgency class ("accumulated and delivered hourly on
// the hour") — a batched notification still passes the same per-item quiet
// hours/budget gate as an instant one (docs/11 section 3's table gives both
// classes the identical "queued until 07:30" quiet-hours behavior, and
// budgets count deliveries regardless of how they're rendered), but cleared
// batched items are grouped into a single Telegram message rather than sent
// individually — see sendBatch. Native push, Web Push, email, and Discord
// (section 5's other channels) are P3+ — Telegram is P1's only channel, per
// the roadmap.
package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/notifier/internal/telegram"
	"github.com/kelyon/scout/apps/notifier/internal/trigger"
	"github.com/kelyon/scout/packages/metrics"
)

// Budgets are docs/11 section 4's table. The per-company cap is not
// implemented (P1 has no per-company grouped-message renderer to demote
// into); per-hour and per-day and per-trigger-per-day are.
const (
	maxPerHour              = 8
	maxPerDay               = 25
	maxPerTriggerDay        = 10
	bengaluruOverride int16 = 92 // docs/11 section 3's quiet-hours exception
)

const deliveryBatchLimit = 200

// Deliverer sends queued notifications over Telegram, subject to quiet
// hours and budgets.
type Deliverer struct {
	pool     *pgxpool.Pool
	telegram *telegram.Client
	log      *slog.Logger
	now      func() time.Time // overridable for tests; defaults to time.Now
}

// New returns a Deliverer.
func New(pool *pgxpool.Pool, tg *telegram.Client, log *slog.Logger) *Deliverer {
	return &Deliverer{pool: pool, telegram: tg, log: log, now: time.Now}
}

// istLocation is docs/11's fixed frame of reference — quiet_hours_start/end
// are wall-clock IST, per docs/03 section 1 ("the user is in IST").
var istLocation = mustLoadIST()

func mustLoadIST() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// A missing tzdata entry is a container/build bug, not a runtime
		// condition — same posture packages/taxonomy's loaders take for a
		// malformed data file.
		panic("deliver: could not load Asia/Kolkata: " + err.Error())
	}
	return loc
}

// Run attempts delivery for every queued notification belonging to userID.
// A notification held by quiet hours or a budget is left queued (no
// notification_delivery row written) rather than dropped — docs/11 section
// 4: "budget overflow is a demotion, not a drop." P1 has no batched/digest
// renderer to demote into, so here "demotion" collapses to "try again next
// tick," which is the same outcome for INSTANT urgency once the window or
// budget resets — a documented simplification, not a silent gap.
func (d *Deliverer) Run(ctx context.Context, userID pgtype.UUID) (sent, queued int, err error) {
	q := db.New(d.pool)

	profile, err := q.GetUserProfile(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("deliver: get user profile: %w", err)
	}
	channel, err := q.GetTelegramChannel(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("deliver: get telegram channel: %w", err)
	}
	chatID, err := chatIDFromConfig(channel.Config)
	if err != nil {
		return 0, 0, fmt.Errorf("deliver: %w", err)
	}

	notifications, err := q.GetUndeliveredNotifications(ctx, db.GetUndeliveredNotificationsParams{
		UserID: userID, Limit: deliveryBatchLimit,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("deliver: get undelivered notifications: %w", err)
	}

	now := d.now()
	var batchReady []db.GetUndeliveredNotificationsRow
	for _, n := range notifications {
		if inQuietHours(now, profile) && !bengaluruOverrideApplies(n) {
			queued++
			continue
		}

		ok, budgetErr := d.withinBudget(ctx, q, userID, n.Trigger, now)
		if budgetErr != nil {
			d.log.Warn("budget check failed", "notification_id", n.ID.String(), "err", budgetErr)
			queued++
			continue
		}
		if !ok {
			queued++
			continue
		}

		// GetUndeliveredNotifications already filters scheduled_for <= now()
		// (packages/db/queries/notification.sql), so a batched row reaching
		// this point has already crossed its hour boundary — cleared for
		// quiet hours and budget the same as an instant one, just rendered
		// together below instead of sent here.
		if n.Urgency == "batched" {
			batchReady = append(batchReady, n)
			continue
		}

		if d.send(ctx, q, n, channel.ID, chatID, now) {
			sent++
		} else {
			queued++
		}
	}

	if len(batchReady) > 0 {
		if d.sendBatch(ctx, q, batchReady, channel.ID, chatID, now) {
			sent += len(batchReady)
		} else {
			queued += len(batchReady)
		}
	}

	return sent, queued, nil
}

func bengaluruOverrideApplies(n db.GetUndeliveredNotificationsRow) bool {
	return n.Trigger == "bengaluru_match" && n.PriorityAtSend != nil && *n.PriorityAtSend >= bengaluruOverride
}

// inQuietHours implements docs/11 section 3. start/end are wall-clock IST
// times; the common case (00:00-07:30) does not wrap past midnight, but
// the comparison handles a wrapping window too (e.g. 22:00-06:00) since
// nothing in the schema prevents a user from configuring one.
func inQuietHours(now time.Time, profile db.GetUserProfileRow) bool {
	if !profile.QuietHoursStart.Valid || !profile.QuietHoursEnd.Valid {
		return false
	}
	nowIST := now.In(istLocation)
	nowMicros := microsSinceMidnight(nowIST)
	start := profile.QuietHoursStart.Microseconds
	end := profile.QuietHoursEnd.Microseconds

	if start <= end {
		return nowMicros >= start && nowMicros < end
	}
	return nowMicros >= start || nowMicros < end
}

func microsSinceMidnight(t time.Time) int64 {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return t.Sub(midnight).Microseconds()
}

func (d *Deliverer) withinBudget(
	ctx context.Context, q *db.Queries, userID pgtype.UUID, trig db.NotificationTrigger, now time.Time,
) (bool, error) {
	hourAgo := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	dayAgo := pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true}

	hourly, err := q.CountDeliveriesSince(ctx, db.CountDeliveriesSinceParams{UserID: userID, Since: hourAgo})
	if err != nil {
		return false, fmt.Errorf("count hourly deliveries: %w", err)
	}
	if hourly >= maxPerHour {
		return false, nil
	}

	daily, err := q.CountDeliveriesSince(ctx, db.CountDeliveriesSinceParams{UserID: userID, Since: dayAgo})
	if err != nil {
		return false, fmt.Errorf("count daily deliveries: %w", err)
	}
	if daily >= maxPerDay {
		return false, nil
	}

	perTrigger, err := q.CountDeliveriesForTriggerSince(ctx, db.CountDeliveriesForTriggerSinceParams{
		UserID: userID, Trigger: trig, Since: dayAgo,
	})
	if err != nil {
		return false, fmt.Errorf("count per-trigger deliveries: %w", err)
	}
	if perTrigger >= maxPerTriggerDay {
		return false, nil
	}

	return true, nil
}

// send renders and delivers one notification, recording the outcome
// either way — a failed send still gets a notification_delivery row
// (status='failed'), which is what makes it show up in channel-health
// monitoring rather than silently retrying forever.
func (d *Deliverer) send(
	ctx context.Context, q *db.Queries, n db.GetUndeliveredNotificationsRow, channelID pgtype.UUID, chatID int64, now time.Time,
) bool {
	text, err := renderMessage(n, now)
	if err != nil {
		d.log.Warn("render message failed", "notification_id", n.ID.String(), "err", err)
		return false
	}

	msgID, sendErr := d.telegram.SendMessage(ctx, chatID, text)
	params := db.InsertNotificationDeliveryParams{
		NotificationID: n.ID,
		ChannelID:      channelID,
		Attempts:       1,
	}
	// docs/16-observability.md section 3.3, the primary SLO. Measured from
	// notification.created_at (when trigger evaluation fired) rather than
	// the job's posted_at the spec literally names — that earlier segment
	// (collector ingestion through trigger evaluation) isn't threaded
	// through to this call today, so this covers exactly the latency
	// apps/notifier itself controls (budget checks, quiet hours, the send
	// call), not the full pipeline. Recorded on both outcomes: a trigger
	// that never manages to send is exactly the SLO breach this metric
	// exists to catch, not a value to omit.
	latency := now.Sub(n.CreatedAt.Time)
	if sendErr != nil {
		errMsg := sendErr.Error()
		params.Status = "failed"
		params.Error = &errMsg
		metrics.NotificationTotal.WithLabelValues(string(n.Trigger), "failed").Inc()
		metrics.NotificationLatency.WithLabelValues(string(n.Trigger), "telegram").Observe(latency.Seconds())
	} else {
		latencyMs := int32(latency.Milliseconds()) //nolint:gosec // notification age in ms fits comfortably in int32
		params.Status = "sent"
		params.SentAt = pgtype.Timestamptz{Time: now, Valid: true}
		params.LatencyMs = &latencyMs
		params.ProviderMsgID = &msgID
		metrics.NotificationTotal.WithLabelValues(string(n.Trigger), "sent").Inc()
		metrics.NotificationLatency.WithLabelValues(string(n.Trigger), "telegram").Observe(latency.Seconds())
	}

	if _, err := q.InsertNotificationDelivery(ctx, params); err != nil {
		d.log.Warn("record notification_delivery failed", "notification_id", n.ID.String(), "err", err)
	}
	return sendErr == nil
}

// sendBatch is the BATCHED counterpart of send: one Telegram message for
// every notification in ns, followed by one notification_delivery row per
// notification (all sharing the same send outcome, sent_at, and provider
// message id) — the grouped rendering docs/11 section 3 asks for ("one
// grouped message rather than eleven separate ones," the same phrase
// section 3 uses for the quiet-hours case), applied here to the hourly
// BATCHED window instead. Returns true only if the send itself succeeded;
// a failed send still gets a 'failed' notification_delivery row for every
// notification in the batch, same reasoning as send's own doc comment.
func (d *Deliverer) sendBatch(
	ctx context.Context, q *db.Queries, ns []db.GetUndeliveredNotificationsRow, channelID pgtype.UUID, chatID int64, now time.Time,
) bool {
	text, err := renderBatchMessage(ns)
	if err != nil {
		d.log.Warn("render batch message failed", "count", len(ns), "err", err)
		return false
	}

	msgID, sendErr := d.telegram.SendMessage(ctx, chatID, text)

	for _, n := range ns {
		params := db.InsertNotificationDeliveryParams{
			NotificationID: n.ID,
			ChannelID:      channelID,
			Attempts:       1,
		}
		latency := now.Sub(n.CreatedAt.Time)
		if sendErr != nil {
			errMsg := sendErr.Error()
			params.Status = "failed"
			params.Error = &errMsg
			metrics.NotificationTotal.WithLabelValues(string(n.Trigger), "failed").Inc()
		} else {
			latencyMs := int32(latency.Milliseconds()) //nolint:gosec // notification age in ms fits comfortably in int32
			params.Status = "sent"
			params.SentAt = pgtype.Timestamptz{Time: now, Valid: true}
			params.LatencyMs = &latencyMs
			params.ProviderMsgID = &msgID
			metrics.NotificationTotal.WithLabelValues(string(n.Trigger), "sent").Inc()
		}
		metrics.NotificationLatency.WithLabelValues(string(n.Trigger), "telegram").Observe(latency.Seconds())

		if _, err := q.InsertNotificationDelivery(ctx, params); err != nil {
			d.log.Warn("record notification_delivery failed", "notification_id", n.ID.String(), "err", err)
		}
	}
	return sendErr == nil
}

// renderBatchMessage lists every batched match in one message, priority
// descending — the same "ordered by priority, single grouped message"
// shape docs/11 section 3 already specifies for the quiet-hours overnight
// case, reused here for the hourly BATCHED window.
func renderBatchMessage(ns []db.GetUndeliveredNotificationsRow) (string, error) {
	sorted := make([]db.GetUndeliveredNotificationsRow, len(ns))
	copy(sorted, ns)
	sort.Slice(sorted, func(i, j int) bool {
		pi, pj := int16(0), int16(0)
		if sorted[i].PriorityAtSend != nil {
			pi = *sorted[i].PriorityAtSend
		}
		if sorted[j].PriorityAtSend != nil {
			pj = *sorted[j].PriorityAtSend
		}
		return pi > pj
	})

	lines := []string{fmt.Sprintf("🎓 %d new-grad match%s", len(sorted), pluralSuffix(len(sorted)))}
	for _, n := range sorted {
		var p trigger.Payload
		if err := json.Unmarshal(n.Payload, &p); err != nil {
			return "", fmt.Errorf("unmarshal payload: %w", err)
		}
		priority := int16(0)
		if n.PriorityAtSend != nil {
			priority = *n.PriorityAtSend
		}
		line := fmt.Sprintf("• %s", p.Title)
		if p.CompanyName != "" {
			line = fmt.Sprintf("• %s — %s", p.CompanyName, p.Title)
		}
		line += fmt.Sprintf(" (%d)", priority)
		if p.LocationCity != "" {
			line += fmt.Sprintf(" · %s", p.LocationCity)
		}
		lines = append(lines, line)
		if p.ApplyURL != "" {
			lines = append(lines, p.ApplyURL)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// renderMessage implements a plain-text reduction of docs/11 section 6.1's
// Telegram format — the full version (skills matched/missing, time-to-apply
// estimate, inline action buttons) needs data and a callback handler this
// pass doesn't build.
func renderMessage(n db.GetUndeliveredNotificationsRow, now time.Time) (string, error) {
	var p trigger.Payload
	if err := json.Unmarshal(n.Payload, &p); err != nil {
		return "", fmt.Errorf("unmarshal payload: %w", err)
	}

	if n.Trigger == "deadline_t72h" || n.Trigger == "deadline_t24h" {
		return renderDeadlineMessage(p, now), nil
	}

	icon := "🎯"
	label := "Match"
	if n.Trigger == "bengaluru_match" {
		label = "Bengaluru"
	}
	priority := int16(0)
	if n.PriorityAtSend != nil {
		priority = *n.PriorityAtSend
	}

	msg := fmt.Sprintf("%s %s %d\n\n%s", icon, label, priority, p.Title)
	if p.LocationCity != "" {
		msg += fmt.Sprintf("\n📍 %s", p.LocationCity)
	}
	if p.CompNormalizedINRMonth != nil {
		msg += fmt.Sprintf("\n💰 ₹%d/month", *p.CompNormalizedINRMonth)
	}
	if p.ApplyURL != "" {
		msg += fmt.Sprintf("\n\n%s", p.ApplyURL)
	}
	return msg, nil
}

// renderDeadlineMessage is deadline_t72h/deadline_t24h's own format — no
// priority number (docs/11's threshold for this trigger is "any saved
// job," not a score), a days-left headline instead, using the same
// ceiling-not-floor day count apps/notifier/internal/digest's own
// "Closing soon" section uses (a deadline 20 hours away reads as "1 day
// left," not "0" — truncating would understate urgency for exactly the
// case this message exists to flag).
func renderDeadlineMessage(p trigger.Payload, now time.Time) string {
	label := "Closing soon"
	if p.DeadlineAt != nil {
		daysLeft := int(math.Ceil(p.DeadlineAt.Sub(now).Hours() / 24))
		switch {
		case daysLeft <= 0:
			label = "Closing today"
		case daysLeft == 1:
			label = "Closing tomorrow — 1 day left"
		default:
			label = fmt.Sprintf("Closing in %d days", daysLeft)
		}
	}

	title := p.Title
	if p.CompanyName != "" {
		title = fmt.Sprintf("%s — %s", p.CompanyName, p.Title)
	}

	msg := fmt.Sprintf("⏰ %s\n\n%s", label, title)
	if p.LocationCity != "" {
		msg += fmt.Sprintf("\n📍 %s", p.LocationCity)
	}
	if p.CompNormalizedINRMonth != nil {
		msg += fmt.Sprintf("\n💰 ₹%d/month", *p.CompNormalizedINRMonth)
	}
	if p.ApplyURL != "" {
		msg += fmt.Sprintf("\n\n%s", p.ApplyURL)
	}
	return msg
}

func chatIDFromConfig(raw []byte) (int64, error) {
	var cfg struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return 0, fmt.Errorf("unmarshal notification_channel.config: %w", err)
	}
	if cfg.ChatID == 0 {
		return 0, fmt.Errorf("notification_channel.config has no chat_id")
	}
	return cfg.ChatID, nil
}
