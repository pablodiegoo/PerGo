package crypto_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/crypto"
)

func TestFlowToken(t *testing.T) {
	wsID := uuid.New()
	connID := uuid.New()
	contactID := "5511999998888"
	flowID := "flow_meta_123"
	secret := []byte("workspace-signing-secret-key-12345")

	t.Run("Generate and validate valid token", func(t *testing.T) {
		expiresAt := time.Now().Add(7 * 24 * time.Hour).Unix()
		tokenPayload := crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    contactID,
			FlowID:       flowID,
			ExpiresAt:    expiresAt,
		}

		tokenStr, err := crypto.GenerateFlowToken(tokenPayload, secret)
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		if tokenStr == "" {
			t.Fatalf("expected non-empty token string")
		}

		// Token must have format <payload_b64url>.<sig_b64url>
		parts := strings.Split(tokenStr, ".")
		if len(parts) != 2 {
			t.Fatalf("expected 2 dot-separated parts in token, got %d: %q", len(parts), tokenStr)
		}

		parsed, err := crypto.ParseAndValidateFlowToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("ParseAndValidateFlowToken failed: %v", err)
		}

		if parsed.WorkspaceID != wsID {
			t.Errorf("expected WorkspaceID %s, got %s", wsID, parsed.WorkspaceID)
		}
		if parsed.ConnectionID != connID {
			t.Errorf("expected ConnectionID %s, got %s", connID, parsed.ConnectionID)
		}
		if parsed.ContactID != contactID {
			t.Errorf("expected ContactID %s, got %s", contactID, parsed.ContactID)
		}
		if parsed.FlowID != flowID {
			t.Errorf("expected FlowID %s, got %s", flowID, parsed.FlowID)
		}
		if parsed.ExpiresAt != expiresAt {
			t.Errorf("expected ExpiresAt %d, got %d", expiresAt, parsed.ExpiresAt)
		}
	})

	t.Run("Default 7-day TTL if ExpiresAt is zero", func(t *testing.T) {
		tokenPayload := crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    contactID,
			FlowID:       flowID,
		}

		now := time.Now()
		tokenStr, err := crypto.GenerateFlowToken(tokenPayload, secret)
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		parsed, err := crypto.ParseAndValidateFlowToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("ParseAndValidateFlowToken failed: %v", err)
		}

		expectedMinExp := now.Add(7*24*time.Hour - 5*time.Second).Unix()
		expectedMaxExp := now.Add(7*24*time.Hour + 5*time.Second).Unix()
		if parsed.ExpiresAt < expectedMinExp || parsed.ExpiresAt > expectedMaxExp {
			t.Errorf("expected ExpiresAt between %d and %d, got %d", expectedMinExp, expectedMaxExp, parsed.ExpiresAt)
		}
	})

	t.Run("Expired token returns ErrFlowTokenExpired", func(t *testing.T) {
		expiredAt := time.Now().Add(-1 * time.Hour).Unix()
		tokenPayload := crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    contactID,
			FlowID:       flowID,
			ExpiresAt:    expiredAt,
		}

		tokenStr, err := crypto.GenerateFlowToken(tokenPayload, secret)
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		parsed, err := crypto.ParseAndValidateFlowToken(tokenStr, secret)
		if err != crypto.ErrFlowTokenExpired {
			t.Fatalf("expected ErrFlowTokenExpired, got err: %v, parsed: %+v", err, parsed)
		}
		if parsed == nil {
			t.Errorf("expected parsed payload to still be returned even when expired for inspection")
		}
	})

	t.Run("Tampered signature returns ErrFlowTokenInvalid", func(t *testing.T) {
		tokenPayload := crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    contactID,
			FlowID:       flowID,
			ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		}

		tokenStr, err := crypto.GenerateFlowToken(tokenPayload, secret)
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		parts := strings.Split(tokenStr, ".")
		tamperedSig := parts[1] + "abc"
		tamperedToken := parts[0] + "." + tamperedSig

		_, err = crypto.ParseAndValidateFlowToken(tamperedToken, secret)
		if err != crypto.ErrFlowTokenInvalid {
			t.Fatalf("expected ErrFlowTokenInvalid for tampered signature, got: %v", err)
		}
	})

	t.Run("Wrong secret returns ErrFlowTokenInvalid", func(t *testing.T) {
		tokenPayload := crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    contactID,
			FlowID:       flowID,
			ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		}

		tokenStr, err := crypto.GenerateFlowToken(tokenPayload, secret)
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		_, err = crypto.ParseAndValidateFlowToken(tokenStr, []byte("wrong-secret"))
		if err != crypto.ErrFlowTokenInvalid {
			t.Fatalf("expected ErrFlowTokenInvalid for wrong secret, got: %v", err)
		}
	})

	t.Run("Malformed token string returns ErrFlowTokenInvalid", func(t *testing.T) {
		malformedCases := []string{
			"",
			"not-a-token",
			"a.b.c",
			"invalid-b64!." + base64.RawURLEncoding.EncodeToString([]byte("sig")),
			base64.RawURLEncoding.EncodeToString([]byte("not-json")) + "." + base64.RawURLEncoding.EncodeToString([]byte("sig")),
		}

		for _, tc := range malformedCases {
			_, err := crypto.ParseAndValidateFlowToken(tc, secret)
			if err != crypto.ErrFlowTokenInvalid {
				t.Errorf("for input %q: expected ErrFlowTokenInvalid, got: %v", tc, err)
			}
		}
	})
}
