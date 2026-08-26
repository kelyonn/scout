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
	"strconv"
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

// maxDeliveryAttempts bounds the retry budget a failed send gets before it
// becomes permanently 'failed' — must match the literal `5` in
// packages/db/queries/notification.sql's GetUndeliveredNotifications
// (see that query's own comment on why it isn't parameterized).
const maxDeliveryAttempts = 5

// retryBackoff is how long to wait before re-attempting a send that has
// already failed `attempt` times. Exponential (2, 4, 8, 16 minutes),
// capped at 30 — long enough that a real Telegram outage doesn't burn
// through the attempt budget in under an hour, short enough that fixing a
// config problem (docs/11 section 11) is visible again within the hour
// without restarting the notifier.
func retryBackoff(attempt int16) time.Duration {
	const maxBackoff = 30 * time.Minute
	d := time.Duration(1<<uint(attempt)) * time.Minute //nolint:gosec // attempt is bounded by maxDeliveryAttempts (5)
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

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
// monitoring. Unlike before 000020, that row is not necessarily terminal:
// while n.PriorAttempts + 1 stays under maxDeliveryAttempts, it carries a
// next_attempt_at that makes GetUndeliveredNotifications offer this
// notification again once backoff elapses — see that query's own comment.
func (d *Deliverer) send(
	ctx context.Context, q *db.Queries, n db.GetUndeliveredNotificationsRow, channelID pgtype.UUID, chatID int64, now time.Time,
) bool {
	text, err := renderMessage(n, now)
	if err != nil {
		d.log.Warn("render message failed", "notification_id", n.ID.String(), "err", err)
		return false
	}

	msgID, sendErr := d.telegram.SendMessage(ctx, chatID, text)
	attempt := n.PriorAttempts + 1
	params := db.UpsertNotificationDeliveryParams{
		NotificationID: n.ID,
		ChannelID:      channelID,
		Attempts:       attempt,
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
		if attempt < maxDeliveryAttempts {
			params.NextAttemptAt = pgtype.Timestamptz{Time: now.Add(retryBackoff(attempt)), Valid: true}
		}
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

	if _, err := q.UpsertNotificationDelivery(ctx, params); err != nil {
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
		attempt := n.PriorAttempts + 1
		params := db.UpsertNotificationDeliveryParams{
			NotificationID: n.ID,
			ChannelID:      channelID,
			Attempts:       attempt,
		}
		latency := now.Sub(n.CreatedAt.Time)
		if sendErr != nil {
			errMsg := sendErr.Error()
			params.Status = "failed"
			params.Error = &errMsg
			if attempt < maxDeliveryAttempts {
				params.NextAttemptAt = pgtype.Timestamptz{Time: now.Add(retryBackoff(attempt)), Valid: true}
			}
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

		if _, err := q.UpsertNotificationDelivery(ctx, params); err != nil {
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

	lines := []string{fmt.Sprintf("%d new-grad match%s", len(sorted), pluralSuffix(len(sorted)))}
	for _, n := range sorted {
		var p trigger.Payload
		if err := json.Unmarshal(n.Payload, &p); err != nil {
			return "", fmt.Errorf("unmarshal payload: %w", err)
		}
		priority := int16(0)
		if n.PriorityAtSend != nil {
			priority = *n.PriorityAtSend
		}
		lines = append(lines, "") // blank line between entries

		line := p.Title
		if p.CompanyName != "" {
			line = fmt.Sprintf("%s — %s", p.CompanyName, p.Title)
		}
		line += fmt.Sprintf(" (%d)", priority)
		lines = append(lines, line)

		if detail := detailLine(p); detail != "" {
			lines = append(lines, detail)
		}
		if p.CompNormalizedINRMonth != nil {
			lines = append(lines, fmt.Sprintf("Rs %s/month", formatThousands(*p.CompNormalizedINRMonth)))
		}
		if p.ApplyURL != "" {
			lines = append(lines, p.ApplyURL)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// detailLine joins location/work mode/role family/seniority into one
// compact line — location_city and work_mode by themselves (docs/07
// section 6: a Tier 3 remote job has no location_city at all), role
// family and seniority whenever classification resolved them, skipped
// individually rather than the whole line dropped for one missing part.
func detailLine(p trigger.Payload) string {
	parts := make([]string, 0, 4)
	switch {
	case p.LocationCity != "" && p.WorkMode != "" && p.WorkMode != "unknown":
		parts = append(parts, fmt.Sprintf("%s (%s)", p.LocationCity, formatWorkMode(p.WorkMode)))
	case p.LocationCity != "":
		parts = append(parts, p.LocationCity)
	case p.WorkMode != "" && p.WorkMode != "unknown":
		parts = append(parts, formatWorkMode(p.WorkMode))
	}
	if role := formatRoleFamily(p.RoleFamily); role != "" {
		parts = append(parts, role)
	}
	if seniority := formatSeniority(p.Seniority); seniority != "" {
		parts = append(parts, seniority)
	}
	return strings.Join(parts, " - ")
}

// formatWorkMode renders work_mode's raw enum value ("onsite", "hybrid",
// "remote", "unknown") for display — "unknown" is deliberately never
// passed through, same admit-unknown-elsewhere/hide-unknown-here split
// AGENTS.md documents for the paid signal, but inverted: paid='unknown'
// must still be admitted into the feed, while a message line with nothing
// to say about work mode should just omit it rather than print "Unknown."
func formatWorkMode(mode string) string {
	switch mode {
	case "onsite":
		return "On-site"
	case "hybrid":
		return "Hybrid"
	case "remote":
		return "Remote"
	default:
		return ""
	}
}

// formatSeniority turns seniority's snake_case enum value into a display
// label ("new_grad" -> "New grad"). "unknown" renders empty, same
// reasoning as formatWorkMode.
func formatSeniority(seniority string) string {
	if seniority == "" || seniority == "unknown" {
		return ""
	}
	words := strings.Split(seniority, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// formatRoleFamily turns role_family's dotted enum value ("swe.infra.devops",
// "advocacy.solutions") into a display label. The first segment names the
// broad family (only two exist today: "swe" and "advocacy" — see docs/07
// section 3's taxonomy); ".general"/".other" carry no more specific
// information than the family itself, so they're dropped rather than
// shown as a redundant "General"/"Other" suffix.
func formatRoleFamily(roleFamily string) string {
	segments := strings.Split(roleFamily, ".")
	if len(segments) == 0 || segments[0] == "" {
		return ""
	}
	family := segments[0]
	switch family {
	case "swe":
		family = "Software Engineering"
	case "advocacy":
		family = "Advocacy"
	default:
		family = strings.ToUpper(family[:1]) + family[1:]
	}
	specifics := make([]string, 0, len(segments)-1)
	for _, s := range segments[1:] {
		if s == "" || s == "general" || s == "other" {
			continue
		}
		specifics = append(specifics, strings.ToUpper(s[:1])+s[1:])
	}
	if len(specifics) == 0 {
		return family
	}
	return family + " (" + strings.Join(specifics, "/") + ")"
}

// formatFreshness renders how long ago a job was posted, coarsening to
// the largest sensible unit — "posted 12 minutes ago" is worth stating
// precisely, "posted 9 days ago" is not worth stating in hours.
func formatFreshness(postedAt *time.Time, now time.Time) string {
	if postedAt == nil {
		return ""
	}
	age := now.Sub(*postedAt)
	switch {
	case age < 0:
		return ""
	case age < time.Hour:
		mins := int(age.Minutes())
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("Posted %d minute%s ago", mins, sSuffix(mins))
	case age < 24*time.Hour:
		hours := int(age.Hours())
		return fmt.Sprintf("Posted %d hour%s ago", hours, sSuffix(hours))
	default:
		days := int(age.Hours() / 24)
		return fmt.Sprintf("Posted %d day%s ago", days, sSuffix(days))
	}
}

// sSuffix is the regular "-s" plural pluralSuffix isn't: that helper
// exists for "match"/"matches" specifically (an "-es" plural) and would
// render "2 minutees ago" if reused here.
func sSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// formatThousands renders 85000 as "85,000" — display formatting only, on
// a value this system already computed (comp_normalized_inr_month), not
// the Indian-numbering parse path (packages/normalize handles parsing "8
// LPA"/"1,00,000" from source text). A small local copy rather than an
// import from apps/collector/internal/scoring: that package is
// collector-internal by its own path, and duplicating one formatting
// function is cheaper than adding a cross-service dependency for it.
func formatThousands(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// renderMessage implements docs/11 section 6.1's Telegram format in plain
// text — no emojis (a deliberate, explicit product decision, not an
// oversight: docs/11's own mockup used them and has been corrected to
// match), and no inline action buttons (those need an HMAC-signed
// callback handler this pass doesn't build). Everything else docs/11
// asks for — location/work mode, role and seniority, compensation,
// freshness, and a real "why this matched you" sentence — is included:
// trigger.Payload carries job_score.explanation and the classification
// fields SelectUnnotifiedJobGroups already had on hand (2026-08-24; see
// that struct's own comment). time-to-apply estimate is still not
// computed anywhere and stays out.
func renderMessage(n db.GetUndeliveredNotificationsRow, now time.Time) (string, error) {
	var p trigger.Payload
	if err := json.Unmarshal(n.Payload, &p); err != nil {
		return "", fmt.Errorf("unmarshal payload: %w", err)
	}

	if n.Trigger == "deadline_t72h" || n.Trigger == "deadline_t24h" {
		return renderDeadlineMessage(p, now), nil
	}

	label := "Match"
	if n.Trigger == "bengaluru_match" {
		label = "Bengaluru match"
	}
	priority := int16(0)
	if n.PriorityAtSend != nil {
		priority = *n.PriorityAtSend
	}

	lines := []string{fmt.Sprintf("%s — %d", label, priority), ""}
	if p.CompanyName != "" {
		lines = append(lines, p.CompanyName)
	}
	lines = append(lines, p.Title)

	if detail := detailLine(p); detail != "" {
		lines = append(lines, "", detail)
	}
	if p.CompNormalizedINRMonth != nil {
		lines = append(lines, fmt.Sprintf("Rs %s/month", formatThousands(*p.CompNormalizedINRMonth)))
	}
	if fresh := formatFreshness(p.PostedAt, now); fresh != "" {
		lines = append(lines, fresh)
	}
	if p.DeadlineAt != nil {
		lines = append(lines, deadlineLine(*p.DeadlineAt, now))
	}

	if p.Explanation != "" {
		lines = append(lines, "", p.Explanation)
	}
	if p.ApplyURL != "" {
		lines = append(lines, "", p.ApplyURL)
	}
	return strings.Join(lines, "\n"), nil
}

// deadlineLine is renderMessage's own deadline countdown — same
// ceiling-not-floor day count renderDeadlineMessage and
// apps/notifier/internal/digest's "Closing soon" section both use, so
// the same job reads the same "N days left" everywhere it can appear.
func deadlineLine(deadlineAt time.Time, now time.Time) string {
	daysLeft := int(math.Ceil(deadlineAt.Sub(now).Hours() / 24))
	switch {
	case daysLeft <= 0:
		return "Deadline: today"
	case daysLeft == 1:
		return "Deadline: tomorrow"
	default:
		return fmt.Sprintf("Deadline: %d days left", daysLeft)
	}
}

// renderDeadlineMessage is deadline_t72h/deadline_t24h's own format — no
// priority number (docs/11's threshold for this trigger is "any saved
// job," not a score), a days-left headline instead, using the same
// ceiling-not-floor day count apps/notifier/internal/digest's own
// "Closing soon" section uses (a deadline 20 hours away reads as "1 day
// left," not "0" — truncating would understate urgency for exactly the
// case this message exists to flag). SelectDeadlineReminderCandidates
// doesn't join role_family/seniority/explanation the way
// SelectUnnotifiedJobGroups does (a much smaller, already-tracked-jobs
// query — see that query's own comment), so this stays shorter than
// renderMessage by necessity, not by choice.
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

	lines := []string{label, ""}
	if p.CompanyName != "" {
		lines = append(lines, p.CompanyName)
	}
	lines = append(lines, p.Title)

	if p.LocationCity != "" {
		lines = append(lines, p.LocationCity)
	}
	if p.CompNormalizedINRMonth != nil {
		lines = append(lines, fmt.Sprintf("Rs %s/month", formatThousands(*p.CompNormalizedINRMonth)))
	}
	if p.ApplyURL != "" {
		lines = append(lines, "", p.ApplyURL)
	}
	return strings.Join(lines, "\n")
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
