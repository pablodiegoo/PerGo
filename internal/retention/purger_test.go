package retention_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/retention"
)

type mockMediaDeleter struct {
	mu          sync.Mutex
	deletedURLs []string
	err         error
}

func (m *mockMediaDeleter) DeleteMedia(ctx context.Context, mediaURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.deletedURLs = append(m.deletedURLs, mediaURL)
	return nil
}

type mockWorkspaceLister struct {
	workspaces []repository.Workspace
	err        error
}

func (m *mockWorkspaceLister) ListActiveRetentionPolicies(ctx context.Context) ([]repository.Workspace, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.workspaces, nil
}

type mockAuditPurgerRepo struct {
	mu               sync.Mutex
	expiredEntries   []repository.AuditEntry
	anonymizedLogs   map[uuid.UUID][]byte
	anonymizeCallCnt int
	queryErr         error
	anonymizeErr     error
}

func (m *mockAuditPurgerRepo) GetExpiredMessages(ctx context.Context, workspaceID uuid.UUID, cutoff time.Time, limit int) ([]repository.AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	res := m.expiredEntries
	// Clear so subsequent batch calls return empty
	m.expiredEntries = nil
	return res, nil
}

func (m *mockAuditPurgerRepo) AnonymizeAuditLog(ctx context.Context, id uuid.UUID, createdAt time.Time, sanitizedPayload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.anonymizeErr != nil {
		return m.anonymizeErr
	}
	if m.anonymizedLogs == nil {
		m.anonymizedLogs = make(map[uuid.UUID][]byte)
	}
	m.anonymizedLogs[id] = sanitizedPayload
	m.anonymizeCallCnt++
	return nil
}

func TestPurger_PurgeWorkspace_InboundAndOutbound(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	inboundPayload := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-in-123",
		"message_id":   "msg-in-123",
		"channel":      "whatsapp",
		"from":         "+5511999998888",
		"to":           "+5511888887777",
		"body":         "Sensitive inbound customer message",
		"sender_name":  "Alice",
		"media": map[string]any{
			"media_url": "/media/" + wsID.String() + "/inbound-photo.jpg",
			"caption":   "Important personal document",
		},
	}
	inboundBytes, _ := json.Marshal(inboundPayload)
	inboundID := uuid.New()

	outboundPayload := map[string]any{
		"event":    "outbound_message",
		"trace_id": "trace-out-456",
		"channel":  "whatsapp",
		"request": map[string]any{
			"body":      "Sensitive agent reply with PII",
			"media_url": "/media/" + wsID.String() + "/outbound-contract.pdf",
			"recipient": "+5511999998888",
		},
		"response": "wamid.HBg...",
		"status":   "sent",
	}
	outboundBytes, _ := json.Marshal(outboundPayload)
	outboundID := uuid.New()

	auditRepo := &mockAuditPurgerRepo{
		expiredEntries: []repository.AuditEntry{
			{
				ID:          inboundID,
				WorkspaceID: wsID,
				TraceID:     "trace-in-123",
				EventType:   "inbound_message",
				Payload:     inboundBytes,
				CreatedAt:   time.Now().Add(-35 * 24 * time.Hour),
			},
			{
				ID:          outboundID,
				WorkspaceID: wsID,
				TraceID:     "trace-out-456",
				EventType:   "outbound_message",
				Payload:     outboundBytes,
				CreatedAt:   time.Now().Add(-35 * 24 * time.Hour),
			},
		},
	}

	mediaDeleter := &mockMediaDeleter{}
	wsRepo := &mockWorkspaceLister{}
	purger := retention.NewPurger(wsRepo, auditRepo, mediaDeleter)

	ws := repository.Workspace{
		ID:                 wsID,
		Name:               "Test Workspace",
		MediaRetentionDays: 30,
	}

	stats, err := purger.PurgeWorkspace(ctx, ws)
	if err != nil {
		t.Fatalf("PurgeWorkspace failed: %v", err)
	}

	if stats.MessagesAnonymized != 2 {
		t.Errorf("expected 2 messages anonymized, got %d", stats.MessagesAnonymized)
	}
	if stats.MediaFilesDeleted != 2 {
		t.Errorf("expected 2 media files deleted, got %d", stats.MediaFilesDeleted)
	}
	if stats.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", stats.Errors)
	}

	// Verify Media files deleted
	if len(mediaDeleter.deletedURLs) != 2 {
		t.Fatalf("expected 2 deleted media URLs, got %d", len(mediaDeleter.deletedURLs))
	}
	expectedInboundURL := "/media/" + wsID.String() + "/inbound-photo.jpg"
	expectedOutboundURL := "/media/" + wsID.String() + "/outbound-contract.pdf"
	if mediaDeleter.deletedURLs[0] != expectedInboundURL {
		t.Errorf("expected %s, got %s", expectedInboundURL, mediaDeleter.deletedURLs[0])
	}
	if mediaDeleter.deletedURLs[1] != expectedOutboundURL {
		t.Errorf("expected %s, got %s", expectedOutboundURL, mediaDeleter.deletedURLs[1])
	}

	// Verify Inbound sanitization
	sanitizedInboundRaw := auditRepo.anonymizedLogs[inboundID]
	var sanitizedInbound map[string]any
	_ = json.Unmarshal(sanitizedInboundRaw, &sanitizedInbound)

	if sanitizedInbound["body"] != retention.ExpungedMarker {
		t.Errorf("expected inbound body to be %s, got %v", retention.ExpungedMarker, sanitizedInbound["body"])
	}
	mediaMap := sanitizedInbound["media"].(map[string]any)
	if mediaMap["caption"] != retention.ExpungedMarker {
		t.Errorf("expected media caption to be %s, got %v", retention.ExpungedMarker, mediaMap["caption"])
	}
	if mediaMap["media_url"] != retention.ExpungedMarker {
		t.Errorf("expected media_url to be %s, got %v", retention.ExpungedMarker, mediaMap["media_url"])
	}
	if sanitizedInbound["purged"] != "true" {
		t.Errorf("expected purged='true', got %v", sanitizedInbound["purged"])
	}
	if sanitizedInbound["channel"] != "whatsapp" || sanitizedInbound["trace_id"] != "trace-in-123" {
		t.Errorf("expected metadata to be preserved, got channel=%v, trace=%v", sanitizedInbound["channel"], sanitizedInbound["trace_id"])
	}

	// Verify Outbound sanitization
	sanitizedOutboundRaw := auditRepo.anonymizedLogs[outboundID]
	var sanitizedOutbound map[string]any
	_ = json.Unmarshal(sanitizedOutboundRaw, &sanitizedOutbound)

	reqMap := sanitizedOutbound["request"].(map[string]any)
	if reqMap["body"] != retention.ExpungedMarker {
		t.Errorf("expected outbound request.body to be %s, got %v", retention.ExpungedMarker, reqMap["body"])
	}
	if reqMap["media_url"] != retention.ExpungedMarker {
		t.Errorf("expected outbound request.media_url to be %s, got %v", retention.ExpungedMarker, reqMap["media_url"])
	}
	if sanitizedOutbound["status"] != "sent" || sanitizedOutbound["channel"] != "whatsapp" {
		t.Errorf("expected outbound status and channel to be preserved for aggregate metrics")
	}
	if sanitizedOutbound["purged"] != "true" {
		t.Errorf("expected purged='true', got %v", sanitizedOutbound["purged"])
	}
}

