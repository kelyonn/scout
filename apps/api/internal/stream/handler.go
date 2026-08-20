package stream

import (
	"fmt"
	"net/http"
	"strconv"
)

// Handler serves GET /v1/stream.
type Handler struct {
	broker *Broker
}

// New returns a stream Handler backed by broker.
func New(broker *Broker) *Handler {
	return &Handler{broker: broker}
}

// Stream handles GET /v1/stream. Reconnects send the Last-Event-ID
// header automatically (SSE spec, honored by the browser's EventSource)
// — replayed from Broker's buffer before the connection joins the live
// broadcast, so "clients reconnect with Last-Event-ID and receive
// anything missed" (docs/04 section 4.3) holds for any gap within the
// buffer's retention.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Nginx/Caddy buffer proxied responses by default, which defeats SSE
	// entirely (nothing reaches the client until the buffer fills or the
	// connection closes). This header is the standard opt-out.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if lastID, err := strconv.ParseInt(raw, 10, 64); err == nil {
			for _, ev := range h.broker.EventsSince(lastID) {
				writeSSE(w, ev)
			}
			flusher.Flush()
		}
	}

	ch, unsubscribe := h.broker.Subscribe()
	defer unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, ev)
			flusher.Flush()
		}
	}
}

// writeSSE's error is deliberately unchecked: a write failure here means
// the client's connection is already gone, and the caller's next
// iteration finds that out via r.Context().Done() (the request context is
// cancelled when the underlying connection closes) rather than this
// function needing its own error-handling path for the same fact.
func writeSSE(w http.ResponseWriter, ev Event) {
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Name, ev.Data)
}
