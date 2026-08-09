package ssrf

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		// The exact ranges docs/06 section 5 names.
		{"10/8", "10.1.2.3", false},
		{"172.16/12", "172.20.1.1", false},
		{"172.16/12 boundary just inside", "172.31.255.255", false},
		{"192.168/16", "192.168.1.1", false},
		{"127/8 loopback", "127.0.0.1", false},
		{"169.254/16 link-local", "169.254.1.1", false},
		{"::1 loopback", "::1", false},
		{"fc00::/7 unique local", "fc00::1", false},
		{"fc00::/7 upper half", "fd12:3456::1", false},
		{"fe80::/10 link-local", "fe80::1", false},

		// Not in the spec's list by name, but the same class of unsafe target.
		{"0.0.0.0 unspecified", "0.0.0.0", false},
		{":: unspecified", "::", false},

		// Genuinely public addresses must pass.
		{"public IPv4", "8.8.8.8", true},
		{"another public IPv4", "1.1.1.1", true},
		{"public IPv6", "2001:4860:4860::8888", true},

		// Addresses just outside the private ranges, to catch an
		// off-by-one in the boundary logic.
		{"just below 172.16/12", "172.15.255.255", true},
		{"just above 172.16/12", "172.32.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("test bug: %q did not parse as an IP", tt.ip)
			}
			if got := IsPublic(ip); got != tt.want {
				t.Errorf("IsPublic(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}

	t.Run("nil IP is rejected", func(t *testing.T) {
		if IsPublic(nil) {
			t.Error("IsPublic(nil) = true, want false")
		}
	})
}

func TestFirstPublicIP(t *testing.T) {
	addr := func(s string) net.IPAddr { return net.IPAddr{IP: net.ParseIP(s)} }

	t.Run("skips private addresses to find the public one", func(t *testing.T) {
		ips := []net.IPAddr{addr("10.0.0.1"), addr("192.168.1.1"), addr("8.8.8.8")}
		got, err := FirstPublicIP(ips)
		if err != nil {
			t.Fatalf("firstPublicIP: %v", err)
		}
		if !got.Equal(net.ParseIP("8.8.8.8")) {
			t.Errorf("firstPublicIP = %s, want 8.8.8.8", got)
		}
	})

	t.Run("all private is an error", func(t *testing.T) {
		ips := []net.IPAddr{addr("10.0.0.1"), addr("127.0.0.1")}
		_, err := FirstPublicIP(ips)
		if err == nil {
			t.Fatal("firstPublicIP succeeded against an all-private candidate list")
		}
	})

	t.Run("empty list is an error", func(t *testing.T) {
		_, err := FirstPublicIP(nil)
		if err == nil {
			t.Fatal("firstPublicIP succeeded against an empty candidate list")
		}
	})
}

func TestSafeDialContextRefusesLoopback(t *testing.T) {
	// A literal IP as the dial target resolves via net.DefaultResolver without
	// an actual DNS query, so this exercises the full safeDialContext path —
	// resolve, classify, refuse — without needing network access or a running
	// listener.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := DialContext(ctx, "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("safeDialContext succeeded against 127.0.0.1, want a refusal")
	}
	if !strings.Contains(err.Error(), "no public address") {
		t.Errorf("error = %q, want it to explain the refusal", err)
	}
}

func TestSafeDialContextRefusesPrivateRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := DialContext(ctx, "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("safeDialContext succeeded against a 10/8 address, want a refusal")
	}
}

func TestSafeDialContextRejectsMalformedAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := DialContext(ctx, "tcp", "not-a-host-port")
	if err == nil {
		t.Fatal("safeDialContext succeeded against an address with no port")
	}
}

func TestSafeDialContextReachesAPublicAddress(t *testing.T) {
	// Requires outbound network access, unlike every other test in this file.
	// Confirms the happy path actually connects, not merely that it classifies
	// correctly — isPublic and firstPublicIP already prove the classification
	// logic in isolation.
	if testing.Short() {
		t.Skip("short mode: skipping the one fetch test that needs real network access")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := DialContext(ctx, "tcp", "dns.google:443")
	if err != nil {
		t.Skipf("no outbound network access in this environment: %v", err)
	}
	_ = conn.Close()
}
