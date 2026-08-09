package robots

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces every key this package writes, so a `redis-cli KEYS`
// during an incident can tell robots.txt cache entries apart from the rate
// limiter's and anything else that later shares this Redis instance.
const keyPrefix = "robots:"

// RedisCache is the Cache implementation used in production. The Postgres
// fallback docs/06 specifies is deliberately not built yet — see the [Cache]
// doc comment — so a Redis flush today means a stampede of robots.txt
// re-fetches rather than silent data loss; every one of them still passes
// through the fail-closed policy in this package, so the worst case is
// temporary over-caution, not a compliance gap.
type RedisCache struct {
	rdb *redis.Client
}

// NewRedisCache wraps an existing client. Scout does not construct its own
// Redis connection pool per package — the collector owns one client and hands
// it to whichever package needs it, which is what keeps `redis-cli CLIENT
// LIST` on the production host showing one connection per process rather than
// one per subsystem.
func NewRedisCache(rdb *redis.Client) *RedisCache {
	return &RedisCache{rdb: rdb}
}

// value is the wire format for one cache entry: the status code and the body,
// colon-delimited with a fixed-width status so decoding never has to guess
// where the status ends and the body begins. A body cannot itself start with
// a digit-colon-digit sequence that would be ambiguous here because the split
// only ever looks at the first ':'.
func encode(body []byte, status int) []byte {
	return []byte(strconv.Itoa(status) + ":" + string(body))
}

func decodeEntry(raw []byte) (body []byte, status int, ok bool) {
	s := string(raw)
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return nil, 0, false
	}
	status, err := strconv.Atoi(s[:i])
	if err != nil {
		return nil, 0, false
	}
	return []byte(s[i+1:]), status, true
}

// Get implements [Cache]. Any Redis error — including a plain connection
// failure — is treated identically to a cache miss: the checker will simply
// fetch fresh. A cache is an optimization, and Get returning ok=false for
// "Redis is having a bad day" is what keeps a Redis outage from becoming a
// compliance-gate outage.
func (c *RedisCache) Get(ctx context.Context, host string) ([]byte, int, bool) {
	raw, err := c.rdb.Get(ctx, keyPrefix+host).Bytes()
	if err != nil {
		return nil, 0, false
	}
	body, status, ok := decodeEntry(raw)
	return body, status, ok
}

// Set implements [Cache].
func (c *RedisCache) Set(ctx context.Context, host string, body []byte, status int, ttl time.Duration) error {
	if err := c.rdb.Set(ctx, keyPrefix+host, encode(body, status), ttl).Err(); err != nil {
		return fmt.Errorf("cache robots.txt for %s: %w", host, err)
	}
	return nil
}
