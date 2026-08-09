package politeness

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
	"github.com/kelyon/scout/apps/collector/internal/source"
)

// testRedis mirrors the helper of the same shape in the ratelimit and robots
// packages. Not shared for the same reason given there: three small,
// independent test-only helpers are cheaper than the coupling a shared
// internal test package would add.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	candidates := []string{os.Getenv("SCOUT_TEST_REDIS_URL")}
	candidates = append(candidates, "redis://localhost:6380/3", "redis://localhost:6379/3")

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

	t.Skip("no reachable Redis (set SCOUT_TEST_REDIS_URL, or run `make dev-db`); skipping politeness tests")
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// uniqueDomain gives every test its own domain so their rate-limit and
// concurrency state in the shared Redis instance cannot interfere.
func uniqueDomain(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s-%d.example.test", t.Name(), time.Now().UnixNano())
}

// newGate wires a real robots.Checker (against srv, which must serve
// /robots.txt) and a real rate limiter, both backed by the same test Redis, so
// these tests exercise the actual composition rather than a mock of it.
func newGate(t *testing.T, srv *httptest.Server) (*Gate, *redis.Client) {
	t.Helper()
	rdb := testRedis(t)
	// NewForTesting, not New: New's dialer refuses 127.0.0.1, which is
	// exactly what srv (an httptest.Server) binds to. See its doc comment in
	// apps/collector/internal/robots/testing.go.
	checker := robots.NewForTesting(robots.NewRedisCache(rdb), robots.PlainDialContext,
		"https://scout.example/bot", "operator@example.com")
	limiter := ratelimit.New(rdb)
	return New(checker, limiter, rdb, testLogger()), rdb
}

func robotsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func baseSource(t *testing.T, srv *httptest.Server) source.Source {
	t.Helper()
	return source.Source{
		ID:           "test-source",
		Kind:         "ats_greenhouse",
		LegalPosture: source.PosturePermitted,
		URL:          srv.URL + "/board",
		// Fast enough that back-to-back calls on the same domain within one test
		// never contend with the rate limiter incidentally: at 100rps a token
		// takes 10ms to refill, which repeated Allow() calls in a tight test loop
		// can easily outrun, exhausting the budget as a side effect of testing
		// something else entirely (concurrency, crawl-delay). 100,000rps makes a
		// token refill in 10µs — comfortably under a single Redis round trip.
		MaxRPS:         100000,
		MaxConcurrency: 10,
	}
}

// resetDomainState clears any rate-limit, crawl-delay, and concurrency state
// left in Redis for the shared "127.0.0.1" domain every httptest.Server binds
// to. RegisteredDomain deliberately strips the port (real domains do not
// rate-limit per port), which means every test in this file that talks to a
// local httptest server shares one bucket key unless it starts from a clean
// slate — this is that clean slate.
func resetDomainState(t *testing.T, rdb *redis.Client) {
	t.Helper()
	ctx := context.Background()
	for _, key := range []string{
		"ratelimit:host:127.0.0.1",
		"ratelimit:host:127.0.0.1#crawl-delay",
		"concurrency:host:127.0.0.1",
	} {
		if err := rdb.Del(ctx, key).Err(); err != nil {
			t.Fatalf("resetDomainState: del %s: %v", key, err)
		}
	}
}

// TestProhibitedSourceMakesZeroRequests is the test docs/14-legal-compliance.md
// section 1 requires by name: "a unit test asserting that a source marked
// `prohibited` produces zero outbound requests through every code path."
func TestProhibitedSourceMakesZeroRequests(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	gate, rdb := newGate(t, srv)
	domain := uniqueDomain(t)
	src := baseSource(t, srv)
	src.LegalPosture = source.PostureProhibited
	src.URL = "http://" + domain + "/board"

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultRefuse {
		t.Fatalf("Result = %v, want ResultRefuse", decision.Result)
	}
	if release != nil {
		t.Fatal("a refused decision returned a non-nil Release")
	}
	if requests != 0 {
		t.Fatalf("the HTTP server received %d requests for a prohibited source, want 0", requests)
	}

	// Zero requests to the source is necessary but not sufficient: a gate that
	// still spent rate-limit or concurrency budget on a prohibited source
	// before refusing would let that source silently starve other sources on
	// the same domain. Neither key should exist.
	ctx := context.Background()
	if n, _ := rdb.Exists(ctx, "ratelimit:host:"+domain).Result(); n != 0 {
		t.Error("a rate-limit bucket was created for a prohibited source")
	}
	if n, _ := rdb.Exists(ctx, "concurrency:host:"+domain).Result(); n != 0 {
		t.Error("a concurrency slot was created for a prohibited source")
	}
}

