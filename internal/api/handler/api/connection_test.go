package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/api"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type mockConnectionRepo struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]*repository.Connection
}

func newMockConnectionRepo() *mockConnectionRepo {
	return &mockConnectionRepo{
		connections: make(map[uuid.UUID]*repository.Connection),
	}
}

func (m *mockConnectionRepo) GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.connections[id]
	if !ok {
		return nil, repository.ErrConnectionNotFound
	}
	return conn, nil
}

func (m *mockConnectionRepo) GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, conn := range m.connections {
		if conn.WorkspaceID == workspaceID && conn.Slug == slug {
			return conn, nil
		}
	}
	return nil, repository.ErrConnectionNotFound
}

func (m *mockConnectionRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*repository.Connection
	for _, conn := range m.connections {
		if conn.WorkspaceID == workspaceID {
			list = append(list, conn)
		}
	}
	return list, nil
}

func (m *mockConnectionRepo) Create(ctx context.Context, c *repository.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	m.connections[c.ID] = c
	return nil
}

func (m *mockConnectionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.connections, id)
	return nil
}

type mockWABAMetaClient struct {
	mu              sync.Mutex
	syncCalled      bool
	detailsToReturn *client.WABAPhoneNumberDetails
	detailsErr      error
	errOnSync       error
}

func (m *mockWABAMetaClient) FetchPhoneNumberDetails(ctx context.Context, phoneNumberID, token string) (*client.WABAPhoneNumberDetails, error) {
	if m.detailsErr != nil {
		return nil, m.detailsErr
	}
	if m.detailsToReturn != nil {
		return m.detailsToReturn, nil
	}
	return nil, errors.New("not found")
}

func (m *mockWABAMetaClient) SyncTemplates(ctx context.Context, connectionID uuid.UUID, wabaAccountID, token string, workspaceID uuid.UUID, repo *repository.WABATemplateRepository, bypassRateLimit bool) ([]repository.WABATemplate, error) {
	m.mu.Lock()
	m.syncCalled = true
	m.mu.Unlock()
	if m.errOnSync != nil {
		return nil, m.errOnSync
	}
	return []repository.WABATemplate{}, nil
}

type mockTelegramBotClient struct {
	mu            sync.Mutex
	validUsername string
	validateErr   error
	webhookErr    error
	webhookCalled bool
	registeredURL string
}

func (m *mockTelegramBotClient) ValidateToken(ctx context.Context, token string) (string, error) {
	if m.validateErr != nil {
		return "", m.validateErr
	}
	if m.validUsername != "" {
		return m.validUsername, nil
	}
	return "@test_bot", nil
}

func (m *mockTelegramBotClient) RegisterWebhook(ctx context.Context, token, webhookURL, secretToken string) error {
	m.mu.Lock()
	m.webhookCalled = true
	m.registeredURL = webhookURL
	m.mu.Unlock()
	return m.webhookErr
}

type mockSessionManager struct {
	mu              sync.RWMutex
	pairingStates   map[string]*session.QREvent
	subscribeChans  map[string]chan session.QREvent
	workspaceMap    map[string]uuid.UUID
	cancelCalls     []uuid.UUID
	cancelPhoneList []string
	errOnStart      error
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		pairingStates:  make(map[string]*session.QREvent),
		subscribeChans: make(map[string]chan session.QREvent),
		workspaceMap:   make(map[string]uuid.UUID),
	}
}

func (m *mockSessionManager) StartPairingSession(ctx context.Context, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) (*session.PairingSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errOnStart != nil {
		return nil, m.errOnStart
	}

	connID := uuid.New()
	if existingConnID != nil {
		connID = *existingConnID
	}

	evt := &session.QREvent{
		Status:       "pending",
		Code:         "test-qr-code",
		QRDataURL:    "data:image/png;base64,mock",
		ConnectionID: &connID,
	}
	m.pairingStates[phone] = evt
	m.pairingStates[connID.String()] = evt
	m.workspaceMap[phone] = workspaceID
	m.workspaceMap[connID.String()] = workspaceID

	return session.NewPairingSession(workspaceID, phone, connID), nil
}

func (m *mockSessionManager) GetPairingState(key string) (*session.QREvent, bool) {
	return m.GetPairingStateForWorkspace(uuid.Nil, key)
}

