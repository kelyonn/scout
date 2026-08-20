package queue

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Depth is one queue's backpressure snapshot — docs/16-observability.md
// section 4's scout_queue_depth and scout_queue_oldest_job_age_seconds,
// read directly against river_job rather than through river.Client, which
// has no simple "how many jobs are waiting" accessor. OldestAgeSeconds is
// 0 when Count is 0 — an empty queue has no oldest job to be stuck.
type Depth struct {
	Count            int
	OldestAgeSeconds float64
}

// QueueDepth reads name's current depth directly from river_job. Not
// through sqlc: river_job is a table River's own migration
// (infra/migrations/000010_river.up.sql) owns and manages, not part of
// this project's domain schema, so it doesn't belong in
// packages/db/queries alongside the tables docs/03 actually models.
func QueueDepth(ctx context.Context, pool *pgxpool.Pool, name string) (Depth, error) {
	var d Depth
	err := pool.QueryRow(ctx, `
		select
			count(*) filter (where state in ('available', 'scheduled', 'retryable')),
			coalesce(extract(epoch from now() - min(scheduled_at) filter (where state = 'available')), 0)
		from river_job
		where queue = $1
	`, name).Scan(&d.Count, &d.OldestAgeSeconds)
	if err != nil {
		return Depth{}, fmt.Errorf("queue: read depth for %q: %w", name, err)
	}
	return d, nil
}
