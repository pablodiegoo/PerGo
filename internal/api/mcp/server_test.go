package mcp

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mark3labs/mcp-go/mcp"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

var (
	testDBURL string
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Start PostgreSQL Container
	var err error
	var pgContainer *tcpostgres.PostgresContainer
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("testcontainers postgres panic: %v", r)
			}
		}()
		pgContainer, err = tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
		)
	}()

	if err != nil || pgContainer == nil {
		log.Printf("postgres testcontainer unavailable: %v; running tests with existing env", err)
		os.Exit(m.Run())
	}
	defer func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			log.Printf("failed to terminate postgres container: %v", err)
		}
	}()

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get postgres connection string: %v", err)
	}
	testDBURL = pgConnStr
	os.Setenv("PERGO_DATABASE_URL", pgConnStr)

	// Connect to pool with retries
	var pool *pgxpool.Pool
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.New(ctx, pgConnStr)
		if err == nil {
			err = pool.Ping(ctx)
			if err == nil {
				break
			}
			pool.Close()
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Fatalf("postgres failed to accept connections after retries: %v", err)
	}

	// Run migrations
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		log.Fatalf("failed to get sql.DB wrapper: %v", err)
	}
	if err := postgres.RunMigrations(db); err != nil {
		db.Close()
		pool.Close()
		log.Fatalf("failed to run migrations: %v", err)
	}
	db.Close()
	pool.Close()

	os.Exit(m.Run())
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := testDBURL
	if dsn == "" {
		dsn = os.Getenv("PERGO_DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

	return pool
}

type mockOutboundProcessor struct {
	mu          sync.Mutex
	lastRequest *domain.CreateMessageRequest
	lastTraceID string
	lastWSID    uuid.UUID
	IngestFunc  func(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error)
}

func (m *mockOutboundProcessor) Ingest(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error) {
	m.mu.Lock()
	m.lastWSID = workspaceID
	m.lastTraceID = traceID
	m.lastRequest = req
	m.mu.Unlock()

	if m.IngestFunc != nil {
		return m.IngestFunc(ctx, workspaceID, traceID, req)
	}
	return &domain.QueueMessage{
		WorkspaceID: workspaceID,
		To:          req.To,
		Channel:     req.Channel,
		Body:        req.Body,
		QueuedAt:    time.Now().UTC(),
	}, nil
}

type mockWhatsAppClient struct {
	jid  types.JID
	qrCh chan whatsmeow.QRChannelItem
	mu   sync.Mutex
}

func (m *mockWhatsAppClient) SetJID(jid types.JID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jid = jid
}

func (m *mockWhatsAppClient) JID() types.JID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jid
}

func (m *mockWhatsAppClient) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (m *mockWhatsAppClient) Connect() error {
	return nil
}

func (m *mockWhatsAppClient) Disconnect() {}

func (m *mockWhatsAppClient) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return m.qrCh, nil
}

type mockClientFactory struct {
	client *mockWhatsAppClient
}

func (f *mockClientFactory) CreateClient(cfg whatsapp.ClientConfig) (session.WhatsAppClientInterface, error) {
	return f.client, nil
}

