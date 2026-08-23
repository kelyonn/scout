package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelfCheckOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := selfCheck(strings.TrimPrefix(srv.URL, "http://")); err != nil {
		t.Fatalf("selfCheck() = %v, want nil", err)
	}
}

func TestSelfCheckNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := selfCheck(strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Fatal("selfCheck() = nil, want error for a 503 response")
	}
}

func TestSelfCheckUnreachable(t *testing.T) {
	// A closed listener: bind then close, so the address is real but nothing
	// is listening on it — a genuine connection-refused case, not a made-up
	// address a firewall or DNS quirk could resolve unpredictably.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	if err := selfCheck(addr); err == nil {
		t.Fatal("selfCheck() = nil, want error for an unreachable address")
	}
}
