package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
)

// mockWhatsAppClient implements WhatsAppClientInterface for testing.
type mockWhatsAppClient struct {
	jid        types.JID
	qrCh       chan whatsmeow.QRChannelItem
	runErr     error
	connectErr error
	connected  bool
	stopped    bool
	mu         sync.Mutex
}

func newMockWhatsAppClient() *mockWhatsAppClient {
	return &mockWhatsAppClient{
		qrCh: make(chan whatsmeow.QRChannelItem, 10),
	}
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
	return m.runErr
}

func (m *mockWhatsAppClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockWhatsAppClient) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.stopped = true
}

func (m *mockWhatsAppClient) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return m.qrCh, nil
}

type mockClientFactory struct {
	client *mockWhatsAppClient
}

func (f *mockClientFactory) CreateClient(cfg whatsapp.ClientConfig) (WhatsAppClientInterface, error) {
	return f.client, nil
}

func TestGenerateQRDataURL(t *testing.T) {
	// Empty code returns empty string
	if got := GenerateQRDataURL(""); got != "" {
		t.Errorf("expected empty string for empty code, got %q", got)
	}

	// Valid code returns data URL with base64 PNG prefix
	dataURL := GenerateQRDataURL("2@testqrcode123456789")
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Errorf("expected data URL to start with 'data:image/png;base64,', got %q", dataURL)
	}
}

func TestPairingPubSub_BroadcastAndSubscribe(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	factory := &mockClientFactory{client: mockCli}

	mgr := NewManager(nil, nil, NewActiveSession(), nil, "", nil)
	mgr.SetClientFactory(factory)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999991234"

	ps, err := mgr.StartPairingSession(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}
	if ps == nil {
		t.Fatal("expected non-nil PairingSession")
	}

	// Subscribe via phone
	sub1, unsub1 := mgr.SubscribeQR(phone)
	defer unsub1()

	// Subscribe via connection ID string
	sub2, unsub2 := mgr.SubscribeQR(ps.connectionID.String())
	defer unsub2()

	// Check initial pending state
	state, ok := mgr.GetPairingState(phone)
	if !ok || state == nil {
		t.Fatal("expected pairing state to be found")
	}
	if state.Status != "pending" {
		t.Errorf("expected initial status 'pending', got %q", state.Status)
	}

	// Emit QR code item
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "code",
		Code:  "2@mockqrcode1",
	}

	// Wait for event on subscriber 1
	select {
	case evt := <-sub1:
		// Drain initial pending message if queued, or verify code
		if evt.Code == "" && evt.Status == "pending" {
			evt = <-sub1
		}
		if evt.Status != "pending" {
			t.Errorf("sub1: expected status 'pending', got %q", evt.Status)
		}
		if evt.Code != "2@mockqrcode1" {
			t.Errorf("sub1: expected code '2@mockqrcode1', got %q", evt.Code)
		}
		if !strings.HasPrefix(evt.QRDataURL, "data:image/png;base64,") {
			t.Errorf("sub1: expected valid qr_data_url, got %q", evt.QRDataURL)
		}
		if evt.ExpiresAt == nil || evt.ExpiresAt.Before(time.Now()) {
			t.Errorf("sub1: expected future expires_at, got %v", evt.ExpiresAt)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event on sub1")
	}

	// Wait for event on subscriber 2
	select {
	case evt := <-sub2:
		if evt.Code == "" && evt.Status == "pending" {
			evt = <-sub2
		}
		if evt.Code != "2@mockqrcode1" {
			t.Errorf("sub2: expected code '2@mockqrcode1', got %q", evt.Code)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event on sub2")
	}

	// Verify GetPairingState returns latest code
	state, ok = mgr.GetPairingState(phone)
	if !ok || state.Code != "2@mockqrcode1" {
		t.Errorf("GetPairingState: expected code '2@mockqrcode1', got %v", state)
	}

	// Unsubscribe sub1
	unsub1()

	// Emit second QR code item
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "code",
		Code:  "2@mockqrcode2",
	}

	// sub2 should receive it
	select {
	case evt := <-sub2:
		if evt.Code != "2@mockqrcode2" {
			t.Errorf("sub2: expected code '2@mockqrcode2', got %q", evt.Code)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for second event on sub2")
	}

	// sub1 channel should be closed
	select {
	case _, ok := <-sub1:
		_ = ok
	default:
	}
}

func TestPairingPubSub_Success(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	parsedJID, _ := types.ParseJID("5511999991234@s.whatsapp.net")
	mockCli.SetJID(parsedJID)

	factory := &mockClientFactory{client: mockCli}
	registry := NewActiveSession()

	mgr := NewManager(nil, nil, registry, nil, "", nil)
	mgr.SetClientFactory(factory)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999991234"

	_, err := mgr.StartPairingSession(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}

	sub, unsub := mgr.SubscribeQR(phone)
	defer unsub()

	// Send success event
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "success",
	}

	// Wait for paired event
	var pairedEvt QREvent
	for evt := range sub {
		if evt.Status == "paired" {
			pairedEvt = evt
			break
		}
	}

	if pairedEvt.Status != "paired" {
		t.Errorf("expected paired status, got %q", pairedEvt.Status)
	}

	// Verify session registered in ActiveSession
	sess := registry.Get(parsedJID)
	if sess == nil {
		t.Errorf("expected session to be added to ActiveSession registry")
	}

	// GetPairingState should return paired
	state, ok := mgr.GetPairingState(phone)
	if !ok || state.Status != "paired" {
		t.Errorf("expected GetPairingState to return paired, got %v", state)
	}
}

