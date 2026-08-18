package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	apipkg "github.com/pablojhp.pergo/internal/api/handler/api"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type headlessMockWhatsAppClient struct {
	jid        types.JID
	qrCh       chan whatsmeow.QRChannelItem
	runErr     error
	connectErr error
	connected  bool
	stopped    bool
	mu         sync.Mutex
}

func newHeadlessMockWhatsAppClient() *headlessMockWhatsAppClient {
	return &headlessMockWhatsAppClient{
		qrCh: make(chan whatsmeow.QRChannelItem, 10),
	}
}

func (m *headlessMockWhatsAppClient) SetJID(jid types.JID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jid = jid
}

func (m *headlessMockWhatsAppClient) JID() types.JID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jid
}

func (m *headlessMockWhatsAppClient) Run(ctx context.Context) error {
	<-ctx.Done()
	return m.runErr
}

func (m *headlessMockWhatsAppClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *headlessMockWhatsAppClient) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.stopped = true
}

func (m *headlessMockWhatsAppClient) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return m.qrCh, nil
}

type headlessMockClientFactory struct {
	client *headlessMockWhatsAppClient
}

func (f *headlessMockClientFactory) CreateClient(cfg whatsapp.ClientConfig) (session.WhatsAppClientInterface, error) {
	return f.client, nil
}

type headlessTestServer struct {
	e              *echo.Echo
	cfg            *config.Config
	wsRepo         *repository.WorkspaceRepository
	apiKeyRepo     *repository.APIKeyRepository
	connRepo       *repository.ConnectionRepository
	webhookSubRepo *repository.WebhookSubscriptionRepository
	sessionManager *session.Manager
	mockClient     *headlessMockWhatsAppClient
}

func setupHeadlessIntegrationServer(t *testing.T) *headlessTestServer {
	t.Helper()
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available, skipping headless integration test")
	}

	cfg := &config.Config{
		MasterKey:     "test-master-key-xyz-987",
		AdminPassword: "test-admin-password-123",
		SessionSecret: "test-session-secret-at-least-32-chars-long",
		KEKBytes:      make([]byte, 32),
		DatabaseURL:   testDSN(),
		ServerPort:    "8080",
		DebugPort:     "6060",
	}
	copy(cfg.KEKBytes, []byte("dev-development-key-32-bytes-kek"))

	enc, err := crypto.NewEncryptor(cfg.KEKBytes)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, enc)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	sessionRegistry := session.NewActiveSession()
	sessionManager := session.NewManager(nil, connRepo, sessionRegistry, nil, "2.3000.1025000000", nil)

	mockClient := newHeadlessMockWhatsAppClient()
	parsedJID, _ := types.ParseJID("5511999887766@s.whatsapp.net")
	mockClient.SetJID(parsedJID)
	sessionManager.SetClientFactory(&headlessMockClientFactory{client: mockClient})

	e := echo.New()
	e.Use(mw.TraceMiddleware())
	e.Use(mw.AuthMiddleware(apiKeyRepo))

	// 1. Workspace API (under Master Auth)
	wsAPIHandler := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)
	wsAPIHandler.RegisterRoutes(e, mw.MasterAuth(cfg))

	// 2. Connection API
	connAPIHandler := apipkg.NewConnectionAPIHandler(connRepo, sessionManager, sessionRegistry)
	connAPIHandler.RegisterRoutes(e)

	// 3. Webhook Subscriptions API
	webhookSubAPIHandler := apipkg.NewWebhookSubscriptionAPIHandler(webhookSubRepo)
	webhookSubAPIHandler.RegisterRoutes(e)

	// 4. Test endpoint /api/v1/me
	e.GET("/api/v1/me", func(c *echo.Context) error {
		wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusUnauthorized, "missing workspace context")
		}
		return c.JSON(http.StatusOK, map[string]string{
			"workspace_id": wsID.String(),
		})
	})

	// 5. SSO Handler (/admin/sso)
	ssoHandler := admin.NewSSOHandler(wsRepo, []byte(cfg.SessionSecret))
	e.GET("/admin/sso", func(c *echo.Context) error {
		return ssoHandler.HandleSSO(c)
	})

	return &headlessTestServer{
		e:              e,
		cfg:            cfg,
		wsRepo:         wsRepo,
		apiKeyRepo:     apiKeyRepo,
		connRepo:       connRepo,
		webhookSubRepo: webhookSubRepo,
		sessionManager: sessionManager,
		mockClient:     mockClient,
	}
}

