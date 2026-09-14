package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/middleware"
)

func TestContentNegotiationMiddleware(t *testing.T) {
	markdownPayload := []byte("# Markdown Title\nContent for agents.")
	htmlPayload := "<html><body><h1>HTML Title</h1><p>Content for humans.</p></body></html>"

	setupEcho := func() *echo.Echo {
		e := echo.New()
		handler := func(c *echo.Context) error {
			c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
			return c.String(http.StatusOK, htmlPayload)
		}
		e.GET("/resource", handler, middleware.ContentNegotiationMiddleware(markdownPayload))
		return e
	}

	t.Run("Emits Vary: Accept, Accept-Encoding on all responses", func(t *testing.T) {
		e := setupEcho()

		// Case 1: HTML request
		reqHTML := httptest.NewRequest(http.MethodGet, "/resource", nil)
		reqHTML.Header.Set("Accept", "text/html")
		recHTML := httptest.NewRecorder()
		e.ServeHTTP(recHTML, reqHTML)
		if recHTML.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, recHTML.Code)
		}
		if got := recHTML.Header().Get("Vary"); got != "Accept, Accept-Encoding" {
			t.Errorf("expected Vary header %q, got %q", "Accept, Accept-Encoding", got)
		}

		// Case 2: Markdown request
		reqMD := httptest.NewRequest(http.MethodGet, "/resource", nil)
		reqMD.Header.Set("Accept", "text/markdown")
		recMD := httptest.NewRecorder()
		e.ServeHTTP(recMD, reqMD)
		if recMD.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, recMD.Code)
		}
		if got := recMD.Header().Get("Vary"); got != "Accept, Accept-Encoding" {
			t.Errorf("expected Vary header %q, got %q", "Accept, Accept-Encoding", got)
		}
	})

	t.Run("Serves markdown directly when Accept: text/markdown is specified", func(t *testing.T) {
		e := setupEcho()

		req := httptest.NewRequest(http.MethodGet, "/resource", nil)
		req.Header.Set("Accept", "text/markdown")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
			t.Errorf("expected Content-Type %q, got %q", "text/markdown; charset=utf-8", got)
		}
		if got := rec.Body.String(); got != string(markdownPayload) {
			t.Errorf("expected body %q, got %q", string(markdownPayload), got)
		}
		if strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("expected markdown body, got HTML: %s", rec.Body.String())
		}
	})

	t.Run("Accept header permutations", func(t *testing.T) {
		tests := []struct {
			name           string
			acceptHeader   string
			expectMarkdown bool
		}{
			{
				name:           "Exact text/markdown",
				acceptHeader:   "text/markdown",
				expectMarkdown: true,
			},
			{
				name:           "text/markdown with charset parameter",
				acceptHeader:   "text/markdown; charset=utf-8",
				expectMarkdown: true,
			},
			{
				name:           "Markdown preferred over HTML (q-factor)",
				acceptHeader:   "text/markdown;q=1.0, text/html;q=0.8",
				expectMarkdown: true,
			},
			{
				name:           "Markdown and HTML equal quality - markdown preferred for agents",
				acceptHeader:   "text/markdown, text/html",
				expectMarkdown: true,
			},
			{
				name:           "Standard browser header - HTML preferred",
				acceptHeader:   "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				expectMarkdown: false,
			},
			{
				name:           "HTML preferred over markdown (q-factor)",
				acceptHeader:   "text/html;q=1.0, text/markdown;q=0.5",
				expectMarkdown: false,
			},
			{
				name:           "Wildcard */* defaults to HTML",
				acceptHeader:   "*/*",
				expectMarkdown: false,
			},
			{
				name:           "Empty Accept header defaults to HTML",
				acceptHeader:   "",
				expectMarkdown: false,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				e := setupEcho()
				req := httptest.NewRequest(http.MethodGet, "/resource", nil)
				if tc.acceptHeader != "" {
					req.Header.Set("Accept", tc.acceptHeader)
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
				}
				if tc.expectMarkdown {
					if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
						t.Errorf("expected Content-Type %q, got %q", "text/markdown; charset=utf-8", got)
					}
					if got := rec.Body.String(); got != string(markdownPayload) {
						t.Errorf("expected body %q, got %q", string(markdownPayload), got)
					}
				} else {
					if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
						t.Errorf("expected text/html Content-Type, got %q", got)
					}
					if got := rec.Body.String(); got != htmlPayload {
						t.Errorf("expected html body %q, got %q", htmlPayload, got)
					}
				}
			})
		}
	})
}
