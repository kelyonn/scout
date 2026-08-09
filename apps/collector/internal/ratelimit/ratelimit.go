// Package ratelimit implements the collector's per-registered-domain request
// budget: docs/06-ingestion-pipeline.md section 4, "Rate limiting".
//
// The bucket is keyed by registered domain rather than by individual source or
// URL, because the domain — not the source — is what a real server's
// infrastructure has to absorb. `boards.greenhouse.io` serves thousands of
// Scout's sources; limiting per-source would let all of them fire at once and
// hammer Greenhouse's single origin with a burst equal to the source count.
// Callers are expected to compute that registered domain (see
// apps/collector/internal/source.Source.RegisteredDomain) and pass it in — this
// package does not know what a Source is.
//
// The bucket lives in Redis so the budget is shared across every collector
// process, present or future — the same reason docs/06 gives: "in Redis so it
// is shared across collector instances."
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces this package's keys in the shared Redis instance,
// distinct from robots.keyPrefix so an incident-time key scan can tell the two
// subsystems apart.
const keyPrefix = "ratelimit:host:"

// script implements a standard token-bucket, refilled continuously and
// evaluated atomically so concurrent callers across processes cannot race each
// other into over-spending the budget.
//
// KEYS[1]  the bucket's Redis key
// ARGV[1]  rate,  tokens added per second
// ARGV[2]  burst, the bucket's capacity — also its starting fill, so the very
//
//	first request against a domain does not have to wait for the bucket
//	to fill from empty
//
// ARGV[3]  now,   unix time in seconds, as a float, supplied by the caller
//
//	rather than read via Redis TIME — see the package-level tradeoff
//	note in [Limiter.Allow]
//
// ARGV[4]  cost,  tokens this call consumes; always 1 today, parameterized so
//
//	a future caller (e.g. a request known to be heavier) does not need a
//	new script
//
// Returns 1 if the request is allowed (and debits the bucket), 0 if it must
// wait (and the bucket is left untouched).
const script = `
local tokens_key = KEYS[1]

local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now   = tonumber(ARGV[3])
local cost  = tonumber(ARGV[4])

local bucket = redis.call('HMGET', tokens_key, 'tokens', 'ts')
local tokens = tonumber(bucket[1])
local ts     = tonumber(bucket[2])

if tokens == nil then
  tokens = burst
  ts = now
end

local elapsed = now - ts
if elapsed < 0 then
  elapsed = 0
end

tokens = math.min(burst, tokens + elapsed * rate)

local allowed = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
end

redis.call('HMSET', tokens_key, 'tokens', tokens, 'ts', now)

-- A bucket nobody has drawn from in long enough to fully refill is a bucket
-- worth forgetting, so a domain we polled once years ago does not sit in
-- Redis forever. +60s of margin absorbs clock skew between this call and the
-- next legitimate one.
local ttl = math.ceil(burst / rate) + 60
redis.call('EXPIRE', tokens_key, ttl)

return allowed
`

// Limiter enforces the per-registered-domain token bucket.
type Limiter struct {
	rdb    *redis.Client
	script *redis.Script
}

// New wraps an existing Redis client. Scout's collector owns one client and
// hands it to whichever package needs it — see the note in
// apps/collector/internal/robots/rediscache.go.
func New(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb, script: redis.NewScript(script)}
}

// Allow reports whether a request to domain may proceed right now under a
// token bucket refilling at rps tokens/second with capacity burst.
//
// It never blocks. A false result means DEFER in the politeness gate's
// vocabulary (docs/06 section 4) — the caller reschedules rather than waiting
// here, because waiting inside Allow would hold whatever goroutine budget the
// scheduler allotted this source hostage to every other source sharing the
// same domain.
//
// The clock is the caller's (time.Now, passed into the script as ARGV[3])
// rather than Redis's own. That trades small clock-skew error, which this
// system's tolerances absorb easily via the burst allowance, for not needing
// every caller's request to round-trip through a second Redis command just to
// read TIME — the script would otherwise need two round trips' worth of
// non-atomicity between reading the clock and evaluating the bucket.
func (l *Limiter) Allow(ctx context.Context, domain string, rps float64, burst int) (bool, error) {
	if rps <= 0 {
		return false, fmt.Errorf("ratelimit: rate must be positive, got %v", rps)
	}
	if burst < 1 {
		return false, fmt.Errorf("ratelimit: burst must be at least 1, got %d", burst)
	}
	if domain == "" {
		return false, fmt.Errorf("ratelimit: domain must not be empty")
	}

	now := float64(time.Now().UnixNano()) / 1e9

	result, err := l.script.Run(ctx, l.rdb, []string{keyPrefix + domain}, rps, burst, now, 1).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: evaluate bucket for %s: %w", domain, err)
	}
	return result == 1, nil
}
