// Command api serves the Scout HTTP API.
//
// Today it serves the health check and the authentication endpoints. The
// resource routes land with the pipeline that produces something to serve — see
// docs/04-api-design.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kelyon/scout/apps/api/internal/auth"
	"github.com/kelyon/scout/apps/api/internal/jobs"
	"github.com/kelyon/scout/apps/api/internal/resume"
	"github.com/kelyon/scout/apps/api/internal/search"
	"github.com/kelyon/scout/apps/api/internal/stream"
	"github.com/kelyon/scout/packages/logging"
	"github.com/kelyon/scout/packages/metrics"
	"github.com/kelyon/scout/packages/queue"
	"github.com/kelyon/scout/packages/tracing"
)

func main() {
	log := slog.New(logging.Scrub(slog.NewJSONHandler(os.Stdout, nil)))

	// `api healthcheck` is what the container healthcheck runs. The image is
	// distroless: no shell, no curl, nothing else that could probe it. Probing
	// itself over the loopback interface also exercises the same path a real
	// caller takes, which a process-liveness check would not.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := selfCheck(listenAddr()); err != nil {
			log.Error("unhealthy", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := run(log); err != nil {
		log.Error("exited with error", "err", err)
		os.Exit(1)
	}
}

func listenAddr() string {
	if addr := os.Getenv("SCOUT_API_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}

// selfCheck probes /health over the loopback interface.
func selfCheck(addr string) error {
	// SCOUT_API_ADDR is host:port, and a bare ":8080" has to become a dialable
	// address rather than a hostname of "".
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse listen address: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+net.JoinHostPort(host, port)+"/health", http.NoBody)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned status %d", resp.StatusCode)
	}
	return nil
}

func run(log *slog.Logger) error {
	addr := listenAddr()

	shutdownTracing, err := tracing.Setup(context.Background(), "api", log)
	if err != nil {
		return fmt.Errorf("configure tracing: %w", err)
	}
	defer func() {
		if shutdownErr := shutdownTracing(context.Background()); shutdownErr != nil {
			log.Warn("tracing shutdown failed", "err", shutdownErr)
		}
	}()

	// Fail closed. An API that starts without authentication because a variable
	// was misspelled is worse than one that refuses to start, and on a system
	// whose entire access-control story is one secret, this is the check that
	// makes the story true. See ADR-015.
	//
	// Secure cookies everywhere except local: production is HTTPS via
	// `tailscale cert`, and a browser will not store a Secure cookie issued over
	// plain HTTP, which is how local development otherwise appears to log in and
	// then immediately does not.
	authenticator, err := auth.New(os.Getenv("SCOUT_AUTH_TOKEN"), os.Getenv("SCOUT_ENV") != "local")
	if err != nil {
		return fmt.Errorf("configure auth: %w", err)
	}

	pool, err := buildDatabasePool(context.Background())
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer pool.Close()

	queueClient, err := queue.New(pool)
	if err != nil {
		return fmt.Errorf("configure queue: %w", err)
	}

	resumeHandler := resume.New(pool, queueClient, log)
	jobsHandler := jobs.New(pool, queueClient, log)
	searchHandler := search.New(pool, log)

	broker := stream.NewBroker()
	streamCtx, stopStream := context.WithCancel(context.Background())
	defer stopStream()
	go broker.Run(streamCtx, pool, log)
	streamHandler := stream.New(broker)

	srv := &http.Server{
		Addr: addr,
		// otelhttp wraps every request in a span, exported per tracing.Setup
		// above. Placed outside observeRequests deliberately: otelhttp needs
		// to see the real net/http ResponseWriter to start the span before
		// routing happens, and observeRequests already forwards Flush
		// through its own wrapper, so nesting them this way doesn't lose
		// SSE's flushing either.
		Handler:           otelhttp.NewHandler(routes(log, authenticator, resumeHandler, jobsHandler, searchHandler, streamHandler), "api"),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen and serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// routes builds the request multiplexer.
//
// The shape matters: only /health and the token exchange are reachable without
// a credential, and everything else — including paths that do not exist yet —
// goes through the authenticator via the "/" catch-all. An unauthenticated
// caller therefore cannot distinguish a route that exists from one that does
// not, and adding a route cannot accidentally add an unauthenticated one.
func routes(
	log *slog.Logger, a *auth.Authenticator, resumeHandler *resume.Handler,
	jobsHandler *jobs.Handler, searchHandler *search.Handler, streamHandler *stream.Handler,
) http.Handler {
	protected := http.NewServeMux()
	protected.HandleFunc("POST /v1/resume", resumeHandler.Upload)
	protected.HandleFunc("GET /v1/resume", resumeHandler.Status)
	protected.HandleFunc("GET /v1/jobs", jobsHandler.List)
	protected.HandleFunc("GET /v1/jobs/{group_id}", jobsHandler.Detail)
	protected.HandleFunc("POST /v1/jobs/{group_id}/state", jobsHandler.State)
	protected.HandleFunc("GET /v1/applications", jobsHandler.Applications)
	protected.HandleFunc("GET /v1/search", searchHandler.Search)
	protected.HandleFunc("GET /v1/stream", streamHandler.Stream)

	root := http.NewServeMux()

	// Shallow liveness only: the process is up and can serve a request. It is
	// unauthenticated because the container runtime probes it and holds no
	// token.
	//
	// /health/deep verifies Postgres, Redis, and provider reachability. It is
	// for monitoring rather than the container runtime — a deep check failing
	// should alert, not restart the container — and it arrives with the database
	// client, which is the first thing that will have a connection to check.
	// See docs/15 section 2.
	root.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
	})

	// Unauthenticated, like /health — Prometheus scrapes this directly and
	// the whole host has no public ingress (ADR-014); see packages/metrics'
	// own comment.
	root.Handle("GET /metrics", metrics.Handler())

	// Typed once per device, ever. See ADR-015.
	root.Handle("POST /auth/session", a.SessionHandler())
	root.Handle("POST /auth/logout", a.LogoutHandler())

	root.Handle("/", a.Middleware(log, protected))

	return observeRequests(root)
}

// observeRequests wraps next with scout_api_request_duration_seconds
// (docs/16-observability.md, the API latency SLO). route is the matched
// mux pattern (e.g. "GET /v1/jobs/{group_id}"), not the raw path — a raw
// path would give every job's detail request its own high-cardinality
// series. http.ServeMux.Handler resolves the pattern without actually
// invoking the handler, so this costs one extra (cheap) mux lookup per
// request rather than requiring next to report its own route.
func observeRequests(next *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, route := next.Handler(r)
		if route == "" {
			route = "unmatched"
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		metrics.APIRequestDuration.WithLabelValues(route, strconv.Itoa(rec.status)).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder captures the status code a handler wrote, since
// net/http gives no way to read it back afterward otherwise.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying ResponseWriter's own Flush when it has
// one — GET /v1/stream's SSE handler type-asserts its ResponseWriter to
// http.Flusher (apps/api/internal/stream/handler.go) and would silently
// stop flushing, breaking the live feed, if this wrapper didn't pass that
// capability through.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// buildDatabasePool mirrors apps/collector/cmd's own helper of the same
// name — same fail-closed posture (a missing SCOUT_DATABASE_URL or an
// unreachable database stops this process from starting rather than
// starting and serving errors on the first real request).
func buildDatabasePool(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("SCOUT_DATABASE_URL")
	if databaseURL == "" {
		return nil, errors.New("SCOUT_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
