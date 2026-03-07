// Package telemetry provides OpenTelemetry SDK initialization and lifecycle management.
package telemetry

import (
	"context"
	"log/slog"
)

// fanOutHandler duplicates every slog record to multiple underlying handlers.
type fanOutHandler struct {
	handlers []slog.Handler
}

// NewFanOutHandler creates a slog.Handler that forwards records to all given handlers.
func NewFanOutHandler(handlers ...slog.Handler) slog.Handler {
	return &fanOutHandler{handlers: handlers}
}

// Enabled reports true if any underlying handler is enabled for the given level.
func (h *fanOutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle forwards the record to every underlying handler, returning the first error.
func (h *fanOutHandler) Handle(ctx context.Context, record slog.Record) error { //nolint:gocritic // slog.Handler interface requires value receiver
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithAttrs returns a new fanOutHandler with the given attributes added to every underlying handler.
func (h *fanOutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &fanOutHandler{handlers: handlers}
}

// WithGroup returns a new fanOutHandler with the given group applied to every underlying handler.
func (h *fanOutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &fanOutHandler{handlers: handlers}
}