func TestPurger_RetentionDisabled(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	auditRepo := &mockAuditPurgerRepo{
		expiredEntries: []repository.AuditEntry{
			{ID: uuid.New(), WorkspaceID: wsID},
		},
	}
	mediaDeleter := &mockMediaDeleter{}
	purger := retention.NewPurger(nil, auditRepo, mediaDeleter)

	ws := repository.Workspace{
		ID:                 wsID,
		Name:               "Unlimited Workspace",
		MediaRetentionDays: 0, // Disabled
	}

	stats, err := purger.PurgeWorkspace(ctx, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MessagesAnonymized != 0 || stats.MediaFilesDeleted != 0 {
		t.Errorf("expected no purge action when retention is 0, got %+v", stats)
	}
	if len(mediaDeleter.deletedURLs) != 0 {
		t.Errorf("expected no media deleted, got %d", len(mediaDeleter.deletedURLs))
	}
}

func TestPurger_PurgeAll(t *testing.T) {
	ctx := context.Background()
	ws1ID := uuid.New()
	ws2ID := uuid.New()

	wsRepo := &mockWorkspaceLister{
		workspaces: []repository.Workspace{
			{ID: ws1ID, Name: "WS 1", MediaRetentionDays: 15},
			{ID: ws2ID, Name: "WS 2", MediaRetentionDays: 60},
		},
	}

	auditRepo := &mockAuditPurgerRepo{}
	mediaDeleter := &mockMediaDeleter{}
	purger := retention.NewPurger(wsRepo, auditRepo, mediaDeleter)

	results, err := purger.PurgeAll(ctx)
	if err != nil {
		t.Fatalf("PurgeAll failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 workspace results, got %d", len(results))
	}
}

func TestDaemon_RunAndStop(t *testing.T) {
	wsRepo := &mockWorkspaceLister{}
	auditRepo := &mockAuditPurgerRepo{}
	mediaDeleter := &mockMediaDeleter{}
	purger := retention.NewPurger(wsRepo, auditRepo, mediaDeleter)

	daemon := retention.NewDaemon(purger, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		daemon.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	daemon.Stop()

	select {
	case <-done:
		// Succeeded
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for daemon to stop")
	}
}
