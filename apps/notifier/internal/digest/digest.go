// Package digest implements docs/11-notifications.md section 6.5's daily
// digest — one Telegram message at 08:00 IST summarizing what didn't
// already fire an instant/batched notification. Runs inside apps/notifier's
// existing 30-second tick loop rather than a separate scheduled process or
// host cron entry: Run's own due-check ("is it past 08:00 IST today, and
// has today's digest not already gone out") makes every call outside the
// window a single cheap no-op query, so checking on every tick costs
// nothing and needs no second binary.
//
// Two sections from docs/11's mockup are deliberately not implemented:
// the week-over-week response-rate trend (needs last week's numbers
// alongside this week's, not just this week's) and "One thing" — the
// skill-gap coaching insight, which docs/19-roadmap.md scopes to P5
// ("skill gap and resume match ... calibrated against a real corpus").
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/notifier/internal/telegram"
)

const (
	digestHourIST     = 8
	overnightTopN     = 3
	closingSoonTopN   = 3
	closingSoonWindow = 4 * 24 * time.Hour
	overnightWindow   = 24 * time.Hour
	weeklyWindow      = 7 * 24 * time.Hour
)

var istLocation = mustLoadIST()

func mustLoadIST() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// A missing tzdata entry is a container/build bug, not a runtime
		// condition — same posture apps/notifier/internal/deliver takes
		// for the identical load.
		panic("digest: could not load Asia/Kolkata: " + err.Error())
	}
	return loc
}

// Generator builds and sends the daily digest — see the package comment.
type Generator struct {
	pool     *pgxpool.Pool
	telegram *telegram.Client
	log      *slog.Logger
	now      func() time.Time // overridable for tests; defaults to time.Now
}

// New returns a digest Generator.
func New(pool *pgxpool.Pool, tg *telegram.Client, log *slog.Logger) *Generator {
	return &Generator{pool: pool, telegram: tg, log: log, now: time.Now}
}