func TestEmailOnlySourceIsRefusedTheSameWay(t *testing.T) {
	// email_only is not "prohibited" in the schema, but docs/14's REFUSE
	// contract applies identically: never fetched directly.
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, _ := newGate(t, srv)
	src := baseSource(t, srv)
	src.LegalPosture = source.PostureEmailOnly

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultRefuse {
		t.Fatalf("Result = %v, want ResultRefuse", decision.Result)
	}
	if release != nil {
		t.Fatal("a refused decision returned a non-nil Release")
	}
}

func TestPermittedAndAPIOnlyPassTheLegalCheck(t *testing.T) {
	for _, posture := range []source.LegalPosture{source.PosturePermitted, source.PostureAPIOnly} {
		t.Run(string(posture), func(t *testing.T) {
			srv := robotsServer(t, "User-agent: *\nAllow: /\n")
			gate, rdb := newGate(t, srv)
			resetDomainState(t, rdb)
			src := baseSource(t, srv)
			src.LegalPosture = posture

			decision, release := gate.Allow(context.Background(), src)

			if decision.Result != ResultAllow {
				t.Fatalf("Result = %v (%s), want ResultAllow", decision.Result, decision.Reason)
			}
			if release == nil {
				t.Fatal("an allowed decision returned a nil Release")
			}
			release(context.Background())
		})
	}
}

func TestRobotsDisallowRefuses(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nDisallow: /board\n")
	gate, _ := newGate(t, srv)
	src := baseSource(t, srv)

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultRefuse {
		t.Fatalf("Result = %v, want ResultRefuse", decision.Result)
	}
	if release != nil {
		t.Fatal("a refused decision returned a non-nil Release")
	}
}

func TestRateBudgetExhaustionDefers(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	src.MaxRPS = 0.5 // the documented default; burst is DefaultBurst (2)

	// First DefaultBurst calls succeed; release each so concurrency is never
	// the thing that blocks the next one.
	for i := 0; i < DefaultBurst; i++ {
		decision, release := gate.Allow(context.Background(), src)
		if decision.Result != ResultAllow {
			t.Fatalf("call %d: Result = %v (%s), want ResultAllow", i, decision.Result, decision.Reason)
		}
		release(context.Background())
	}

	decision, release := gate.Allow(context.Background(), src)
	if decision.Result != ResultDefer {
		t.Fatalf("Result = %v, want ResultDefer once the burst is spent", decision.Result)
	}
	if release != nil {
		t.Fatal("a deferred decision returned a non-nil Release")
	}
}

func TestConcurrencyCapDefers(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	src.MaxConcurrency = 1

	decision1, release1 := gate.Allow(context.Background(), src)
	if decision1.Result != ResultAllow {
		t.Fatalf("first call: Result = %v (%s), want ResultAllow", decision1.Result, decision1.Reason)
	}
	defer release1(context.Background())

	// A second, distinct source on the same domain must be deferred: the cap
	// is per domain, not per source row.
	src2 := src
	src2.ID = "test-source-2"

	decision2, release2 := gate.Allow(context.Background(), src2)
	if decision2.Result != ResultDefer {
		t.Fatalf("second call: Result = %v, want ResultDefer while the first slot is held", decision2.Result)
	}
	if release2 != nil {
		t.Fatal("a deferred decision returned a non-nil Release")
	}
}

