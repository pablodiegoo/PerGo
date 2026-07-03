package session

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/types"

	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func TestStartPairing_LimitExceeded(t *testing.T) {
	pool := getTestPool(t)
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	repo := NewDeviceRepository(pool)
	registry := NewActiveSession()

	manager := NewManager(
		db,
		repo,
		registry,
		nil, // dispatchers
		"",  // waVersion
		nil, // inboundProcessor
	)

	ctx := context.Background()
	workspaceID := uuid.New()

	// Insert workspace for FK constraint
	_, err = pool.Exec(ctx, "INSERT INTO workspaces (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", workspaceID, "limit-test-workspace-"+workspaceID.String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces WHERE id = $1", workspaceID)
	}()

	// Configure connection limit environment variable
	t.Setenv("PERGO_MAX_WHATSAPP_CONNECTIONS", "2")

	// Create two connected devices in the DB and register them in-memory
	deviceIDs := []uuid.UUID{uuid.New(), uuid.New()}
	jids := []string{"5511999990001@s.whatsapp.net", "5511999990002@s.whatsapp.net"}

	for i, id := range deviceIDs {
		d := &Device{
			ID:          id,
			WorkspaceID: workspaceID,
			Channel:     "whatsapp",
			JID:         jids[i],
			Phone:       "551199999000" + string(rune('1'+i)),
			Status:      DeviceStatusConnected,
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("failed to create device %d: %v", i, err)
		}
		defer func(dID uuid.UUID) {
			_ = repo.Delete(ctx, dID)
		}(id)

		// Add session to registry to make it count as active
		parsedJID, _ := types.ParseJID(jids[i])
		registry.Add(&Session{
			DeviceID: id.String(),
			JID:      parsedJID,
			Client:   nil, // not needed for check
			Cancel:   func() {},
		})
	}

	// Try starting a third pairing. Since limit is 2 and both are active, it must fail with ErrMaxConnectionsExceeded.
	_, err = manager.StartPairing(ctx, workspaceID, "5511999990003", nil)
	if err != ErrMaxConnectionsExceeded {
		t.Errorf("expected ErrMaxConnectionsExceeded, got %v", err)
	}

	// Try starting a third pairing passing one of the existing connection IDs as existingConnID (re-pairing flow).
	// This should bypass the limit check for that slot and NOT return ErrMaxConnectionsExceeded.
	// Note: it will try to call NewWhatsAppClient, which will fail or succeed depending on mock,
	// but it won't fail with ErrMaxConnectionsExceeded!
	_, err = manager.StartPairing(ctx, workspaceID, "5511999990001", &deviceIDs[0])
	if err == ErrMaxConnectionsExceeded {
		t.Errorf("expected re-pairing to bypass limit check, but got ErrMaxConnectionsExceeded")
	}
}
