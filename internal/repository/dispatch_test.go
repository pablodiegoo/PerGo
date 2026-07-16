package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMessageDispatchRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewMessageDispatchRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "dispatch_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	traceID := uuid.New().String()
	initialChannel := "whatsapp_web"

	// 1. GetOrCreateDispatch: creation case
	d, err := repo.GetOrCreateDispatch(ctx, ws.ID, traceID, initialChannel, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to get/create dispatch: %v", err)
	}

	if d.TraceID != traceID {
		t.Errorf("expected TraceID %s, got %s", traceID, d.TraceID)
	}
	if d.WorkspaceID != ws.ID {
		t.Errorf("expected WorkspaceID %s, got %s", ws.ID, d.WorkspaceID)
	}
	if d.CurrentChannel != initialChannel {
		t.Errorf("expected CurrentChannel %s, got %s", initialChannel, d.CurrentChannel)
	}
	if d.Status != "queued" {
		t.Errorf("expected Status 'queued', got %s", d.Status)
	}
	if d.FallbackIndex != 0 {
		t.Errorf("expected FallbackIndex 0, got %d", d.FallbackIndex)
	}
	if d.ErrorMessage != nil {
		t.Errorf("expected ErrorMessage nil, got %s", *d.ErrorMessage)
	}

	// 2. GetOrCreateDispatch: retrieve existing (idempotency) case
	d2, err := repo.GetOrCreateDispatch(ctx, ws.ID, traceID, "different_channel", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to get/create existing dispatch: %v", err)
	}

	if d2.ID != d.ID {
		t.Errorf("expected ID %s, got %s", d.ID, d2.ID)
	}
	// The channel should still be initialChannel since it was already created
	if d2.CurrentChannel != initialChannel {
		t.Errorf("expected CurrentChannel to remain %s, got %s", initialChannel, d2.CurrentChannel)
	}

	// 3. UpdateDispatchStatus
	errMsg := "some transient error"
	err = repo.UpdateDispatchStatus(ctx, d.ID, "failed_transient", "telegram", 1, &errMsg)
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	// 4. GetByTraceID
	d3, err := repo.GetByTraceID(ctx, traceID)
	if err != nil {
		t.Fatalf("failed to get by trace id: %v", err)
	}

	if d3.Status != "failed_transient" {
		t.Errorf("expected Status 'failed_transient', got %s", d3.Status)
	}
	if d3.CurrentChannel != "telegram" {
		t.Errorf("expected CurrentChannel 'telegram', got %s", d3.CurrentChannel)
	}
	if d3.FallbackIndex != 1 {
		t.Errorf("expected FallbackIndex 1, got %d", d3.FallbackIndex)
	}
	if d3.ErrorMessage == nil || *d3.ErrorMessage != errMsg {
		t.Errorf("expected ErrorMessage %s, got %v", errMsg, d3.ErrorMessage)
	}

	// 5. GetByTraceID: non-existent case
	_, err = repo.GetByTraceID(ctx, "non-existent-trace-id")
	if !errors.Is(err, ErrDispatchNotFound) {
		t.Errorf("expected ErrDispatchNotFound, got %v", err)
	}
}

func TestMessageDispatchProviderMessageID(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "dispatch_provider_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	traceID := uuid.New().String()
	d, err := repo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp_cloud", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create dispatch: %v", err)
	}

	providerID := "wamid.test12345"
	err = repo.UpdateProviderMessageID(ctx, d.ID, providerID)
	if err != nil {
		t.Fatalf("failed to update provider message id: %v", err)
	}

	retrieved, err := repo.GetByProviderMessageID(ctx, providerID)
	if err != nil {
		t.Fatalf("failed to get by provider message id: %v", err)
	}

	if retrieved.ID != d.ID {
		t.Errorf("expected ID %s, got %s", d.ID, retrieved.ID)
	}
	if retrieved.ProviderMessageID == nil || *retrieved.ProviderMessageID != providerID {
		t.Errorf("expected ProviderMessageID %s, got %v", providerID, retrieved.ProviderMessageID)
	}

	_, err = repo.GetByProviderMessageID(ctx, "non-existent-provider-id")
	if !errors.Is(err, ErrDispatchNotFound) {
		t.Errorf("expected ErrDispatchNotFound, got %v", err)
	}
}