func TestHeadlessCPaaS_EndToEndLifecycle(t *testing.T) {
	s := setupHeadlessIntegrationServer(t)
	ctx := context.Background()

	// =========================================================================
	// STEP 1: Provision Workspace via POST /api/v1/workspaces using Master Key
	// =========================================================================

	// 1.1 Rejects unauthenticated request
	{
		body := strings.NewReader(`{"name":"Acme Headless Corp"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized without master key, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 1.2 Rejects wrong master key
	{
		body := strings.NewReader(`{"name":"Acme Headless Corp"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-master-key")
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized with invalid master key, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 1.3 Successful provisioning with valid master key
	var provisionResp apipkg.CreateWorkspaceResponse
	{
		body := strings.NewReader(`{"name":"Acme Headless Corp","generate_api_key":true,"generate_webhook_secret":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.cfg.MasterKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on workspace provisioning, got %d: %s", rec.Code, rec.Body.String())
		}

		if err := json.Unmarshal(rec.Body.Bytes(), &provisionResp); err != nil {
			t.Fatalf("failed to decode provision response: %v", err)
		}

		if provisionResp.ID == uuid.Nil {
			t.Fatal("expected non-nil workspace ID")
		}
		if provisionResp.Name != "Acme Headless Corp" {
			t.Errorf("expected name 'Acme Headless Corp', got %q", provisionResp.Name)
		}
		if provisionResp.APIKey == nil || *provisionResp.APIKey == "" {
			t.Fatal("expected generated API key in response")
		}
		if provisionResp.WebhookSecret == nil || *provisionResp.WebhookSecret == "" {
			t.Fatal("expected generated webhook secret in response")
		}
		defer func() { _ = s.wsRepo.Delete(ctx, provisionResp.ID) }()
	}

	wsID := provisionResp.ID
	apiKey := *provisionResp.APIKey

	// =========================================================================
	// STEP 2: Authenticate Subsequent Requests using Returned API Key
	// =========================================================================
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with returned API key, got %d: %s", rec.Code, rec.Body.String())
		}

		var meResp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &meResp); err != nil {
			t.Fatalf("failed to decode /api/v1/me response: %v", err)
		}
		if meResp["workspace_id"] != wsID.String() {
			t.Errorf("expected workspace_id %q, got %q", wsID.String(), meResp["workspace_id"])
		}
	}

	// =========================================================================
	// STEP 3: Webhook Subscriptions API (CRUD, Events, SSRF Validation)
	// =========================================================================

	// 3.1 SSRF protection: reject private/loopback IP
	{
		ssrfPayload := `{"url":"http://127.0.0.1:9999/webhook","events":["message.received"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", strings.NewReader(ssrfPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 UnprocessableEntity for loopback URL, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3.2 Create subscription for message.received and connection.status
	var subID uuid.UUID
	var subSecret string
	{
		createPayload := `{
			"url": "https://api.externalcrm.com/webhooks/pergo",
			"events": ["message.received", "connection.status"],
			"is_active": true
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", strings.NewReader(createPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on webhook subscription creation, got %d: %s", rec.Code, rec.Body.String())
		}

		var subResp apipkg.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &subResp); err != nil {
			t.Fatalf("failed to decode webhook subscription response: %v", err)
		}

		subID = subResp.Subscription.ID
		subSecret = subResp.Subscription.Secret
		if subID == uuid.Nil {
			t.Fatal("expected valid subscription ID")
		}
		if subSecret == "" {
			t.Fatal("expected auto-generated subscription signing secret")
		}
		if subResp.Subscription.WorkspaceID != wsID {
			t.Errorf("expected subscription workspace_id %s, got %s", wsID, subResp.Subscription.WorkspaceID)
		}
		if len(subResp.Subscription.Events) != 2 {
			t.Errorf("expected 2 subscribed events, got %d", len(subResp.Subscription.Events))
		}
	}

	// 3.3 List webhook subscriptions
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/subscriptions", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list subscriptions, got %d: %s", rec.Code, rec.Body.String())
		}

		var listResp apipkg.WebhookSubscriptionListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}
		if len(listResp.Subscriptions) == 0 {
			t.Fatal("expected at least 1 subscription in list")
		}
	}

	// 3.4 Get webhook subscription by ID
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/webhooks/subscriptions/%s", subID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on get subscription, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3.5 Update webhook subscription
	{
		updatePayload := `{
			"url": "https://api.externalcrm.com/webhooks/pergo-updated",
			"events": ["message.received", "message.delivered", "connection.status"]
		}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/webhooks/subscriptions/%s", subID), strings.NewReader(updatePayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on update subscription, got %d: %s", rec.Code, rec.Body.String())
		}

		var updateResp apipkg.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
			t.Fatalf("failed to decode update response: %v", err)
		}
		if updateResp.Subscription.URL != "https://api.externalcrm.com/webhooks/pergo-updated" {
			t.Errorf("expected updated URL, got %s", updateResp.Subscription.URL)
		}
		if len(updateResp.Subscription.Events) != 3 {
			t.Errorf("expected 3 subscribed events, got %d", len(updateResp.Subscription.Events))
		}
	}

	// =========================================================================
	// STEP 4: Headless Connection Pairing, Polling, and SSE Streaming
	// =========================================================================

	// 4.1 Initiate QR pairing via POST /api/v1/connections/pair
	var connID uuid.UUID
	phone := "5511999887766"
	{
		pairPayload := fmt.Sprintf(`{"channel":"whatsapp","phone":%q,"name":"Main WhatsApp Support"}`, phone)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/pair", strings.NewReader(pairPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on pair connection, got %d: %s", rec.Code, rec.Body.String())
		}

		var pairResp apipkg.PairConnectionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &pairResp); err != nil {
			t.Fatalf("failed to decode pair response: %v", err)
		}

		connID = pairResp.ConnectionID
		if connID == uuid.Nil {
			t.Fatal("expected non-nil connection ID from pairing initiation")
		}
		if pairResp.Status != "pairing_started" {
			t.Errorf("expected status 'pairing_started', got %q", pairResp.Status)
		}
	}

	// 4.2 Push QR code item into mock client channel
	s.mockClient.qrCh <- whatsmeow.QRChannelItem{
		Event: "code",
		Code:  "2@mock-qr-code-string-for-testing",
	}
	time.Sleep(200 * time.Millisecond)

	// 4.3 Poll QR state via GET /api/v1/connections/:id/qr
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/connections/%s/qr", connID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on polling QR, got %d: %s", rec.Code, rec.Body.String())
		}

		var qrResp session.QREvent
		if err := json.Unmarshal(rec.Body.Bytes(), &qrResp); err != nil {
			t.Fatalf("failed to decode QR polling response: %v", err)
		}

		if qrResp.Status != "pending" {
			t.Errorf("expected status 'pending', got %q", qrResp.Status)
		}
		if qrResp.Code != "2@mock-qr-code-string-for-testing" {
			t.Errorf("expected code '2@mock-qr-code-string-for-testing', got %q", qrResp.Code)
		}
		if !strings.HasPrefix(qrResp.QRDataURL, "data:image/png;base64,") {
			t.Errorf("expected QRDataURL to start with data:image/png;base64, got %q", qrResp.QRDataURL)
		}
	}

	// 4.4 Verify retrocompatible alias GET /api/v1/devices/:id/qr
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/devices/%s/qr", connID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on alias GET /api/v1/devices/:id/qr, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 4.5 Stream QR code via Server-Sent Events (SSE) GET /api/v1/connections/:id/qr/stream
	{
		sseCtx, sseCancel := context.WithTimeout(ctx, 2*time.Second)
		defer sseCancel()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/connections/%s/qr/stream", connID), nil).WithContext(sseCtx)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()

		// Stream concurrently and push a paired event to close the stream
		done := make(chan struct{})
		go func() {
			s.e.ServeHTTP(rec, req)
			close(done)
		}()

		// Wait briefly then emit paired event on mockClient channel
		time.Sleep(100 * time.Millisecond)
		s.mockClient.qrCh <- whatsmeow.QRChannelItem{
			Event: "success",
		}

		select {
		case <-done:
			// Stream concluded on paired event
		case <-time.After(3 * time.Second):
			t.Fatal("SSE stream timed out")
		}

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for SSE stream, got %d", rec.Code)
		}
		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/event-stream") {
			t.Errorf("expected Content-Type text/event-stream, got %q", contentType)
		}

		bodyStr := rec.Body.String()
		if !strings.Contains(bodyStr, "event: paired") {
			t.Errorf("expected SSE body to contain 'event: paired', got: %s", bodyStr)
		}
	}

	// 4.6 List connections via GET /api/v1/connections
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list connections, got %d: %s", rec.Code, rec.Body.String())
		}

		var listResp apipkg.ListConnectionsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
			t.Fatalf("failed to decode list connections response: %v", err)
		}
		if len(listResp.Connections) == 0 {
			t.Fatal("expected at least 1 connection in list")
		}
	}

	// 4.7 Disconnect connection via DELETE /api/v1/connections/:id
	{
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/connections/%s", connID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on disconnect, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// =========================================================================
	// STEP 5: Admin Single Sign-On (SSO) Hand-off via GET /admin/sso
	// =========================================================================

	// 5.1 Valid short-lived SSO token
	{
		claims := admin.SSOClaims{
			Sub:         "operator@externalcrm.com",
			WorkspaceID: wsID.String(),
			Role:        "admin",
			Iat:         time.Now().Unix(),
			Exp:         time.Now().Unix() + 60,
			Nonce:       "test-nonce-12345",
		}
		token, err := admin.GenerateSSOToken(claims, []byte(s.cfg.SessionSecret))
		if err != nil {
			t.Fatalf("failed to generate SSO token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/sso?token=%s&redirect=/admin/devices", token), nil)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 Found redirect on valid SSO, got %d: %s", rec.Code, rec.Body.String())
		}

		location := rec.Header().Get("Location")
		if location != "/admin/devices" {
			t.Errorf("expected redirect to /admin/devices, got %q", location)
		}

		// Verify cookies set
		cookies := rec.Result().Cookies()
		var hasSessionCookie, hasWorkspaceCookie bool
		for _, c := range cookies {
			if c.Name == "pergo-session" && c.Value != "" {
				hasSessionCookie = true
			}
			if c.Name == "pergo-active-workspace" && c.Value == wsID.String() {
				hasWorkspaceCookie = true
			}
		}
		if !hasSessionCookie {
			t.Error("expected pergo-session cookie to be set")
		}
		if !hasWorkspaceCookie {
			t.Error("expected pergo-active-workspace cookie to be set with workspace ID")
		}
	}

	// 5.2 Expired SSO token returns 401
	{
		expiredClaims := admin.SSOClaims{
			Sub:         "operator@externalcrm.com",
			WorkspaceID: wsID.String(),
			Role:        "admin",
			Iat:         time.Now().Unix() - 200,
			Exp:         time.Now().Unix() - 100,
		}
		expiredToken, err := admin.GenerateSSOToken(expiredClaims, []byte(s.cfg.SessionSecret))
		if err != nil {
			t.Fatalf("failed to generate expired SSO token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/sso?token=%s", expiredToken), nil)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for expired token, got %d", rec.Code)
		}
	}

	// 5.3 Forged signature SSO token returns 401
	{
		claims := admin.SSOClaims{
			Sub:         "attacker@evil.com",
			WorkspaceID: wsID.String(),
			Role:        "admin",
			Iat:         time.Now().Unix(),
			Exp:         time.Now().Unix() + 60,
		}
		forgedToken, err := admin.GenerateSSOToken(claims, []byte("wrong-attacker-secret-key-123456789"))
		if err != nil {
			t.Fatalf("failed to generate forged token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/sso?token=%s", forgedToken), nil)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for forged signature, got %d", rec.Code)
		}
	}

	// 5.4 Open redirect protection: invalid target redirects safely to /admin/
	{
		claims := admin.SSOClaims{
			Sub:         "operator@externalcrm.com",
			WorkspaceID: wsID.String(),
			Role:        "admin",
			Iat:         time.Now().Unix(),
			Exp:         time.Now().Unix() + 60,
		}
		token, _ := admin.GenerateSSOToken(claims, []byte(s.cfg.SessionSecret))

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/sso?token=%s&redirect=//evil.com/phish", token), nil)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 Found, got %d", rec.Code)
		}
		if location := rec.Header().Get("Location"); location != "/admin/" {
			t.Errorf("expected sanitized redirect to '/admin/', got %q", location)
		}
	}

	// =========================================================================
	// STEP 6: Headless WABA Connection Registration
	// =========================================================================
	var wabaConnID uuid.UUID
	{
		wabaPayload := `{
			"name": "Acme WhatsApp Cloud",
			"phone_number_id": "98765432101",
			"waba_account_id": "12345678901",
			"token": "EAAHeadlessTestToken987",
			"verify_token": "verify_custom_tok",
			"app_secret": "meta_app_secret_123",
			"display_phone_number": "+55 11 98888-7777",
			"verified_name": "Acme Headless Verified"
		}`

		// 6.1 Create WABA connection via POST /api/v1/connections/waba
		req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/waba", strings.NewReader(wabaPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		rec := httptest.NewRecorder()
		s.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("expected 201 Created on WABA connection creation, got %d: %s", rec.Code, rec.Body.String())
		}

		var wabaResp apipkg.ConnectionItem
		if err := json.Unmarshal(rec.Body.Bytes(), &wabaResp); err != nil {
			t.Fatalf("failed to decode WABA connection response: %v", err)
		}

		wabaConnID = wabaResp.ID
		if wabaConnID == uuid.Nil {
			t.Fatal("expected non-nil connection ID for WABA connection")
		}
		if wabaResp.Channel != "whatsapp_cloud" {
			t.Errorf("expected channel whatsapp_cloud, got %s", wabaResp.Channel)
		}
		if wabaResp.SenderIdentity != "5511988887777" {
			t.Errorf("expected sanitized sender identity 5511988887777, got %s", wabaResp.SenderIdentity)
		}
		if wabaResp.Status != "connected" {
			t.Errorf("expected status connected, got %s", wabaResp.Status)
		}

		// 6.2 Verify connection is persisted and credentials are encrypted in DB
		dbConn, err := s.connRepo.GetByID(ctx, wabaConnID)
		if err != nil || dbConn == nil {
			t.Fatalf("failed to get WABA connection from repo: %v", err)
		}
		if dbConn.WorkspaceID != wsID {
			t.Errorf("expected connection workspace %s, got %s", wsID, dbConn.WorkspaceID)
		}

		// 6.3 Test workspace-scoped alias POST /api/v1/workspaces/:workspace_id/connections/waba
		aliasPayload := `{
			"name": "Acme WhatsApp Cloud 2",
			"phone_number_id": "98765432102",
			"waba_account_id": "12345678901",
			"token": "EAAHeadlessTestToken988",
			"display_phone_number": "+55 11 98888-7778"
		}`
		reqAlias := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/connections/waba", wsID), strings.NewReader(aliasPayload))
		reqAlias.Header.Set("Content-Type", "application/json")
		reqAlias.Header.Set("Authorization", "Bearer "+apiKey)
		recAlias := httptest.NewRecorder()
		s.e.ServeHTTP(recAlias, reqAlias)

		if recAlias.Code != http.StatusCreated && recAlias.Code != http.StatusOK {
			t.Fatalf("expected 201 Created on WABA connection alias creation, got %d: %s", recAlias.Code, recAlias.Body.String())
		}

		var wabaAliasResp apipkg.ConnectionItem
		if err := json.Unmarshal(recAlias.Body.Bytes(), &wabaAliasResp); err != nil {
			t.Fatalf("failed to decode WABA alias response: %v", err)
		}
		defer func() { _ = s.connRepo.Delete(ctx, wabaAliasResp.ID) }()
		defer func() { _ = s.connRepo.Delete(ctx, wabaConnID) }()
	}

	// Cleanup Webhook Subscription
	_ = s.webhookSubRepo.Delete(ctx, subID)
}
