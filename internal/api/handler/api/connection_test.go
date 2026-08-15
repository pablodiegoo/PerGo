package api_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func (m *mockConnectionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.connections, id)
	return nil
}

type mockSessionManager struct {
	mu              sync.RWMutex
	pairingStates   map[string]*session.QREvent
	subscribeChans  map[string]chan session.QREvent
	cancelCalls     []uuid.UUID
	cancelPhoneList []string
	errOnStart      error
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		pairingStates:  make(map[string]*session.QREvent),
		subscribeChans: make(map[string]chan session.QREvent),
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

	return session.NewPairingSession(workspaceID, phone, connID), nil
}

func (m *mockSessionManager) GetPairingState(key string) (*session.QREvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	evt, ok := m.pairingStates[key]
	return evt, ok
}

func (m *mockSessionManager) SubscribeQR(key string) (<-chan session.QREvent, func()) {
	m.mu.Lock()
	ch, ok := m.subscribeChans[key]
	if !ok {
		ch = make(chan session.QREvent, 10)
		m.subscribeChans[key] = ch
	}
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		delete(m.subscribeChans, key)
		m.mu.Unlock()
	}
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
		"/api/v1/devices/pair",
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
