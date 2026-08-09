// Package heartbeat implements the outside-in dead-man's switch.
//
// Self-hosted monitoring cannot detect its own host being down, and at ₹0 there
// is no second host to watch the first. The answer (docs/16 section 2.2) is
// inverted from a normal health check: instead of a probe that fires when
// something is wrong, a heartbeat keeps arriving at a service outside the
// failure domain, and *its absence* is the alert. A probe that must fire to
// report a problem cannot report that it stopped being able to fire.
//
// The collector pings every 5 minutes; healthchecks.io alerts Telegram if the
// pings stop for 15. That covers the failure mode docs/20-risks.md rates most
// dangerous — Scout silently stops, everything looks fine because nothing is
// running to say otherwise, and the user notices two weeks later.
//
// # This is not source egress
//
// AGENTS.md rule 1 requires every outbound request to pass PolitenessGate.Allow().
// That rule is about fetching sources: robots.txt, per-host rate limits, and the
// prohibited-source list. This heartbeat is neither a fetch nor a source — it is
// Scout reporting on itself to Scout's own monitoring.
//
// When the gate lands, the heartbeat must stay outside it, and that is a
// correctness requirement rather than a convenience: a gate that can throttle,
// defer, or refuse the heartbeat would make the switch fire spuriously when the
// system is healthy, and an alert that cries wolf is one that gets muted. The
// gate's own scope check is the enforcement point — it applies to source
// fetches, and this is not one.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// DefaultInterval is how often the collector reports in.
//
// It pairs with a 15-minute grace period configured on the healthchecks.io
// check: three missed pings before an alert, so one transient network blip does
// not page anybody, and the exit criterion "killing the host triggers a Telegram
// alert within 15 minutes" still holds.
const DefaultInterval = 5 * time.Minute

// pingTimeout bounds one ping. It is deliberately far shorter than the interval:
// a hung ping must not be able to delay the next one into the grace period.
const pingTimeout = 10 * time.Second

// Pinger reports liveness to an external dead-man's switch.
//
// A Pinger with an empty URL is valid and does nothing. That is the local
// development case — a laptop that gets closed at night is not a host outage,
// and wiring it to the same check would produce a nightly false alarm and teach
// the user to ignore the one alert in this system that always means something.
type Pinger struct {
	url    string
	client *http.Client
	log    *slog.Logger

	// livenessPath, when set, is touched after every cycle for the container
	// healthcheck to read. See liveness.go.
	livenessPath string
}

// New returns a Pinger for the given healthchecks.io ping URL.
//
// An empty url disables it; see [Pinger].
func New(url string, log *slog.Logger) *Pinger {
	return &Pinger{
		url: url,
		client: &http.Client{
			Timeout: pingTimeout,
		},
		log: log,
	}
}

// WithLiveness makes Run touch path after every cycle, so the container
// healthcheck can tell a ticking process from a wedged one.
func (p *Pinger) WithLiveness(path string) *Pinger {
	p.livenessPath = path
	return p
}

// Enabled reports whether a ping URL was configured.
func (p *Pinger) Enabled() bool { return p.url != "" }

// Ping reports one successful cycle.
func (p *Pinger) Ping(ctx context.Context) error { return p.send(ctx, p.url) }

// Fail reports a cycle that did not succeed, which alerts immediately rather
// than after the grace period.
//
// The distinction is worth having: a collector that is running but failing every
// poll is a different incident from a host that vanished, and waiting 15 minutes
// to say so is 15 minutes of known-broken silence.
func (p *Pinger) Fail(ctx context.Context) error { return p.send(ctx, p.url+"/fail") }

func (p *Pinger) send(ctx context.Context, url string) error {
	if url == "" || p.url == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	// The URL is a configured secret (it contains the check's UUID) and must
	// never be logged — AGENTS.md rule 7 covers tokens, and a ping URL is a
	// bearer credential wearing a URL's clothes.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("build heartbeat request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send heartbeat: %w", redactURL(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("heartbeat rejected: status %d", resp.StatusCode)
	}
	return nil
}

// redactURL strips the request URL out of a transport error.
//
// The stdlib wraps every client failure in a *url.Error whose Error() quotes the
// full URL, and the ping URL contains the check's UUID — a bearer credential
// wearing a URL's clothes. Wrapping it unmodified is how that credential ends up
// in a log file the first time the network flakes. The operation and the
// underlying cause survive, which is the diagnostic half.
func redactURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}

// Run pings on a ticker until ctx is cancelled.
//
// A failed ping is logged and never fatal. The switch exists to detect Scout
// dying; killing Scout because it could not tell the switch it was alive would
// be an outage caused by the monitoring, and the grace period already covers a
// blip.
//
// Today this reports "the process is up", which is all the collector has to say.
// When it does real work, the ping belongs at the end of a successful poll cycle
// instead, with [Pinger.Fail] on a cycle that failed — a heartbeat that keeps
// arriving from a process that stopped polling is exactly the false comfort this
// package exists to avoid.
// Run blocks until ctx is cancelled, whether or not a ping URL is configured —
// it is the collector's main loop, and a service that exits because its
// monitoring is unconfigured is a service that will not start on a laptop.
func (p *Pinger) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}

	if p.Enabled() {
		p.log.Info("dead-man's switch armed", "interval", interval.String())
	} else {
		p.log.Warn("dead-man's switch disabled: SCOUT_HEALTHCHECK_URL is not set")
	}

	// Report once immediately. Waiting a full interval after a restart leaves a
	// gap that looks identical to a crash to anything watching.
	p.cycle(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("dead-man's switch stopping")
			return
		case <-ticker.C:
			p.cycle(ctx)
		}
	}
}

func (p *Pinger) cycle(ctx context.Context) {
	if err := p.Ping(ctx); err != nil {
		p.log.Warn("heartbeat failed", "err", err)
	}
	if err := Touch(p.livenessPath); err != nil {
		// Not fatal on its own, but the container healthcheck reads this file,
		// so a persistent failure here will restart the container — which is the
		// correct outcome for a collector that cannot write to its own tmpfs.
		p.log.Warn("liveness touch failed", "err", err)
	}
}
