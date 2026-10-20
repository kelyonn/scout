package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
)

// deferPoll is how long requestGate sleeps between retries of a deferred
// request. Short relative to the default 0.5 rps budget (one token per 2s),
// long enough not to hammer Redis.
const deferPoll = 250 * time.Millisecond

// requestGate adapts the politeness gate to padapter.RequestGate for an
// OwnFetcher adapter that makes more than one request per poll (the career
// page adapter: a sitemap, then each job page). Every request goes through
// the same Gate.Allow as the scheduler's own — AGENTS.md rule 1 — evaluated
// against a copy of the source whose URL is the request's URL, so robots.txt
// is checked for the actual path being fetched.
type requestGate struct {
	gate Gate
	src  source.Source
}

// NewRequestGate returns the padapter.RequestGate for src, backed by g.
func NewRequestGate(g Gate, src source.Source) padapter.RequestGate {
	return requestGate{gate: g, src: src}
}

// Allow waits out rate-budget and crawl-delay deferrals. The concurrency cap
// is raised by one for these calls only: pollOne already holds a slot on this
// domain for the poll itself, so with max_concurrency = 1 the follow-ups
// would otherwise defer forever against the scheduler's own reservation.
// Follow-ups are sequential, so the domain still never has more than the
// configured number of requests in flight.
func (g requestGate) Allow(ctx context.Context, rawURL string) (func(context.Context), error) {
	s := g.src
	s.URL = rawURL
	s.MaxConcurrency++

	for {
		decision, release := g.gate.Allow(ctx, s)
		switch decision.Result {
		case politeness.ResultAllow:
			return release, nil
		case politeness.ResultRefuse:
			return nil, fmt.Errorf("%w: %s", padapter.ErrRequestRefused, decision.Reason)
		case politeness.ResultSkip:
			return nil, fmt.Errorf("gate skipped request: %s", decision.Reason)
		case politeness.ResultDefer:
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("waiting for gate: %w", ctx.Err())
			case <-time.After(deferPoll):
			}
		default:
			return nil, fmt.Errorf("gate returned unknown result %q", decision.Result)
		}
	}
}
