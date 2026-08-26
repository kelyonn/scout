package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PartitionMaintenanceInterval governs how often Run checks
// raw_observation's partition window while a session is running —
// docs/03-data-model.md section 10's "nightly job," running more often
// than nightly because ADR-018's on-demand sessions are frequently shorter
// than 24h; Run also calls this once immediately at startup for the same
// reason (see Run's own comment).
const PartitionMaintenanceInterval = 6 * time.Hour

// partitionCreateAheadMonths and partitionRetentionMonths are docs/03
// section 10's literal numbers.
const (
	partitionCreateAheadMonths = 3
	partitionRetentionMonths   = 6
)

const (
	rawObservationTable       = "raw_observation"
	rawObservationDefaultName = "raw_observation_default"
	partitionMonthLayout      = "2006_01" // matches infra/migrations/000005's to_char(..., 'YYYY_MM')
)

// MaintainRawObservationPartitions ensures raw_observation has a real
// partition for every month from now through partitionCreateAheadMonths
// ahead, and detaches+drops any partition whose entire range is older than
// partitionRetentionMonths — infra/migrations/000005's own DO block,
// re-run on a schedule instead of only once at migration time. That
// migration's comment describes this job in the future tense; until now
// it did not exist, and the only thing standing between "the collector
// keeps running" and "every insert past the pre-created window fails
// outright" was how far ahead that one-time migration happened to reach.
//
// Idempotent: a month that already has a partition is skipped (not
// recreated), and a run with nothing old enough to drop is a no-op.
//
// Retention safety: docs/03 section 10 says to drop "after verifying the
// snapshot archive," written before ADR-017 settled the real backup
// design. ADR-017 classifies raw_observation as Bulk, not Irreplaceable —
// it is covered only by the nightly whole-database pg_dump (docs/15
// section 5), not backed up per-row or per-partition — so by the time a
// partition is six months old it has already been captured in on the
// order of 180 nightly dumps. There is no separate snapshot archive to
// check here; the nightly dump is the actual safety net, and it already
// runs independently of this job.
func (s *Scheduler) MaintainRawObservationPartitions(ctx context.Context) (created, dropped int, err error) {
	now := s.now().UTC()

	existing, err := s.rawObservationPartitions(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("scheduler: list raw_observation partitions: %w", err)
	}

	created, err = s.createMissingPartitions(ctx, now, existing)
	if err != nil {
		return created, 0, fmt.Errorf("scheduler: create raw_observation partitions: %w", err)
	}

	dropped, err = s.dropExpiredPartitions(ctx, now, existing)
	if err != nil {
		return created, dropped, fmt.Errorf("scheduler: drop raw_observation partitions: %w", err)
	}

	return created, dropped, nil
}

// rawObservationPartitions returns the name of every partition currently
// attached to raw_observation, via pg_inherits — the same catalog view
// Postgres itself uses to resolve partition routing, so this can never
// drift from what the parent table actually has regardless of how a given
// partition was created.
func (s *Scheduler) rawObservationPartitions(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `
		select child.relname
		from pg_inherits inh
		join pg_class child on child.oid = inh.inhrelid
		join pg_class parent on parent.oid = inh.inhparent
		where parent.relname = $1
	`, rawObservationTable)
	if err != nil {
		return nil, fmt.Errorf("query pg_inherits for %s: %w", rawObservationTable, err)
	}
	defer rows.Close()

	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan partition name: %w", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partition rows: %w", err)
	}
	return names, nil
}

func partitionNameForMonth(month time.Time) string {
	return rawObservationTable + "_" + month.Format(partitionMonthLayout)
}

// createMissingPartitions creates whatever partitions are missing from now's
// month through partitionCreateAheadMonths ahead.
func (s *Scheduler) createMissingPartitions(ctx context.Context, now time.Time, existing map[string]bool) (int, error) {
	created := 0
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i <= partitionCreateAheadMonths; i++ {
		lowerBound := startOfMonth.AddDate(0, i, 0)
		upperBound := lowerBound.AddDate(0, 1, 0)
		name := partitionNameForMonth(lowerBound)

		if existing[name] {
			continue
		}

		table := pgx.Identifier{name}.Sanitize()
		indexName := pgx.Identifier{name + "_dedup_idx"}.Sanitize()

		// IF NOT EXISTS on both statements: existing is a snapshot from
		// moments ago, and this is cheap insurance against a concurrent
		// collector instance (or a re-run of this same session) racing to
		// create the identical partition, rather than a real expectation
		// of drift.
		if _, err := s.pool.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%s) TO (%s)",
			table, rawObservationTable, quoteDate(lowerBound), quoteDate(upperBound),
		)); err != nil {
			return created, fmt.Errorf("create partition %s: %w", name, err)
		}
		if _, err := s.pool.Exec(ctx, fmt.Sprintf(
			"CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (source_id, content_hash)",
			indexName, table,
		)); err != nil {
			return created, fmt.Errorf("create dedup index for partition %s: %w", name, err)
		}

		s.log.Info("raw_observation partition created", "partition", name,
			"from", lowerBound.Format("2006-01-02"), "to", upperBound.Format("2006-01-02"))
		created++
	}

	return created, nil
}

// dropExpiredPartitions detaches and drops every partition whose entire
// date range is older than partitionRetentionMonths. A plain (non-
// CONCURRENTLY) DETACH is used deliberately: this table has no
// user-facing read path (docs/02's architecture keeps raw_observation an
// internal audit log, never queried by apps/api), so briefly holding a
// lock during a maintenance pass that runs at most a few times a day
// costs nothing real, and it avoids CONCURRENTLY's own two-phase-commit
// edge cases for a job that gains little from them.
func (s *Scheduler) dropExpiredPartitions(ctx context.Context, now time.Time, existing map[string]bool) (int, error) {
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -partitionRetentionMonths, 0)
	dropped := 0

	for name := range existing {
		if name == rawObservationDefaultName {
			continue
		}
		month, ok := parsePartitionMonth(name)
		if !ok {
			// Not one of ours to manage — e.g. some other partition a
			// human created by hand. Leave it alone rather than guess.
			continue
		}
		upperBound := month.AddDate(0, 1, 0)
		if !upperBound.Before(cutoff) {
			continue // still within retention
		}

		table := pgx.Identifier{name}.Sanitize()
		if _, err := s.pool.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE %s DETACH PARTITION %s", rawObservationTable, table,
		)); err != nil {
			return dropped, fmt.Errorf("detach partition %s: %w", name, err)
		}
		if _, err := s.pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			return dropped, fmt.Errorf("drop partition %s: %w", name, err)
		}

		s.log.Info("raw_observation partition dropped", "partition", name, "month", month.Format("2006-01"))
		dropped++
	}

	return dropped, nil
}

// parsePartitionMonth extracts the month a raw_observation_YYYY_MM
// partition covers. Returns ok=false for anything not matching that exact
// shape (raw_observation_default, or a partition this job did not create).
func parsePartitionMonth(name string) (time.Time, bool) {
	suffix, found := strings.CutPrefix(name, rawObservationTable+"_")
	if !found {
		return time.Time{}, false
	}
	month, err := time.Parse(partitionMonthLayout, suffix)
	if err != nil {
		return time.Time{}, false
	}
	return month, true
}

// quoteDate renders a date as a SQL string literal — these are computed
// entirely from time.Time arithmetic, never from external input, but
// quoted literals rather than bare interpolation regardless.
func quoteDate(t time.Time) string {
	return "'" + t.Format("2006-01-02") + "'"
}
