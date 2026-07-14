package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mau.fi/whatsmeow/types"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// getTestPool connects to a local test PostgreSQL database.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

func TestStartPairing_LimitExceeded(t *testing.T) {
	pool := getTestPool(t)
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	enc, _ := crypto.NewEncryptor(make([]byte, 32))
	repo := repository.NewConnectionRepository(pool, enc)
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
		jidStr := jids[i]
		d := &repository.Connection{
			ID:             id,
			WorkspaceID:    workspaceID,
			Name:           "Test Web Client",
			Channel:        "whatsapp",
			JID:            &jidStr,
			SenderIdentity: "551199999000" + string(rune('1'+i)),
			Status:         string(DeviceStatusConnected),
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
	_, err = manager.StartPairing(ctx, workspaceID, "5511999990003", nil, "")
	if err != ErrMaxConnectionsExceeded {
		t.Errorf("expected ErrMaxConnectionsExceeded, got %v", err)
	}

	// Try starting a third pairing passing one of the existing connection IDs as existingConnID (re-pairing flow).
	// This should bypass the limit check for that slot and NOT return ErrMaxConnectionsExceeded.
	// Note: it will try to call NewWhatsAppClient, which will fail or succeed depending on mock,
	// but it won't fail with ErrMaxConnectionsExceeded!
	_, err = manager.StartPairing(ctx, workspaceID, "5511999990001", &deviceIDs[0], "")
	if err == ErrMaxConnectionsExceeded {
		t.Errorf("expected re-pairing to bypass limit check, but got ErrMaxConnectionsExceeded")
	}
}