func (m *mockSessionManager) GetPairingStateForWorkspace(wsID uuid.UUID, key string) (*session.QREvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if wsID != uuid.Nil {
		if mappedWs, ok := m.workspaceMap[key]; ok && mappedWs != wsID {
			return nil, false
		}
	}
	evt, ok := m.pairingStates[key]
	return evt, ok
}

func (m *mockSessionManager) SubscribeQR(key string) (<-chan session.QREvent, func()) {
	ch, unsub, _ := m.SubscribeQRForWorkspace(uuid.Nil, key)
	return ch, unsub
}

func (m *mockSessionManager) SubscribeQRForWorkspace(wsID uuid.UUID, key string) (<-chan session.QREvent, func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wsID != uuid.Nil {
		if mappedWs, ok := m.workspaceMap[key]; ok && mappedWs != wsID {
			ch := make(chan session.QREvent, 1)
			close(ch)
			return ch, func() {}, false
		}
	}
	ch, ok := m.subscribeChans[key]
	if !ok {
		ch = make(chan session.QREvent, 1)
		close(ch)
		return ch, func() {}, false
	}

	return ch, func() {
		m.mu.Lock()
		delete(m.subscribeChans, key)
		m.mu.Unlock()
	}, true
}

func (m *mockSessionManager) CancelPairing(connectionID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelCalls = append(m.cancelCalls, connectionID)
}

func (m *mockSessionManager) CancelPairingByPhone(phone string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelPhoneList = append(m.cancelPhoneList, phone)
}

func (m *mockSessionManager) EmitStatusEvent(ctx context.Context, wsID uuid.UUID, connID uuid.UUID, channelName, senderIdentity, status string) error {
	return nil
}

type mockActiveSessions struct {
	disconnectedJIDs []string
}

func (m *mockActiveSessions) DisconnectByJID(jid string) {
	m.disconnectedJIDs = append(m.disconnectedJIDs, jid)
}

func (m *mockActiveSessions) GetClient(jid string) *whatsapp.WhatsAppClient {
	return nil
}

