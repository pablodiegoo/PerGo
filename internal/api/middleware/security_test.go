package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestSecurityHeaders_Default(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := SecurityHeaders()(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		header   string
		expected string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Strict-Transport-Security", "31536000; includeSubDomains"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		got := rec.Header().Get(tt.header)
		if got != tt.expected {
			t.Errorf("header %s = %q; want %q", tt.header, got, tt.expected)
		}
	}

	if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("expected Content-Security-Policy to be empty by default, got %q", csp)
	}
}

func TestSecurityHeaders_CustomConfig(t *testing.T) {
	cfg := SecurityConfig{
		XContentTypeOptions:   "nosniff",
		XFrameOptions:         "SAMEORIGIN",
		XXSSProtection:        "0",
		HSTSMaxAge:            "63072000",
		ReferrerPolicy:        "no-referrer",
		ContentSecurityPolicy: "default-src 'self'",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := SecurityHeadersWithConfig(cfg)(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		header   string
		expected string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "SAMEORIGIN"},
		{"X-XSS-Protection", "0"},
		{"Strict-Transport-Security", "63072000"},
		{"Referrer-Policy", "no-referrer"},
		{"Content-Security-Policy", "default-src 'self'"},
	}

	for _, tt := range tests {
		got := rec.Header().Get(tt.header)
		if got != tt.expected {
			t.Errorf("header %s = %q; want %q", tt.header, got, tt.expected)
		}
	}
}

func TestSecurityHeaders_OmitEmptyHeaders(t *testing.T) {
	cfg := SecurityConfig{
		XContentTypeOptions: "",
		XFrameOptions:       "",
		XXSSProtection:      "",
		HSTSMaxAge:          "",
		ReferrerPolicy:      "",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := SecurityHeadersWithConfig(cfg)(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"X-XSS-Protection",
		"Strict-Transport-Security",
		"Referrer-Policy",
		"Content-Security-Policy",
	}

	for _, h := range headers {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("expected header %s to be empty, got %q", h, got)
		}
	}
}
