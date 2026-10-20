package scheduler

import (
	"context"
	"fmt"
	"time"

	db "github.com/kelyon/scout/packages/db/gen"
)

// RetentionConfig configures how far back cold data is kept before it gets
// compacted, scrubbed or pruned, keeping the hosted database under Neon's
// free-tier ~0.5 GB
// (ADR-019, docs/03-data-model.md section 11). Measured on the local database
// after one live day: job 805 MB total (720 MB of it TOAST — descriptions and
// raw_payload), raw_observation 204 MB (160 MB payload), job_score 77 MB,
// river_job 26 MB. None of that is needed again once a posting is cold.
//
// What it does, and — as importantly — what it never does:
//
//   - COMPACTS cold jobs (job.sql CompactColdJobs): clears description_*,
//     requirements_text, embedding and raw_payload. The row, canonical_url_hash,
//     simhash, job_group membership and merge history stay, so a re-observed
//     posting resolves to the same job and the same job_group and cannot be
//     notified twice (rule 2). It never deletes a job or a job_group.
//   - NEVER touches a job whose group has user_job_state, user_job_state_event,
//     notification or google_calendar_event rows: saved, applied, noted,
//     rated, notified or calendared opportunities stay whole.
//   - SCRUBS old raw_observation payloads but keeps the rows, because
//     (source_id, content_hash) is the idempotency key (migration 000005):
//     deleting rows would make unchanged content re-insert and re-run the
//     pipeline on the next poll.
//   - Trims score audit JSON for cold jobs, and deletes finished and stale
//     River rows.
//
// Retention writes no notification, status or score column, so it cannot
// trigger or suppress a notification (rule 3). Every clearing it does can only
// reduce dedup input, which can only make Stage 2/3 under-merge (rule 4).
type RetentionConfig struct {
	// ClosedGrace: closed/expired/filled jobs are compacted this long after
	// closing, so a posting that flaps closed->open keeps its text.
	ClosedGrace time.Duration
	// IrrelevantGrace: non-software jobs are never shown (feed hard-filters
	// is_software); they keep text only this long for dedup/boilerplate
	// learning, which read recent descriptions.
	IrrelevantGrace time.Duration
	// StaleOpenAge: open jobs first posted longer ago than this — the 45-day
	// dedup window, past which freshness is near zero.
	StaleOpenAge time.Duration
	// UnseenAge: not seen on the source for this long.
	UnseenAge time.Duration
	// JobRawPayloadAge: job.raw_payload (write-only, never read back) is cleared
	// from every unprotected job this long after first sight.
	JobRawPayloadAge time.Duration
	// ObservationPayloadAge: raw_observation payloads older than this are
	// cleared (rows kept).
	ObservationPayloadAge time.Duration
	// QueueFinalizedAge / QueueStaleAge: River rows.
	QueueFinalizedAge time.Duration
	QueueStaleAge     time.Duration
	// BatchSize bounds each statement so no transaction is long-lived on a
	// serverless database.
	BatchSize int32
	// MaxBatches bounds one run per step; the next cycle continues.
	MaxBatches int
}

// DefaultRetention is sized from the live measurement (docs/03 section 11):
// at these windows ~38k jobs (5.4k of them hot) settle at ~240 MB.
func DefaultRetention() RetentionConfig {
	return RetentionConfig{
		ClosedGrace:           3 * 24 * time.Hour,
		IrrelevantGrace:       time.Hour,
		StaleOpenAge:          45 * 24 * time.Hour,
		UnseenAge:             14 * 24 * time.Hour,
		JobRawPayloadAge:      24 * time.Hour,
		ObservationPayloadAge: 2 * 24 * time.Hour,
		QueueFinalizedAge:     24 * time.Hour,
		QueueStaleAge:         7 * 24 * time.Hour,
		BatchSize:             2000,
		MaxBatches:            50,
	}
}

// Aggressive returns the emergency windows used when the database is over the
// high-water mark: grace periods collapse so every cold row is compacted now.
func (c RetentionConfig) Aggressive() RetentionConfig {
	c.ClosedGrace = 0
	c.IrrelevantGrace = 0
	c.StaleOpenAge = 30 * 24 * time.Hour
	c.JobRawPayloadAge = 0
	c.ObservationPayloadAge = time.Hour
	c.QueueStaleAge = 24 * time.Hour
	return c
}

// Size thresholds. Neon free is ~512 MB; writes fail at the limit, so warn well
// before it and switch to the aggressive windows.
const (
	RetentionHighWaterBytes = 380 << 20
	RetentionWarnBytes      = 300 << 20
)

// RetentionResult reports what one run changed.
type RetentionResult struct {
	JobsCompacted        int64
	ScoresTrimmed        int64
	RawPayloadsCleared   int64
	PayloadsScrubbed     int64
	QueueFinalizedPruned int64
	QueueStalePruned     int64
	SizeBefore           int64
	SizeAfter            int64
	Aggressive           bool
}