// Run sends today's digest if it is due and hasn't already been sent.
// Returns sent=false, err=nil when it simply isn't time yet — that is the
// common case on almost every tick, not an error.
func (g *Generator) Run(ctx context.Context, userID pgtype.UUID, weightVersion string) (sent bool, err error) {
	q := db.New(g.pool)

	nowIST := g.now().In(istLocation)
	todayBoundary := time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), digestHourIST, 0, 0, 0, istLocation)
	if nowIST.Before(todayBoundary) {
		return false, nil
	}

	already, err := q.DigestAlreadySentSince(ctx, db.DigestAlreadySentSinceParams{
		UserID: userID, Since: pgtype.Timestamptz{Time: todayBoundary, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("digest: check already sent: %w", err)
	}
	if already {
		return false, nil
	}

	channel, err := q.GetTelegramChannel(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("digest: get telegram channel: %w", err)
	}
	chatID, err := chatIDFromConfig(channel.Config)
	if err != nil {
		return false, fmt.Errorf("digest: %w", err)
	}

	data, err := g.gather(ctx, q, userID, weightVersion, todayBoundary.Add(-overnightWindow))
	if err != nil {
		return false, fmt.Errorf("digest: gather data: %w", err)
	}

	text := render(nowIST, data)

	payload, err := json.Marshal(data)
	if err != nil {
		return false, fmt.Errorf("digest: marshal payload: %w", err)
	}
	notifID, err := q.InsertDigestNotification(ctx, db.InsertDigestNotificationParams{UserID: userID, Payload: payload})
	if err != nil {
		return false, fmt.Errorf("digest: insert notification: %w", err)
	}

	msgID, sendErr := g.telegram.SendMessage(ctx, chatID, text)
	delivery := db.InsertNotificationDeliveryParams{NotificationID: notifID, ChannelID: channel.ID, Attempts: 1}
	if sendErr != nil {
		errMsg := sendErr.Error()
		delivery.Status = "failed"
		delivery.Error = &errMsg
	} else {
		now := g.now()
		latencyMs := int32(0)
		delivery.Status = "sent"
		delivery.SentAt = pgtype.Timestamptz{Time: now, Valid: true}
		delivery.LatencyMs = &latencyMs
		delivery.ProviderMsgID = &msgID
	}
	if _, dbErr := q.InsertNotificationDelivery(ctx, delivery); dbErr != nil {
		g.log.Warn("digest: record notification_delivery failed", "err", dbErr)
	}

	if sendErr != nil {
		return false, fmt.Errorf("digest: send telegram message: %w", sendErr)
	}
	return true, nil
}

// data is the digest's rendered content — also stored verbatim as the
// notification.payload JSONB, so a past digest's exact contents are
// reconstructible from the audit trail alone.
type data struct {
	OvernightCount    int64            `json:"overnight_count"`
	OvernightTop      []overnightJob   `json:"overnight_top"`
	WeekCount         int64            `json:"week_count"`
	ClosingSoon       []closingSoonJob `json:"closing_soon"`
	AppliedThisWeek   int64            `json:"applied_this_week"`
	InterviewingCount int32            `json:"interviewing_count"`
	PendingCount      int32            `json:"pending_count"`
}

type overnightJob struct {
	JobGroupID   string `json:"job_group_id"`
	Company      string `json:"company"`
	Title        string `json:"title"`
	LocationCity string `json:"location_city,omitempty"`
	CompINRMonth *int64 `json:"comp_inr_month,omitempty"`
	Priority     int16  `json:"priority"`
}

type closingSoonJob struct {
	JobGroupID string    `json:"job_group_id"`
	Company    string    `json:"company"`
	Title      string    `json:"title"`
	DeadlineAt time.Time `json:"deadline_at"`
}

func (g *Generator) gather(ctx context.Context, q *db.Queries, userID pgtype.UUID, weightVersion string, since time.Time) (data, error) {
	var d data
	sinceTS := pgtype.Timestamptz{Time: since, Valid: true}
	weekAgoTS := pgtype.Timestamptz{Time: g.now().Add(-weeklyWindow), Valid: true}

	overnightRows, err := q.SelectDigestOvernightJobs(ctx, db.SelectDigestOvernightJobsParams{
		UserID: userID, WeightVersion: weightVersion, Since: sinceTS, Limit: overnightTopN,
	})
	if err != nil {
		return d, fmt.Errorf("overnight jobs: %w", err)
	}
	for _, r := range overnightRows {
		d.OvernightTop = append(d.OvernightTop, overnightJob{
			JobGroupID: r.JobGroupID.String(), Company: r.CompanyName, Title: r.Title,
			LocationCity: derefOr(r.LocationCity, ""), CompINRMonth: numericToInt64(r.CompNormalizedInrMonth),
			Priority: r.Priority,
		})
	}

	d.OvernightCount, err = q.CountDigestEligibleJobsSince(ctx, sinceTS)
	if err != nil {
		return d, fmt.Errorf("overnight count: %w", err)
	}
	d.WeekCount, err = q.CountDigestEligibleJobsSince(ctx, weekAgoTS)
	if err != nil {
		return d, fmt.Errorf("week count: %w", err)
	}

	closingRows, err := q.SelectDigestClosingSoon(ctx, db.SelectDigestClosingSoonParams{
		UserID: userID,
		After:  pgtype.Timestamptz{Time: g.now(), Valid: true},
		Before: pgtype.Timestamptz{Time: g.now().Add(closingSoonWindow), Valid: true},
		Limit:  closingSoonTopN,
	})
	if err != nil {
		return d, fmt.Errorf("closing soon: %w", err)
	}
	for _, r := range closingRows {
		d.ClosingSoon = append(d.ClosingSoon, closingSoonJob{
			JobGroupID: r.JobGroupID.String(), Company: r.CompanyName, Title: r.Title, DeadlineAt: r.DeadlineAt.Time,
		})
	}

	d.AppliedThisWeek, err = q.SelectDigestWeeklyAppliedCount(ctx, db.SelectDigestWeeklyAppliedCountParams{
		UserID: userID, Since: weekAgoTS,
	})
	if err != nil {
		return d, fmt.Errorf("weekly applied count: %w", err)
	}

	pipeline, err := q.SelectDigestPipelineCounts(ctx, userID)
	if err != nil {
		return d, fmt.Errorf("pipeline counts: %w", err)
	}
	d.InterviewingCount = pipeline.InterviewingCount
	d.PendingCount = pipeline.PendingCount

	return d, nil
}

// render implements docs/11 section 6.5's format, minus the response-rate
// trend and "One thing" sections (see the package doc comment).
func render(nowIST time.Time, d data) string {
	var b strings.Builder

	fmt.Fprintf(&b, "☀️ Good morning · %s\n\n", nowIST.Format("Monday 2 January"))
	fmt.Fprintf(&b, "Overnight: %d new · This week: %d new\n", d.OvernightCount, d.WeekCount)

	if len(d.OvernightTop) > 0 {
		b.WriteString("\n━━ New while you slept ━━━━━━━━━━━━━\n\n")
		for _, j := range d.OvernightTop {
			fmt.Fprintf(&b, "🎯 %s · %s", j.Company, j.Title)
			if j.LocationCity != "" {
				fmt.Fprintf(&b, " · %s", j.LocationCity)
			}
			fmt.Fprintf(&b, " · %d\n", j.Priority)
			if j.CompINRMonth != nil {
				fmt.Fprintf(&b, "   ₹%d/mo\n", *j.CompINRMonth)
			}
		}
	}

	if len(d.ClosingSoon) > 0 {
		b.WriteString("\n━━ Closing soon ━━━━━━━━━━━━━━━━━━━━\n\n")
		for _, j := range d.ClosingSoon {
			// Rounds up: a deadline 20 hours away reads as "1 day left,"
			// not "0" — truncating would understate urgency for exactly
			// the case this section exists to flag.
			//
			// j.DeadlineAt.Sub(nowIST), not time.Until (== Sub(time.Now())):
			// this function takes nowIST specifically so its output is a
			// pure function of its arguments, testable without real-clock
			// dependence — time.Until silently breaks that by reading the
			// real system clock instead, which is also a latent production
			// bug, not just a test-determinism one: if render() ever runs
			// even slightly after the data it's rendering was queried,
			// the countdown would drift from the digest's actual as-of time.
			daysLeft := int(math.Ceil(j.DeadlineAt.Sub(nowIST).Hours() / 24))
			plural := "s"
			if daysLeft == 1 {
				plural = ""
			}
			fmt.Fprintf(&b, "⏰ %s %s — %d day%s left\n", j.Company, j.Title, daysLeft, plural)
		}
	}

	b.WriteString("\n━━ Your week ━━━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&b, "Applied %d · Interviews %d · Pending %d\n", d.AppliedThisWeek, d.InterviewingCount, d.PendingCount)

	return b.String()
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func numericToInt64(n pgtype.Numeric) *int64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := int64(f.Float64)
	return &v
}

// chatIDFromConfig duplicates apps/notifier/internal/deliver's unexported
// helper of the same name — three lines, not worth a cross-package export
// for.
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
