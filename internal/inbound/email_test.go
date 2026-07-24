package inbound

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestHandleOpen(t *testing.T) {
	e := echo.New()
	handler := NewEmailTrackingHandler("test-secret", nil)

	msgID := "msg-100"
	sig := generateTrackingHMAC(msgID, "open", "test-secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/email/open?m="+msgID+"&sig="+sig, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.HandleOpen(c); err != nil {
		t.Fatalf("HandleOpen error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/gif" {
		t.Errorf("expected image/gif, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestHandleClickRedirect(t *testing.T) {
	e := echo.New()
	handler := NewEmailTrackingHandler("test-secret", nil)

	msgID := "msg-100"
	targetURL := "https://example.com/dest"
	sig := generateTrackingHMAC(msgID, targetURL, "test-secret")

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/email/click?m="+msgID+"&url="+targetURL+"&sig="+sig, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.HandleClick(c); err != nil {
		t.Fatalf("HandleClick error: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Errorf("expected HTTP 302, got %d", rec.Code)
	}
	if rec.Header().Get("Location") != targetURL {
		t.Errorf("expected redirect location %s, got %s", targetURL, rec.Header().Get("Location"))
	}
}

func TestHandleSESWebhook(t *testing.T) {
	e := echo.New()
	handler := NewEmailTrackingHandler("test-secret", nil)

	payload := `{
		"eventType": "Delivery",
		"mail": {
			"messageId": "ses-msg-777"
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/email/ses", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.HandleSESWebhook(c); err != nil {
		t.Fatalf("HandleSESWebhook error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rec.Code)
	}
}
