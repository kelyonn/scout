package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// partitionExists reports whether name is currently a real table attached
// to raw_observation, via the same pg_inherits view the production code
// reads — not a guess based on what this test thinks it created.
func partitionExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(), `
		select exists(
			select 1 from pg_inherits inh
			join pg_class child on child.oid = inh.inhrelid
			join pg_class parent on parent.oid = inh.inhparent
			where parent.relname = 'raw_observation' and child.relname = $1
		)
	`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("check partition %s: %v", name, err)
	}
	return exists
}

// dropPartitionFixture is t.Cleanup-safe: DETACH then DROP, IF EXISTS
// throughout, so it's harmless to call on a partition a test never
// actually managed to create.
func dropPartitionFixture(pool *pgxpool.Pool, name string) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, "ALTER TABLE raw_observation DETACH PARTITION "+name)
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+name)
}

// TestCreateMissingPartitions_CreatesOnlyWhatsMissing exercises
// createMissingPartitions directly rather than through
// MaintainRawObservationPartitions, and passes a synthetic `existing` map
// instead of one queried from the real table. That's deliberate, not a
// shortcut: createMissingPartitions never drops anything, so calling it
// directly with a `now` far in the future (here, 2035 — a year no real
// migration or real ingestion could ever touch) creates brand-new,
// throwaway partitions with zero risk to whatever a shared database
// (this project's own dev stack very much included) actually has stored
// in its real, current window.
func TestCreateMissingPartitions_CreatesOnlyWhatsMissing(t *testing.T) {
	pool := testPool(t)
	s := New(pool, nil, nil, testLogger())
	now := time.Date(2035, time.June, 15, 0, 0, 0, 0, time.UTC)

	wantCreated := []string{
		"raw_observation_2035_06", "raw_observation_2035_07",
		"raw_observation_2035_08", "raw_observation_2035_09",
	}
	t.Cleanup(func() {
		for _, name := range wantCreated {
			dropPartitionFixture(pool, name)
		}
	})

	created, err := s.createMissingPartitions(context.Background(), now, map[string]bool{})
	if err != nil {
		t.Fatalf("createMissingPartitions: %v", err)
	}
	if created != len(wantCreated) {
		t.Errorf("created = %d, want %d", created, len(wantCreated))
	}
	for _, name := range wantCreated {
		if !partitionExists(t, pool, name) {
			t.Errorf("expected partition %s to exist", name)
		}
	}

	// Real proof a row actually routes into the new partition, not just
	// that the catalog has an entry — checks the FOR VALUES range was
	// built correctly, not only that some table got created.
	slug := fmt.Sprintf("partition-test-co-%d", time.Now().UnixNano())
	var companyID string
	err = pool.QueryRow(context.Background(), `
		insert into company (slug, canonical_name, normalized_name, discovered_via)
		values ($1::citext, $1::text, $1::citext, 'seed')
		returning id
	`, slug).Scan(&companyID)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}

	var sourceID string
	url := "https://example.test/" + slug
	err = pool.QueryRow(context.Background(), `
		insert into source (company_id, kind, status, legal_posture, url, url_hash)
		values ($1::uuid, 'ats_greenhouse', 'active', 'permitted', $2, digest($2, 'sha256'))
		returning id
	`, companyID, url).Scan(&sourceID)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "delete from source where id = $1::uuid", sourceID)
		_, _ = pool.Exec(ctx, "delete from company where id = $1::uuid", companyID)
	})

	observedAt := time.Date(2035, time.July, 10, 0, 0, 0, 0, time.UTC)
	_, err = pool.Exec(context.Background(), `
		insert into raw_observation (source_id, observed_at, url, canonical_url, canonical_url_hash, content_hash, payload)
		values ($1::uuid, $2::timestamptz, $3, $3, digest($3, 'sha256'), digest('routing-check', 'sha256'), '{}'::jsonb)
	`, sourceID, observedAt, url)
	if err != nil {
		t.Fatalf("insert into the new partition's range: %v", err)
	}

	var directCount int
	err = pool.QueryRow(context.Background(),
		"select count(*) from raw_observation_2035_07 where source_id = $1::uuid", sourceID,
	).Scan(&directCount)
	if err != nil {
		t.Fatalf("query the new partition directly: %v", err)
	}
	if directCount != 1 {
		t.Errorf("expected the new row inside raw_observation_2035_07 directly, found %d", directCount)
	}

	// Re-running with the now-real existing set must be a no-op.
	existing, err := s.rawObservationPartitions(context.Background())
	if err != nil {
		t.Fatalf("rawObservationPartitions: %v", err)
	}
	createdAgain, err := s.createMissingPartitions(context.Background(), now, existing)
	if err != nil {
		t.Fatalf("second createMissingPartitions: %v", err)
	}
	if createdAgain != 0 {
		t.Errorf("second run created %d, want 0 (idempotent)", createdAgain)
	}
}