func TestMCPServerTools(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Initialize repositories
	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	contactRepo := repository.NewContactRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "MCP Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	mockIngestor := &mockOutboundProcessor{}
	sessionRegistry := session.NewActiveSession()
	sessionManager := session.NewManager(nil, connRepo, sessionRegistry, nil, "2.3000.1025000000", nil)

	mockCli := &mockWhatsAppClient{
		qrCh: make(chan whatsmeow.QRChannelItem, 10),
	}
	sessionManager.SetClientFactory(&mockClientFactory{client: mockCli})

	srv := NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		auditRepo,
		mockIngestor,
		apiKeyRepo,
		webhookSubRepo,
		sessionManager,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
	)

	t.Run("CreateWorkspace_WithDefaults", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"name":                    "Provisioned Workspace Via MCP",
			"generate_api_key":        true,
			"generate_webhook_secret": true,
		}

		res, err := srv.handleCreateWorkspace(ctx, req)
		if err != nil {
			t.Fatalf("handleCreateWorkspace returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleCreateWorkspace returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var wsRes map[string]any
		if err := json.Unmarshal([]byte(text), &wsRes); err != nil {
			t.Fatalf("failed to unmarshal create workspace response: %v", err)
		}

		if wsRes["name"] != "Provisioned Workspace Via MCP" {
			t.Errorf("expected name 'Provisioned Workspace Via MCP', got %v", wsRes["name"])
		}
		if wsRes["api_key"] == nil || wsRes["api_key"] == "" {
			t.Errorf("expected generated api_key in response")
		}
		if wsRes["webhook_secret"] == nil || wsRes["webhook_secret"] == "" {
			t.Errorf("expected generated webhook_secret in response")
		}

		// Cleanup provisioned workspace
		if idStr, ok := wsRes["id"].(string); ok {
			if id, err := uuid.Parse(idStr); err == nil {
				defer func() { _ = wsRepo.Delete(ctx, id) }()
			}
		}
	})

	t.Run("CreateWorkspace_WithoutKeyAndSecret", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"name":                    "Bare Workspace",
			"generate_api_key":        false,
			"generate_webhook_secret": false,
		}

		res, err := srv.handleCreateWorkspace(ctx, req)
		if err != nil {
			t.Fatalf("handleCreateWorkspace returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleCreateWorkspace returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var wsRes map[string]any
		if err := json.Unmarshal([]byte(text), &wsRes); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if wsRes["name"] != "Bare Workspace" {
			t.Errorf("expected name 'Bare Workspace', got %v", wsRes["name"])
		}
		if wsRes["api_key"] != nil {
			t.Errorf("expected nil api_key, got %v", wsRes["api_key"])
		}
		if wsRes["webhook_secret"] != nil {
			t.Errorf("expected nil webhook_secret, got %v", wsRes["webhook_secret"])
		}

		if idStr, ok := wsRes["id"].(string); ok {
			if id, err := uuid.Parse(idStr); err == nil {
				defer func() { _ = wsRepo.Delete(ctx, id) }()
			}
		}
	})

	t.Run("GetWorkspace", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleGetWorkspace(ctx, req)
		if err != nil {
			t.Fatalf("handleGetWorkspace returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleGetWorkspace returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var wsData repository.Workspace
		if err := json.Unmarshal([]byte(text), &wsData); err != nil {
			t.Fatalf("failed to unmarshal workspace response: %v", err)
		}

		if wsData.ID != ws.ID {
			t.Errorf("expected workspace ID %s, got %s", ws.ID, wsData.ID)
		}
		if wsData.Name != ws.Name {
			t.Errorf("expected workspace name %s, got %s", ws.Name, wsData.Name)
		}
	})

	t.Run("ListWorkspaces", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		res, err := srv.handleListWorkspaces(ctx, req)
		if err != nil {
			t.Fatalf("handleListWorkspaces returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleListWorkspaces returned error result: %+v", res.Content)
		}

		var workspaces []repository.Workspace
		text := extractText(t, res)
		if err := json.Unmarshal([]byte(text), &workspaces); err != nil {
			t.Fatalf("failed to unmarshal workspaces: %v", err)
		}

		found := false
		for _, w := range workspaces {
			if w.ID == ws.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected workspace %s to be listed", ws.ID)
		}
	})

	t.Run("CreateConnection_Telegram", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"name":            "Support Bot",
			"channel":         "telegram",
			"sender_identity": "@support_bot",
			"credentials":     "bot123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			"is_default":      true,
		}

		res, err := srv.handleCreateConnection(ctx, req)
		if err != nil {
			t.Fatalf("handleCreateConnection returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleCreateConnection returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var connRes map[string]any
		if err := json.Unmarshal([]byte(text), &connRes); err != nil {
			t.Fatalf("failed to unmarshal create connection response: %v", err)
		}

		if connRes["channel"] != "telegram" {
			t.Errorf("expected channel 'telegram', got %v", connRes["channel"])
		}
		if connRes["sender_identity"] != "@support_bot" {
			t.Errorf("expected sender_identity '@support_bot', got %v", connRes["sender_identity"])
		}
		if connRes["is_default"] != true {
			t.Errorf("expected is_default true, got %v", connRes["is_default"])
		}

		connIDStr, ok := connRes["id"].(string)
		if !ok {
			t.Fatalf("expected id in create connection response")
		}
		connID, err := uuid.Parse(connIDStr)
		if err != nil {
			t.Fatalf("invalid connection ID returned: %v", err)
		}

		// Verify GetConnectionStatus
		statusReq := mcp.CallToolRequest{}
		statusReq.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": connID.String(),
		}

		statusRes, err := srv.handleGetConnectionStatus(ctx, statusReq)
		if err != nil {
			t.Fatalf("handleGetConnectionStatus returned error: %v", err)
		}
		if statusRes.IsError {
			t.Fatalf("handleGetConnectionStatus returned error result: %+v", statusRes.Content)
		}

		statusText := extractText(t, statusRes)
		var statusData map[string]any
		if err := json.Unmarshal([]byte(statusText), &statusData); err != nil {
			t.Fatalf("failed to unmarshal connection status response: %v", err)
		}
		if statusData["status"] != "active" {
			t.Errorf("expected status 'active', got %v", statusData["status"])
		}

		// Verify DisconnectConnection
		discReq := mcp.CallToolRequest{}
		discReq.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": connID.String(),
		}

		discRes, err := srv.handleDisconnectConnection(ctx, discReq)
		if err != nil {
			t.Fatalf("handleDisconnectConnection returned error: %v", err)
		}
		if discRes.IsError {
			t.Fatalf("handleDisconnectConnection returned error result: %+v", discRes.Content)
		}
	})

	t.Run("CreateConnection_WhatsAppCloud", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"name":            "WABA Official",
			"channel":         "whatsapp_cloud",
			"sender_identity": "+15551234567",
			"credentials":     `{"phone_number_id":"12345678","access_token":"EAAB..."}`,
			"is_default":      false,
		}

		res, err := srv.handleCreateConnection(ctx, req)
		if err != nil {
			t.Fatalf("handleCreateConnection returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleCreateConnection returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var connRes map[string]any
		if err := json.Unmarshal([]byte(text), &connRes); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if connRes["channel"] != "whatsapp_cloud" {
			t.Errorf("expected channel 'whatsapp_cloud', got %v", connRes["channel"])
		}
		if connRes["status"] != "active" {
			t.Errorf("expected status 'active', got %v", connRes["status"])
		}

		connIDStr, ok := connRes["id"].(string)
		if !ok {
			t.Fatalf("missing id in create connection response")
		}
		connID, _ := uuid.Parse(connIDStr)
		defer func() { _ = connRepo.Delete(ctx, connID) }()
	})

	t.Run("CreateConnection_WhatsApp_Pairing", func(t *testing.T) {
		phone := "5511988887777"
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"name":            "WhatsApp Web Conn",
			"channel":         "whatsapp",
			"sender_identity": phone,
			"proxy_url":       "http://proxy.local:8080",
		}

		res, err := srv.handleCreateConnection(ctx, req)
		if err != nil {
			t.Fatalf("handleCreateConnection returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleCreateConnection returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var connRes map[string]any
		if err := json.Unmarshal([]byte(text), &connRes); err != nil {
			t.Fatalf("failed to unmarshal create connection response: %v", err)
		}

		if connRes["status"] != "pairing_started" {
			t.Errorf("expected status 'pairing_started', got %v", connRes["status"])
		}
		connIDStr, ok := connRes["id"].(string)
		if !ok {
			t.Fatalf("missing id in response")
		}
		connID, _ := uuid.Parse(connIDStr)
		defer func() {
			sessionManager.CancelPairing(connID)
			_ = connRepo.Delete(ctx, connID)
		}()

		// Broadcast QR code via channel to simulate whatsmeow emitting QR
		mockCli.qrCh <- whatsmeow.QRChannelItem{
			Event: "code",
			Code:  "2@mockwhatsappqrcodecontent",
		}

		// Wait briefly for broadcaster goroutine
		time.Sleep(50 * time.Millisecond)

		// Test handleGetConnectionQRCode
		qrReq := mcp.CallToolRequest{}
		qrReq.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": connID.String(),
		}

		qrRes, err := srv.handleGetConnectionQRCode(ctx, qrReq)
		if err != nil {
			t.Fatalf("handleGetConnectionQRCode returned error: %v", err)
		}
		if qrRes.IsError {
			t.Fatalf("handleGetConnectionQRCode returned error result: %+v", qrRes.Content)
		}

		qrText := extractText(t, qrRes)
		var qrEvent session.QREvent
		if err := json.Unmarshal([]byte(qrText), &qrEvent); err != nil {
			t.Fatalf("failed to unmarshal qr event response: %v", err)
		}

		if qrEvent.Status != "qr_ready" && qrEvent.Status != "pending" {
			t.Errorf("expected qr_ready or pending status, got %s", qrEvent.Status)
		}
		if qrEvent.Code != "" && !strings.HasPrefix(qrEvent.QRDataURL, "data:image/png;base64,") {
			t.Errorf("expected QRDataURL to start with 'data:image/png;base64,', got %s", qrEvent.QRDataURL)
		}
	})

	t.Run("GetConnectionQRCode_NonWhatsApp", func(t *testing.T) {
		// Create a temporary telegram connection
		testConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Non-WA Conn",
			Slug:           "non-wa-conn",
			Channel:        "telegram",
			SenderIdentity: "@telegram_test_bot",
			Status:         "active",
		}
		if err := connRepo.Create(ctx, testConn); err != nil {
			t.Fatalf("failed to create test connection: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, testConn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": testConn.ID.String(),
		}

		res, err := srv.handleGetConnectionQRCode(ctx, req)
		if err != nil {
			t.Fatalf("handleGetConnectionQRCode returned unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error result when calling get_connection_qr_code on non-whatsapp connection")
		}
	})

	t.Run("ListConnections", func(t *testing.T) {
		// Create 2 connections
		conn1 := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Conn 1",
			Slug:           "conn-1",
			Channel:        "telegram",
			SenderIdentity: "@bot1",
			Status:         "active",
		}
		conn2 := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Conn 2",
			Slug:           "conn-2",
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+1234567890",
			Status:         "active",
		}
		_ = connRepo.Create(ctx, conn1)
		_ = connRepo.Create(ctx, conn2)
		defer func() {
			_ = connRepo.Delete(ctx, conn1.ID)
			_ = connRepo.Delete(ctx, conn2.ID)
		}()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleListConnections(ctx, req)
		if err != nil {
			t.Fatalf("handleListConnections returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleListConnections returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var connsList []map[string]any
		if err := json.Unmarshal([]byte(text), &connsList); err != nil {
			t.Fatalf("failed to unmarshal list connections: %v", err)
		}

		if len(connsList) < 2 {
			t.Errorf("expected at least 2 connections, got %d", len(connsList))
		}
	})

	t.Run("SearchContacts_WithResults", func(t *testing.T) {
		// Seed contacts in DB
		contactName := "Alice Smith"
		email := "alice@example.com"
		_, err := contactRepo.CreateContact(ctx, ws.ID, contactName, &email, nil)
		if err != nil {
			t.Fatalf("failed to seed contact: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"query":        "Alice",
			"limit":        5,
		}

		res, err := srv.handleSearchContacts(ctx, req)
		if err != nil {
			t.Fatalf("handleSearchContacts returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleSearchContacts returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var contacts []domain.Contact
		if err := json.Unmarshal([]byte(text), &contacts); err != nil {
			t.Fatalf("failed to unmarshal contacts: %v", err)
		}

		found := false
		for _, c := range contacts {
			if c.Name == contactName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected contact 'Alice Smith' in search results")
		}
	})

	t.Run("SendMessage_TextAndMediaWithFallbacks", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"to":           "+5511999990002",
			"channel":      "whatsapp",
			"body":         "Check out this document",
			"media": map[string]any{
				"media_url":  "https://cdn.example.com/invoice.pdf",
				"media_type": "document",
				"filename":   "invoice.pdf",
				"caption":    "August Statement",
			},
			"fallback_channels": []any{"whatsapp_cloud", "telegram"},
		}

		res, err := srv.handleSendMessage(ctx, req)
		if err != nil {
			t.Fatalf("handleSendMessage returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleSendMessage returned error result: %+v", res.Content)
		}

		var sendRes map[string]any
		text := extractText(t, res)
		if err := json.Unmarshal([]byte(text), &sendRes); err != nil {
			t.Fatalf("failed to unmarshal send message response: %v", err)
		}

		if sendRes["status"] != "queued" {
			t.Errorf("expected status 'queued', got %v", sendRes["status"])
		}
		if sendRes["trace_id"] == nil || !strings.HasPrefix(sendRes["trace_id"].(string), "mcp-") {
			t.Errorf("expected mcp- trace ID, got %v", sendRes["trace_id"])
		}

		// Verify that mockIngestor received full media and fallbacks
		mockIngestor.mu.Lock()
		lastReq := mockIngestor.lastRequest
		lastWS := mockIngestor.lastWSID
		mockIngestor.mu.Unlock()

		if lastWS != ws.ID {
			t.Errorf("expected ingestor workspaceID %s, got %s", ws.ID, lastWS)
		}
		if lastReq == nil {
			t.Fatalf("ingestor did not receive request")
		}
		if lastReq.Media == nil || lastReq.Media.Filename != "invoice.pdf" {
			t.Errorf("expected media filename 'invoice.pdf', got %+v", lastReq.Media)
		}
		if len(lastReq.FallbackChannels) != 2 || lastReq.FallbackChannels[0] != "whatsapp_cloud" {
			t.Errorf("expected 2 fallback channels, got %+v", lastReq.FallbackChannels)
		}
	})

	t.Run("GetAuditLogs_WithFilter", func(t *testing.T) {
		// Insert audit log entries directly
		traceID := "test-trace-" + uuid.New().String()
		_, _ = pool.Exec(ctx,
			`INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), ws.ID, traceID, "mcp_test_event", `{"action":"tested"}`, time.Now().UTC(),
		)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"event_type":   "mcp_test_event",
			"limit":        10,
		}

		res, err := srv.handleGetAuditLogs(ctx, req)
		if err != nil {
			t.Fatalf("handleGetAuditLogs returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleGetAuditLogs returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var logs []map[string]any
		if err := json.Unmarshal([]byte(text), &logs); err != nil {
			t.Fatalf("failed to unmarshal audit logs: %v", err)
		}

		found := false
		for _, l := range logs {
			if l["event_type"] == "mcp_test_event" && l["trace_id"] == traceID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected seeded audit log to be found in filtered audit logs")
		}
	})

	t.Run("WebhookSubscriptionLifecycle", func(t *testing.T) {
		// 1. Create Webhook Subscription (with SSRF-safe public mock URL)
		reqCreate := mcp.CallToolRequest{}
		reqCreate.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"url":          "https://example.com/webhook/endpoint",
			"events":       []string{"message.received", "connection.status"},
			"active":       true,
		}

		resCreate, err := srv.handleCreateWebhookSubscription(ctx, reqCreate)
		if err != nil {
			t.Fatalf("handleCreateWebhookSubscription returned error: %v", err)
		}
		if resCreate.IsError {
			t.Fatalf("handleCreateWebhookSubscription returned error result: %+v", resCreate.Content)
		}

		textCreate := extractText(t, resCreate)
		var subData map[string]any
		if err := json.Unmarshal([]byte(textCreate), &subData); err != nil {
			t.Fatalf("failed to unmarshal create webhook subscription result: %v", err)
		}

		if subData["url"] != "https://example.com/webhook/endpoint" {
			t.Errorf("expected url 'https://example.com/webhook/endpoint', got %v", subData["url"])
		}
		if subData["secret"] == nil || subData["secret"] == "" {
			t.Errorf("expected generated secret in create webhook subscription response")
		}

		subIDStr, ok := subData["id"].(string)
		if !ok {
			t.Fatalf("expected id in create subscription response")
		}
		subID, err := uuid.Parse(subIDStr)
		if err != nil {
			t.Fatalf("invalid subscription id: %v", err)
		}

		// 2. List Webhook Subscriptions
		reqList := mcp.CallToolRequest{}
		reqList.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		resList, err := srv.handleListWebhookSubscriptions(ctx, reqList)
		if err != nil {
			t.Fatalf("handleListWebhookSubscriptions returned error: %v", err)
		}
		if resList.IsError {
			t.Fatalf("handleListWebhookSubscriptions returned error result: %+v", resList.Content)
		}

		textList := extractText(t, resList)
		var subsList []map[string]any
		if err := json.Unmarshal([]byte(textList), &subsList); err != nil {
			t.Fatalf("failed to unmarshal list webhook subscriptions: %v", err)
		}

		found := false
		for _, s := range subsList {
			if s["id"] == subID.String() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected created subscription %s in list", subID)
		}

		// 3. Rotate Webhook Secret
		reqRotate := mcp.CallToolRequest{}
		reqRotate.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": subID.String(),
			"new_secret":      "super-secret-rotated-key-12345678",
		}

		resRotate, err := srv.handleRotateWebhookSecret(ctx, reqRotate)
		if err != nil {
			t.Fatalf("handleRotateWebhookSecret returned error: %v", err)
		}
		if resRotate.IsError {
			t.Fatalf("handleRotateWebhookSecret returned error result: %+v", resRotate.Content)
		}

		textRotate := extractText(t, resRotate)
		var rotateData map[string]any
		if err := json.Unmarshal([]byte(textRotate), &rotateData); err != nil {
			t.Fatalf("failed to unmarshal rotate secret result: %v", err)
		}
		if rotateData["secret"] != "super-secret-rotated-key-12345678" {
			t.Errorf("expected rotated secret 'super-secret-rotated-key-12345678', got %v", rotateData["secret"])
		}

		// 4. Test Ping Webhook (against local httptest server)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-PerGo-Signature")
			if sig == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.Header.Get("X-PerGo-Simulated") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"received":true}`))
		}))
		defer ts.Close()

		// Update sub URL directly in repo to point to test server
		_ = webhookSubRepo.Update(ctx, subID, ts.URL, []string{"*"}, true, []byte("super-secret-rotated-key-12345678"))

		reqPing := mcp.CallToolRequest{}
		reqPing.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": subID.String(),
		}

		resPing, err := srv.handleTestPingWebhook(ctx, reqPing)
		if err != nil {
			t.Fatalf("handleTestPingWebhook returned error: %v", err)
		}
		if resPing.IsError {
			t.Fatalf("handleTestPingWebhook returned error result: %+v", resPing.Content)
		}

		textPing := extractText(t, resPing)
		var pingData map[string]any
		if err := json.Unmarshal([]byte(textPing), &pingData); err != nil {
			t.Fatalf("failed to unmarshal ping response: %v", err)
		}
		if pingData["status_code"] != float64(200) {
			t.Errorf("expected ping status 200, got %v", pingData["status_code"])
		}
		if pingData["success"] != true {
			t.Errorf("expected ping success true, got %v", pingData["success"])
		}

		// 5. Delete Webhook Subscription
		reqDelete := mcp.CallToolRequest{}
		reqDelete.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": subID.String(),
		}

		resDelete, err := srv.handleDeleteWebhookSubscription(ctx, reqDelete)
		if err != nil {
			t.Fatalf("handleDeleteWebhookSubscription returned error: %v", err)
		}
		if resDelete.IsError {
			t.Fatalf("handleDeleteWebhookSubscription returned error result: %+v", resDelete.Content)
		}
	})

	t.Run("GenerateAdminSSOURL", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"subject":      "admin@partner.com",
			"redirect":     "/admin/webhooks",
			"ttl_seconds":  45,
		}

		res, err := srv.handleGenerateAdminSSOURL(ctx, req)
		if err != nil {
			t.Fatalf("handleGenerateAdminSSOURL returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleGenerateAdminSSOURL returned error result: %+v", res.Content)
		}

		text := extractText(t, res)
		var ssoData map[string]any
		if err := json.Unmarshal([]byte(text), &ssoData); err != nil {
			t.Fatalf("failed to unmarshal sso result: %v", err)
		}

		if ssoData["subject"] != "admin@partner.com" {
			t.Errorf("expected subject 'admin@partner.com', got %v", ssoData["subject"])
		}
		if ssoData["redirect"] != "/admin/webhooks" {
			t.Errorf("expected redirect '/admin/webhooks', got %v", ssoData["redirect"])
		}
		if ssoData["ttl_seconds"] != float64(45) {
			t.Errorf("expected ttl_seconds 45, got %v", ssoData["ttl_seconds"])
		}

		token, ok := ssoData["token"].(string)
		if !ok || token == "" {
			t.Fatalf("expected non-empty token")
		}

		// Verify token claims
		claims, err := admin.VerifySSOToken(token, []byte("test-sso-secret"))
		if err != nil {
			t.Fatalf("failed to verify sso token: %v", err)
		}
		if claims.WorkspaceID != ws.ID.String() {
			t.Errorf("expected workspace ID %s in token claims, got %s", ws.ID, claims.WorkspaceID)
		}
		if claims.Sub != "admin@partner.com" {
			t.Errorf("expected sub 'admin@partner.com' in claims, got %s", claims.Sub)
		}

		ssoURL, ok := ssoData["sso_url"].(string)
		if !ok || !strings.Contains(ssoURL, "/admin/sso?token=") {
			t.Errorf("expected valid sso_url format, got %s", ssoURL)
		}
	})

	t.Run("SSEServerTransport", func(t *testing.T) {
		if srv.SSEServer == nil {
			t.Fatalf("expected SSEServer to be initialized on Server")
		}

		mux := http.NewServeMux()
		mux.Handle("/api/mcp/", srv.SSEServer)

		ts := httptest.NewServer(mux)
		defer ts.Close()

		// Send request to SSE endpoint
		client := &http.Client{Timeout: 500 * time.Millisecond}
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/mcp/sse", nil)
		if err != nil {
			t.Fatalf("failed to create SSE request: %v", err)
		}
		req.Header.Set("Accept", "text/event-stream")

		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected HTTP 200 for SSE endpoint, got %d", resp.StatusCode)
			}
			if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
				t.Errorf("expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
			}
		}
	})

	t.Run("ValidationErrors", func(t *testing.T) {
		testCases := []struct {
			name    string
			handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
			args    map[string]any
		}{
			{
				name:    "get_workspace_missing_id",
				handler: srv.handleGetWorkspace,
				args:    map[string]any{},
			},
			{
				name:    "get_workspace_invalid_uuid",
				handler: srv.handleGetWorkspace,
				args:    map[string]any{"workspace_id": "invalid-uuid"},
			},
			{
				name:    "create_workspace_empty_name",
				handler: srv.handleCreateWorkspace,
				args:    map[string]any{"name": "   "},
			},
			{
				name:    "create_connection_missing_ws",
				handler: srv.handleCreateConnection,
				args:    map[string]any{"name": "Test", "channel": "telegram"},
			},
			{
				name:    "create_connection_invalid_channel",
				handler: srv.handleCreateConnection,
				args:    map[string]any{"workspace_id": ws.ID.String(), "name": "Test", "channel": "invalid_channel"},
			},
			{
				name:    "create_connection_whatsapp_missing_phone",
				handler: srv.handleCreateConnection,
				args:    map[string]any{"workspace_id": ws.ID.String(), "name": "WA", "channel": "whatsapp", "sender_identity": ""},
			},
			{
				name:    "get_connection_status_missing_conn_id",
				handler: srv.handleGetConnectionStatus,
				args:    map[string]any{"workspace_id": ws.ID.String()},
			},
			{
				name:    "get_connection_status_not_found",
				handler: srv.handleGetConnectionStatus,
				args:    map[string]any{"workspace_id": ws.ID.String(), "connection_id": uuid.New().String()},
			},
			{
				name:    "get_connection_qr_code_not_found",
				handler: srv.handleGetConnectionQRCode,
				args:    map[string]any{"workspace_id": ws.ID.String(), "connection_id": uuid.New().String()},
			},
			{
				name:    "disconnect_connection_not_found",
				handler: srv.handleDisconnectConnection,
				args:    map[string]any{"workspace_id": ws.ID.String(), "connection_id": uuid.New().String()},
			},
			{
				name:    "list_connections_invalid_ws",
				handler: srv.handleListConnections,
				args:    map[string]any{"workspace_id": "not-a-uuid"},
			},
			{
				name:    "search_contacts_invalid_ws",
				handler: srv.handleSearchContacts,
				args:    map[string]any{"workspace_id": "not-a-uuid"},
			},
			{
				name:    "send_message_missing_to",
				handler: srv.handleSendMessage,
				args:    map[string]any{"workspace_id": ws.ID.String(), "channel": "telegram", "body": "hello"},
			},
			{
				name:    "send_message_missing_channel",
				handler: srv.handleSendMessage,
				args:    map[string]any{"workspace_id": ws.ID.String(), "to": "+123", "body": "hello"},
			},
			{
				name:    "send_message_missing_body",
				handler: srv.handleSendMessage,
				args:    map[string]any{"workspace_id": ws.ID.String(), "to": "+123", "channel": "telegram"},
			},
			{
				name:    "get_audit_logs_invalid_ws",
				handler: srv.handleGetAuditLogs,
				args:    map[string]any{"workspace_id": "not-a-uuid"},
			},
			{
				name:    "create_webhook_subscription_ssrf_loopback",
				handler: srv.handleCreateWebhookSubscription,
				args:    map[string]any{"workspace_id": ws.ID.String(), "url": "http://127.0.0.1:8080/hook"},
			},
			{
				name:    "create_webhook_subscription_ssrf_private",
				handler: srv.handleCreateWebhookSubscription,
				args:    map[string]any{"workspace_id": ws.ID.String(), "url": "http://192.168.1.10/hook"},
			},
			{
				name:    "list_webhook_subscriptions_invalid_ws",
				handler: srv.handleListWebhookSubscriptions,
				args:    map[string]any{"workspace_id": "not-a-uuid"},
			},
			{
				name:    "delete_webhook_subscription_not_found",
				handler: srv.handleDeleteWebhookSubscription,
				args:    map[string]any{"workspace_id": ws.ID.String(), "subscription_id": uuid.New().String()},
			},
			{
				name:    "rotate_webhook_secret_not_found",
				handler: srv.handleRotateWebhookSecret,
				args:    map[string]any{"workspace_id": ws.ID.String(), "subscription_id": uuid.New().String()},
			},
			{
				name:    "test_ping_webhook_not_found",
				handler: srv.handleTestPingWebhook,
				args:    map[string]any{"workspace_id": ws.ID.String(), "subscription_id": uuid.New().String()},
			},
			{
				name:    "generate_admin_sso_url_invalid_ws",
				handler: srv.handleGenerateAdminSSOURL,
				args:    map[string]any{"workspace_id": "not-a-uuid"},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := mcp.CallToolRequest{}
				req.Params.Arguments = tc.args
				res, err := tc.handler(ctx, req)
				if err != nil {
					t.Fatalf("handler returned unexpected error: %v", err)
				}
				if !res.IsError {
					t.Errorf("expected error result for test %s", tc.name)
				}
			})
		}
	})
}

func extractText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) > 0 {
		if tc, ok := mcp.AsTextContent(res.Content[0]); ok {
			return tc.Text
		}
	}
	t.Fatalf("expected text content in tool result")
	return ""
}
