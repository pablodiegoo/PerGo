package email

import (
	"strings"
	"testing"
)

func TestTrackingHMAC(t *testing.T) {
	secret := "super-secret-key-123"
	msgID := "msg-001"
	payload := "open"

	sig := GenerateTrackingHMAC(msgID, payload, secret)
	if sig == "" {
		t.Fatalf("expected non-empty HMAC signature")
	}

	if !VerifyTrackingHMAC(msgID, payload, sig, secret) {
		t.Errorf("HMAC verification failed for valid signature")
	}

	if VerifyTrackingHMAC(msgID, payload, sig+"tampered", secret) {
		t.Errorf("HMAC verification passed for tampered signature")
	}
}

func TestInjectOpenPixel(t *testing.T) {
	html := "<html><body><h1>Hello World</h1></body></html>"
	baseURL := "https://api.pergo.dev"
	secret := "secret-key"
	msgID := "msg-123"

	result := InjectOpenPixel(html, msgID, baseURL, secret)

	if !strings.Contains(result, "<img src=\"https://api.pergo.dev/v1/webhooks/email/open?m=msg-123&sig=") {
		t.Errorf("open pixel tag missing or incorrectly formatted: %s", result)
	}
	if !strings.Contains(result, "</body>") {
		t.Errorf("closing body tag lost: %s", result)
	}
}

func TestRewriteClickLinks(t *testing.T) {
	html := `<html><body><a href="https://example.com/promo">Click here</a></body></html>`
	baseURL := "https://api.pergo.dev"
	secret := "secret-key"
	msgID := "msg-456"

	result := RewriteClickLinks(html, msgID, baseURL, secret)

	if !strings.Contains(result, "https://api.pergo.dev/v1/webhooks/email/click?m=msg-456&url=https%3A%2F%2Fexample.com%2Fpromo&sig=") {
		t.Errorf("link rewriting failed: %s", result)
	}
}