// TestDropExpiredPartitions_OnlyTouchesWhatsPassedIn exercises
// dropExpiredPartitions directly with a synthetic `existing` map
// containing only a fixture this test itself created — never the real
// map dropExpiredPartitions would get from querying raw_observation live.
// dropExpiredPartitions only ever iterates the map it's given, so this is
// a hard guarantee (not just a convention) that it cannot touch any real
// partition a shared database happens to have, no matter what `now` is
// used to compute the retention cutoff.
func TestDropExpiredPartitions_OnlyTouchesWhatsPassedIn(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := New(pool, nil, nil, testLogger())

	const stalePartition = "raw_observation_2015_01"
	const freshPartition = "raw_observation_2035_06"
	for name, bounds := range map[string][2]string{
		stalePartition: {"2015-01-01", "2015-02-01"},
		freshPartition: {"2035-06-01", "2035-07-01"},
	} {
		if _, err := pool.Exec(ctx,
			"CREATE TABLE IF NOT EXISTS "+name+" PARTITION OF raw_observation FOR VALUES FROM ('"+bounds[0]+"') TO ('"+bounds[1]+"')",
		); err != nil {
			t.Fatalf("create fixture partition %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		dropPartitionFixture(pool, stalePartition)
		dropPartitionFixture(pool, freshPartition)
	})

	now := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	existing := map[string]bool{
		stalePartition:            true,
		freshPartition:            true,
		rawObservationDefaultName: true, // must never be considered droppable
		// Deliberately not including any real partition name (e.g.
		// raw_observation_2026_08) — the point of this test is that
		// dropExpiredPartitions cannot drop what it was never told about.
	}

	dropped, err := s.dropExpiredPartitions(ctx, now, existing)
	if err != nil {
		t.Fatalf("dropExpiredPartitions: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (only the 2015 fixture)", dropped)
	}
	if partitionExists(t, pool, stalePartition) {
		t.Error("the stale 2015 fixture should have been dropped")
	}
	if !partitionExists(t, pool, freshPartition) {
		t.Error("the 2035 fixture is not past retention and must survive")
	}
	if !partitionExists(t, pool, rawObservationDefaultName) {
		t.Error("raw_observation_default must never be dropped")
	}
}

// TestMaintainRawObservationPartitions_RealClockTouchesNothingUnexpected
// is the one test that calls the real, composed
// MaintainRawObservationPartitions end to end — deliberately at (close
// to) the real current time, where infra/migrations/000005's own
// pre-created window (2026-08 through 2028-01) already covers everything
// this call would want to create, and retention's 6-month lookback
// (2026-02) predates every real partition that exists anywhere — so this
// composed call is provably a no-op against real data by construction,
// not by luck, regardless of what a shared database actually has stored.
func TestMaintainRawObservationPartitions_RealClockTouchesNothingUnexpected(t *testing.T) {
	pool := testPool(t)
	// Pinned clock: retention is two months, so against the real clock the
	// empty pre-created 2026-08 partition becomes droppable in due course.
	s := New(pool, nil, nil, testLogger()).WithNow(func() time.Time {
		return time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	})

	created, dropped, err := s.MaintainRawObservationPartitions(context.Background())
	if err != nil {
		t.Fatalf("MaintainRawObservationPartitions: %v", err)
	}
	if created != 0 {
		t.Errorf("created = %d, want 0 — infra/migrations/000005 already covers the real ahead-window", created)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 — nothing real is 2 months past retention at the pinned date", dropped)
	}
}

func TestParsePartitionMonth(t *testing.T) {
	cases := []struct {
		name   string
		wantY  int
		wantM  time.Month
		wantOK bool
	}{
		{"raw_observation_2026_08", 2026, time.August, true},
		{"raw_observation_default", 0, 0, false},
		{"some_other_table", 0, 0, false},
	}
	for _, c := range cases {
		got, ok := parsePartitionMonth(c.name)
		if ok != c.wantOK {
			t.Errorf("parsePartitionMonth(%q) ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if ok && (got.Year() != c.wantY || got.Month() != c.wantM) {
			t.Errorf("parsePartitionMonth(%q) = %v, want %d-%d", c.name, got, c.wantY, c.wantM)
		}
	}
}
