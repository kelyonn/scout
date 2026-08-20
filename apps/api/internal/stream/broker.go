// Package stream implements GET /v1/stream — docs/04-api-design.md
// section 4.3's Server-Sent Events feed. One Broker per process, started
// once at startup (see apps/api/cmd/main.go), holds a single dedicated
// Postgres LISTEN connection regardless of how many browser tabs are
// subscribed — a per-request LISTEN connection would work but wastes a
// pool connection per open tab for no benefit in a single-user system.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kelyon/scout/packages/realtime"
)

// bufferSize bounds the in-memory replay log Last-Event-ID reconnects use
// to catch up. Not durable across a process restart — an accepted limit
// for a single-user system with no other consumer of this stream; a
// durable event log is the kind of scale this system doesn't have
// (AGENTS.md's "no speculative abstraction").
const bufferSize = 200

const heartbeatInterval = 30 * time.Second

// listenRetryDelay is how long Run waits before re-establishing LISTEN
// after the dedicated connection drops (network blip, Postgres restart).
const listenRetryDelay = 5 * time.Second

// Event is one SSE message — an id/event/data triple in docs/04-api-
// design.md section 4.3's wire format.
type Event struct {
	ID   int64
	Name string
	Data string // raw JSON, already the inner realtime.Envelope.Data
}

// Broker fans out published events to every current Subscribe caller and
// buffers recent ones for Last-Event-ID replay. See the package comment
// for why one Broker per process, not one per request.
type Broker struct {
	mu     sync.Mutex
	nextID int64
	buffer []Event
	subs   map[chan Event]struct{}
}

// NewBroker returns an empty Broker — call Run to start it.
func NewBroker() *Broker {
	return &Broker{subs: make(map[chan Event]struct{})}
}

func (b *Broker) publish(name, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	ev := Event{ID: b.nextID, Name: name, Data: data}
	b.buffer = append(b.buffer, ev)
	if len(b.buffer) > bufferSize {
		b.buffer = b.buffer[len(b.buffer)-bufferSize:]
	}

	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// A slow subscriber gets dropped events, not a blocked
			// broker — every other subscriber would otherwise wait on
			// the slowest one. A reconnect with Last-Event-ID (or the
			// next heartbeat noticing the gap) is how it catches up.
		}
	}
}

// Subscribe registers a new listener. The caller must read ch until
// unsubscribe is called or the channel closes; unsubscribe is safe to
// call more than once.
func (b *Broker) Subscribe() (ch chan Event, unsubscribe func()) {
	ch = make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// EventsSince returns buffered events with ID > lastID, for a
// reconnecting client's Last-Event-ID replay.
func (b *Broker) EventsSince(lastID int64) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Event
	for _, ev := range b.buffer {
		if ev.ID > lastID {
			out = append(out, ev)
		}
	}
	return out
}

// Run drives the broker until ctx is done: a heartbeat every
// heartbeatInterval, and a Postgres LISTEN loop that reconnects on
// failure rather than giving up. Call once, in its own goroutine, at
// process startup.
func (b *Broker) Run(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	go b.runHeartbeat(ctx)

	for ctx.Err() == nil {
		if err := b.listenOnce(ctx, pool, log); err != nil && ctx.Err() == nil {
			log.Warn("stream: listen connection lost, retrying", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(listenRetryDelay):
			}
		}
	}
}

func (b *Broker) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			payload, _ := json.Marshal(map[string]string{"ts": t.UTC().Format(time.RFC3339)})
			b.publish("heartbeat", string(payload))
		}
	}
}

// listenOnce holds one dedicated Postgres connection (Hijacked out of the
// pool so it is never handed to an unrelated query while LISTEN is
// active) until it errors or ctx is done.
func (b *Broker) listenOnce(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	pooled, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	conn := pooled.Hijack()
	// Cleanup deliberately uses a fresh background context, not ctx: by the
	// time this runs, ctx may already be the reason listenOnce is
	// returning (its own cancellation), and closing a connection with an
	// already-cancelled context can no-op the close and leak it.
	defer func() { _ = conn.Close(context.Background()) }() //nolint:contextcheck // see comment: closing with ctx risks a leak if ctx is what's cancelling

	if _, execErr := conn.Exec(ctx, "listen "+realtime.Channel); execErr != nil {
		return fmt.Errorf("listen %s: %w", realtime.Channel, execErr)
	}
	log.Info("stream: listening for realtime events", "channel", realtime.Channel)

	for {
		n, waitErr := conn.WaitForNotification(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait for notification: %w", waitErr)
		}

		var env realtime.Envelope
		if unmarshalErr := json.Unmarshal([]byte(n.Payload), &env); unmarshalErr != nil {
			log.Warn("stream: malformed notification payload, skipping", "err", unmarshalErr)
			continue
		}
		dataJSON, marshalErr := json.Marshal(env.Data)
		if marshalErr != nil {
			log.Warn("stream: re-marshal notification data failed, skipping", "err", marshalErr)
			continue
		}
		b.publish(env.Event, string(dataJSON))
	}
}
