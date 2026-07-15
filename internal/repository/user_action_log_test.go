package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestUserActionLogRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up tables
	_, _ = pool.Exec(ctx, "DELETE FROM user_action_logs")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewUserActionLogRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "user_logs_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 1. Initial list should be empty
	logs, total, err := repo.ListByWorkspace(ctx, ws.ID, 10, 0, "", "")
	if err != nil {
		t.Fatalf("failed to list action logs: %v", err)
	}
	if total != 0 || len(logs) != 0 {
		t.Errorf("expected empty list, got total %d, count %d", total, len(logs))
	}

	// 2. Insert console user log
	ipVal := "192.168.1.1"
	uaVal := "Mozilla/5.0"
	metadataVal := map[string]any{"campaign_id": uuid.New().String()}
	metaBytes, err := json.Marshal(metadataVal)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}

	log1 := &repository.UserActionLog{
		WorkspaceID: ws.ID,
		ActorType:   "user",
		ActorID:     "operator@test.com",
		ActorName:   "Operator User",
		Action:      "campaign.create",
		Source:      "dashboard",
		IPAddress:   &ipVal,
		UserAgent:   &uaVal,
		Metadata:    metaBytes,
	}

	err = repo.Insert(ctx, log1)
	if err != nil {
		t.Fatalf("failed to insert action log 1: %v", err)
	}

	if log1.ID == uuid.Nil {
		t.Error("expected populated log UUID after insert")
	}
	if log1.CreatedAt.IsZero() {
		t.Error("expected populated created_at after insert")
	}

	// 3. Insert API Key log
	ipVal2 := "10.0.0.5"
	uaVal2 := "Go-http-client/1.1"
	metadataVal2 := map[string]any{"message_id": uuid.New().String()}
	metaBytes2, err := json.Marshal(metadataVal2)
	if err != nil {
		t.Fatalf("failed to marshal metadata 2: %v", err)
	}

	log2 := &repository.UserActionLog{
		WorkspaceID: ws.ID,
		ActorType:   "api_key",
		ActorID:     uuid.New().String(),
		ActorName:   "CRM Integrator Key",
		Action:      "message.send",
		Source:      "api",
		IPAddress:   &ipVal2,
		UserAgent:   &uaVal2,
		Metadata:    metaBytes2,
	}

	err = repo.Insert(ctx, log2)
	if err != nil {
		t.Fatalf("failed to insert action log 2: %v", err)
	}

	// 4. Verify pagination and content
	logs, total, err = repo.ListByWorkspace(ctx, ws.ID, 10, 0, "", "")
	if err != nil {
		t.Fatalf("failed to list logs: %v", err)
	}

	if total != 2 {
		t.Errorf("expected total count 2, got %d", total)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 items, got %d", len(logs))
	}

	// First item (ordered by created_at DESC, so log2)
	item1 := logs[0]
	if item1.ActorType != "api_key" {
		t.Errorf("expected item1 actor type 'api_key', got '%s'", item1.ActorType)
	}
	if item1.IPAddress == nil || *item1.IPAddress != ipVal2 {
		t.Errorf("expected item1 IP '%s', got '%v'", ipVal2, item1.IPAddress)
	}
	var parsedMeta2 map[string]any
	if err := json.Unmarshal(item1.Metadata, &parsedMeta2); err != nil {
		t.Fatalf("failed to unmarshal item1 metadata: %v", err)
	}
	if parsedMeta2["message_id"] != metadataVal2["message_id"] {
		t.Errorf("metadata mismatch: expected %v, got %v", metadataVal2, parsedMeta2)
	}

	// Second item (log1)
	item2 := logs[1]
	if item2.ActorType != "user" {
		t.Errorf("expected item2 actor type 'user', got '%s'", item2.ActorType)
	}
	if item2.UserAgent == nil || *item2.UserAgent != uaVal {
		t.Errorf("expected item2 user agent '%s', got '%v'", uaVal, item2.UserAgent)
	}

	// 5. Test pagination limits
	logsLimit, totalLimit, err := repo.ListByWorkspace(ctx, ws.ID, 1, 0, "", "")
	if err != nil {
		t.Fatalf("failed to list with limit: %v", err)
	}
	if totalLimit != 2 {
		t.Errorf("expected totalLimit to remain 2, got %d", totalLimit)
	}
	if len(logsLimit) != 1 {
		t.Errorf("expected 1 item with limit=1, got %d", len(logsLimit))
	}
	if logsLimit[0].ID != item1.ID {
		t.Error("expected first page item to match item1")
	}

	// Test offset
	logsOffset, totalOffset, err := repo.ListByWorkspace(ctx, ws.ID, 1, 1, "", "")
	if err != nil {
		t.Fatalf("failed to list with offset: %v", err)
	}
	if totalOffset != 2 {
		t.Errorf("expected totalOffset to remain 2, got %d", totalOffset)
	}
	if len(logsOffset) != 1 {
		t.Errorf("expected 1 item with offset=1, got %d", len(logsOffset))
	}
	if logsOffset[0].ID != item2.ID {
		t.Error("expected second page item to match item2")
	}

	// 6. Test filters
	t.Run("filter by actorType=user", func(t *testing.T) {
		filteredLogs, filteredTotal, err := repo.ListByWorkspace(ctx, ws.ID, 10, 0, "user", "")
		if err != nil {
			t.Fatalf("filter list failed: %v", err)
		}
		if filteredTotal != 1 {
			t.Errorf("expected 1 user log, got %d", filteredTotal)
		}
		if len(filteredLogs) != 1 || filteredLogs[0].ActorType != "user" {
			t.Errorf("expected user actor type, got %+v", filteredLogs)
		}
	})

	t.Run("filter by source=api", func(t *testing.T) {
		filteredLogs, filteredTotal, err := repo.ListByWorkspace(ctx, ws.ID, 10, 0, "", "api")
		if err != nil {
			t.Fatalf("filter list failed: %v", err)
		}
		if filteredTotal != 1 {
			t.Errorf("expected 1 api log, got %d", filteredTotal)
		}
		if len(filteredLogs) != 1 || filteredLogs[0].Source != "api" {
			t.Errorf("expected source api, got %+v", filteredLogs)
		}
	})
}
