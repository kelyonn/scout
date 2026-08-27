// Command notifier evaluates scored jobs against docs/11-notifications.md's
// triggers and delivers matches over Telegram.
//
// Two guarantees live in this binary and must not be weakened: the unique
// index on (user_id, job_group_id, trigger) — apps/notifier/internal/trigger
// relies on InsertNotification's ON CONFLICT DO NOTHING rather than any
// read-then-write check — and the suppress_notifications guard for
// backfills/rescores, threaded through Evaluator.Run even though no
// backfill or rescore feature exists yet to set it (see that package's
// doc comment).
//
// `notifier -setup` runs the one-time Telegram onboarding flow instead of
// the steady-state loop: long-polls for the first message sent to the bot,
// stores its chat id, sends a confirmation, exits. See docs/11 section 11.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/notifier/internal/deliver"
	"github.com/kelyon/scout/apps/notifier/internal/digest"
	"github.com/kelyon/scout/apps/notifier/internal/fcm"
	"github.com/kelyon/scout/apps/notifier/internal/telegram"
	"github.com/kelyon/scout/apps/notifier/internal/trigger"
	"github.com/kelyon/scout/packages/logging"
	"github.com/kelyon/scout/packages/metrics"
)

// DefaultTickInterval governs how often trigger evaluation and delivery
// run. Short relative to the collector's own poll interval — docs/11
// section 9's latency budget spends almost its entire allowance on
// "posting -> our poll" and treats "scored -> notification decided ->
// delivered" as rounding error, so this loop being fast costs nothing and
// keeps that portion of the budget genuinely negligible rather than merely
// assumed so.
const DefaultTickInterval = 30 * time.Second

func main() {
	log := slog.New(logging.Scrub(slog.NewJSONHandler(os.Stdout, nil)))

	// `notifier healthcheck` is what the container healthcheck runs — the
	// image is distroless (no shell, no curl), and a real HTTP probe against
	// the /metrics server beats a process-liveness check, same reasoning as
	// apps/api/cmd's own selfCheck. Checked before flag.Parse() so it's a
	// plain positional arg, not a flag, matching apps/collector/cmd's
	// `collector healthcheck`.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := selfCheck(metricsAddr()); err != nil {
			log.Error("unhealthy", "err", err)
			os.Exit(1)
		}
		return
	}

	setup := flag.Bool("setup", false, "run the one-time Telegram onboarding flow and exit")
	flag.Parse()

	var err error
	if *setup {
		err = runSetup(log)
	} else {
		err = run(log)
	}
	if err != nil {
		log.Error("exited with error", "err", err)
		os.Exit(1)
	}
}

// selfCheck probes /metrics over the loopback interface — mirrors
// apps/api/cmd's own selfCheck against /health; notifier has no /health
// route of its own, only the one packages/metrics.Serve exposes.
func selfCheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse metrics address: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+net.JoinHostPort(host, port)+"/metrics", http.NoBody)
	if err != nil {
		return fmt.Errorf("build metrics request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("metrics request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics endpoint returned %d", resp.StatusCode)
	}
	return nil
}

func run(log *slog.Logger) error {
	pool, err := buildDatabasePool(context.Background())
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer pool.Close()

	token := os.Getenv("SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN")
	if token == "" {
		return errors.New("SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN is not set — run `notifier -setup` after setting it")
	}
	tg := telegram.New(token)

	// Unlike the Telegram token above, an unconfigured FCM project is not
	// fatal — ADR-012's native push is additive to Telegram, not a
	// replacement, and the notifier must keep working for every day
	// before a Firebase project and Android build actually exist. A
	// malformed (but non-empty) service account degrades the same way,
	// logged rather than refused, matching heartbeat.Pinger's own
	// "disabled" posture elsewhere in this codebase.
	fcmClient, err := fcm.New(context.Background(),
		os.Getenv("SCOUT_FCM_PROJECT_ID"), os.Getenv("SCOUT_FCM_SERVICE_ACCOUNT_JSON"))
	if err != nil {
		log.Warn("fcm disabled: could not parse SCOUT_FCM_SERVICE_ACCOUNT_JSON", "err", err)
		fcmClient = &fcm.Client{}
	}
	if fcmClient.Enabled() {
		log.Info("native push armed", "transport", "fcm")
	} else {
		log.Warn("native push disabled: SCOUT_FCM_PROJECT_ID/SCOUT_FCM_SERVICE_ACCOUNT_JSON not fully set")
	}

	eval := trigger.New(pool, log)
	deadlines := trigger.NewDeadlineSweeper(pool, log)
	deliverer := deliver.New(pool, tg, fcmClient, log)
	digester := digest.New(pool, tg, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// docs/16-observability.md — no other HTTP surface exists on this
	// binary for Prometheus to scrape (see packages/metrics' own comment
	// on why it's unauthenticated).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		metrics.Serve(ctx, metricsAddr(), log)
	}()

	log.Info("notifier starting", "tick_interval", DefaultTickInterval)

	ticker := time.NewTicker(DefaultTickInterval)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			runTick(ctx, pool, eval, deadlines, deliverer, digester, log)
		}
	}

	wg.Wait()
	log.Info("notifier stopped")
	return nil
}

