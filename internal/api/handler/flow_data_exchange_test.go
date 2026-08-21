package handler_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockConnectionRepo struct {
	connections map[uuid.UUID]*repository.Connection
}

func (m *mockConnectionRepo) GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error) {
	if conn, ok := m.connections[id]; ok {
		return conn, nil
	}
	return nil, repository.ErrConnectionNotFound
}

type mockWorkspaceRepo struct {
	workspaces map[uuid.UUID]*repository.Workspace
}

func (m *mockWorkspaceRepo) GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error) {
	if ws, ok := m.workspaces[id]; ok {
		return ws, nil
	}
	return nil, repository.ErrWorkspaceNotFound
}

type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.doFunc != nil {
		return m.doFunc(req)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"screen": "SUCCESS", "data": {}}`))),
		Header:     make(http.Header),
	}, nil
}

// helper to prepare RSA keypair PEM and encrypted request payload
func setupFlowCryptoEnv(t *testing.T) (*rsa.PrivateKey, string, []byte, []byte) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	aesKey := make([]byte, 16)
	if _, err := rand.Read(aesKey); err != nil {
		t.Fatalf("failed to generate AES key: %v", err)
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		t.Fatalf("failed to generate IV: %v", err)
	}

	return privKey, string(privPEM), aesKey, iv
}

func createEncryptedRequest(t *testing.T, pubKey *rsa.PublicKey, aesKey, iv, flowData []byte) handler.FlowDataExchangeRequest {
	t.Helper()
	encFlowData, tag, err := crypto.EncryptAES128GCM(aesKey, iv, flowData)
	if err != nil {
		t.Fatalf("EncryptAES128GCM failed: %v", err)
	}
	encFlowDataWithTag := append(encFlowData, tag...)

	encAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		t.Fatalf("EncryptOAEP failed: %v", err)
	}

	return handler.FlowDataExchangeRequest{
		EncryptedFlowData: base64.StdEncoding.EncodeToString(encFlowDataWithTag),
		EncryptedAESKey:   base64.StdEncoding.EncodeToString(encAESKey),
		InitialVector:     base64.StdEncoding.EncodeToString(iv),
	}
}

func decryptResponse(t *testing.T, aesKey, iv []byte, responseBody string) []byte {
	t.Helper()
	encResp, err := base64.StdEncoding.DecodeString(responseBody)
	if err != nil {
		t.Fatalf("failed to base64 decode response: %v, raw: %q", err, responseBody)
	}
	if len(encResp) < 16 {
		t.Fatalf("encrypted response too short: %d", len(encResp))
	}

	tagSize := 16
	ciphertext := encResp[:len(encResp)-tagSize]
	tag := encResp[len(encResp)-tagSize:]

	invIV := crypto.InvertIV(iv)
	decrypted, err := crypto.DecryptAES128GCM(aesKey, invIV, ciphertext, tag)
	if err != nil {
		t.Fatalf("failed to decrypt response with inverted IV: %v", err)
	}
	return decrypted
}

func TestFlowDataExchange_PingAction(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, []byte(`{"version":"3.0","action":"ping"}`))
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Version string            `json:"version"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted ping response: %v, raw: %s", err, string(decrypted))
	}
	if resp.Data["status"] != "active" {
		t.Errorf("expected status 'active', got %q", resp.Data["status"])
	}
}

