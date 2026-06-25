package middleware

import (
	"context"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type traceKey struct{}

// TraceMiddleware returns an Echo middleware that generates or extracts a
// trace_id for each request. If the X-Trace-Id header is present, it is
// used; otherwise a new UUID is generated. The trace_id is stored in the
// request context for downstream handlers and logging.
func TraceMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			traceID := c.Request().Header.Get("X-Trace-Id")
			if traceID == "" {
				traceID = uuid.New().String()
			}
			ctx := context.WithValue(c.Request().Context(), traceKey{}, traceID)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// TraceIDFrom extracts the trace_id from the context. Returns false if
// the trace_id is not present.
func TraceIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(traceKey{}).(string)
	return id, ok
}

// WithContext stores a trace_id in the context (for non-HTTP contexts
// like batch workers).
func WithContext(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceID)
}