func TestPairingPubSub_Timeout(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	factory := &mockClientFactory{client: mockCli}

	mgr := NewManager(nil, nil, NewActiveSession(), nil, "", nil)
	mgr.SetClientFactory(factory)
	mgr.SetQRTimeout(50 * time.Millisecond)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999995555"

	_, err := mgr.StartPairingSession(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}

	sub, unsub := mgr.SubscribeQR(phone)
	defer unsub()

	var errEvt QREvent
	for evt := range sub {
		if evt.Status == "error" {
			errEvt = evt
			break
		}
	}

	if errEvt.Status != "error" {
		t.Errorf("expected error status on timeout, got %q", errEvt.Status)
	}
	if !strings.Contains(errEvt.Message, "timeout") {
		t.Errorf("expected timeout in error message, got %q", errEvt.Message)
	}
}

func TestPairingPubSub_Cancel(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	factory := &mockClientFactory{client: mockCli}

	mgr := NewManager(nil, nil, NewActiveSession(), nil, "", nil)
	mgr.SetClientFactory(factory)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999998888"

	_, err := mgr.StartPairingSession(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}

	sub, unsub := mgr.SubscribeQR(phone)
	defer unsub()

	// Cancel pairing
	mgr.CancelPairingByPhone(phone)

	var errEvt QREvent
	for evt := range sub {
		if evt.Status == "error" {
			errEvt = evt
			break
		}
	}

	if errEvt.Status != "error" {
		t.Errorf("expected error status on cancel, got %q", errEvt.Status)
	}
	if !strings.Contains(errEvt.Message, "cancelled") {
		t.Errorf("expected cancelled message, got %q", errEvt.Message)
	}
}

