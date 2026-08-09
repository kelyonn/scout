package heartbeat

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recorder captures the paths a Pinger requested.
type recorder struct {
	mu     sync.Mutex
	paths  []string
	status int
	count  atomic.Int64
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec.mu.Lock()
	rec.paths = append(rec.paths, r.URL.Path)
	rec.mu.Unlock()
	rec.count.Add(1)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
}

func (rec *recorder) seen() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.paths...)
}

func TestPing(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"200 is a successful ping", http.StatusOK, false},
		{"201 is a successful ping", http.StatusCreated, false},
		{"500 is an error", http.StatusInternalServerError, true},
		{"404 is an error", http.StatusNotFound, true},
		{"302 is an error, not a success", http.StatusFound, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{status: tt.status}
			srv := httptest.NewServer(rec)
			defer srv.Close()

			err := New(srv.URL, testLogger()).Ping(context.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("Ping error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPingErrorNeverContainsTheURL(t *testing.T) {
	// The ping URL embeds the check's UUID, which makes it a bearer credential.
	// An error string that quotes it puts it in the log — AGENTS.md rule 7. The
	// stdlib's *url.Error does exactly this, so the wrapping has to be checked
	// rather than assumed.
	srv := httptest.NewServer(&recorder{status: http.StatusInternalServerError})
	defer srv.Close()

	err := New(srv.URL, testLogger()).Ping(context.Background())
	if err == nil {
		t.Fatal("want an error from a 500")
	}
	if strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error leaks the ping URL: %v", err)
	}
}

func TestTransportErrorNeverContainsTheURL(t *testing.T) {
	// The transport path is the one that leaks by default: the stdlib wraps
	// every client failure in a *url.Error whose Error() quotes the full URL,
	// UUID and all. This is the case redactURL exists for.
	srv := httptest.NewServer(&recorder{})
	pingURL := srv.URL + "/deadbeef-0000-1111-2222-333344445555"
	srv.Close() // nothing is listening now, so Do fails at the transport layer

	err := New(pingURL, testLogger()).Ping(context.Background())
	if err == nil {
		t.Fatal("want an error when nothing is listening")
	}
	if strings.Contains(err.Error(), "deadbeef-0000-1111-2222-333344445555") {
		t.Fatalf("error leaks the check UUID: %v", err)
	}
}

func TestFailUsesTheFailEndpoint(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	if err := New(srv.URL, testLogger()).Fail(context.Background()); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	paths := rec.seen()
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/fail") {
		t.Fatalf("requested %v, want a single /fail path", paths)
	}
}

func TestDisabledPingerSendsNothing(t *testing.T) {
	// Local development. A laptop that gets closed at night is not a host
	// outage, and a nightly false alarm is how the one alert that always matters
	// gets muted.
	p := New("", testLogger())

	if p.Enabled() {
		t.Fatal("a Pinger with no URL reports itself enabled")
	}
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("disabled Ping returned an error: %v", err)
	}
	if err := p.Fail(context.Background()); err != nil {
		t.Fatalf("disabled Fail returned an error: %v", err)
	}
}

func TestRunKeepsRunningWithoutAPingURL(t *testing.T) {
	// Run is the collector's main loop. A service that exits because its
	// monitoring is unconfigured is a service that will not start on a laptop.
	dir := t.TempDir()
	livenessPath := filepath.Join(dir, "alive")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		New("", testLogger()).WithLiveness(livenessPath).Run(ctx, 20*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Run returned early with no URL configured")
	case <-time.After(100 * time.Millisecond):
	}

	// It should still be reporting liveness, so the container healthcheck of an
	// unmonitored collector does not flap.
	if err := Fresh(livenessPath, time.Second); err != nil {
		t.Fatalf("liveness not maintained without a ping URL: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestRunPingsImmediatelyAndOnTheTicker(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		New(srv.URL, testLogger()).Run(ctx, 20*time.Millisecond)
		close(done)
	}()

	// The immediate ping matters on its own: waiting a full interval after a
	// restart leaves a gap indistinguishable from a crash.
	waitFor(t, func() bool { return rec.count.Load() >= 1 }, time.Second,
		"no ping was sent before the first tick")
	waitFor(t, func() bool { return rec.count.Load() >= 3 }, 2*time.Second,
		"the ticker did not keep pinging")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestRunSurvivesAFailingSwitch(t *testing.T) {
	// A failed ping is never fatal. Killing the collector because it could not
	// tell the switch it was alive would be an outage caused by the monitoring.
	rec := &recorder{status: http.StatusInternalServerError}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		New(srv.URL, testLogger()).Run(ctx, 20*time.Millisecond)
		close(done)
	}()

	waitFor(t, func() bool { return rec.count.Load() >= 3 }, 2*time.Second,
		"Run stopped pinging after a failure")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func waitFor(t *testing.T, cond func() bool, limit time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}
