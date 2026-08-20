package scheduler

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/kelyon/scout/packages/db/gen"

	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
	"github.com/kelyon/scout/packages/fetch"
)

// toSource narrows a SelectDueSources row to exactly the fields
// apps/collector/internal/source.Source — and therefore the politeness
// gate — needs. db.SourceKind and db.LegalPosture are sqlc-generated string
// types whose values already match source's own enum strings one for one
// (both are the `source_kind` and `legal_posture` Postgres enums from
// infra/migrations/000002_enums.up.sql), so the conversion is a direct cast,
// not a translation table that could drift out of sync with the schema.
func toSource(row db.SelectDueSourcesRow) source.Source {
	var crawlDelay *float64
	if row.RobotsCrawlDelayS != nil {
		v := float64(*row.RobotsCrawlDelayS)
		crawlDelay = &v
	}

	return source.Source{
		ID:                row.ID.String(),
		Kind:              string(row.Kind),
		LegalPosture:      source.LegalPosture(row.LegalPosture),
		URL:               row.Url,
		MaxRPS:            float64(row.MaxRps),
		MaxConcurrency:    int(row.MaxConcurrency),
		RobotsCrawlDelayS: crawlDelay,
		CircuitOpenUntil:  fromTimestamptz(row.CircuitOpenUntil),
	}
}

// toAdapterSource narrows a SelectDueSources row to padapter.Source — the
// single place row.AdapterConfig's jsonb gets decoded, used both by
// fetchResult (for an OwnFetcher adapter's Fetch) and runIngestion (for
// every adapter's Parse), so a source's per-adapter config is available
// consistently wherever a padapter.Source gets built from a row rather
// than only at one of the two call sites.
func toAdapterSource(row db.SelectDueSourcesRow) (padapter.Source, error) {
	config := map[string]any{}
	if len(row.AdapterConfig) > 0 {
		if err := json.Unmarshal(row.AdapterConfig, &config); err != nil {
			return padapter.Source{}, fmt.Errorf("decode adapter_config: %w", err)
		}
	}
	return padapter.Source{ID: row.ID.String(), URL: row.Url, AdapterConfig: config}, nil
}

// resultFromRaw is NewRawResponse's inverse — needed because fetchResult
// hands an OwnFetcher adapter's RawResponse to the same pollOne logic
// (classifyStatus, outcomeFromResult, change detection) that otherwise
// consumes a *fetch.Result straight from s.fetcher. NotModified is derived
// rather than carried, matching fetch.Result's own doc comment describing
// it as "a convenience for StatusCode == http.StatusNotModified" — the
// same derivation, just computed here instead of inside package fetch.
func resultFromRaw(raw *padapter.RawResponse) *fetch.Result {
	return &fetch.Result{
		StatusCode:   raw.StatusCode,
		Body:         raw.Body,
		ETag:         raw.ETag,
		LastModified: raw.LastModified,
		RetryAfter:   raw.RetryAfter,
		NotModified:  raw.StatusCode == http.StatusNotModified,
		FetchedAt:    raw.FetchedAt,
	}
}

// derefStr returns "" for a nil pointer rather than requiring every call site
// to nil-check a possibly-absent ETag or Last-Modified value.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// nullableStr is derefStr's inverse: "" becomes nil, so an absent validator
// is written to the database as SQL NULL rather than as the string "" — the
// two mean different things (no ETag was ever seen, versus an ETag that is
// literally empty), and only the pointer form preserves that distinction
// through UpdateSourceAfterPoll's sqlc.narg columns.
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fromTimestamptz converts a nullable Postgres timestamp into *time.Time —
// nil for SQL NULL, matching source.Source.CircuitOpenUntil's own contract
// (nil means the breaker is closed).
func fromTimestamptz(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// toTimestamptz is fromTimestamptz's inverse, for writing
// nextCircuitState's result back through UpdateSourceAfterPoll.
func toTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// daysSinceLastActivity is apps/collector/internal/interval's
// DaysSinceLastActivity input, computed from source.last_changed_at. See the
// extended discussion in interval's package comment on why this is a proxy
// for the spec's intended "days since a job was last posted," not that value
// itself.
//
// A source with no last_changed_at yet (Valid == false — it has never
// registered a Layer-2 content change, which includes every source before its
// first successful poll) is treated as maximally recent (0 days): the same
// "no evidence yet, poll it more rather than less" posture
// interval.YieldFactor already takes at yield_ratio = 0, applied consistently
// here rather than defaulting to "stale" for a source about which nothing is
// actually known.
func daysSinceLastActivity(t pgtype.Timestamptz, now time.Time) float64 {
	if !t.Valid {
		return 0
	}
	days := now.Sub(t.Time).Hours() / 24
	if days < 0 {
		return 0
	}
	return days
}

// rngFor returns a fresh, independently-seeded *rand.Rand for jittering one
// source's interval. A shared *rand.Rand is not safe for the concurrent use
// RunOnce's worker pool would give it (math/rand/v2's Rand has no internal
// locking, matching the stdlib rand.Rand it supersedes), and this package has
// no need for the jitter draws across different sources, or different polls
// of the same source, to relate to each other at all — only that they are not
// predictable enough to reintroduce the synchronized-thundering-herd problem
// jitter exists to solve (docs/06 section 3.3). Seeding from the source's own
// ID plus the current time gives each call a distinct seed without a shared,
// mutex-guarded generator becoming a contention point across a 200-source
// batch.
func rngFor(id pgtype.UUID) *rand.Rand {
	seed1 := binary.BigEndian.Uint64(id.Bytes[0:8])
	seed2 := binary.BigEndian.Uint64(id.Bytes[8:16]) ^ uint64(time.Now().UnixNano())
	return rand.New(rand.NewPCG(seed1, seed2)) //nolint:gosec // jitter, not a security boundary
}
