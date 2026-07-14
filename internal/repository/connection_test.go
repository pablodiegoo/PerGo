package repository_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

func getTestPoolWithMigrations(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := getMigrationTestPool(t)
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool: %v", err)
	}
	defer db.Close()
	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	return pool
}

func TestConnectionRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up all tables to ensure a clean slate and avoid decrypting dirty rows with mock KEK
	_, _ = pool.Exec(ctx, "DELETE FROM audit_logs")
	_, _ = pool.Exec(ctx, "DELETE FROM waba_templates")
	_, _ = pool.Exec(ctx, "DELETE FROM recipient_sessions")
	_, _ = pool.Exec(ctx, "DELETE FROM message_dispatches")
	_, _ = pool.Exec(ctx, "DELETE FROM webhooks_dlq")
	_, _ = pool.Exec(ctx, "DELETE FROM webhooks")
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM devices")
	_, _ = pool.Exec(ctx, "DELETE FROM channel_credentials")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	// 1. Setup Encryptor
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	repo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "conn_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 2. Test Create & GetByID
	jidVal := "5511999990002@s.whatsapp.net"
	proxyVal := "socks5://127.0.0.1:1080"
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Web Connection",
		Channel:        "whatsapp",
		SenderIdentity: "5511999990002",
		Status:         "pending",
		IsDefault:      true,
		Credentials:    []byte("my-whatsapp-token-secret"),
		JID:            &jidVal,
		ProxyURL:       &proxyVal,
	}

	err = repo.Create(ctx, conn)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, conn.ID)
	}()

	// Retrieve by ID
	retrieved, err := repo.GetByID(ctx, conn.ID)
	if err != nil {
		t.Fatalf("failed to retrieve connection: %v", err)
	}

	if retrieved.Name != conn.Name {
		t.Errorf("got name %q, want %q", retrieved.Name, conn.Name)
	}
	if !bytes.Equal(retrieved.Credentials, conn.Credentials) {
		t.Errorf("credentials mismatch: got %q, want %q", string(retrieved.Credentials), string(conn.Credentials))
	}
	if retrieved.JID == nil || *retrieved.JID != jidVal {
		t.Errorf("JID mismatch: got %v, want %q", retrieved.JID, jidVal)
	}
	if retrieved.ProxyURL == nil || *retrieved.ProxyURL != proxyVal {
		t.Errorf("ProxyURL mismatch: got %v, want %q", retrieved.ProxyURL, proxyVal)
	}

	// Verify encryption in DB (credentials should be encrypted, not plaintext)
	var dbCredentials []byte
	err = pool.QueryRow(ctx, "SELECT credentials FROM connections WHERE id = $1", conn.ID).Scan(&dbCredentials)
	if err != nil {
		t.Fatalf("failed to query raw credentials from DB: %v", err)
	}
	if bytes.Equal(dbCredentials, conn.Credentials) {
		t.Error("expected credentials to be stored encrypted, but they matched plaintext")
	}

	// 3. Test GetBySenderIdentity
	retrieved2, err := repo.GetBySenderIdentity(ctx, ws.ID, conn.SenderIdentity)
	if err != nil {
		t.Fatalf("failed to retrieve by sender identity: %v", err)
	}
	if retrieved2.ID != conn.ID {
		t.Errorf("got connection ID %s, want %s", retrieved2.ID, conn.ID)
	}

	// 4. Test GetDefaultChannelConnection
	retrievedDefault, err := repo.GetDefaultChannelConnection(ctx, ws.ID, conn.Channel)
	if err != nil {
		t.Fatalf("failed to get default connection: %v", err)
	}
	if retrievedDefault.ID != conn.ID {
		t.Errorf("got default connection ID %s, want %s", retrievedDefault.ID, conn.ID)
	}

	// 5. Test default override: create second connection with IsDefault = true
	conn2 := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Web Connection 2",
		Channel:        "whatsapp",
		SenderIdentity: "5511999990003",
		Status:         "pending",
		IsDefault:      true,
	}
	err = repo.Create(ctx, conn2)
	if err != nil {
		t.Fatalf("failed to create second default connection: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, conn2.ID)
	}()

	// Verify conn is no longer default, but conn2 is
	c1, _ := repo.GetByID(ctx, conn.ID)
	c2, _ := repo.GetByID(ctx, conn2.ID)
	if c1.IsDefault {
		t.Error("expected first connection to no longer be default")
	}
	if !c2.IsDefault {
		t.Error("expected second connection to be default")
	}

	// 6. Test ListByWorkspace
	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list connections by workspace: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 connections in workspace, got %d", len(list))
	}

	// 7. Test ListAll
	allConns, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("failed to list all connections: %v", err)
	}
	if len(allConns) < 2 {
		t.Errorf("expected at least 2 connections globally, got %d", len(allConns))
	}

	// 8. Test UpdateStatus & connected_since
	err = repo.UpdateStatus(ctx, conn.ID, "connected")
	if err != nil {
		t.Fatalf("failed to update status to connected: %v", err)
	}
	updatedConn, _ := repo.GetByID(ctx, conn.ID)
	if updatedConn.Status != "connected" {
		t.Errorf("status was not updated: got %q, want %q", updatedConn.Status, "connected")
	}
	if updatedConn.ConnectedSince == nil {
		t.Error("connected_since should have been populated upon connecting")
	}

	// WhatsApp Web terminal status lock test
	err = repo.UpdateStatus(ctx, conn.ID, "terminal")
	if err != nil {
		t.Fatalf("failed to update status to terminal: %v", err)
	}
	terminalConn, _ := repo.GetByID(ctx, conn.ID)
	if terminalConn.Status != "terminal" {
		t.Errorf("status was not updated: got %q, want %q", terminalConn.Status, "terminal")
	}

	// Disconnected update should be ignored on terminal
	err = repo.UpdateStatus(ctx, conn.ID, "disconnected")
	if err != nil {
		t.Fatalf("failed to update status to disconnected: %v", err)
	}
	ignoredConn, _ := repo.GetByID(ctx, conn.ID)
	if ignoredConn.Status != "terminal" {
		t.Errorf("expected status to remain terminal, got %q", ignoredConn.Status)
	}

	// Connected update should override terminal (re-pair)
	err = repo.UpdateStatus(ctx, conn.ID, "connected")
	if err != nil {
		t.Fatalf("failed to update status to connected: %v", err)
	}
	reconnectedConn, _ := repo.GetByID(ctx, conn.ID)
	if reconnectedConn.Status != "connected" {
		t.Errorf("expected status to override terminal to connected, got %q", reconnectedConn.Status)
	}

	// 9. Test SaveCredentials / GetCredentials
	newSecret := []byte("new-decrypted-secret-api-token")
	err = repo.SaveCredentials(ctx, conn.ID, newSecret)
	if err != nil {
		t.Fatalf("failed to save credentials: %v", err)
	}

	decryptedCreds, err := repo.GetCredentials(ctx, conn.ID)
	if err != nil {
		t.Fatalf("failed to get and decrypt credentials: %v", err)
	}
	if !bytes.Equal(decryptedCreds, newSecret) {
		t.Errorf("retrieved credentials mismatch: got %q, want %q", string(decryptedCreds), string(newSecret))
	}

	// 10. Test Delete
	err = repo.Delete(ctx, conn.ID)
	if err != nil {
		t.Fatalf("failed to delete connection: %v", err)
	}

	_, err = repo.GetByID(ctx, conn.ID)
	if !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Errorf("expected ErrConnectionNotFound on get after delete, got %v", err)
	}
}

