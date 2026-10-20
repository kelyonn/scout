package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kelyon/scout/apps/collector/internal/politeness"
	"github.com/kelyon/scout/apps/collector/internal/source"
	padapter "github.com/kelyon/scout/packages/adapter"
)

// seqGate returns scripted decisions in order and records the sources it saw.
type seqGate struct {
	decisions []politeness.Decision
	seen      []source.Source
	released  atomic.Int32
}

func (g *seqGate) Allow(_ context.Context, s source.Source) (politeness.Decision, politeness.Release) {
	g.seen = append(g.seen, s)
	d := g.decisions[0]
	if len(g.decisions) > 1 {
		g.decisions = g.decisions[1:]
	}
	if d.Result != politeness.ResultAllow {
		return d, nil
	}
	return d, func(context.Context) { g.released.Add(1) }
}

func TestRequestGate_ChecksTheActualURLThroughTheGate(t *testing.T) {
	g := &seqGate{decisions: []politeness.Decision{{Result: politeness.ResultAllow}}}
	base := source.Source{ID: "s", URL: "https://careers.acme.example/sitemap.xml", LegalPosture: source.PosturePermitted, MaxConcurrency: 1}

	release, err := NewRequestGate(g, base).Allow(context.Background(), "https://careers.acme.example/jobs/1")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	release(context.Background())

	if len(g.seen) != 1 || g.seen[0].URL != "https://careers.acme.example/jobs/1" {
		t.Fatalf("gate saw %+v, want the follow-up URL (robots must be checked for the real path)", g.seen)
	}
	if g.seen[0].MaxConcurrency != 2 {
		t.Errorf("MaxConcurrency = %d, want 2 (the poll's own slot is already held)", g.seen[0].MaxConcurrency)
	}
	if base.MaxConcurrency != 1 || base.URL != "https://careers.acme.example/sitemap.xml" {
		t.Error("the caller's source must not be mutated")
	}
	if g.released.Load() != 1 {
		t.Error("release was not wired through")
	}
}

func TestRequestGate_RefusalSkipAndDeferral(t *testing.T) {
	base := source.Source{ID: "s", URL: "https://careers.acme.example/x", LegalPosture: source.PostureProhibited}

	t.Run("refuse is terminal and typed", func(t *testing.T) {
		g := &seqGate{decisions: []politeness.Decision{{Result: politeness.ResultRefuse, Reason: "legal_posture=prohibited"}}}
		rel, err := NewRequestGate(g, base).Allow(context.Background(), "https://careers.acme.example/jobs/1")
		if !errors.Is(err, padapter.ErrRequestRefused) || rel != nil {
			t.Fatalf("err=%v release-nil=%v, want ErrRequestRefused and no release", err, rel == nil)
		}
		if len(g.seen) != 1 {
			t.Errorf("a refusal must not be retried, gate called %d times", len(g.seen))
		}
	})

	t.Run("skip is an error but not a refusal", func(t *testing.T) {
		g := &seqGate{decisions: []politeness.Decision{{Result: politeness.ResultSkip, Reason: "circuit open"}}}
		_, err := NewRequestGate(g, base).Allow(context.Background(), "https://careers.acme.example/jobs/1")
		if err == nil || errors.Is(err, padapter.ErrRequestRefused) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("defer waits then allows", func(t *testing.T) {
		g := &seqGate{decisions: []politeness.Decision{
			{Result: politeness.ResultDefer, Reason: "rate"}, {Result: politeness.ResultAllow},
		}}
		start := time.Now()
		rel, err := NewRequestGate(g, base).Allow(context.Background(), "https://careers.acme.example/jobs/1")
		if err != nil || rel == nil {
			t.Fatalf("err=%v", err)
		}
		if len(g.seen) != 2 || time.Since(start) < deferPoll/2 {
			t.Errorf("gate calls=%d elapsed=%s", len(g.seen), time.Since(start))
		}
	})

	t.Run("defer ends with the context", func(t *testing.T) {
		g := &seqGate{decisions: []politeness.Decision{{Result: politeness.ResultDefer}}}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := NewRequestGate(g, base).Allow(ctx, "https://careers.acme.example/jobs/1"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}
