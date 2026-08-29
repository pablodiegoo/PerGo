package echosrv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestSecurityHeaders_CustomConfig_MissingHeaderAssertions(t *testing.T) {
	// Only set X-Frame-Options; leave all others empty
	cfg := SecurityConfig{
		XFrameOptions: "SAMEORIGIN",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := SecurityHeadersWithConfig(cfg)(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	if err := handler(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert X-Frame-Options is set
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q; want %q", got, "SAMEORIGIN")
	}

	// Assert ALL other security headers are completely MISSING (empty string)
	missingHeaders := []string{
		"X-Content-Type-Options",
		"X-XSS-Protection",
		"Strict-Transport-Security",
		"Referrer-Policy",
		"Content-Security-Policy",
	}

	for _, h := range missingHeaders {
		if val, exists := rec.Header()[h]; exists {
			t.Errorf("header %s should be missing/omitted when empty in config, but exists with value %v", h, val)
		}
	}
}

func TestSecurityHeaders_DefaultConfig_FullAssertions(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := SecurityHeaders()(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	if err := handler(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Strict-Transport-Security": "31536000; includeSubDomains",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	}

	for h, want := range expected {
		if got := rec.Header().Get(h); got != want {
			t.Errorf("header %s = %q; want %q", h, got, want)
		}
	}

	if val, exists := rec.Header()["Content-Security-Policy"]; exists {
		t.Errorf("Content-Security-Policy should be omitted by default, got %v", val)
	}
}
