package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestMessageHandler_HashIdempotencyKey_Empirical(t *testing.T) {
	h := &MessageHandler{}

	t.Run("With Explicit Header Key", func(t *testing.T) {
		headerKey := "unique-client-key-999"
		body := []byte(`{"to":"5511999998888","channel":"telegram","type":"text","text":{"body":"Hello"}}`)

		idKey, keyHash := h.hashIdempotencyKey(headerKey, body)

		if idKey != headerKey {
			t.Errorf("idempotencyKey = %q, want %q", idKey, headerKey)
		}

		expectedHash := sha256.Sum256([]byte(headerKey))
		expectedHashHex := hex.EncodeToString(expectedHash[:])
		if keyHash != expectedHashHex {
			t.Errorf("keyHash = %q, want %q", keyHash, expectedHashHex)
		}
	})

	t.Run("With Empty Header Key (Body Hashing Fallback)", func(t *testing.T) {
		headerKey := ""
		body := []byte(`{"to":"5511999998888","channel":"telegram","type":"text","text":{"body":"Hello"}}`)

		idKey, keyHash := h.hashIdempotencyKey(headerKey, body)

		expectedHash := sha256.Sum256(body)
		expectedHashHex := hex.EncodeToString(expectedHash[:])

		if idKey != expectedHashHex {
			t.Errorf("idempotencyKey = %q, want %q", idKey, expectedHashHex)
		}
		if keyHash != expectedHashHex {
			t.Errorf("keyHash = %q, want %q", keyHash, expectedHashHex)
		}
	})
}

func TestMessageHandler_CheckAndRecordIdempotency_NilRepoOrWorkspace(t *testing.T) {
	h := &MessageHandler{IdempotencyRepo: nil}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	wsID := uuid.New()
	msgReq := &domain.CreateMessageRequest{Channel: "telegram", To: "12345"}

	// 1. Nil IdempotencyRepo
	cached, err := h.checkAndRecordIdempotency(c, wsID, "trace-1", "hash-1", "key-1", msgReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached {
		t.Errorf("expected cached = false when IdempotencyRepo is nil, got true")
	}

	// 2. Nil WorkspaceID
	h.IdempotencyRepo = nil // even if non-nil, uuid.Nil workspaceID returns false
	cached, err = h.checkAndRecordIdempotency(c, uuid.Nil, "trace-1", "hash-1", "key-1", msgReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached {
		t.Errorf("expected cached = false when workspaceID is uuid.Nil, got true")
	}
}

func TestMessageHandler_RecordIdempotencyCompletion_NilRepoOrWorkspace(t *testing.T) {
	h := &MessageHandler{IdempotencyRepo: nil}
	ctx := context.Background()

	// Should not panic with nil repo
	h.recordIdempotencyCompletion(ctx, uuid.New(), "trace-1", "hash-1", []byte(`{"status":"queued"}`))

	// Should not panic with uuid.Nil workspace
	h.recordIdempotencyCompletion(ctx, uuid.Nil, "trace-1", "hash-1", []byte(`{"status":"queued"}`))
}
