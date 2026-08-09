// Package politeness implements the collector's compliance gate:
// docs/06-ingestion-pipeline.md section 4 and docs/14-legal-compliance.md
// section 1.
//
// AGENTS.md rule 1 is the reason this package exists at all: "Every outbound
// HTTP request goes through PolitenessGate.Allow(). No exceptions, no direct
// http.Get, no 'just for testing'." Nothing in the collector is permitted to
// construct an HTTP request to a source without a passing Decision from this
// package first.
package politeness

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kelyon/scout/apps/collector/internal/ratelimit"
	"github.com/kelyon/scout/apps/collector/internal/robots"
	"github.com/kelyon/scout/apps/collector/internal/source"
)

// Result is the gate's verdict, in the exact vocabulary
// docs/06-ingestion-pipeline.md section 4 defines.
type Result int

const (
	// resultUnspecified only exists so the zero value of Result is not a
	// meaningful decision — a Decision left unset by a bug reads as neither
	// Allow nor any real refusal, and fails loudly if ever compared or logged.
	resultUnspecified Result = iota

	// ResultAllow means the request may proceed now.
	ResultAllow
	// ResultRefuse is terminal: this exact request will never be allowed by
	// asking again. It is logged as a compliance event — see [Gate.Allow].
	ResultRefuse
	// ResultDefer means try again later; nothing about this source is wrong,
	// its turn has not come up yet (rate budget, concurrency, or crawl-delay).
	ResultDefer
	// ResultSkip means the circuit breaker for this source is open; wait for
	// it, do not reschedule sooner.
	ResultSkip
)

func (r Result) String() string {
	switch r {
	case resultUnspecified:
		return "unspecified"
	case ResultAllow:
		return "allow"
	case ResultRefuse:
		return "refuse"
	case ResultDefer:
		return "defer"
	case ResultSkip:
		return "skip"
	default:
		return "unspecified"
	}
}

// Decision is the gate's answer for one source at one moment.
type Decision struct {
	Result Result
	// Reason is a short, human-readable, log-safe explanation — never derived
	// from response bodies or anything else that could carry attacker-supplied
	// or sensitive content into a log line.
	Reason string
}

// Release must be called exactly once after a Decision with Result ==
// ResultAllow led to an actual fetch attempt, regardless of whether that fetch
// succeeded — it frees the per-domain concurrency slot the decision reserved.
// It is nil on every Decision whose Result is not ResultAllow, and calling a
// nil Release is a programming error the caller is expected to avoid by
// checking Result first, not something this package defends against by
// returning a no-op — a no-op here would make "I forgot to check Result"
// silently correct instead of an obvious panic during development.
type Release func(context.Context)

// DefaultBurst is the token-bucket burst docs/06 section 4 specifies for the
// general per-domain rate limit: "rate: source.max_rps, default 0.5/s, burst:
// 2." The rate itself is per-source (src.MaxRPS); the burst is not a column on
// `source` and is this fixed policy value.
const DefaultBurst = 2

// Gate is the single choke point every outbound source fetch passes through.
type Gate struct {
	robots      *robots.Checker
	rate        *ratelimit.Limiter
	concurrency *concurrencyLimiter
	log         *slog.Logger
}

// New constructs a Gate. rdb is used for the concurrency reservation only —
// the rate limiter and the robots cache own their own Redis usage and are
// passed in already constructed, so a Gate does not have to know how either of
// them chooses to store its state.
func New(robotsChecker *robots.Checker, rate *ratelimit.Limiter, rdb *redis.Client, log *slog.Logger) *Gate {
	return &Gate{
		robots:      robotsChecker,
		rate:        rate,
		concurrency: newConcurrencyLimiter(rdb),
		log:         log,
	}
}

