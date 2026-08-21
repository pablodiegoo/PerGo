package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrFlowTokenInvalid is returned when a flow token has an invalid signature, structure, or encoding.
	ErrFlowTokenInvalid = errors.New("invalid flow token")

	// ErrFlowTokenExpired is returned when a flow token is well-formed and validly signed but its TTL has expired.
	ErrFlowTokenExpired = errors.New("flow token expired")
)

// DefaultFlowTokenTTL is the default validity duration for generated flow tokens (7 days).
const DefaultFlowTokenTTL = 7 * 24 * time.Hour

// FlowTokenPayload holds the unencrypted metadata encapsulated in a stateless Flow Token.
type FlowTokenPayload struct {
	WorkspaceID  uuid.UUID `json:"workspace_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	ContactID    string    `json:"contact_id"`
	FlowID       string    `json:"flow_id"`
	ExpiresAt    int64     `json:"expires_at"`
}

// GenerateFlowToken creates a stateless HMAC-SHA256 signed flow token with the given payload and secret.
// If payload.ExpiresAt is 0, DefaultFlowTokenTTL (7 days) is applied.
func GenerateFlowToken(payload FlowTokenPayload, secret []byte) (string, error) {
	if payload.ExpiresAt <= 0 {
		payload.ExpiresAt = time.Now().Add(DefaultFlowTokenTTL).Unix()
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal flow token payload: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payloadB64))
	sig := h.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s", payloadB64, sigB64), nil
}

// ParseAndValidateFlowToken parses and validates an HMAC-SHA256 signed flow token.
// If the signature or structure is invalid, it returns ErrFlowTokenInvalid.
// If the token is valid but expired, it returns the parsed payload along with ErrFlowTokenExpired.
func ParseAndValidateFlowToken(tokenStr string, secret []byte) (*FlowTokenPayload, error) {
	if tokenStr == "" || len(secret) == 0 {
		return nil, ErrFlowTokenInvalid
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 2 {
		return nil, ErrFlowTokenInvalid
	}

	payloadB64 := parts[0]
	sigB64 := parts[1]

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, ErrFlowTokenInvalid
	}

	// Verify HMAC signature in constant time
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payloadB64))
	expectedSig := h.Sum(nil)

	if !hmac.Equal(sig, expectedSig) {
		return nil, ErrFlowTokenInvalid
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrFlowTokenInvalid
	}

	var payload FlowTokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, ErrFlowTokenInvalid
	}

	if payload.ExpiresAt > 0 && time.Now().Unix() > payload.ExpiresAt {
		return &payload, ErrFlowTokenExpired
	}

	return &payload, nil
}
