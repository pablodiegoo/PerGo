package middleware

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

const (
	// VaryHeaderValue is the Vary header emitted on negotiable routes to prevent cache poisoning across reverse proxies and CDNs.
	VaryHeaderValue = "Accept, Accept-Encoding"

	// ContentTypeMarkdown is the canonical MIME type for Markdown content.
	ContentTypeMarkdown = "text/markdown; charset=utf-8"
)

// ContentNegotiationMiddleware evaluates the Accept header on negotiable routes.
// When an agent prefers text/markdown, it intercepts the request and directly serves
// the provided Markdown payload with Content-Type: text/markdown; charset=utf-8.
// In all cases, it emits Vary: Accept, Accept-Encoding to ensure downstream caches isolate representations.
func ContentNegotiationMiddleware(markdownPayload []byte) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Response().Header().Set("Vary", VaryHeaderValue)

			acceptHeader := c.Request().Header.Get("Accept")
			if PrefersMarkdown(acceptHeader) {
				return c.Blob(http.StatusOK, ContentTypeMarkdown, markdownPayload)
			}

			return next(c)
		}
	}
}

type mediaPreference struct {
	q       float64
	matched bool
}

func (m *mediaPreference) record(q float64) {
	m.matched = true
	if q > m.q {
		m.q = q
	}
}

// PrefersMarkdown inspects an HTTP Accept header and determines whether text/markdown is preferred over text/html.
func PrefersMarkdown(accept string) bool {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return false
	}

	parts := strings.Split(accept, ",")
	markdown := mediaPreference{q: -1.0}
	html := mediaPreference{q: -1.0}

	for _, part := range parts {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}

		q := 1.0
		if qVal, ok := params["q"]; ok {
			if parsedQ, err := strconv.ParseFloat(strings.TrimSpace(qVal), 64); err == nil {
				q = parsedQ
			}
		}

		switch strings.ToLower(mediaType) {
		case "text/markdown":
			markdown.record(q)
		case "text/html":
			html.record(q)
		}
	}

	if !markdown.matched || markdown.q <= 0 {
		return false
	}

	if !html.matched {
		return true
	}

	return markdown.q >= html.q
}
