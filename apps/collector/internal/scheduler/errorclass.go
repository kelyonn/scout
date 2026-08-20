package scheduler

import (
	"crypto/tls"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// statusAction is docs/06 section 10's per-status-code table, reduced to the
// branches pollOne needs beyond the existing success/transient-failure path
// outcomeFromResult already handles. Anything not listed here — 2xx, 304,
// 5xx, other 4xx — keeps going through that existing coarse path; see the
// package comment for why that coarsening is the safe direction.
type statusAction int

const (
	actionNormal statusAction = iota
	actionQuarantine
	actionNotFound
	actionRateLimited
)

// classifyStatus implements the four rows of docs/06 section 10's table
// this package can act on without a schema change: 401/403 (quarantine),
// 404 (retire after three consecutive), 429 (honor Retry-After, halve
// max_rps). TLS errors are classified separately, in classifyTransportErr,
// since they arrive as a Go error rather than an HTTP status.
func classifyStatus(code int) statusAction {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return actionQuarantine
	case http.StatusNotFound:
		return actionNotFound
	case http.StatusTooManyRequests:
		return actionRateLimited
	default:
		return actionNormal
	}
}

// isTLSError reports whether err is a TLS handshake or certificate failure
// — docs/06 section 10: "TLS error: Persistent. Fail immediately, alert —
// usually a real config change." Matched by type first (the precise cases),
// falling back to *tls.RecordHeaderError's own message text for the case Go
// represents as a raw "tls:" prefixed error rather than a typed one — the
// same "coarse but safe" tradeoff as the rest of this package's error
// handling, not a claim of exhaustive TLS error coverage.
func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}
	var recordErr tls.RecordHeaderError
	return errors.As(err, &recordErr)
}

// parseRetryAfter implements the two forms RFC 9110 allows: delay-seconds
// ("120") or an HTTP-date. Returns ok=false if value is empty or neither
// form parses, leaving the caller to fall back to its own default backoff.
func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return now.Add(time.Duration(secs) * time.Second), true
	}
	if t, err := http.ParseTime(value); err == nil {
		return t, true
	}
	return time.Time{}, false
}
