package middleware

import (
	"context"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v5"
)

type htmxKey struct{}

// HTMXMiddleware returns an Echo middleware that detects HTMX requests
// via the HX-Request header and stores the result in the request context.
func HTMXMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			isHTMX := c.Request().Header.Get("HX-Request") == "true"
			ctx := context.WithValue(c.Request().Context(), htmxKey{}, isHTMX)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// IsHTMX checks if the request is from HTMX (fragment request).
func IsHTMX(c *echo.Context) bool {
	v, _ := c.Request().Context().Value(htmxKey{}).(bool)
	return v
}

// Render writes a templ component to the Echo response.
// Uses templ.GetBuffer() with defer ReleaseBuffer for safe concurrent usage.
func Render(c *echo.Context, statusCode int, t templ.Component) error {
	buf := templ.GetBuffer()
	defer templ.ReleaseBuffer(buf)
	if err := t.Render(c.Request().Context(), buf); err != nil {
		return err
	}
	return c.HTML(statusCode, buf.String())
}
