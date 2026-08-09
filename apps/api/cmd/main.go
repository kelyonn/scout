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
	"syscall"
	"time"

	"github.com/kelyon/scout/apps/api/internal/auth"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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

	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(log, authenticator),
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
func routes(log *slog.Logger, a *auth.Authenticator) http.Handler {
	protected := http.NewServeMux()

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

	// Typed once per device, ever. See ADR-015.
	root.Handle("POST /auth/session", a.SessionHandler())
	root.Handle("POST /auth/logout", a.LogoutHandler())

	root.Handle("/", a.Middleware(log, protected))

	return root
}
