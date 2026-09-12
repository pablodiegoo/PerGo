package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/retention"
)

func TestRetentionPurge_EndToEnd(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("database not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	// Setup S3 storage and MediaEngine
	s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
	if err != nil {
		t.Fatalf("failed to create s3 client: %v", err)
	}
	mediaEngine := media.NewDefaultEngine(s3Client)

	// 1. Create Workspace with 30-day retention
	wsName := "retention_e2e_ws_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	if err := wsRepo.SetMediaRetentionDays(ctx, ws.ID, 30); err != nil {
		t.Fatalf("failed to set media retention days: %v", err)
	}
	ws.MediaRetentionDays = 30

	now := time.Now().UTC()
	expiredTime := now.Add(-45 * 24 * time.Hour)
	recentTime := now.Add(-2 * 24 * time.Hour)

	// 2. Upload media for expired and recent messages
	expiredMediaData := []byte("fake-expired-photo-content")
	expiredMediaURL, err := mediaEngine.ProcessInbound(ctx, ws.ID, "image", expiredMediaData)
	if err != nil {
		t.Fatalf("failed to upload expired media: %v", err)
	}

	recentMediaData := []byte("fake-recent-photo-content")
	recentMediaURL, err := mediaEngine.ProcessInbound(ctx, ws.ID, "image", recentMediaData)
	if err != nil {
		t.Fatalf("failed to upload recent media: %v", err)
	}

	// 3. Insert audit logs
	expiredPayload := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-retention-old",
		"message_id":   "msg-retention-old",
		"channel":      "whatsapp",
		"timestamp":    expiredTime.Format(time.RFC3339),
		"workspace_id": ws.ID.String(),
		"body":         "Confidential financial statement 2026",
		"media": map[string]any{
			"media_url": expiredMediaURL,
			"caption":   "Document receipt.jpg",
		},
	}
	expiredBytes, _ := json.Marshal(expiredPayload)
	var expiredLogID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, 'inbound_message', $3, $4)
		RETURNING id`,
		ws.ID, "trace-retention-old", expiredBytes, expiredTime,
	).Scan(&expiredLogID)
	if err != nil {
		t.Fatalf("failed to insert expired audit log: %v", err)
	}

	recentPayload := map[string]any{
		"event":        "inbound_message",
		"trace_id":     "trace-retention-recent",
		"message_id":   "msg-retention-recent",
		"channel":      "whatsapp",
		"timestamp":    recentTime.Format(time.RFC3339),
		"workspace_id": ws.ID.String(),
		"body":         "Recent active inquiry",
		"media": map[string]any{
			"media_url": recentMediaURL,
			"caption":   "Recent photo.jpg",
		},
	}
	recentBytes, _ := json.Marshal(recentPayload)
	var recentLogID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, 'inbound_message', $3, $4)
		RETURNING id`,
		ws.ID, "trace-retention-recent", recentBytes, recentTime,
	).Scan(&recentLogID)
	if err != nil {
		t.Fatalf("failed to insert recent audit log: %v", err)
	}

	// 4. Run Purger
	purger := retention.NewPurger(wsRepo, auditRepo, mediaEngine)
	stats, err := purger.PurgeWorkspace(ctx, *ws)
	if err != nil {
		t.Fatalf("PurgeWorkspace failed: %v", err)
	}

	if stats.MessagesAnonymized != 1 {
		t.Errorf("expected 1 message anonymized, got %d", stats.MessagesAnonymized)
	}
	if stats.MediaFilesDeleted != 1 {
		t.Errorf("expected 1 media file deleted, got %d", stats.MediaFilesDeleted)
	}

	// 5. Verify expired message is anonymized in DB
	var expiredBody string
	var expiredCaption string
	var expiredPurged string
	err = pool.QueryRow(ctx, `
		SELECT payload->>'body', payload->'media'->>'caption', payload->>'purged'
		FROM audit_logs
		WHERE id = $1 AND created_at = $2`,
		expiredLogID, expiredTime,
	).Scan(&expiredBody, &expiredCaption, &expiredPurged)
	if err != nil {
		t.Fatalf("failed to query expired audit log: %v", err)
	}

	if expiredBody != retention.ExpungedMarker {
		t.Errorf("expected expired body %s, got %q", retention.ExpungedMarker, expiredBody)
	}
	if expiredCaption != retention.ExpungedMarker {
		t.Errorf("expected expired caption %s, got %q", retention.ExpungedMarker, expiredCaption)
	}
	if expiredPurged != "true" {
		t.Errorf("expected purged='true', got %q", expiredPurged)
	}

	// 6. Verify expired media file is deleted from S3
	expiredKey := media.ExtractS3KeyFromURL(expiredMediaURL)
	_, _, err = s3Client.Download(ctx, expiredKey)
	if err == nil {
		t.Errorf("expected expired media key %s to be deleted from S3, but it was found", expiredKey)
	}

	// 7. Verify recent message is untouched in DB
	var recentBody string
	var recentCaption string
	var recentPurged *string
	err = pool.QueryRow(ctx, `
		SELECT payload->>'body', payload->'media'->>'caption', payload->>'purged'
		FROM audit_logs
		WHERE id = $1 AND created_at = $2`,
		recentLogID, recentTime,
	).Scan(&recentBody, &recentCaption, &recentPurged)
	if err != nil {
		t.Fatalf("failed to query recent audit log: %v", err)
	}

	if recentBody != "Recent active inquiry" {
		t.Errorf("expected recent body 'Recent active inquiry', got %q", recentBody)
	}
	if recentCaption != "Recent photo.jpg" {
		t.Errorf("expected recent caption 'Recent photo.jpg', got %q", recentCaption)
	}
	if recentPurged != nil {
		t.Errorf("expected recent message to not have purged marker, got %v", *recentPurged)
	}

	// 8. Verify recent media file is still present in S3
	recentKey := media.ExtractS3KeyFromURL(recentMediaURL)
	rc, _, err := s3Client.Download(ctx, recentKey)
	if err != nil {
		t.Errorf("expected recent media key %s to exist in S3, got error: %v", recentKey, err)
	} else {
		_ = rc.Close()
	}

	// 9. Verify aggregate count in audit_logs is still 2
	var totalAuditLogs int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE workspace_id = $1`, ws.ID).Scan(&totalAuditLogs)
	if err != nil {
		t.Fatalf("failed to count audit logs: %v", err)
	}
	if totalAuditLogs != 2 {
		t.Errorf("expected 2 total audit logs preserved for aggregate metrics, got %d", totalAuditLogs)
	}
}