func setupEchoWithTenant(method, path string, body []byte, wsID uuid.UUID) (*echo.Echo, *echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if wsID != uuid.Nil {
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return e, c, rec
}

func TestConnectionAPIHandler_StartPairing_Unauthorized(t *testing.T) {
	handler := api.NewConnectionAPIHandler(nil, nil, nil)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/pair", []byte(`{"phone":"5511999999999"}`), uuid.Nil)

	err := handler.StartPairing(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestConnectionAPIHandler_StartPairing_MissingPhone(t *testing.T) {
	wsID := uuid.New()
	handler := api.NewConnectionAPIHandler(nil, nil, nil)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/pair", []byte(`{"channel":"whatsapp"}`), wsID)

	err := handler.StartPairing(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestConnectionAPIHandler_StartPairing_InvalidChannel(t *testing.T) {
	wsID := uuid.New()
	handler := api.NewConnectionAPIHandler(nil, nil, nil)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/pair", []byte(`{"channel":"telegram","phone":"123"}`), wsID)

	err := handler.StartPairing(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestConnectionAPIHandler_StartPairing_LimitExceeded(t *testing.T) {
	wsID := uuid.New()
	mgr := newMockSessionManager()
	mgr.errOnStart = session.ErrMaxConnectionsExceeded

	handler := api.NewConnectionAPIHandler(nil, mgr, nil)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/pair", []byte(`{"phone":"5511999999999"}`), wsID)

	err := handler.StartPairing(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status %d, got %d", http.StatusUnprocessableEntity, rec.Code)
	}
}

func TestConnectionAPIHandler_StartPairing_Success(t *testing.T) {
	wsID := uuid.New()
	mgr := newMockSessionManager()
	handler := api.NewConnectionAPIHandler(nil, mgr, nil)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/pair", []byte(`{"phone":"5511999999999"}`), wsID)

	err := handler.StartPairing(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d, body: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var res api.PairConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Phone != "5511999999999" {
		t.Errorf("expected phone %s, got %s", "5511999999999", res.Phone)
	}
	if res.Status != "pairing_started" {
		t.Errorf("expected status pairing_started, got %s", res.Status)
	}
}

func TestConnectionAPIHandler_GetQR(t *testing.T) {
	wsID := uuid.New()
	mgr := newMockSessionManager()
	connRepo := newMockConnectionRepo()

	connID := uuid.New()
	evt := &session.QREvent{
		Status:       "pending",
		Code:         "qr-raw-string",
		QRDataURL:    "data:image/png;base64,abcd",
		ConnectionID: &connID,
	}
	mgr.pairingStates[connID.String()] = evt
	mgr.workspaceMap[connID.String()] = wsID

	handler := api.NewConnectionAPIHandler(connRepo, mgr, nil)

	// 1. Found in active pairing states
	{
		e, c, rec := setupEchoWithTenant(http.MethodGet, "/api/v1/connections/"+connID.String()+"/qr", nil, wsID)
		c.SetPath("/api/v1/connections/:id/qr")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: connID.String()}})
		_ = e

		if err := handler.GetQR(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var qrRes session.QREvent
		_ = json.Unmarshal(rec.Body.Bytes(), &qrRes)
		if qrRes.Code != "qr-raw-string" {
			t.Errorf("expected code qr-raw-string, got %s", qrRes.Code)
		}
	}

	// 2. Found in repo as connected
	{
		connectedID := uuid.New()
		connRepo.connections[connectedID] = &repository.Connection{
			ID:          connectedID,
			WorkspaceID: wsID,
			Status:      "connected",
		}
		_, c, rec := setupEchoWithTenant(http.MethodGet, "/api/v1/connections/"+connectedID.String()+"/qr", nil, wsID)
		c.SetPath("/api/v1/connections/:id/qr")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: connectedID.String()}})

		if err := handler.GetQR(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var qrRes session.QREvent
		_ = json.Unmarshal(rec.Body.Bytes(), &qrRes)
		if qrRes.Status != "paired" {
			t.Errorf("expected status paired, got %s", qrRes.Status)
		}
	}

	// 3. Not found
	{
		unknownID := uuid.New()
		_, c, rec := setupEchoWithTenant(http.MethodGet, "/api/v1/connections/"+unknownID.String()+"/qr", nil, wsID)
		c.SetPath("/api/v1/connections/:id/qr")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: unknownID.String()}})

		if err := handler.GetQR(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	}
}

func TestConnectionAPIHandler_StreamQR(t *testing.T) {
	wsID := uuid.New()
	mgr := newMockSessionManager()
	handler := api.NewConnectionAPIHandler(nil, mgr, nil)

	connID := uuid.New().String()
	ch := make(chan session.QREvent, 5)
	mgr.subscribeChans[connID] = ch
	mgr.workspaceMap[connID] = wsID

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connID+"/qr/stream", nil)
	req = req.WithContext(tenant.WithWorkspaceID(ctx, wsID))
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/connections/:id/qr/stream")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: connID}})

	go func() {
		time.Sleep(50 * time.Millisecond)
		ch <- session.QREvent{
			Status: "pending",
			Code:   "qr-stream-code",
		}
		time.Sleep(50 * time.Millisecond)
		ch <- session.QREvent{
			Status:  "paired",
			Message: "paired successfully",
		}
	}()

	if err := handler.StreamQR(c); err != nil {
		t.Fatalf("StreamQR failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: qr") {
		t.Errorf("expected event: qr in SSE output: %s", body)
	}
	if !strings.Contains(body, "event: paired") {
		t.Errorf("expected event: paired in SSE output: %s", body)
	}
}

func TestConnectionAPIHandler_List(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	cID1 := uuid.New()
	cID2 := uuid.New()

	connRepo.connections[cID1] = &repository.Connection{
		ID:             cID1,
		WorkspaceID:    wsID,
		Name:           "Conn 1",
		Slug:           "conn-1",
		Channel:        "whatsapp",
		SenderIdentity: "5511999990001",
		Status:         "connected",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	connRepo.connections[cID2] = &repository.Connection{
		ID:             cID2,
		WorkspaceID:    uuid.New(), // other workspace
		Name:           "Conn 2",
		Status:         "connected",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)
	_, c, rec := setupEchoWithTenant(http.MethodGet, "/api/v1/connections", nil, wsID)

	if err := handler.List(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var res api.ListConnectionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(res.Connections))
	}
	if res.Connections[0].ID != cID1 {
		t.Errorf("expected connection ID %s, got %s", cID1, res.Connections[0].ID)
	}
}

func TestConnectionAPIHandler_Disconnect(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	activeSess := &mockActiveSessions{}
	mgr := newMockSessionManager()

	cID := uuid.New()
	jid := "5511999990001@s.whatsapp.net"
	connRepo.connections[cID] = &repository.Connection{
		ID:             cID,
		WorkspaceID:    wsID,
		Channel:        "whatsapp",
		SenderIdentity: "5511999990001",
		JID:            &jid,
		Status:         "connected",
	}

	handler := api.NewConnectionAPIHandler(connRepo, mgr, activeSess)

	// 1. Success disconnect
	{
		_, c, rec := setupEchoWithTenant(http.MethodDelete, "/api/v1/connections/"+cID.String(), nil, wsID)
		c.SetPath("/api/v1/connections/:id")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: cID.String()}})

		if err := handler.Disconnect(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(activeSess.disconnectedJIDs) != 1 || activeSess.disconnectedJIDs[0] != jid {
			t.Errorf("expected JID disconnected %s, got %v", jid, activeSess.disconnectedJIDs)
		}
		if len(mgr.cancelCalls) != 1 || mgr.cancelCalls[0] != cID {
			t.Errorf("expected pairing cancelled for %s, got %v", cID, mgr.cancelCalls)
		}
	}

	// 2. Disconnect not found
	{
		_, c, rec := setupEchoWithTenant(http.MethodDelete, "/api/v1/connections/"+cID.String(), nil, wsID)
		c.SetPath("/api/v1/connections/:id")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: cID.String()}})

		if err := handler.Disconnect(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	}
}