func TestConcurrencySlotIsFreedOnRelease(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	src.MaxConcurrency = 1

	decision1, release1 := gate.Allow(context.Background(), src)
	if decision1.Result != ResultAllow {
		t.Fatalf("first call: Result = %v (%s), want ResultAllow", decision1.Result, decision1.Reason)
	}
	release1(context.Background())

	// After releasing, the same domain must be allowed again immediately.
	decision2, release2 := gate.Allow(context.Background(), src)
	if decision2.Result != ResultAllow {
		t.Fatalf("second call after release: Result = %v (%s), want ResultAllow", decision2.Result, decision2.Reason)
	}
	release2(context.Background())
}

func TestCrawlDelayDefersAndReleasesConcurrency(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nCrawl-delay: 10\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	src.MaxConcurrency = 1
	delay := 10.0
	src.RobotsCrawlDelayS = &delay

	decision1, release1 := gate.Allow(context.Background(), src)
	if decision1.Result != ResultAllow {
		t.Fatalf("first call: Result = %v (%s), want ResultAllow", decision1.Result, decision1.Reason)
	}
	release1(context.Background())

	// Immediately again: crawl-delay of 10s has not elapsed, so this must
	// defer — even though MaxConcurrency=1 was just released and would
	// otherwise allow it, proving crawl-delay is enforced independently of
	// concurrency, not merely as a side effect of it.
	decision2, release2 := gate.Allow(context.Background(), src)
	if decision2.Result != ResultDefer {
		t.Fatalf("second call: Result = %v, want ResultDefer (crawl-delay not elapsed)", decision2.Result)
	}
	if release2 != nil {
		t.Fatal("a deferred decision returned a non-nil Release")
	}

	// And the concurrency slot the second call reserved-then-released must
	// truly be free, not leaked — proven by a third source being able to
	// acquire it.
	src3 := src
	src3.ID = "test-source-3"
	src3.RobotsCrawlDelayS = nil // isolate: only checking the concurrency slot here
	decision3, release3 := gate.Allow(context.Background(), src3)
	if decision3.Result != ResultAllow {
		t.Fatalf("third call: Result = %v (%s), want ResultAllow (slot should have been released)", decision3.Result, decision3.Reason)
	}
	release3(context.Background())
}

func TestCircuitOpenSkips(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	openUntil := time.Now().Add(time.Hour)
	src.CircuitOpenUntil = &openUntil

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultSkip {
		t.Fatalf("Result = %v, want ResultSkip", decision.Result)
	}
	if release != nil {
		t.Fatal("a skipped decision returned a non-nil Release")
	}
}

func TestCircuitOpenInThePastAllows(t *testing.T) {
	// CircuitOpenUntil in the past means the breaker is half-open or closed —
	// ready for a probe request, not still blocking.
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, rdb := newGate(t, srv)
	resetDomainState(t, rdb)
	src := baseSource(t, srv)
	past := time.Now().Add(-time.Minute)
	src.CircuitOpenUntil = &past

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultAllow {
		t.Fatalf("Result = %v (%s), want ResultAllow", decision.Result, decision.Reason)
	}
	release(context.Background())
}

func TestInvalidURLRefuses(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nAllow: /\n")
	gate, _ := newGate(t, srv)
	src := baseSource(t, srv)
	src.URL = "://not-a-valid-url"

	decision, release := gate.Allow(context.Background(), src)

	if decision.Result != ResultRefuse {
		t.Fatalf("Result = %v, want ResultRefuse for an unparseable URL", decision.Result)
	}
	if release != nil {
		t.Fatal("a refused decision returned a non-nil Release")
	}
}

func TestCheckOrderLegalPostureBeatsRobots(t *testing.T) {
	// A source that fails both check 1 (legal posture) and check 2 (robots)
	// must report the check-1 reason, per the gate's documented "preserve
	// spec order" contract — proven by asserting robots.txt was never fetched.
	robotsHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHit = true
		}
		w.WriteHeader(http.StatusForbidden) // would also disallow via robots if reached
	}))
	defer srv.Close()

	gate, _ := newGate(t, srv)
	src := baseSource(t, srv)
	src.LegalPosture = source.PostureProhibited

	decision, _ := gate.Allow(context.Background(), src)

	if decision.Result != ResultRefuse {
		t.Fatalf("Result = %v, want ResultRefuse", decision.Result)
	}
	if robotsHit {
		t.Fatal("robots.txt was fetched for a source that should have been refused on legal posture alone")
	}
}
