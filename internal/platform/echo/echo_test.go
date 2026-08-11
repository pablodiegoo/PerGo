package echosrv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestNew_SecurityHeaders(t *testing.T) {
	e := New()

	e.GET("/ping", func(c *echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	headers := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Strict-Transport-Security": "31536000; includeSubDomains",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	}

	for h, expected := range headers {
		got := rec.Header().Get(h)
		if got != expected {
			t.Errorf("header %s = %q; want %q", h, got, expected)
		}
	}
}
