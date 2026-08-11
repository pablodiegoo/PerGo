package harness

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	echosrv "github.com/pablojhp.pergo/internal/platform/echo"
)

func TestSecurityHeaders_EmpiricalChallenge(t *testing.T) {
	t.Run("Default Config Sets Expected Headers", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := echosrv.SecurityHeaders()(func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		if err := handler(c); err != nil {
			t.Fatalf("handler failed: %v", err)
		}

		expected := map[string]string{
			"X-Content-Type-Options":    "nosniff",
			"X-Frame-Options":          "DENY",
			"X-XSS-Protection":         "1; mode=block",
			"Strict-Transport-Security": "31536000; includeSubDomains",
			"Referrer-Policy":           "strict-origin-when-cross-origin",
		}

		for k, want := range expected {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("header %s = %q, want %q", k, got, want)
			}
		}

		// CSP should be missing by default
		if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
			t.Errorf("Content-Security-Policy expected empty, got %q", csp)
		}
	})

	t.Run("Custom Config Overrides Defaults", func(t *testing.T) {
		cfg := echosrv.SecurityConfig{
			XContentTypeOptions:   "nosniff",
			XFrameOptions:         "SAMEORIGIN",
			XXSSProtection:        "1",
			HSTSMaxAge:            "max-age=63072000",
			ReferrerPolicy:        "no-referrer",
			ContentSecurityPolicy: "default-src 'self'",
		}

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/custom", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := echosrv.SecurityHeadersWithConfig(cfg)(func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		if err := handler(c); err != nil {
			t.Fatalf("handler failed: %v", err)
		}

		checks := map[string]string{
			"X-Frame-Options":          "SAMEORIGIN",
			"X-XSS-Protection":         "1",
			"Strict-Transport-Security": "max-age=63072000",
			"Referrer-Policy":           "no-referrer",
			"Content-Security-Policy":  "default-src 'self'",
		}

		for k, want := range checks {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("custom header %s = %q, want %q", k, got, want)
			}
		}
	})

	t.Run("Empty Config Omits All Headers", func(t *testing.T) {
		cfg := echosrv.SecurityConfig{} // all fields empty string

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/empty", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := echosrv.SecurityHeadersWithConfig(cfg)(func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		if err := handler(c); err != nil {
			t.Fatalf("handler failed: %v", err)
		}

		headersToCheck := []string{
			"X-Content-Type-Options",
			"X-Frame-Options",
			"X-XSS-Protection",
			"Strict-Transport-Security",
			"Referrer-Policy",
			"Content-Security-Policy",
		}

		for _, h := range headersToCheck {
			if got := rec.Header().Get(h); got != "" {
				t.Errorf("header %s expected missing/empty, got %q", h, got)
			}
		}
	})

	t.Run("Echo New Instance Middleware Stack Integrates SecurityHeaders", func(t *testing.T) {
		e := echosrv.New()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		rec := httptest.NewRecorder()

		e.GET("/ping", func(c *echo.Context) error {
			return c.String(http.StatusOK, "pong")
		})

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("X-Frame-Options missing from echosrv.New() router response")
		}
	})
}