// Allow runs the six checks from docs/06 section 4, in the order specified
// there, and returns a verdict plus — only when that verdict is ResultAllow —
// a [Release] the caller must invoke after the fetch completes.
//
// The order matters and is preserved exactly rather than reordered for
// efficiency: an earlier check can make a later one moot (there is no point
// reserving a concurrency slot for a source robots.txt already disallows), and
// swapping the order would change which Reason a caller sees for a source that
// fails two checks at once — which is the string that ends up in a compliance
// log, so it needs to be the spec's answer, not an implementation detail's.
func (g *Gate) Allow(ctx context.Context, src source.Source) (Decision, Release) {
	// 1. legal_posture == 'permitted' or 'api_only' → else REFUSE (hard)
	if src.LegalPosture != source.PosturePermitted && src.LegalPosture != source.PostureAPIOnly {
		return g.refuse(src, fmt.Sprintf("legal_posture=%s", src.LegalPosture)), nil
	}

	u, err := url.Parse(src.URL)
	if err != nil || u.Hostname() == "" {
		// Not one of the spec's six named checks, but the same failure mode:
		// a source Scout cannot safely address at all must not proceed, and
		// this is exactly as terminal as a legal-posture refusal — no amount
		// of retrying changes an unparseable URL.
		return g.refuse(src, fmt.Sprintf("invalid source url: %v", err)), nil
	}

	domain, err := src.RegisteredDomain()
	if err != nil {
		return g.refuse(src, fmt.Sprintf("invalid source url: %v", err)), nil
	}

	// 2. robots.txt allows this path for our UA → else REFUSE
	if !g.robots.Allowed(ctx, u.Scheme, u.Host, u.Path) {
		return g.refuse(src, fmt.Sprintf("robots.txt disallows %s", u.Path)), nil
	}

	// 3. per-host rate budget available → else DEFER
	rateOK, err := g.rate.Allow(ctx, domain, float64(src.MaxRPS), DefaultBurst)
	if err != nil {
		// Infra trouble, not a compliance decision — Defer, not Refuse, so it
		// never lands in the compliance-refusal signal docs/14 section 9 says
		// must alert on any spike. A Redis outage is an ops problem with its
		// own alert path, not a legal one.
		return Decision{ResultDefer, fmt.Sprintf("rate limiter unavailable: %v", err)}, nil
	}
	if !rateOK {
		return Decision{ResultDefer, fmt.Sprintf("rate budget exhausted for %s", domain)}, nil
	}

	// 4. per-host concurrency below cap → else DEFER
	release, concurrencyOK, err := g.concurrency.acquire(ctx, domain, src.MaxConcurrency)
	if err != nil {
		return Decision{ResultDefer, fmt.Sprintf("concurrency limiter unavailable: %v", err)}, nil
	}
	if !concurrencyOK {
		return Decision{ResultDefer, fmt.Sprintf("concurrency cap reached for %s", domain)}, nil
	}

	// 5. Crawl-delay elapsed since last request → else DEFER
	//
	// Modeled as a second, independent token bucket on the same domain: a
	// Crawl-delay of N seconds is exactly "at most one request every N
	// seconds," which is a rate of 1/N with a burst of 1. Reusing the token
	// bucket here means crawl-delay enforcement gets the same atomicity and
	// cross-process sharing as the primary rate limit for free, instead of a
	// second, differently-shaped piece of state to keep consistent with it.
	if src.RobotsCrawlDelayS != nil && *src.RobotsCrawlDelayS > 0 {
		crawlOK, err := g.rate.Allow(ctx, domain+"#crawl-delay", 1.0 / *src.RobotsCrawlDelayS, 1)
		if err != nil {
			release(ctx)
			return Decision{ResultDefer, fmt.Sprintf("crawl-delay check unavailable: %v", err)}, nil
		}
		if !crawlOK {
			release(ctx)
			return Decision{ResultDefer, fmt.Sprintf("crawl-delay not yet elapsed for %s", domain)}, nil
		}
	}

	// 6. circuit breaker closed → else SKIP
	//
	// The gate only reads CircuitOpenUntil; recording a failure and setting it
	// is the fetcher's job, because only the fetcher knows a request actually
	// failed — see the field's doc comment in the source package.
	if src.CircuitOpenUntil != nil && src.CircuitOpenUntil.After(time.Now()) {
		release(ctx)
		return Decision{ResultSkip, fmt.Sprintf("circuit open until %s", src.CircuitOpenUntil.Format(time.RFC3339))}, nil
	}

	return Decision{ResultAllow, ""}, release
}

// refuse logs the compliance event every REFUSE decision requires
// (docs/14-legal-compliance.md section 1: "REFUSE is terminal and logged as a
// compliance event") and returns the Decision. Putting the logging here, in
// the one place every REFUSE path already funnels through, is what makes
// SCOUT-LEGAL-002 enforcement structural rather than a line a future call site
// could forget to add.
func (g *Gate) refuse(src source.Source, reason string) Decision {
	g.log.Warn("compliance refusal",
		"source_id", src.ID,
		"source_kind", src.Kind,
		"reason", reason,
	)
	return Decision{ResultRefuse, reason}
}
