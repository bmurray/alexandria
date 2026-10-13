// Package logging builds the process slog logger (stdout JSON, optionally
// fanned out to the OpenTelemetry log bridge).
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// ParseLevel maps a config string to a slog level (defaults to info).
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New returns a JSON logger on stdout. Extra handlers receive every record too.
func New(level slog.Level, extra ...slog.Handler) *slog.Logger {
	handlers := []slog.Handler{slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})}
	for _, h := range extra {
		if h != nil {
			handlers = append(handlers, h)
		}
	}
	if len(handlers) == 1 {
		return slog.New(handlers[0])
	}
	return slog.New(fanout{handlers: handlers})
}

type fanout struct{ handlers []slog.Handler }

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return fanout{handlers: out}
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		out[i] = h.WithGroup(name)
	}
	return fanout{handlers: out}
}