func TestConnectionAPIHandler_RouteAliases(t *testing.T) {
	e := echo.New()
	handler := api.NewConnectionAPIHandler(nil, nil, nil)
	handler.RegisterRoutes(e)

	routes := e.Router().Routes()
	expectedPaths := []string{
		"/api/v1/connections/pair",
		"/api/v1/connections/waba",
		"/api/v1/workspaces/:workspace_id/connections/waba",
		"/api/v1/devices/pair",
		"/api/v1/devices/waba",
		"/api/v1/workspaces/:workspace_id/devices/waba",
		"/api/v1/connections",
		"/api/v1/devices",
		"/api/v1/connections/:id/qr",
		"/api/v1/devices/:id/qr",
		"/api/v1/connections/:id/qr/stream",
		"/api/v1/devices/:id/qr/stream",
		"/api/v1/connections/:id",
		"/api/v1/devices/:id",
	}

	for _, expected := range expectedPaths {
		found := false
		for _, r := range routes {
			if r.Path == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected route %s to be registered", expected)
		}
	}
}

func TestConnectionAPIHandler_StreamQR_NotFoundOrUnauthorized(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	mgr := newMockSessionManager()
	handler := api.NewConnectionAPIHandler(connRepo, mgr, nil)

	// Mock SubscribeQRForWorkspace to simulate not found / forbidden
	mgr.subscribeChans = make(map[string]chan session.QREvent)

	e, c, rec := setupEchoWithTenant(http.MethodGet, "/api/v1/connections/unknown-id/qr/stream", nil, wsID)
	c.SetPath("/api/v1/connections/:id/qr/stream")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "unknown-id"}})
	_ = e

	// Override SubscribeQRForWorkspace in a local custom mock if needed or verify repo fallback
	otherWsID := uuid.New()
	foreignConnID := uuid.New()
	connRepo.connections[foreignConnID] = &repository.Connection{
		ID:          foreignConnID,
		WorkspaceID: otherWsID,
		Status:      "pending",
	}

	// Requesting foreign connection
	_, cForeign, recForeign := setupEchoWithTenant(http.MethodGet, "/api/v1/connections/"+foreignConnID.String()+"/qr/stream", nil, wsID)
	cForeign.SetPath("/api/v1/connections/:id/qr/stream")
	cForeign.SetPathValues(echo.PathValues{{Name: "id", Value: foreignConnID.String()}})

	err := handler.StreamQR(cForeign)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recForeign.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for foreign connection stream, got %d", recForeign.Code)
	}
	_ = rec
}