func TestFlowDataExchange_ValidDataExchange(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}

	// Generate valid flow token
	flowToken, err := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte(secret))
	if err != nil {
		t.Fatalf("GenerateFlowToken failed: %v", err)
	}

	var downstreamReceivedBody []byte
	var downstreamSigHeader string
	httpClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != webhookURL {
				t.Errorf("expected request URL %q, got %q", webhookURL, req.URL.String())
			}
			downstreamSigHeader = req.Header.Get("X-PerGo-Signature")
			downstreamReceivedBody, _ = io.ReadAll(req.Body)

			respData := `{"screen": "SUMMARY", "data": {"order_id": "12345", "total": 99.9}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(respData))),
				Header:     make(http.Header),
			}, nil
		},
	}

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data": map[string]interface{}{
			"input": "User selected option A",
		},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify downstream received signed payload
	if downstreamSigHeader == "" {
		t.Errorf("expected X-PerGo-Signature on downstream request")
	} else {
		if !webhook.VerifyPerGoSignature(downstreamReceivedBody, downstreamSigHeader, secret) {
			t.Errorf("downstream X-PerGo-Signature validation failed")
		}
	}

	// Verify encrypted response returned to Meta
	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "SUMMARY" {
		t.Errorf("expected screen 'SUMMARY', got %q", resp.Screen)
	}
	if resp.Data["order_id"] != "12345" {
		t.Errorf("expected order_id '12345', got %v", resp.Data["order_id"])
	}
}

func TestFlowDataExchange_ExpiredFlowToken(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	// Generate EXPIRED token (1 hour in the past)
	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
	}, []byte(secret))

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted EXPIRED payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "EXPIRED" {
		t.Errorf("expected screen 'EXPIRED', got %q", resp.Screen)
	}
}

func TestFlowDataExchange_InvalidSignatureFlowToken(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	// Generate token signed with wrong secret
	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte("forged-secret"))

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted ERROR payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "ERROR" {
		t.Errorf("expected screen 'ERROR', got %q", resp.Screen)
	}
}

func TestFlowDataExchange_MissingFlowToken(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	// Omit flow_token completely
	flowReqJSON := map[string]interface{}{
		"version": "3.0",
		"action":  "data_exchange",
		"screen":  "DETAILS",
		"data":    map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted ERROR payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "ERROR" {
		t.Errorf("expected screen 'ERROR' when flow_token is missing, got %q", resp.Screen)
	}
}

func TestFlowDataExchange_DualSecretValidation(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	customSecret := "custom-wh-secret-999"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &customSecret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	// Token signed with default workspaceID string (e.g. from waba dispatch)
	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte(wsID.String()))

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "SUCCESS" {
		t.Errorf("expected screen 'SUCCESS', got %q", resp.Screen)
	}
}

func TestFlowDataExchange_DownstreamWebhookTimeout(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}

	// HTTP client returns timeout error
	httpClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		},
	}

	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte(secret))

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted ERROR payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "ERROR" {
		t.Errorf("expected screen 'ERROR' on downstream timeout, got %q", resp.Screen)
	}
}

func TestFlowDataExchange_DownstreamWebhook5xxError(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"
	webhookURL := "https://crm.partner.com/flow/webhook"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: &webhookURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}

	// HTTP client returns 502 Bad Gateway
	httpClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(bytes.NewReader([]byte("upstream server crashed"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte(secret))

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted ERROR payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "ERROR" {
		t.Errorf("expected screen 'ERROR' on downstream 5xx, got %q", resp.Screen)
	}
}

func TestFlowDataExchange_UnconfiguredFlowWebhookURL(t *testing.T) {
	privKey, privPEM, aesKey, iv := setupFlowCryptoEnv(t)

	wsID := uuid.New()
	connID := uuid.New()
	secret := "test-secret-123"

	// No FlowWebhookURL
	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Test WS",
		WebhookSecret:  &secret,
		FlowWebhookURL: nil,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:          connID,
		WorkspaceID: wsID,
		Channel:     "whatsapp_cloud",
		Credentials: credsJSON,
	}

	connRepo := &mockConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{connID: conn},
	}
	wsRepo := &mockWorkspaceRepo{
		workspaces: map[uuid.UUID]*repository.Workspace{wsID: ws},
	}
	httpClient := &mockHTTPClient{}

	flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
		WorkspaceID:  wsID,
		ConnectionID: connID,
		ContactID:    "5511999998888",
		FlowID:       "flow_001",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	}, []byte(secret))

	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, httpClient)

	flowReqJSON := map[string]interface{}{
		"version":    "3.0",
		"action":     "data_exchange",
		"screen":     "DETAILS",
		"flow_token": flowToken,
		"data":       map[string]interface{}{},
	}
	flowReqBytes, _ := json.Marshal(flowReqJSON)

	reqPayload := createEncryptedRequest(t, &privKey.PublicKey, aesKey, iv, flowReqBytes)
	bodyBytes, _ := json.Marshal(reqPayload)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.HandleFlowDataExchange(c); err != nil {
		t.Fatalf("HandleFlowDataExchange failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with encrypted ERROR payload, got %d: %s", rec.Code, rec.Body.String())
	}

	decrypted := decryptResponse(t, aesKey, iv, rec.Body.String())
	var resp struct {
		Screen string                 `json:"screen"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("failed to parse decrypted response: %v", err)
	}
	if resp.Screen != "ERROR" {
		t.Errorf("expected screen 'ERROR' when flow webhook url is unconfigured, got %q", resp.Screen)
	}
}

func TestFlowDataExchange_ValidationErrors(t *testing.T) {
	connRepo := &mockConnectionRepo{connections: map[uuid.UUID]*repository.Connection{}}
	wsRepo := &mockWorkspaceRepo{workspaces: map[uuid.UUID]*repository.Workspace{}}
	h := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, nil)

	e := echo.New()

	t.Run("missing connection_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		_ = h.HandleFlowDataExchange(c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("non-existent connection_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+uuid.New().String(), strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		_ = h.HandleFlowDataExchange(c)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}
