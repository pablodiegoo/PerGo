package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

func TestSignPayload(t *testing.T) {
	secret := []byte("test-secret-key-xyz")
	payload := []byte(`{"event":"message.sent","id":"123"}`)
	timestamp := "1754179200"

	sigHeader := webhook.SignPayload(payload, secret, timestamp)

	// Format must be t=<ts>,v1=<hex>
	if !strings.HasPrefix(sigHeader, "t=1754179200,v1=") {
		t.Fatalf("unexpected signature format: %s", sigHeader)
	}

	// Verify the HMAC manually
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("1754179200."))
	mac.Write(payload)
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	expectedHeader := fmt.Sprintf("t=1754179200,v1=%s", expectedHex)
	if sigHeader != expectedHeader {
		t.Errorf("got %s, want %s", sigHeader, expectedHeader)
	}
}

func TestVerifyPerGoSignature(t *testing.T) {
	secret := "my-secret-key-321"
	payload := []byte(`{"event":"order.created","amount":100}`)
	nowTs := fmt.Sprintf("%d", time.Now().Unix())

	sigHeader := webhook.SignPayload(payload, []byte(secret), nowTs)

	t.Run("Valid signature succeeds", func(t *testing.T) {
		if !webhook.VerifyPerGoSignature(payload, sigHeader, secret) {
			t.Errorf("expected signature verification to succeed")
		}
	})

	t.Run("Wrong secret fails", func(t *testing.T) {
		if webhook.VerifyPerGoSignature(payload, sigHeader, "wrong-secret") {
			t.Errorf("expected signature verification to fail with wrong secret")
		}
	})

	t.Run("Tampered payload fails", func(t *testing.T) {
		tampered := []byte(`{"event":"order.created","amount":999}`)
		if webhook.VerifyPerGoSignature(tampered, sigHeader, secret) {
			t.Errorf("expected signature verification to fail with tampered payload")
		}
	})

	t.Run("Empty signature header fails", func(t *testing.T) {
		if webhook.VerifyPerGoSignature(payload, "", secret) {
			t.Errorf("expected empty header verification to fail")
		}
	})

	t.Run("Empty secret fails", func(t *testing.T) {
		if webhook.VerifyPerGoSignature(payload, sigHeader, "") {
			t.Errorf("expected empty secret verification to fail")
		}
	})

	t.Run("Malformed header fails", func(t *testing.T) {
		badHeaders := []string{
			"invalid-header",
			"t=12345",
			"v1=abcde",
			"t=invalid,v1=1234",
		}
		for _, bh := range badHeaders {
			if webhook.VerifyPerGoSignature(payload, bh, secret) {
				t.Errorf("expected malformed header %q to fail", bh)
			}
		}
	})

	t.Run("Expired timestamp (> 5 minutes old) fails replay check", func(t *testing.T) {
		oldTs := fmt.Sprintf("%d", time.Now().Add(-6*time.Minute).Unix())
		oldSig := webhook.SignPayload(payload, []byte(secret), oldTs)
		if webhook.VerifyPerGoSignature(payload, oldSig, secret) {
			t.Errorf("expected expired timestamp to fail replay check")
		}
	})

	t.Run("Future timestamp (> 5 minutes ahead) fails replay check", func(t *testing.T) {
		futureTs := fmt.Sprintf("%d", time.Now().Add(6*time.Minute).Unix())
		futureSig := webhook.SignPayload(payload, []byte(secret), futureTs)
		if webhook.VerifyPerGoSignature(payload, futureSig, secret) {
			t.Errorf("expected future timestamp to fail replay check")
		}
	})

	t.Run("Timestamp within tolerance (e.g. 2 minutes old) succeeds", func(t *testing.T) {
		recentTs := fmt.Sprintf("%d", time.Now().Add(-2*time.Minute).Unix())
		recentSig := webhook.SignPayload(payload, []byte(secret), recentTs)
		if !webhook.VerifyPerGoSignature(payload, recentSig, secret) {
			t.Errorf("expected timestamp within 5-minute tolerance to succeed")
		}
	})
}

func TestDispatcher_SignatureOmittedWhenNoSecret(t *testing.T) {
	wsID := uuid.New()
	subID := uuid.New()

	subStore := &mockSubscriptionStore{
		sub: &repository.WebhookSubscription{
			ID:          subID,
			WorkspaceID: wsID,
			URL:         "https://example.com/webhook",
			Secret:      nil, // No subscription secret
			Active:      true,
		},
	}
	wsStore := &mockWorkspaceStore{
		ws: &repository.Workspace{
			ID:            wsID,
			WebhookSecret: nil, // No workspace secret
		},
	}
	httpClient := &mockHTTPClient{}
	d := webhook.NewDefaultDispatcher(subStore, nil, wsStore, httpClient, nil)

	task := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: subID,
		WorkspaceID:    wsID,
		Event:          "message.sent",
		Payload:        []byte(`{"test":true}`),
		Mode:           "outbound",
	}

	err := d.Dispatch(context.Background(), task)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if len(httpClient.requests) != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
	}
	req := httpClient.requests[0]
	sigHeader := req.Header.Get("X-PerGo-Signature")
	if sigHeader != "" {
		t.Errorf("expected X-PerGo-Signature to be omitted when no secret configured, got %q", sigHeader)
	}
}

func TestDispatcher_SignatureAttachedAndVerifiedFromSnippet(t *testing.T) {
	wsID := uuid.New()
	subID := uuid.New()
	secret := "super-secure-secret-hex-string-1234567890"

	subStore := &mockSubscriptionStore{
		sub: &repository.WebhookSubscription{
			ID:          subID,
			WorkspaceID: wsID,
			URL:         "https://example.com/webhook",
			Secret:      nil, // Fallback to workspace secret
			Active:      true,
		},
	}
	wsStore := &mockWorkspaceStore{
		ws: &repository.Workspace{
			ID:            wsID,
			WebhookSecret: &secret,
		},
	}
	httpClient := &mockHTTPClient{}
	d := webhook.NewDefaultDispatcher(subStore, nil, wsStore, httpClient, nil)

	payload := []byte(`{"event":"contact.created","contact_id":"c-123"}`)
	task := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: subID,
		WorkspaceID:    wsID,
		Event:          "contact.created",
		Payload:        payload,
		Mode:           "outbound",
	}

	err := d.Dispatch(context.Background(), task)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if len(httpClient.requests) != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
	}
	req := httpClient.requests[0]
	sigHeader := req.Header.Get("X-PerGo-Signature")
	if sigHeader == "" {
		t.Fatalf("expected X-PerGo-Signature header to be set")
	}

	// Verify using the exact snippet logic from docs/WEBHOOK_SIGNATURES.md
	verified := webhook.VerifyPerGoSignature(payload, sigHeader, secret)
	if !verified {
		t.Errorf("signature verification using docs/WEBHOOK_SIGNATURES.md logic failed")
	}
}