func TestConnectionAPIHandler_CreateWABA_Success(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	metaClient := &mockWABAMetaClient{}

	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)
	handler.SetMetaClient(metaClient)

	reqPayload := map[string]interface{}{
		"name":                 "WABA Agencia",
		"phone_number_id":      "123456789",
		"waba_account_id":      "987654321",
		"token":                "EAABbCcDd123",
		"verify_token":         "custom_verify_token",
		"app_secret":           "secret_xyz",
		"display_phone_number": "+55 11 99999-9999",
		"verified_name":        "Nome da Empresa",
	}
	payloadBytes, _ := json.Marshal(reqPayload)

	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payloadBytes, wsID)

	if err := handler.CreateWABA(c); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("expected status 201 or 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var res api.ConnectionItem
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if res.ID == uuid.Nil {
		t.Errorf("expected valid connection ID, got Nil")
	}
	if res.Name != "WABA Agencia" {
		t.Errorf("expected name 'WABA Agencia', got %q", res.Name)
	}
	if res.Slug != "waba-agencia" {
		t.Errorf("expected slug 'waba-agencia', got %q", res.Slug)
	}
	if res.Channel != "whatsapp_cloud" {
		t.Errorf("expected channel 'whatsapp_cloud', got %q", res.Channel)
	}
	if res.SenderIdentity != "5511999999999" {
		t.Errorf("expected sanitized sender_identity '5511999999999', got %q", res.SenderIdentity)
	}
	if res.Status != "connected" {
		t.Errorf("expected status 'connected', got %q", res.Status)
	}

	// Verify persistence in repository
	saved, err := connRepo.GetByID(context.Background(), res.ID)
	if err != nil || saved == nil {
		t.Fatalf("connection not found in repository: %v", err)
	}
	if saved.WorkspaceID != wsID {
		t.Errorf("expected workspace ID %s, got %s", wsID, saved.WorkspaceID)
	}

	// Verify credentials JSON contains all fields
	type storedCreds struct {
		PhoneNumberID string `json:"phone_number_id"`
		WABAAccountID string `json:"waba_account_id"`
		Token         string `json:"token"`
		VerifyToken   string `json:"verify_token"`
		AppSecret     string `json:"app_secret"`
	}
	var creds storedCreds
	if err := json.Unmarshal(saved.Credentials, &creds); err != nil {
		t.Fatalf("failed to unmarshal stored credentials: %v", err)
	}
	if creds.PhoneNumberID != "123456789" || creds.WABAAccountID != "987654321" || creds.Token != "EAABbCcDd123" {
		t.Errorf("stored credentials mismatch: %+v", creds)
	}
	if creds.VerifyToken != "custom_verify_token" || creds.AppSecret != "secret_xyz" {
		t.Errorf("stored optional credentials mismatch: %+v", creds)
	}
}

func TestConnectionAPIHandler_CreateWABA_FetchMetaDetailsFallback(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	metaClient := &mockWABAMetaClient{
		detailsToReturn: &client.WABAPhoneNumberDetails{
			DisplayPhoneNumber: "+55 (21) 98888-7777",
			VerifiedName:       "Empresa do Rio",
		},
	}

	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)
	handler.SetMetaClient(metaClient)

	reqPayload := map[string]interface{}{
		"phone_number_id": "meta_phone_123",
		"waba_account_id": "meta_account_456",
		"token":           "meta_token_789",
	}
	payloadBytes, _ := json.Marshal(reqPayload)

	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payloadBytes, wsID)

	if err := handler.CreateWABA(c); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("expected status 201 or 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var res api.ConnectionItem
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if res.SenderIdentity != "5521988887777" {
		t.Errorf("expected sender_identity '5521988887777' from Meta details fallback, got %q", res.SenderIdentity)
	}
	if res.Name != "Empresa do Rio" {
		t.Errorf("expected name 'Empresa do Rio' from Meta details fallback, got %q", res.Name)
	}
}

