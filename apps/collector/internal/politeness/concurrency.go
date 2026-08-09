package politeness

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// concurrencyKeyPrefix mirrors the naming convention of
// apps/collector/internal/ratelimit's keyPrefix, in its own namespace so an
// incident-time key scan can tell "how many requests are in flight" apart from
// "how much rate budget is left."
const concurrencyKeyPrefix = "concurrency:host:"

// concurrencySlotTTL is a safety net, not the intended release path. The
// intended path is always an explicit release() call when a fetch completes.
// This exists only for the case a process is killed mid-fetch and never gets
// to call it — five minutes is generous against docs/06's own timeout table,
// whose longest entry (rendered HTML, total) is 60s, so a leaked slot recovers
// well within one scheduler cycle rather than lingering for the token bucket's
// much longer natural TTL.
const concurrencySlotTTL = 5 * time.Minute

// acquireScript atomically checks the in-flight count against the cap and, if
// there is room, reserves a slot. Without the Lua script, a GET-then-INCR pair
// from separate calls would let two concurrent callers both read "1 below cap"
// and both proceed, exceeding the cap by exactly the race window every time
// load is high enough to matter — which is precisely when the cap is doing
// its job.
const acquireScript = `
local key = KEYS[1]
local slot_cap = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])

local count = tonumber(redis.call('GET', key) or '0')
if count >= slot_cap then
  return 0
end

redis.call('INCR', key)
redis.call('EXPIRE', key, ttl)
return 1
`

// releaseScript decrements but never below zero, so a bug that calls release
// twice for one acquire cannot hand out phantom capacity to a third caller.
const releaseScript = `
local key = KEYS[1]
local count = tonumber(redis.call('GET', key) or '0')
if count > 0 then
  redis.call('DECR', key)
end
return 1
`

type concurrencyLimiter struct {
	rdb           *redis.Client
	acquireScript *redis.Script
	releaseScript *redis.Script
}

func newConcurrencyLimiter(rdb *redis.Client) *concurrencyLimiter {
	return &concurrencyLimiter{
		rdb:           rdb,
		acquireScript: redis.NewScript(acquireScript),
		releaseScript: redis.NewScript(releaseScript),
	}
}

// acquire reserves one of slotCap concurrent slots for domain. On success it
// returns a release func the caller MUST invoke exactly once, whether the
// fetch that followed succeeded or failed — a slot held open by a fetch that
// errored is indistinguishable, from the next source's perspective, from one
// held by a fetch that is still legitimately in flight.
func (c *concurrencyLimiter) acquire(
	ctx context.Context, domain string, slotCap int,
) (release func(context.Context), ok bool, err error) {
	key := concurrencyKeyPrefix + domain

	result, err := c.acquireScript.Run(ctx, c.rdb, []string{key}, slotCap, int(concurrencySlotTTL.Seconds())).Int64()
	if err != nil {
		return nil, false, fmt.Errorf("concurrency: evaluate slot for %s: %w", domain, err)
	}
	if result != 1 {
		return nil, false, nil
	}

	released := false
	return func(releaseCtx context.Context) {
		if released {
			// A double-release is a caller bug, not a reason to corrupt shared
			// state — silently ignored rather than double-decrementing.
			return
		}
		released = true
		// Best-effort: a failed release leaves a slot occupied until
		// concurrencySlotTTL reclaims it, which is the same outcome as a crash
		// mid-fetch and already has a bound on it.
		_ = c.releaseScript.Run(releaseCtx, c.rdb, []string{key}).Err()
	}, true, nil
}
