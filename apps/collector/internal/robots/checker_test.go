package robots

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCache is an in-memory Cache for tests that do not need Redis.
type fakeCache struct {
	mu    sync.Mutex
	store map[string]cacheEntry
	// setCalls counts writes, so tests can assert whether a failure path
	// avoided caching — see TestNetworkFailureIsNotCached.
	setCalls int
}

type cacheEntry struct {
	body   []byte
	status int
}

func newFakeCache() *fakeCache { return &fakeCache{store: map[string]cacheEntry{}} }

func (f *fakeCache) Get(_ context.Context, host string) ([]byte, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.store[host]
	return e.body, e.status, ok
}

func (f *fakeCache) Set(_ context.Context, host string, body []byte, status int, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[host] = cacheEntry{body: body, status: status}
	f.setCalls++
	return nil
}

func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// A real robots.txt fetch identifies us; a checker that forgot to set
		// these headers would be silently violating SCOUT-LEGAL-003.
		if r.Header.Get("User-Agent") == "" {
			t.Error("robots.txt request sent with no User-Agent")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hostOf strips the scheme off an httptest server URL, since Checker.Allowed
// takes scheme and host separately (it constructs the robots.txt URL itself).
func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return srv.Listener.Addr().String()
}

func TestCheckerAllowed(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		path   string
		want   bool
	}{
		{
			name:   "2xx is parsed normally",
			status: http.StatusOK,
			body:   "User-agent: *\nDisallow: /private",
			path:   "/private/x",
			want:   false,
		},
		{
			name:   "4xx means unrestricted",
			status: http.StatusNotFound,
			body:   "",
			path:   "/private/x",
			want:   true,
		},
		{
			name:   "5xx fails closed",
			status: http.StatusInternalServerError,
			body:   "",
			path:   "/anything",
			want:   false,
		},
		{
			name:   "503 fails closed just like 500",
			status: http.StatusServiceUnavailable,
			body:   "",
			path:   "/anything",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, tt.status, tt.body)
			c := NewForTesting(newFakeCache(), PlainDialContext, "https://scout.example/bot", "operator@example.com")

			got := c.Allowed(context.Background(), "http", hostOf(t, srv), tt.path)
			if got != tt.want {
				t.Errorf("Allowed(%q) = %v, want %v (status %d)", tt.path, got, tt.want, tt.status)
			}
		})
	}
}

func TestCheckerFailsClosedOnUnreachableHost(t *testing.T) {
	// No server at all. This is the "unreachable" case from docs/06 section 4,
	// distinct from a 5xx but required to fail the same way. Uses the real
	// New() deliberately — see TestCheckerRefusesLoopbackTarget below for why
	// that no longer changes the outcome of this particular assertion, and is
	// worth keeping on the real constructor anyway.
	c := New(newFakeCache(), "https://scout.example/bot", "operator@example.com")

	got := c.Allowed(context.Background(), "http", "127.0.0.1:1", "/anything")
	if got {
		t.Error("Allowed() = true against an unreachable host, want false (fail closed)")
	}
}

// TestCheckerRefusesLoopbackTarget uses the REAL New() constructor, not the
// test dialer — proving the production Checker, exactly as the collector
// will construct it, actually refuses to reach an internal address rather
// than merely that ssrf.DialContext would if called directly (which
// apps/collector/internal/ssrf's own tests already cover). robots.txt fetches
// carry the same SSRF posture as content fetches — see the comment on New().
func TestCheckerRefusesLoopbackTarget(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, "User-agent: *\nAllow: /\n")
	defer srv.Close()

	c := New(newFakeCache(), "https://scout.example/bot", "operator@example.com")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// A real httptest.Server on 127.0.0.1 must fail closed exactly like the
	// unreachable-host case above — the fail-closed *answer* is identical
	// whether the underlying cause is "nothing is listening" or "the SSRF
	// guard refused to dial a private address," which is the point: the
	// caller never has to distinguish them.
	got := c.Allowed(ctx, "http", hostOf(t, srv), "/anything")
	if got {
		t.Error("the production Checker reached an httptest.Server on 127.0.0.1, want a refusal")
	}
}

func TestNetworkFailureIsNotCached(t *testing.T) {
	// A transient network blip must not be cached as "disallowed" for 24
	// hours — see the comment on Checker.rules. Caching it would quietly
	// starve a source that was never actually blocked.
	cache := newFakeCache()
	c := New(cache, "https://scout.example/bot", "operator@example.com")

	c.Allowed(context.Background(), "http", "127.0.0.1:1", "/x")

	if cache.setCalls != 0 {
		t.Errorf("cache.Set was called %d times after a network failure, want 0", cache.setCalls)
	}
}

