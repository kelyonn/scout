// Package ssrf implements the collector's SSRF-safe outbound dialer:
// docs/06-ingestion-pipeline.md section 5. Any package that sends a request
// to an attacker-influenceable host — a source's own URL, a URL discovered in
// an email alert or an HTML link, or that source's robots.txt — needs this,
// not just the fetcher. robots.txt is served by the same host a source's
// content is, so it is exactly as capable of pointing DNS at an internal
// address as the content fetch is; a checker that fetched robots.txt through
// the default transport while the content fetch went through this one would
// be defended on one path and not the other, for no reason a site operator
// couldn't discover and exploit.
package ssrf

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DNSTimeout and ConnectTimeout are the first two tiers of docs/06 section
// 5's timeout table. They are enforced here, inside the dialer, rather than
// as a single combined dial timeout, because this dialer is also where DNS
// resolution happens — it has to be, since SSRF protection requires resolving
// the hostname ourselves before anything connects to it (see the package
// comment).
const (
	DNSTimeout     = 3 * time.Second
	ConnectTimeout = 5 * time.Second
)

// DialContext resolves host, rejects it if every resolved address is private
// or reserved, and dials the first public address directly — by IP, never by
// hostname a second time.
//
// This is the whole SSRF defense docs/06 section 5 describes: "resolve DNS
// first, then check the resolved IP, then connect to that IP with the
// hostname pinned for TLS. Checking the hostname alone is vulnerable to DNS
// rebinding" — a server could answer the DNS check with a public IP and then
// answer the real connection with a private one if the two steps used
// different lookups. Using this func as http.Transport.DialContext makes
// every connection net/http ever opens, including ones triggered by
// following a redirect to a different host, go through the same check: a
// redirect to a new host dials fresh via this function, so "re-check the IP
// at every hop" falls out for free rather than needing separate handling in
// the redirect policy.
//
// TLS is still pinned to the original hostname despite dialing by IP: this
// function returns a plain net.Conn, and http.Transport performs the TLS
// handshake on top of it itself, deriving the certificate's expected
// ServerName from the request's URL host — not from whatever address actually
// got dialed. Connecting to the right (validated) IP and verifying the
// certificate against the right (original) hostname are two different
// concerns, and this split is what lets both be correct at once.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("ssrf: split host and port for %q: %w", addr, err)
	}

	dnsCtx, cancel := context.WithTimeout(ctx, DNSTimeout)
	ips, err := net.DefaultResolver.LookupIPAddr(dnsCtx, host)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("ssrf: resolve %s: %w", host, err)
	}

	safeIP, err := FirstPublicIP(ips)
	if err != nil {
		return nil, fmt.Errorf("ssrf: %s: %w", host, err)
	}

	dialer := &net.Dialer{Timeout: ConnectTimeout}
	connCtx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()

	conn, err := dialer.DialContext(connCtx, network, net.JoinHostPort(safeIP.String(), port))
	if err != nil {
		return nil, fmt.Errorf("ssrf: connect to %s (%s): %w", host, safeIP, err)
	}
	return conn, nil
}

// FirstPublicIP returns the first address in ips that is not private,
// loopback, link-local, or unspecified, or an error naming why none
// qualified.
//
// It does not try every address and fall back on connect failure — a source
// whose DNS only resolves to a private address is refused outright rather
// than retried against a second candidate, and a source with a genuinely
// flaky public endpoint is the scheduler's circuit breaker's problem, not
// this function's.
func FirstPublicIP(ips []net.IPAddr) (net.IP, error) {
	for _, addr := range ips {
		if IsPublic(addr.IP) {
			return addr.IP, nil
		}
	}
	return nil, fmt.Errorf("no public address among %d resolved (private, loopback, or link-local)", len(ips))
}

// IsPublic rejects exactly the ranges docs/06 section 5 lists — 10/8,
// 172.16/12, 192.168/16, 127/8, 169.254/16, ::1, fc00::/7, fe80::/10 — plus
// the unspecified address (0.0.0.0, ::), which the spec does not name but
// which is just as unsafe a dial target and costs nothing extra to reject.
func IsPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}