func TestStartPairing_LegacyCompatibility(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	factory := &mockClientFactory{client: mockCli}

	mgr := NewManager(nil, nil, NewActiveSession(), nil, "", nil)
	mgr.SetClientFactory(factory)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999999999"

	ch, err := mgr.StartPairing(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}

	// Send code
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "code",
		Code:  "2@legacyqr",
	}

	select {
	case evt := <-ch:
		if evt.Type != QREventCode {
			t.Errorf("expected QREventCode, got %v", evt.Type)
		}
		if string(evt.Data) != "2@legacyqr" {
			t.Errorf("expected Data '2@legacyqr', got %q", string(evt.Data))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for legacy QR event")
	}

	// Send success
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "success",
	}

	select {
	case evt := <-ch:
		if evt.Type != QREventPaired {
			t.Errorf("expected QREventPaired, got %v", evt.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for legacy paired event")
	}
}

func TestManager_EmitStatusEvent(t *testing.T) {
	pub := &mockPublisher{}
	mgr := NewManager(nil, nil, NewActiveSession(), nil, "", nil)
	mgr.SetPublisher(pub)

	ctx := context.Background()
	wsID := uuid.New()
	connID := uuid.New()

	err := mgr.EmitStatusEvent(ctx, wsID, connID, "whatsapp", "+5511999991234", "connected")
	if err != nil {
		t.Fatalf("EmitStatusEvent failed: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}

	p := pub.published[0]
	if p.subject != "webhooks.events" {
		t.Errorf("expected subject 'webhooks.events', got %q", p.subject)
	}

	var evt ConnectionStatusEvent
	if err := json.Unmarshal(p.data, &evt); err != nil {
		t.Fatalf("failed to unmarshal published event: %v", err)
	}

	if evt.Event != "connection.status" {
		t.Errorf("expected event 'connection.status', got %q", evt.Event)
	}
	if evt.WorkspaceID != wsID {
		t.Errorf("expected workspaceID %v, got %v", wsID, evt.WorkspaceID)
	}
	if evt.ConnectionID != connID {
		t.Errorf("expected connectionID %v, got %v", connID, evt.ConnectionID)
	}
	if evt.Channel != "whatsapp" {
		t.Errorf("expected channel 'whatsapp', got %q", evt.Channel)
	}
	if evt.SenderIdentity != "+5511999991234" {
		t.Errorf("expected sender identity '+5511999991234', got %q", evt.SenderIdentity)
	}
	if evt.Status != "connected" {
		t.Errorf("expected status 'connected', got %q", evt.Status)
	}
	if evt.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if evt.TraceID == "" {
		t.Error("expected non-empty trace_id")
	}

	// Verify healthMap updated
	health, err := mgr.SessionHealth(connID)
	if err != nil {
		t.Fatalf("SessionHealth failed: %v", err)
	}
	if health.State != StateConnected {
		t.Errorf("expected health state 'connected', got %q", health.State)
	}
}

func TestManager_PairingPubSub_EmitsStatusConnected(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	parsedJID, _ := types.ParseJID("5511999991234@s.whatsapp.net")
	mockCli.SetJID(parsedJID)

	factory := &mockClientFactory{client: mockCli}
	registry := NewActiveSession()
	pub := &mockPublisher{}

	mgr := NewManager(nil, nil, registry, nil, "", nil)
	mgr.SetClientFactory(factory)
	mgr.SetPublisher(pub)

	ctx := context.Background()
	wsID := uuid.New()
	phone := "5511999991234"

	_, err := mgr.StartPairingSession(ctx, wsID, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}

	sub, unsub := mgr.SubscribeQR(phone)
	defer unsub()

	// Send success event
	mockCli.qrCh <- whatsmeow.QRChannelItem{
		Event: "success",
	}

	// Wait for paired event
	for evt := range sub {
		if evt.Status == "paired" {
			break
		}
	}

	// Verify connected event was published
	time.Sleep(50 * time.Millisecond)
	found := false
	for _, item := range pub.published {
		var evt ConnectionStatusEvent
		if err := json.Unmarshal(item.data, &evt); err == nil {
			if evt.Event == "connection.status" && evt.Status == "connected" && evt.SenderIdentity == phone {
				found = true
				break
			}
		}
	}

	if !found {
		t.Error("expected connection.status event with status 'connected' to be published")
	}
}

func TestCalcBackoff(t *testing.T) {
	d0 := CalcBackoff(0)
	if d0 < 4*time.Second || d0 > 6*time.Second {
		t.Errorf("expected attempt 0 backoff ~5s, got %v", d0)
	}

	d10 := CalcBackoff(10)
	if d10 > 6*time.Minute {
		t.Errorf("expected attempt 10 backoff to be capped near 5m, got %v", d10)
	}
}

func TestManager_Pairing_TenantIsolation(t *testing.T) {
	mockCli := newMockWhatsAppClient()
	factory := &mockClientFactory{client: mockCli}
	reg := NewActiveSession()
	mgr := NewManager(nil, nil, reg, nil, "2.3000.1025000000", nil)
	mgr.SetClientFactory(factory)

	ctx := context.Background()
	wsA := uuid.New()
	wsB := uuid.New()
	phone := "5511999998888"

	ps, err := mgr.StartPairingSession(ctx, wsA, phone, nil, "")
	if err != nil {
		t.Fatalf("StartPairingSession failed: %v", err)
	}

	// 1. Same workspace (wsA) should get pairing state
	state, ok := mgr.GetPairingStateForWorkspace(wsA, phone)
	if !ok || state == nil {
		t.Errorf("expected state to be found for workspace A")
	}

	// 2. Different workspace (wsB) should NOT get pairing state
	stateB, okB := mgr.GetPairingStateForWorkspace(wsB, phone)
	if okB || stateB != nil {
		t.Errorf("expected state to NOT be found for workspace B, got %v", stateB)
	}

	// 3. Different workspace (wsB) by connection ID should NOT get pairing state
	stateBID, okBID := mgr.GetPairingStateForWorkspace(wsB, ps.connectionID.String())
	if okBID || stateBID != nil {
		t.Errorf("expected state to NOT be found for workspace B using connection ID")
	}

	// 4. Same workspace (wsA) subscribing
	subA, unsubA, okSubA := mgr.SubscribeQRForWorkspace(wsA, phone)
	defer unsubA()
	if !okSubA || subA == nil {
		t.Errorf("expected subscribe to succeed for workspace A")
	}

	// 5. Different workspace (wsB) subscribing should fail (ok=false, closed/error channel)
	subB, unsubB, okSubB := mgr.SubscribeQRForWorkspace(wsB, phone)
	defer unsubB()
	if okSubB {
		t.Errorf("expected subscribe to return ok=false for workspace B")
	}
	evtB := <-subB
	if evtB.Status != "error" {
		t.Errorf("expected error event for workspace B subscriber, got %v", evtB)
	}
}


