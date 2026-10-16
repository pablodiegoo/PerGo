package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditRepository_RetentionQueries(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	auditRepo := NewAuditRepository(pool)

	ws, err := wsRepo.Create(ctx, "retention_audit_test_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	now := time.Now().UTC()
	oldTime := now.Add(-40 * 24 * time.Hour)
	recentTime := now.Add(-5 * 24 * time.Hour)

	// Insert an expired inbound message
	payloadOld := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-old-1",
		"message_id":   "msg-old-1",
		"channel":      "whatsapp",
		"timestamp":    oldTime.Format(time.RFC3339),
		"workspace_id": ws.ID.String(),
		"body":         "Secret personal message from the past",
	}
	payloadBytesOld, _ := json.Marshal(payloadOld)
	var oldEntryID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, 'inbound_message', $3, $4)
		RETURNING id`,
		ws.ID, "trace-old-1", payloadBytesOld, oldTime,
	).Scan(&oldEntryID)
	if err != nil {
		t.Fatalf("failed to insert old audit log: %v", err)
	}

	// Insert a recent inbound message
	payloadRecent := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-recent-1",
		"message_id":   "msg-recent-1",
		"channel":      "whatsapp",
		"timestamp":    recentTime.Format(time.RFC3339),
		"workspace_id": ws.ID.String(),
		"body":         "Recent message",
	}
	payloadBytesRecent, _ := json.Marshal(payloadRecent)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, 'inbound_message', $3, $4)`,
		ws.ID, "trace-recent-1", payloadBytesRecent, recentTime,
	)
	if err != nil {
		t.Fatalf("failed to insert recent audit log: %v", err)
	}

	cutoff := now.Add(-30 * 24 * time.Hour)

	// 1. Get expired messages
	expired, err := auditRepo.GetExpiredMessages(ctx, ws.ID, cutoff, 100)
	if err != nil {
		t.Fatalf("GetExpiredMessages failed: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired message, got %d", len(expired))
	}
	if expired[0].ID != oldEntryID {
		t.Errorf("expected expired message ID %s, got %s", oldEntryID, expired[0].ID)
	}

	// 2. Anonymize the expired message
	sanitizedPayload := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-old-1",
		"message_id":   "msg-old-1",
		"channel":      "whatsapp",
		"timestamp":    oldTime.Format(time.RFC3339),
		"workspace_id": ws.ID.String(),
		"body":         "[EXPURGADO]",
		"purged":       "true",
	}
	sanitizedBytes, _ := json.Marshal(sanitizedPayload)
	err = auditRepo.AnonymizeAuditLog(ctx, oldEntryID, expired[0].CreatedAt, sanitizedBytes)
	if err != nil {
		t.Fatalf("AnonymizeAuditLog failed: %v", err)
	}

	// 3. Verify GetExpiredMessages no longer returns it
	expiredAfter, err := auditRepo.GetExpiredMessages(ctx, ws.ID, cutoff, 100)
	if err != nil {
		t.Fatalf("GetExpiredMessages failed: %v", err)
	}
	if len(expiredAfter) != 0 {
		t.Fatalf("expected 0 expired unpurged messages, got %d", len(expiredAfter))
	}

	// 4. Verify the row is still intact in audit_logs with [EXPURGADO]
	var updatedBody string
	err = pool.QueryRow(ctx, `SELECT payload->>'body' FROM audit_logs WHERE id = $1 AND created_at = $2`, oldEntryID, expired[0].CreatedAt).Scan(&updatedBody)
	if err != nil {
		t.Fatalf("failed to query updated audit log: %v", err)
	}
	if updatedBody != "[EXPURGADO]" {
		t.Errorf("expected body '[EXPURGADO]', got %q", updatedBody)
	}
}

func TestAuditRepository_InsertAuditLog(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsRepo := NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "Audit Insert Test WS "+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	auditRepo := NewAuditRepository(pool)

	entry := &AuditEntry{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		TraceID:     "trace-test-insert-1",
		EventType:   "chat.message.sent",
		Payload:     []byte(`{"body":"hello world","channel":"whatsapp"}`),
		CreatedAt:   time.Now().UTC(),
	}

	if err := auditRepo.InsertAuditLog(ctx, entry); err != nil {
		t.Fatalf("InsertAuditLog failed: %v", err)
	}

	// Verify using ListFiltered
	entries, total, err := auditRepo.ListFiltered(ctx, AuditFilters{
		WorkspaceID: &ws.ID,
		TraceID:     "trace-test-insert-1",
	})
	if err != nil {
		t.Fatalf("ListFiltered failed: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d (total: %d)", len(entries), total)
	}
	if entries[0].EventType != "chat.message.sent" {
		t.Errorf("expected event_type 'chat.message.sent', got %q", entries[0].EventType)
	}

	// Test invalid workspace ID error
	invalidEntry := &AuditEntry{
		WorkspaceID: uuid.Nil,
		TraceID:     "trace-nil",
	}
	if err := auditRepo.InsertAuditLog(ctx, invalidEntry); err != ErrInvalidWorkspaceID {
		t.Errorf("expected ErrInvalidWorkspaceID, got %v", err)
	}
}