func TestCheckerUsesTheCache(t *testing.T) {
	// Once cached, a second call must not hit the network at all. Proven here
	// by closing the server before the second call.
	srv := newTestServer(t, http.StatusOK, "User-agent: *\nDisallow: /blocked")
	cache := newFakeCache()
	c := NewForTesting(cache, PlainDialContext, "https://scout.example/bot", "operator@example.com")
	host := hostOf(t, srv)

	if got := c.Allowed(context.Background(), "http", host, "/blocked"); got {
		t.Fatal("first call: Allowed(/blocked) = true, want false")
	}

	srv.Close() // the second call must not need the network

	if got := c.Allowed(context.Background(), "http", host, "/blocked"); got {
		t.Fatal("cached call: Allowed(/blocked) = true, want false")
	}
	if got := c.Allowed(context.Background(), "http", host, "/open"); !got {
		t.Fatal("cached call: Allowed(/open) = false, want true")
	}
}

func TestCheckerCachesTheStatusNotJustTheBody(t *testing.T) {
	// A cached 4xx must decode as unrestricted on replay, not be parsed as if
	// it were a 200 with an empty body (which would also happen to allow
	// everything, but for the wrong reason — this pins the real one).
	srv := newTestServer(t, http.StatusForbidden, "")
	cache := newFakeCache()
	c := NewForTesting(cache, PlainDialContext, "https://scout.example/bot", "operator@example.com")
	host := hostOf(t, srv)

	c.Allowed(context.Background(), "http", host, "/x") // populate the cache

	_, status, ok := cache.Get(context.Background(), host)
	if !ok {
		t.Fatal("nothing was cached")
	}
	if status != http.StatusForbidden {
		t.Fatalf("cached status = %d, want %d", status, http.StatusForbidden)
	}
}

func TestCrawlDelayThroughChecker(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, "User-agent: *\nCrawl-delay: 3\n")
	c := NewForTesting(newFakeCache(), PlainDialContext, "https://scout.example/bot", "operator@example.com")

	got := c.CrawlDelay(context.Background(), "http", hostOf(t, srv))
	if got == nil || *got != 3 {
		t.Fatalf("CrawlDelay() = %v, want 3", got)
	}
}

func TestIdentification(t *testing.T) {
	// SCOUT-LEGAL-003: every request identifies Scout honestly with a contact
	// URL and an operator email, so a site operator's first move is an email
	// rather than a block.
	var gotUA, gotFrom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotFrom = r.Header.Get("From")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewForTesting(newFakeCache(), PlainDialContext, "https://scout.example/bot", "operator@example.com")
	c.Allowed(context.Background(), "http", hostOf(t, srv), "/x")

	if gotFrom != "operator@example.com" {
		t.Errorf("From header = %q, want the operator email", gotFrom)
	}
	if !strings.Contains(gotUA, "Scout") || !strings.Contains(gotUA, "https://scout.example/bot") {
		t.Errorf("User-Agent = %q, want it to name Scout and the contact URL", gotUA)
	}
}

func TestCheckerNoCacheStillWorks(t *testing.T) {
	// A nil Cache must degrade to "always fetch", not panic. Useful for a
	// process that has not wired Redis yet.
	srv := newTestServer(t, http.StatusOK, "User-agent: *\nDisallow: /x")
	c := NewForTesting(nil, PlainDialContext, "https://scout.example/bot", "operator@example.com")

	if got := c.Allowed(context.Background(), "http", hostOf(t, srv), "/x"); got {
		t.Error("Allowed(/x) = true with no cache configured, want false")
	}
}

func TestFetchRespectsContextCancellation(t *testing.T) {
	// A slow server past the caller's deadline must fail closed promptly, not
	// hang until the package's own 15s timeout.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	// Registered in this order so defers unwind close(block) BEFORE srv.Close():
	// srv.Close() waits for the in-flight handler to return, and the handler is
	// parked on <-block until it does.
	defer srv.Close()
	defer close(block)

	c := NewForTesting(newFakeCache(), PlainDialContext, "https://scout.example/bot", "operator@example.com")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	got := c.Allowed(ctx, "http", hostOf(t, srv), "/x")
	elapsed := time.Since(start)

	if got {
		t.Error("Allowed() = true after the request timed out, want false (fail closed)")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Allowed() took %v to fail after a 100ms deadline; it waited for the package timeout instead", elapsed)
	}
}
