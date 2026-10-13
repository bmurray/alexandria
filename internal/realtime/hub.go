// Package realtime lets an ordinary GET endpoint also serve a live stream.
//
// A handler calls Hub.Serve with a snapshot function and the topics that
// announce changes. Without an Upgrade header the response is one JSON
// snapshot (plain CRUD). With `Upgrade: websocket` the response is a
// WebSocket: a `snapshot` frame, then an `event` frame per published message,
// with pings to keep it open. No dedicated websocket paths exist.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Message is one published event.
type Message struct {
	Topic   string
	Payload json.RawMessage
}

// Frame is the wire shape of WebSocket messages.
type Frame struct {
	Type  string          `json:"type"`
	Topic string          `json:"topic,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Hub is an in-process topic broker that also serves WebSockets.
type Hub struct {
	// Origins allowed to open WebSockets (scheme://host[:port]); same-origin is always allowed.
	Origins []string
	// Heartbeat interval for idle streams (default 20s).
	Heartbeat time.Duration

	mu   sync.RWMutex
	subs map[string]map[*subscription]struct{}
}

type subscription struct {
	topics []string
	c      chan Message
	once   sync.Once
	hub    *Hub
}

var (
	meter   = otel.Meter("alexandria/realtime")
	streams metric.Int64UpDownCounter
	events  metric.Int64Counter
)

func init() {
	streams, _ = meter.Int64UpDownCounter("alexandria.realtime.streams", metric.WithDescription("Open WebSocket streams"))
	events, _ = meter.Int64Counter("alexandria.realtime.events", metric.WithDescription("Events published"))
}

// Publish sends v (JSON-encoded) to every subscriber of topic. Slow
// subscribers drop the message rather than block the publisher.
func (h *Hub) Publish(ctx context.Context, topic string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.WarnContext(ctx, "realtime publish", slog.String("topic", topic), slog.String("error", err.Error()))
		return
	}
	events.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", topic)))
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[topic] {
		select {
		case s.c <- Message{Topic: topic, Payload: data}:
		default:
		}
	}
}

// Subscribe returns a channel of messages on topics and a cancel function.
func (h *Hub) Subscribe(topics ...string) (<-chan Message, func()) {
	s := &subscription{topics: topics, c: make(chan Message, 64), hub: h}
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[string]map[*subscription]struct{}{}
	}
	for _, t := range topics {
		if h.subs[t] == nil {
			h.subs[t] = map[*subscription]struct{}{}
		}
		h.subs[t][s] = struct{}{}
	}
	h.mu.Unlock()
	return s.c, s.close
}

func (s *subscription) close() {
	s.once.Do(func() {
		s.hub.mu.Lock()
		for _, t := range s.topics {
			delete(s.hub.subs[t], s)
			if len(s.hub.subs[t]) == 0 {
				delete(s.hub.subs, t)
			}
		}
		s.hub.mu.Unlock()
	})
}

// Options describe one streamed endpoint.
type Options struct {
	// Topics whose messages are forwarded as events.
	Topics []string
	// Snapshot returns the current state; it is sent first in every mode.
	Snapshot func(ctx context.Context) (any, error)
	// OnError writes the error for a failed Snapshot; defaults to a JSON 500.
	OnError func(w http.ResponseWriter, r *http.Request, err error)
}

// IsUpgrade reports whether r asks for a WebSocket.
func IsUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// Serve answers r as a JSON snapshot or, on Upgrade, as a WebSocket stream.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, opts Options) {
	if IsUpgrade(r) {
		h.serveWebSocket(w, r, opts)
		return
	}
	data, err := opts.Snapshot(r.Context())
	if err != nil {
		fail(w, r, opts, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(data)
}

func fail(w http.ResponseWriter, r *http.Request, opts Options, err error) {
	if opts.OnError != nil {
		opts.OnError(w, r, err)
		return
	}
	slog.ErrorContext(r.Context(), "realtime snapshot", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"error":"internal error"}`))
}

func (h *Hub) heartbeat() time.Duration {
	if h.Heartbeat > 0 {
		return h.Heartbeat
	}
	return 20 * time.Second
}

func (h *Hub) originPatterns() []string {
	out := make([]string, 0, len(h.Origins))
	for _, o := range h.Origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func (h *Hub) serveWebSocket(w http.ResponseWriter, r *http.Request, opts Options) {
	ctx := r.Context()
	snap, err := opts.Snapshot(ctx)
	if err != nil {
		fail(w, r, opts, err)
		return
	}
	msgs, unsubscribe := h.Subscribe(opts.Topics...)
	defer unsubscribe()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns()})
	if err != nil {
		return // Accept wrote the HTTP error
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	streams.Add(ctx, 1)
	defer streams.Add(context.WithoutCancel(ctx), -1)

	send := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return c.Write(wctx, websocket.MessageText, data)
	}
	snapData, _ := json.Marshal(snap)
	if err := send(Frame{Type: "snapshot", Data: snapData}); err != nil {
		return
	}
	// Reader: answers pings and surfaces close.
	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var f Frame
			if json.Unmarshal(data, &f) == nil && f.Type == "ping" {
				_ = send(Frame{Type: "pong"})
			}
		}
	}()
	ticker := time.NewTicker(h.heartbeat())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.Close(websocket.StatusNormalClosure, "bye")
			return
		case m := <-msgs:
			if err := send(Frame{Type: "event", Topic: m.Topic, Data: m.Payload}); err != nil {
				return
			}
		case <-ticker.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				return
			}
		}
	}
}
