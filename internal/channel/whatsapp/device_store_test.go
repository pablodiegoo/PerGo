package whatsapp_test

import (
	"context"
	"os"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func testDSN() string {
	if dsn := os.Getenv("PERGO_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://postgres:postgres@localhost:5432/pergo_test?sslmode=disable"
}

func TestWhatsmeowDeviceStore_Save(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	container := sqlstore.NewWithDB(db, "postgres", waLog.Noop)
	defer container.Close()

	if err := container.Upgrade(ctx); err != nil {
		t.Fatalf("failed to upgrade whatsmeow container: %v", err)
	}

	device := container.NewDevice()
	jid := types.NewJID("5511999999999", types.DefaultUserServer)
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:            []byte("test-details"),
		AccountSignature:   make([]byte, 64),
		DeviceSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
	}

	// 1. Initial save (Insert)
	err = device.Save(ctx)
	if err != nil {
		t.Fatalf("failed to save device store on insert: %v", err)
	}

	// 2. Subsequent save (Update via ON CONFLICT)
	device.PushName = "Test WhatsApp Device"
	err = device.Save(ctx)
	if err != nil {
		t.Fatalf("failed to save device store on update (ON CONFLICT): %v", err)
	}

	// 3. Read back device
	loadedDevice, err := container.GetDevice(ctx, jid)
	if err != nil {
		t.Fatalf("failed to get device from store: %v", err)
	}
	if loadedDevice == nil {
		t.Fatalf("expected loaded device to not be nil")
	}
	if loadedDevice.PushName != "Test WhatsApp Device" {
		t.Errorf("expected push name %q, got %q", "Test WhatsApp Device", loadedDevice.PushName)
	}

	// 4. PreKeys and Sessions child table operations (foreign key to whatsmeow_device)
	preKey, err := loadedDevice.PreKeys.GenOnePreKey(ctx)
	if err != nil {
		t.Fatalf("failed to generate pre key: %v", err)
	}
	if preKey == nil {
		t.Fatalf("expected pre key to not be nil")
	}

	// 5. Delete device
	err = loadedDevice.Delete(ctx)
	if err != nil {
		t.Fatalf("failed to delete device: %v", err)
	}

	// Verify device is deleted
	deletedDevice, err := container.GetDevice(ctx, jid)
	if err != nil {
		t.Fatalf("failed to check deleted device: %v", err)
	}
	if deletedDevice != nil {
		t.Errorf("expected deleted device to be nil, got %+v", deletedDevice)
	}
}
