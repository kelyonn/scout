package robots

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedis returns a client against a reachable Redis, or skips the test.
//
// It tries, in order: SCOUT_TEST_REDIS_URL (set by CI's redis service),
// then the local dev stack's port from infra/compose/local.yml. Skipping
// rather than failing means a contributor without Docker running still gets a
// green `go test ./...` for everything else in the package — only this file's
// tests are conditional.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	candidates := []string{os.Getenv("SCOUT_TEST_REDIS_URL")}
	candidates = append(candidates, "redis://localhost:6380/1", "redis://localhost:6379/1")

	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		opt, err := redis.ParseURL(raw)
		if err != nil {
			continue
		}
		rdb := redis.NewClient(opt)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err = rdb.Ping(ctx).Err()
		cancel()
		if err != nil {
			_ = rdb.Close()
			continue
		}

		t.Cleanup(func() { _ = rdb.Close() })
		return rdb
	}

	t.Skip("no reachable Redis (set SCOUT_TEST_REDIS_URL, or run `make dev-db`); skipping RedisCache tests")
	return nil
}

func TestRedisCacheRoundTrip(t *testing.T) {
	rdb := testRedis(t)
	cache := NewRedisCache(rdb)
	ctx := context.Background()
	host := "roundtrip.example.test"
	t.Cleanup(func() { _ = rdb.Del(context.Background(), keyPrefix+host).Err() })

	if _, _, ok := cache.Get(ctx, host); ok {
		t.Fatal("Get on an unset key returned ok=true")
	}

	body := []byte("User-agent: *\nDisallow: /private\n")
	if err := cache.Set(ctx, host, body, http.StatusOK, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	gotBody, gotStatus, ok := cache.Get(ctx, host)
	if !ok {
		t.Fatal("Get after Set returned ok=false")
	}
	if gotStatus != http.StatusOK {
		t.Errorf("status = %d, want %d", gotStatus, http.StatusOK)
	}
	if string(gotBody) != string(body) {
		t.Errorf("body = %q, want %q", gotBody, body)
	}
}

func TestRedisCacheStoresA4xxStatusFaithfully(t *testing.T) {
	// The status is what the checker uses to pick AllowAll vs Parse vs
	// DisallowAll on replay — a round trip that lost or mangled it would make
	// a cached 403 decode as if it were a 200.
	rdb := testRedis(t)
	cache := NewRedisCache(rdb)
	ctx := context.Background()
	host := "status.example.test"
	t.Cleanup(func() { _ = rdb.Del(context.Background(), keyPrefix+host).Err() })

	if err := cache.Set(ctx, host, nil, http.StatusForbidden, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, status, ok := cache.Get(ctx, host)
	if !ok || status != http.StatusForbidden {
		t.Fatalf("Get = (status %d, ok %v), want (403, true)", status, ok)
	}
}

func TestRedisCacheExpires(t *testing.T) {
	rdb := testRedis(t)
	cache := NewRedisCache(rdb)
	ctx := context.Background()
	host := "expiry.example.test"
	t.Cleanup(func() { _ = rdb.Del(context.Background(), keyPrefix+host).Err() })

	if err := cache.Set(ctx, host, []byte("x"), http.StatusOK, 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, _, ok := cache.Get(ctx, host); !ok {
		t.Fatal("Get immediately after Set returned ok=false")
	}

	time.Sleep(200 * time.Millisecond)

	if _, _, ok := cache.Get(ctx, host); ok {
		t.Fatal("Get after the TTL elapsed returned ok=true")
	}
}

func TestRedisCacheKeysAreNamespaced(t *testing.T) {
	// A `redis-cli KEYS` scan during an incident needs to tell this package's
	// entries apart from the rate limiter's once both share the instance.
	rdb := testRedis(t)
	cache := NewRedisCache(rdb)
	ctx := context.Background()
	host := "namespace.example.test"
	t.Cleanup(func() { _ = rdb.Del(context.Background(), keyPrefix+host).Err() })

	if err := cache.Set(ctx, host, []byte("x"), http.StatusOK, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := rdb.Get(ctx, keyPrefix+host).Err(); err != nil {
		t.Fatalf("key %q not found under the expected prefix: %v", keyPrefix+host, err)
	}
}

func TestCheckerAgainstRealRedis(t *testing.T) {
	// The integration the unit tests can only simulate with fakeCache: a real
	// Checker, backed by a real Redis, actually skipping the network on the
	// second call.
	rdb := testRedis(t)
	host := "checker-integration.example.test"
	t.Cleanup(func() { _ = rdb.Del(context.Background(), keyPrefix+host).Err() })

	srv := newTestServer(t, http.StatusOK, "User-agent: *\nDisallow: /blocked\n")
	c := NewForTesting(NewRedisCache(rdb), PlainDialContext, "https://scout.example/bot", "operator@example.com")
	realHost := hostOf(t, srv)

	if got := c.Allowed(context.Background(), "http", realHost, "/blocked"); got {
		t.Fatal("first call: Allowed(/blocked) = true, want false")
	}

	srv.Close()

	if got := c.Allowed(context.Background(), "http", realHost, "/blocked"); got {
		t.Fatal("second call (should be served from Redis): Allowed(/blocked) = true, want false")
	}
}