func TestConnectionAPIHandler_CreateWABA_MissingRequiredFields(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)

	testCases := []struct {
		name    string
		payload string
	}{
		{
			name:    "missing phone_number_id",
			payload: `{"waba_account_id":"acc123","token":"tok123"}`,
		},
		{
			name:    "missing waba_account_id",
			payload: `{"phone_number_id":"pn123","token":"tok123"}`,
		},
		{
			name:    "missing token",
			payload: `{"phone_number_id":"pn123","waba_account_id":"acc123"}`,
		},
		{
			name:    "empty fields",
			payload: `{"phone_number_id":"","waba_account_id":"","token":""}`,
		},
		{
			name:    "malformed JSON",
			payload: `{"phone_number_id": 123...invalid`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", []byte(tc.payload), wsID)

			if err := handler.CreateWABA(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestConnectionAPIHandler_CreateWABA_UnauthorizedAndWorkspaceIsolation(t *testing.T) {
	wsID := uuid.New()
	otherWsID := uuid.New()
	connRepo := newMockConnectionRepo()
	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)

	validPayload := []byte(`{"phone_number_id":"123","waba_account_id":"456","token":"tok","display_phone_number":"+5511999998888"}`)

	// 1. Missing workspace context
	{
		_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", validPayload, uuid.Nil)
		if err := handler.CreateWABA(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized when workspace is missing, got %d", rec.Code)
		}
	}

	// 2. Workspace ID in path matches context
	{
		_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/workspaces/"+wsID.String()+"/connections/waba", validPayload, wsID)
		c.SetPath("/api/v1/workspaces/:workspace_id/connections/waba")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: wsID.String()}})

		if err := handler.CreateWABA(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Errorf("expected 201 Created for matching workspace_id, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3. Workspace ID in path does not match authenticated context (Workspace Isolation)
	{
		_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/workspaces/"+otherWsID.String()+"/connections/waba", validPayload, wsID)
		c.SetPath("/api/v1/workspaces/:workspace_id/connections/waba")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: otherWsID.String()}})

		if err := handler.CreateWABA(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 403 Forbidden or 401 Unauthorized for mismatched workspace_id, got %d", rec.Code)
		}
	}
}

func TestConnectionAPIHandler_CreateWABA_SlugGenerationAndConflict(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)

	payload1 := []byte(`{"name":"Loja Matriz","phone_number_id":"111","waba_account_id":"acc1","token":"tok1","display_phone_number":"+5511999991111"}`)
	payload2 := []byte(`{"name":"Loja Matriz","phone_number_id":"222","waba_account_id":"acc2","token":"tok2","display_phone_number":"+5511999992222"}`)

	// First creation -> slug: loja-matriz
	_, c1, rec1 := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payload1, wsID)
	if err := handler.CreateWABA(c1); err != nil {
		t.Fatalf("unexpected error 1: %v", err)
	}
	var res1 api.ConnectionItem
	_ = json.Unmarshal(rec1.Body.Bytes(), &res1)
	if res1.Slug != "loja-matriz" {
		t.Errorf("expected slug 'loja-matriz', got %q", res1.Slug)
	}

	// Second creation with same name -> slug: loja-matriz-2
	_, c2, rec2 := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payload2, wsID)
	if err := handler.CreateWABA(c2); err != nil {
		t.Fatalf("unexpected error 2: %v", err)
	}
	var res2 api.ConnectionItem
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if res2.Slug != "loja-matriz-2" {
		t.Errorf("expected slug 'loja-matriz-2', got %q", res2.Slug)
	}
}

func TestConnectionAPIHandler_CreateWABA_TemplateSyncTrigger(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	metaClient := &mockWABAMetaClient{}

	handler := api.NewConnectionAPIHandler(connRepo, nil, nil, api.WithWABAMetaClient(metaClient))

	payload := []byte(`{"name":"Sync Test","phone_number_id":"999","waba_account_id":"acc999","token":"tok999","display_phone_number":"+5511999990000"}`)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payload, wsID)

	if err := handler.CreateWABA(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("expected success status, got %d", rec.Code)
	}

	if !metaClient.syncCalled {
		t.Errorf("expected template sync to be triggered upon WABA connection creation")
	}

	// Even if template sync returns error, connection creation should succeed gracefully
	metaClientErr := &mockWABAMetaClient{errOnSync: errors.New("Meta API sync timeout")}
	handlerErr := api.NewConnectionAPIHandler(connRepo, nil, nil, api.WithWABAMetaClient(metaClientErr))

	payloadErr := []byte(`{"name":"Sync Err Test","phone_number_id":"888","waba_account_id":"acc888","token":"tok888","display_phone_number":"+5511999990001"}`)
	_, cErr, recErr := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/waba", payloadErr, wsID)

	if err := handlerErr.CreateWABA(cErr); err != nil {
		t.Fatalf("unexpected error on handler: %v", err)
	}
	if recErr.Code != http.StatusCreated && recErr.Code != http.StatusOK {
		t.Errorf("expected success status even if template sync fails, got %d", recErr.Code)
	}
}

