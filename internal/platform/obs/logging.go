// Package obs provides observability utilities for PerGo, including
// structured logging with trace context propagation.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/pablojhp.pergo/internal/api/middleware"
)

type loggerKey struct{}

// NewLogger creates a slog.Logger with a trace_id attribute.
func NewLogger(traceID string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("trace_id", traceID)
}

// NewLoggerWithWriter creates a slog.Logger that writes to the given writer
// with a trace_id attribute. Useful for testing.
func NewLoggerWithWriter(traceID string, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil)).With("trace_id", traceID)
}

// LoggerFromContext extracts the trace_id from context and returns a logger
// with the trace_id attribute. If no trace_id is present, a default logger
// is returned.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	traceID, ok := middleware.TraceIDFrom(ctx)
	if !ok {
		return slog.Default()
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("trace_id", traceID)
}

// WithContext stores a logger in the context.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFromCtx retrieves a logger stored in the context. Falls back to
// slog.Default() if none is stored.
func LoggerFromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