// RunRetention applies retention once. It is DB-only (no outbound request, so
// it is outside the compliance gate's remit) and idempotent, which makes it
// safe to run every cycle in -once mode and from the long-running loop. It
// logs counts and sizes only — never row content (rule 7).
func (s *Scheduler) RunRetention(ctx context.Context) (RetentionResult, error) {
	q := db.New(s.pool)
	cfg := DefaultRetention()
	if s.retention != nil {
		cfg = *s.retention
	}
	var res RetentionResult

	size, err := q.SelectDatabaseSizeBytes(ctx)
	if err != nil {
		return res, fmt.Errorf("scheduler: retention: database size: %w", err)
	}
	res.SizeBefore = size
	if size >= RetentionHighWaterBytes {
		cfg = cfg.Aggressive()
		res.Aggressive = true
		s.log.Warn("database over retention high-water mark; using aggressive windows",
			"size_mb", size>>20, "high_water_mb", RetentionHighWaterBytes>>20)
	}

	if applyErr := applyRetention(ctx, q, cfg, &res); applyErr != nil {
		return res, applyErr
	}

	if size, err = q.SelectDatabaseSizeBytes(ctx); err == nil {
		res.SizeAfter = size
		if size >= RetentionWarnBytes {
			s.log.Warn("database size is near the free-tier limit after retention",
				"size_mb", size>>20, "limit_mb", 512,
				"hint", "dead space needs VACUUM FULL; see docs/hosted-setup.md")
		}
	}
	s.log.Info("retention",
		"jobs_compacted", res.JobsCompacted, "raw_payloads_cleared", res.RawPayloadsCleared, "scores_trimmed", res.ScoresTrimmed,
		"payloads_scrubbed", res.PayloadsScrubbed,
		"queue_finalized_pruned", res.QueueFinalizedPruned, "queue_stale_pruned", res.QueueStalePruned,
		"size_before_mb", res.SizeBefore>>20, "size_after_mb", res.SizeAfter>>20,
		"aggressive", res.Aggressive)
	return res, nil
}

// applyRetention runs every step against q, which may be a pool or — in
// tests — a transaction that is rolled back.
func applyRetention(ctx context.Context, q db.Querier, cfg RetentionConfig, res *RetentionResult) error {
	var err error

	res.JobsCompacted, err = drain(ctx, cfg, func() (int64, error) {
		return q.CompactColdJobs(ctx, db.CompactColdJobsParams{
			ClosedGraceSecs:     cfg.ClosedGrace.Seconds(),
			IrrelevantGraceSecs: cfg.IrrelevantGrace.Seconds(),
			StaleOpenAgeSecs:    cfg.StaleOpenAge.Seconds(),
			UnseenAgeSecs:       cfg.UnseenAge.Seconds(),
			BatchSize:           cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: compact jobs: %w", err)
	}

	res.RawPayloadsCleared, err = drain(ctx, cfg, func() (int64, error) {
		return q.ClearOldJobRawPayload(ctx, db.ClearOldJobRawPayloadParams{
			MaxAgeSecs: cfg.JobRawPayloadAge.Seconds(),
			BatchSize:  cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: clear job raw_payload: %w", err)
	}

	res.ScoresTrimmed, err = drain(ctx, cfg, func() (int64, error) {
		return q.TrimColdJobScores(ctx, db.TrimColdJobScoresParams{
			ClosedGraceSecs:     cfg.ClosedGrace.Seconds(),
			IrrelevantGraceSecs: cfg.IrrelevantGrace.Seconds(),
			StaleOpenAgeSecs:    cfg.StaleOpenAge.Seconds(),
			UnseenAgeSecs:       cfg.UnseenAge.Seconds(),
			BatchSize:           cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: trim scores: %w", err)
	}

	res.PayloadsScrubbed, err = drain(ctx, cfg, func() (int64, error) {
		return q.ScrubOldObservationPayloads(ctx, db.ScrubOldObservationPayloadsParams{
			MaxAgeSecs: cfg.ObservationPayloadAge.Seconds(),
			BatchSize:  cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: scrub observations: %w", err)
	}

	res.QueueFinalizedPruned, err = drain(ctx, cfg, func() (int64, error) {
		return q.DeleteFinalizedQueueJobs(ctx, db.DeleteFinalizedQueueJobsParams{
			MaxAgeSecs: cfg.QueueFinalizedAge.Seconds(),
			BatchSize:  cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: prune finished queue rows: %w", err)
	}

	res.QueueStalePruned, err = drain(ctx, cfg, func() (int64, error) {
		return q.DeleteStaleQueueJobs(ctx, db.DeleteStaleQueueJobsParams{
			MaxAgeSecs: cfg.QueueStaleAge.Seconds(),
			BatchSize:  cfg.BatchSize,
		})
	})
	if err != nil {
		return fmt.Errorf("scheduler: retention: prune stale queue rows: %w", err)
	}
	return nil
}

// drain repeats step until it affects fewer rows than a full batch, ctx is
// done, or MaxBatches is reached, returning the total affected.
func drain(ctx context.Context, cfg RetentionConfig, step func() (int64, error)) (int64, error) {
	var total int64
	for i := 0; i < cfg.MaxBatches && ctx.Err() == nil; i++ {
		n, err := step()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(cfg.BatchSize) {
			break
		}
	}
	return total, nil
}