func TestConnectionAPIHandler_CreateTelegram_Success(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	tgClient := &mockTelegramBotClient{validUsername: "@pergo_support_bot"}

	handler := api.NewConnectionAPIHandler(
		connRepo,
		nil,
		nil,
		api.WithTelegramClient(tgClient),
		api.WithExternalURL("https://example.com"),
	)

	payload := []byte(`{
		"name": "Suporte PerGo Telegram",
		"token": "123456789:ABCDefGhIJKlmNoPQR",
		"secret_token": "custom_tg_secret"
	}`)

	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/telegram", payload, wsID)

	if err := handler.CreateTelegram(c); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var res api.ConnectionItem
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if res.Channel != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", res.Channel)
	}
	if res.SenderIdentity != "@pergo_support_bot" {
		t.Errorf("expected sender_identity '@pergo_support_bot', got %q", res.SenderIdentity)
	}
	if res.Name != "Suporte PerGo Telegram" {
		t.Errorf("expected name 'Suporte PerGo Telegram', got %q", res.Name)
	}
	if res.Slug != "suporte-pergo-telegram" {
		t.Errorf("expected slug 'suporte-pergo-telegram', got %q", res.Slug)
	}
	if res.Status != "connected" {
		t.Errorf("expected status 'connected', got %q", res.Status)
	}

	// Verify webhook was registered with HTTPS URL
	if !tgClient.webhookCalled {
		t.Errorf("expected webhook registration to be called when HTTPS external URL is set")
	}
	expectedWebhookURL := "https://example.com/webhooks/telegram/" + wsID.String()
	if tgClient.registeredURL != expectedWebhookURL {
		t.Errorf("expected webhook URL %q, got %q", expectedWebhookURL, tgClient.registeredURL)
	}

	// Verify credentials in DB
	saved, err := connRepo.GetByID(context.Background(), res.ID)
	if err != nil || saved == nil {
		t.Fatalf("connection not found in repository: %v", err)
	}

	var creds api.StoredTelegramConfig
	if err := json.Unmarshal(saved.Credentials, &creds); err != nil {
		t.Fatalf("failed to decode stored credentials: %v", err)
	}
	if creds.Token != "123456789:ABCDefGhIJKlmNoPQR" || creds.SecretToken != "custom_tg_secret" || creds.BotUsername != "@pergo_support_bot" {
		t.Errorf("stored credentials mismatch: %+v", creds)
	}
}

func TestConnectionAPIHandler_CreateTelegram_InvalidToken(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	tgClient := &mockTelegramBotClient{validateErr: errors.New("Unauthorized")}

	handler := api.NewConnectionAPIHandler(connRepo, nil, nil, api.WithTelegramClient(tgClient))

	payload := []byte(`{"token": "invalid_token_123"}`)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/telegram", payload, wsID)

	if err := handler.CreateTelegram(c); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConnectionAPIHandler_CreateTelegram_MissingToken(t *testing.T) {
	wsID := uuid.New()
	connRepo := newMockConnectionRepo()
	handler := api.NewConnectionAPIHandler(connRepo, nil, nil)

	payload := []byte(`{"name": "No Token Bot"}`)
	_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/telegram", payload, wsID)

	if err := handler.CreateTelegram(c); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConnectionAPIHandler_CreateTelegram_WorkspaceIsolation(t *testing.T) {
	wsID := uuid.New()
	otherWsID := uuid.New()
	connRepo := newMockConnectionRepo()
	tgClient := &mockTelegramBotClient{validUsername: "@isolation_bot"}
	handler := api.NewConnectionAPIHandler(connRepo, nil, nil, api.WithTelegramClient(tgClient))

	validPayload := []byte(`{"token":"valid_tg_tok"}`)

	// 1. Missing workspace
	{
		_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/connections/telegram", validPayload, uuid.Nil)
		if err := handler.CreateTelegram(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}
	}

	// 2. Mismatched workspace in path
	{
		_, c, rec := setupEchoWithTenant(http.MethodPost, "/api/v1/workspaces/"+otherWsID.String()+"/connections/telegram", validPayload, wsID)
		c.SetPath("/api/v1/workspaces/:workspace_id/connections/telegram")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: otherWsID.String()}})

		if err := handler.CreateTelegram(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	}
}


