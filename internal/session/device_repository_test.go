package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/platform/postgres"
)

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
		t.Skipf("PostgreSQL not available: %v", err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed: %v", err)
	}

	sqlDB, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := postgres.RunMigrations(sqlDB); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

func TestDeviceStatus_TerminalLock(t *testing.T) {
	pool := getTestPool(t)
	repo := NewDeviceRepository(pool)
	ctx := context.Background()

	// Create test device
	wsID := uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO workspaces (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", wsID, "test-workspace-device-"+wsID.String())
	if err != nil {
		t.Fatalf("failed to create test workspace for device FK: %v", err)
	}

	d := &Device{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Channel:     "whatsapp",
		JID:         "5511999999999@s.whatsapp.net",
		Phone:       "5511999999999",
		Status:      DeviceStatusPending,
	}

	// Insert into DB
	err = repo.Create(ctx, d)
	if err != nil {
		t.Fatalf("failed to create test device: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, d.ID)
	}()

	// 1. Update to Connected (should work)
	err = repo.UpdateStatus(ctx, d.ID, DeviceStatusConnected)
	if err != nil {
		t.Errorf("failed to update status to connected: %v", err)
	}
	d2, _ := repo.GetByID(ctx, d.ID)
	if d2.Status != DeviceStatusConnected {
		t.Errorf("expected status connected, got %s", d2.Status)
	}

	// 2. Update to Terminal (should work)
	err = repo.UpdateStatus(ctx, d.ID, DeviceStatusTerminal)
	if err != nil {
		t.Errorf("failed to update status to terminal: %v", err)
	}
	d2, _ = repo.GetByID(ctx, d.ID)
	if d2.Status != DeviceStatusTerminal {
		t.Errorf("expected status terminal, got %s", d2.Status)
	}

	// 3. Update to Disconnected on a terminal device (should be a no-op / not overwrite)
	err = repo.UpdateStatus(ctx, d.ID, DeviceStatusDisconnected)
	if err != nil {
		t.Errorf("failed to update status: %v", err)
	}
	d2, _ = repo.GetByID(ctx, d.ID)
	if d2.Status != DeviceStatusTerminal {
		t.Errorf("expected status to remain terminal, got %s", d2.Status)
	}

	// 4. Update to Connected again (should allow transition out of terminal on re-pair)
	err = repo.UpdateStatus(ctx, d.ID, DeviceStatusConnected)
	if err != nil {
		t.Errorf("failed to update status: %v", err)
	}
	d2, _ = repo.GetByID(ctx, d.ID)
	if d2.Status != DeviceStatusConnected {
		t.Errorf("expected status to transition back to connected, got %s", d2.Status)
	}
}
