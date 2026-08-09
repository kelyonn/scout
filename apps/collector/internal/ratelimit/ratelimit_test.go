package ratelimit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedis returns a client against a reachable Redis, or skips. Mirrors
// apps/collector/internal/robots's helper of the same shape; not shared
// between the two packages because two small test-only helpers are cheaper
// than the coupling a shared internal test package would add for this little
// code.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	candidates := []string{os.Getenv("SCOUT_TEST_REDIS_URL")}
	candidates = append(candidates, "redis://localhost:6380/2", "redis://localhost:6379/2")

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

	t.Skip("no reachable Redis (set SCOUT_TEST_REDIS_URL, or run `make dev-db`); skipping ratelimit tests")
	return nil
}

// uniqueDomain gives each test its own bucket key, so tests run with
// -parallel or reusing the same Redis DB between runs cannot see each other's
// state.
func uniqueDomain(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s.%d.example.test", t.Name(), time.Now().UnixNano())
}

func TestAllowRespectsBurst(t *testing.T) {
	rdb := testRedis(t)
	l := New(rdb)
	domain := uniqueDomain(t)
	ctx := context.Background()

	// burst=2: the first two calls succeed immediately (the bucket starts
	// full), the third must wait — this is the exact shape docs/06 specifies
	// ("rate: source.max_rps, default 0.5/s, burst: 2").
	for i := 0; i < 2; i++ {
		allowed, err := l.Allow(ctx, domain, 0.5, 2)
		if err != nil {
			t.Fatalf("Allow #%d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("Allow #%d = false, want true (within burst)", i)
		}
	}

	allowed, err := l.Allow(ctx, domain, 0.5, 2)
	if err != nil {
		t.Fatalf("Allow #3: %v", err)
	}
	if allowed {
		t.Fatal("Allow #3 = true, want false (burst exhausted)")
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	rdb := testRedis(t)
	l := New(rdb)
	domain := uniqueDomain(t)
	ctx := context.Background()

	// A fast rate for a fast test: 10 tokens/sec, burst 1. Exhaust it, wait
	// past one refill interval, confirm it opened back up.
	const rate = 10.0
	const burst = 1

	allowed, err := l.Allow(ctx, domain, rate, burst)
	if err != nil || !allowed {
		t.Fatalf("first Allow = (%v, %v), want (true, nil)", allowed, err)
	}

	allowed, err = l.Allow(ctx, domain, rate, burst)
	if err != nil {
		t.Fatalf("second Allow: %v", err)
	}
	if allowed {
		t.Fatal("second Allow = true immediately after exhausting burst 1, want false")
	}

	time.Sleep(200 * time.Millisecond) // > 1/rate = 100ms

	allowed, err = l.Allow(ctx, domain, rate, burst)
	if err != nil {
		t.Fatalf("Allow after refill wait: %v", err)
	}
	if !allowed {
		t.Fatal("Allow after waiting past the refill interval = false, want true")
	}
}

func TestDomainsAreIndependent(t *testing.T) {
	// The entire reason the bucket is keyed by registered domain: exhausting
	// one domain's budget must not touch a different domain's.
	rdb := testRedis(t)
	l := New(rdb)
	ctx := context.Background()
	domainA := uniqueDomain(t) + ".a"
	domainB := uniqueDomain(t) + ".b"

	// Exhaust domain A's single-token burst.
	if allowed, err := l.Allow(ctx, domainA, 0.5, 1); err != nil || !allowed {
		t.Fatalf("exhausting domain A: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := l.Allow(ctx, domainA, 0.5, 1); err != nil || allowed {
		t.Fatalf("domain A should now be exhausted: allowed=%v err=%v", allowed, err)
	}

	// Domain B must be unaffected.
	if allowed, err := l.Allow(ctx, domainB, 0.5, 1); err != nil || !allowed {
		t.Fatalf("domain B affected by domain A's exhaustion: allowed=%v err=%v", allowed, err)
	}
}

func TestAllowIsAtomicUnderConcurrency(t *testing.T) {
	// The whole point of evaluating the bucket as a Lua script rather than as
	// separate GET/SET calls: concurrent callers must never collectively admit
	// more than burst requests, which a check-then-set race would allow.
	rdb := testRedis(t)
	l := New(rdb)
	domain := uniqueDomain(t)
	ctx := context.Background()

	const burst = 5
	const callers = 50

	var admitted atomic.Int32
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			// A very slow rate so refill during the test is negligible —
			// isolates the assertion to "not more than burst admitted."
			allowed, err := l.Allow(ctx, domain, 0.001, burst)
			if err != nil {
				t.Errorf("Allow: %v", err)
				return
			}
			if allowed {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := admitted.Load(); got != burst {
		t.Fatalf("admitted %d requests against a burst of %d, want exactly %d", got, burst, burst)
	}
}

func TestAllowValidatesInput(t *testing.T) {
	rdb := testRedis(t)
	l := New(rdb)
	ctx := context.Background()

	tests := []struct {
		name   string
		domain string
		rps    float64
		burst  int
	}{
		{"zero rate", uniqueDomain(t), 0, 2},
		{"negative rate", uniqueDomain(t), -1, 2},
		{"zero burst", uniqueDomain(t), 0.5, 0},
		{"negative burst", uniqueDomain(t), 0.5, -1},
		{"empty domain", "", 0.5, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := l.Allow(ctx, tt.domain, tt.rps, tt.burst)
			if err == nil {
				t.Fatalf("Allow(%q, %v, %v) succeeded, want a validation error", tt.domain, tt.rps, tt.burst)
			}
		})
	}
}
