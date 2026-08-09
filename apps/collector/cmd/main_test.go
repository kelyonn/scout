package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testRedis mirrors the helper of the same shape used across the collector's
// internal packages. See the comment there on why it is duplicated rather than
// shared: a handful of small, independent test-only helpers cost less than the
// coupling a shared internal test package would add.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	candidates := []string{os.Getenv("SCOUT_TEST_REDIS_URL")}
	candidates = append(candidates, "redis://localhost:6380/4", "redis://localhost:6379/4")

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

	t.Skip("no reachable Redis (set SCOUT_TEST_REDIS_URL, or run `make dev-db`); skipping collector main tests")
	return nil
}

// TestBuildPolitenessGateRequiresConfiguration covers only Redis
// configuration now: the contact URL and operator email (SCOUT-LEGAL-003)
// moved to run(), which validates them once and passes both down to
// buildPolitenessGate and fetch.New as plain parameters — see run()'s comment
// on why the same identity feeds both. That split is exercised by
// TestRunRequiresContactAndOperatorEmail below.
func TestBuildPolitenessGateRequiresConfiguration(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "no SCOUT_REDIS_URL at all",
			env:  map[string]string{},
		},
		{
			name: "unparseable SCOUT_REDIS_URL",
			env:  map[string]string{"SCOUT_REDIS_URL": "not a url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SCOUT_REDIS_URL", "")
			_ = os.Unsetenv("SCOUT_REDIS_URL")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			gate, rdb, err := buildPolitenessGate(testLogger(), "https://scout.example/bot", "operator@example.com")

			if err == nil {
				t.Fatal("buildPolitenessGate succeeded with incomplete configuration, want an error")
			}
			if gate != nil {
				t.Error("buildPolitenessGate returned a non-nil Gate alongside an error")
			}
			if rdb != nil {
				t.Error("buildPolitenessGate returned a non-nil Redis client alongside an error")
			}
		})
	}
}

func TestBuildPolitenessGateSucceedsWhenFullyConfigured(t *testing.T) {
	testRedis(t) // requires Redis to be reachable; skips otherwise

	t.Setenv("SCOUT_REDIS_URL", "redis://localhost:6379/4")
	if os.Getenv("SCOUT_TEST_REDIS_URL") != "" {
		t.Setenv("SCOUT_REDIS_URL", os.Getenv("SCOUT_TEST_REDIS_URL"))
	}

	gate, rdb, err := buildPolitenessGate(testLogger(), "https://scout.example/bot", "operator@example.com")
	if err != nil {
		t.Fatalf("buildPolitenessGate: %v", err)
	}
	defer func() { _ = rdb.Close() }()

	if gate == nil {
		t.Fatal("buildPolitenessGate returned a nil Gate with no error")
	}
}

func TestRunRequiresContactAndOperatorEmail(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"neither set", map[string]string{}},
		{"only contact URL", map[string]string{"SCOUT_COLLECTOR_CONTACT_URL": "https://example.com/bot"}},
		{"only operator email", map[string]string{"SCOUT_COLLECTOR_OPERATOR_EMAIL": "operator@example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"SCOUT_COLLECTOR_CONTACT_URL", "SCOUT_COLLECTOR_OPERATOR_EMAIL"} {
				t.Setenv(k, "")
				_ = os.Unsetenv(k)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// run() fails on this check before it ever touches Redis or
			// Postgres, so no live backing services are needed to prove it.
			err := run(testLogger())
			if err == nil {
				t.Fatal("run() succeeded with the contact URL or operator email missing, want an error")
			}
		})
	}
}

// TestSelfCheckPolitenessGatePasses proves the self-check that runs at every
// collector startup actually detects the healthy case correctly — the
// complementary case (a broken gate correctly failing the check) is exercised
// indirectly by every test in apps/collector/internal/politeness that asserts
// a prohibited source is refused; selfCheckPolitenessGate is a thin wrapper
// around that exact assertion, not a second implementation of it.
func TestSelfCheckPolitenessGatePasses(t *testing.T) {
	rdb := testRedis(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The self-check uses a prohibited source, which must never reach this
		// handler at all — if it does, something is badly wrong upstream of the
		// assertion below, so failing the request loudly here too is cheap
		// insurance.
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	checker := robots.New(robots.NewRedisCache(rdb), "https://scout.example/bot", "operator@example.com")
	limiter := ratelimit.New(rdb)
	gate := politeness.New(checker, limiter, rdb, testLogger())

	if err := selfCheckPolitenessGate(context.Background(), gate); err != nil {
		t.Fatalf("selfCheckPolitenessGate on a correctly wired gate: %v", err)
	}
}

func TestHealthcheckIntervalParsing(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset uses the default", "", 5 * time.Minute},
		{"valid duration is used", "10m", 10 * time.Minute},
		{"unparseable value falls back to the default", "soon", 5 * time.Minute},
		{"empty value falls back to the default", "", 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SCOUT_HEALTHCHECK_INTERVAL", tt.env)
			if got := healthcheckInterval(testLogger()); got != tt.want {
				t.Errorf("healthcheckInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLivenessPath(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		t.Setenv("SCOUT_LIVENESS_FILE", "")
		_ = os.Unsetenv("SCOUT_LIVENESS_FILE")
		if got := livenessPath(); got != defaultLivenessPath {
			t.Errorf("livenessPath() = %q, want %q", got, defaultLivenessPath)
		}
	})

	t.Run("override when set", func(t *testing.T) {
		t.Setenv("SCOUT_LIVENESS_FILE", "/custom/path")
		if got := livenessPath(); got != "/custom/path" {
			t.Errorf("livenessPath() = %q, want /custom/path", got)
		}
	})
}