// metricsAddr mirrors apps/collector/cmd's own helper of the same name.
func metricsAddr() string {
	if v := os.Getenv("SCOUT_METRICS_ADDR"); v != "" {
		return v
	}
	return ":9090"
}

// runTick runs one evaluate-then-deliver-then-digest cycle for the sole
// user (ADR-015). A tick that errors is logged and does not stop the loop
// — the same reasoning apps/collector/internal/scheduler.Run gives for not
// letting one bad tick stop discovery.
func runTick(
	ctx context.Context, pool *pgxpool.Pool, eval *trigger.Evaluator, deadlines *trigger.DeadlineSweeper,
	deliverer *deliver.Deliverer, digester *digest.Generator, log *slog.Logger,
) {
	user, err := db.New(pool).GetSoleUser(ctx)
	if err != nil {
		log.Warn("tick: get sole user failed", "err", err)
		return
	}

	weightVersion, err := activeWeightVersion(ctx, pool)
	if err != nil {
		log.Warn("tick: get active weight_version failed", "err", err)
		return
	}

	evaluated, fired, err := eval.Run(ctx, user.ID, weightVersion, false)
	if err != nil {
		log.Warn("tick: trigger evaluation failed", "err", err)
	} else if evaluated > 0 {
		log.Info("trigger evaluation", "evaluated", evaluated, "fired", fired)
	}

	deadlinesEvaluated, deadlinesFired, err := deadlines.Run(ctx, user.ID, false)
	if err != nil {
		log.Warn("tick: deadline sweep failed", "err", err)
	} else if deadlinesEvaluated > 0 {
		log.Info("deadline sweep", "evaluated", deadlinesEvaluated, "fired", deadlinesFired)
	}

	sent, queued, err := deliverer.Run(ctx, user.ID)
	if err != nil {
		log.Warn("tick: delivery failed", "err", err)
	} else if sent > 0 || queued > 0 {
		log.Info("delivery", "sent", sent, "queued", queued)
	}

	digestSent, err := digester.Run(ctx, user.ID, weightVersion)
	if err != nil {
		log.Warn("tick: digest failed", "err", err)
	} else if digestSent {
		log.Info("digest sent")
	}
}

func activeWeightVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	wv, err := db.New(pool).GetActiveWeightVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("get active weight_version: %w", err)
	}
	return wv.Version, nil
}

// runSetup is docs/11 section 11's Telegram onboarding flow, minus the
// 6-digit-code confirmation step: that step exists to let a user enter a
// code into a settings *page*, which does not exist before P3. For a
// single-user system operated from a terminal, taking the first message
// sent to the bot as authoritative and confirming immediately by replying
// to it is the same guarantee (only someone who can message the bot can
// register a chat id) with one less round trip.
func runSetup(log *slog.Logger) error {
	token := os.Getenv("SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN")
	if token == "" {
		return errors.New("SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN is not set")
	}
	tg := telegram.New(token)

	pool, err := buildDatabasePool(context.Background())
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer pool.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("Send any message to your bot now. Waiting (Ctrl+C to cancel)...")

	const longPollSeconds = 30
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return errors.New("setup cancelled")
		default:
		}

		updates, err := tg.GetUpdates(ctx, offset, longPollSeconds)
		if err != nil {
			return fmt.Errorf("get updates: %w", err)
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil {
				continue
			}
			return completeSetup(ctx, pool, tg, u.Message.Chat.ID, log)
		}
	}
}

func completeSetup(ctx context.Context, pool *pgxpool.Pool, tg *telegram.Client, chatID int64, log *slog.Logger) error {
	user, err := db.New(pool).GetSoleUser(ctx)
	if err != nil {
		return fmt.Errorf("get sole user: %w", err)
	}

	config := fmt.Sprintf(`{"chat_id": %d}`, chatID)
	_, err = db.New(pool).UpsertTelegramChannel(ctx, db.UpsertTelegramChannelParams{
		UserID: user.ID,
		Config: []byte(config),
	})
	if err != nil {
		return fmt.Errorf("save telegram channel: %w", err)
	}

	if _, err := tg.SendMessage(ctx, chatID, "Scout is connected. You'll get job alerts here."); err != nil {
		log.Warn("setup: confirmation message failed to send", "err", err)
	}

	fmt.Println("Done — Telegram channel saved.")
	return nil
}

func buildDatabasePool(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("SCOUT_DATABASE_URL")
	if databaseURL == "" {
		return nil, errors.New("SCOUT_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