func TestConnectionRepository_CountActiveByWorkspace(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up connections and workspaces
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	repo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "conn_count_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 1. Initially active connections count should be 0
	count, err := repo.CountActiveByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active connections: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 active connections, got %d", count)
	}

	// 2. Create connection with non-active status (e.g. 'pending')
	conn1 := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Pending Conn",
		Channel:        "telegram",
		SenderIdentity: "pending_sender",
		Status:         "pending",
	}
	err = repo.Create(ctx, conn1)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, conn1.ID)
	}()

	count, err = repo.CountActiveByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active connections: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 active connections with status 'pending', got %d", count)
	}

	// 3. Create connection with active status
	conn2 := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Active Conn",
		Channel:        "telegram",
		SenderIdentity: "active_sender",
		Status:         "active",
	}
	err = repo.Create(ctx, conn2)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, conn2.ID)
	}()

	count, err = repo.CountActiveByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active connections: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active connection, got %d", count)
	}

	// 4. Create connection with connected status
	conn3 := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Connected Conn",
		Channel:        "telegram",
		SenderIdentity: "connected_sender",
		Status:         "connected",
	}
	err = repo.Create(ctx, conn3)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, conn3.ID)
	}()

	count, err = repo.CountActiveByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active connections: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 active/connected connections, got %d", count)
	}

	// 5. Update one to disconnected
	err = repo.UpdateStatus(ctx, conn2.ID, "disconnected")
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	count, err = repo.CountActiveByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active connections: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active/connected connection after updating status to disconnected, got %d", count)
	}
}
