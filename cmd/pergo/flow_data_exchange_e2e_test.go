package main

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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

func TestFlowDataExchange_E2E(t *testing.T) {
	// Setup real RSA keypair for connection
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	privPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	}))

	aesKey := make([]byte, 16)
	rand.Read(aesKey)
	iv := make([]byte, 12)
	rand.Read(iv)

	// Mock partner flow webhook backend
	var mu sync.Mutex
	var receivedPartnerPayload []byte
	var receivedPartnerSig string
	partnerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("X-PerGo-Signature")
		body, _ := io.ReadAll(r.Body)

		mu.Lock()
		receivedPartnerSig = sig
		receivedPartnerPayload = body
		mu.Unlock()

		if r.URL.Path == "/timeout" {
			time.Sleep(3 * time.Second)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.URL.Path == "/500" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"screen": "ORDER_SUCCESS", "data": {"confirmation_code": "PERGO-999"}}`))
	}))
	defer partnerServer.Close()

	wsID := uuid.New()
	connID := uuid.New()
	secret := "flow-e2e-secret-key-123"
	partnerURL := partnerServer.URL + "/exchange"

	ws := &repository.Workspace{
		ID:             wsID,
		Name:           "Flow E2E Workspace",
		WebhookSecret:  &secret,
		FlowWebhookURL: &partnerURL,
	}

	credsJSON, _ := json.Marshal(map[string]string{
		"private_key": privPEM,
	})

	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+5511888880001",
		Credentials:    credsJSON,
		Status:         "connected",
	}

	connRepo := &mockConnStore{conn: conn}
	wsRepo := &mockWsStore{ws: ws}

	// FlowDataExchangeHandler with standard HTTP client
	flowHandler := handler.NewFlowDataExchangeHandler(connRepo, wsRepo, partnerServer.Client())

	e := echo.New()
	e.POST("/api/v1/waba/flows/data-exchange", flowHandler.HandleFlowDataExchange)

	// Helper for creating encrypted requests
	createEncReq := func(payload []byte) []byte {
		encFlowData, tag, _ := crypto.EncryptAES128GCM(aesKey, iv, payload)
		encFlowDataWithTag := append(encFlowData, tag...)
		encAESKey, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, aesKey, nil)

		reqPayload := handler.FlowDataExchangeRequest{
			EncryptedFlowData: base64.StdEncoding.EncodeToString(encFlowDataWithTag),
			EncryptedAESKey:   base64.StdEncoding.EncodeToString(encAESKey),
			InitialVector:     base64.StdEncoding.EncodeToString(iv),
		}
		b, _ := json.Marshal(reqPayload)
		return b
	}

	// Helper for decrypting response from PerGo
	decryptResp := func(body string) []byte {
		encResp, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			t.Fatalf("failed to decode b64 response: %v", err)
		}
		ciphertext := encResp[:len(encResp)-16]
		tag := encResp[len(encResp)-16:]
		invIV := crypto.InvertIV(iv)
		dec, err := crypto.DecryptAES128GCM(aesKey, invIV, ciphertext, tag)
		if err != nil {
			t.Fatalf("failed to decrypt response: %v", err)
		}
		return dec
	}

	t.Run("E2E Meta Flow Ping Action", func(t *testing.T) {
		reqBody := createEncReq([]byte(`{"version": "3.0", "action": "ping"}`))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		decrypted := decryptResp(rec.Body.String())
		var resp struct {
			Version string            `json:"version"`
			Data    map[string]string `json:"data"`
		}
		if err := json.Unmarshal(decrypted, &resp); err != nil {
			t.Fatalf("failed to parse decrypted ping response: %v", err)
		}
		if resp.Data["status"] != "active" {
			t.Errorf("expected status 'active', got %q", resp.Data["status"])
		}
	})

	t.Run("E2E Dynamic Data Exchange with Valid Token and Inverted IV", func(t *testing.T) {
		flowToken, err := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    "5511999998888",
			FlowID:       "flow_checkout_01",
			ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		}, []byte(secret))
		if err != nil {
			t.Fatalf("GenerateFlowToken failed: %v", err)
		}

		flowReq := map[string]interface{}{
			"version":    "3.0",
			"action":     "data_exchange",
			"screen":     "CART",
			"flow_token": flowToken,
			"data": map[string]interface{}{
				"item_count": 3,
			},
		}
		reqBytes, _ := json.Marshal(flowReq)
		reqBody := createEncReq(reqBytes)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify partner webhook received request with valid HMAC signature
		mu.Lock()
		payloadCopy := append([]byte(nil), receivedPartnerPayload...)
		sigCopy := receivedPartnerSig
		mu.Unlock()

		if !webhook.VerifyPerGoSignature(payloadCopy, sigCopy, secret) {
			t.Errorf("partner webhook X-PerGo-Signature validation failed")
		}

		// Verify encrypted response returned to Meta
		decrypted := decryptResp(rec.Body.String())
		var resp struct {
			Screen string                 `json:"screen"`
			Data   map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(decrypted, &resp); err != nil {
			t.Fatalf("failed to parse decrypted response: %v", err)
		}
		if resp.Screen != "ORDER_SUCCESS" {
			t.Errorf("expected screen 'ORDER_SUCCESS', got %q", resp.Screen)
		}
		if resp.Data["confirmation_code"] != "PERGO-999" {
			t.Errorf("expected confirmation_code 'PERGO-999', got %v", resp.Data["confirmation_code"])
		}
	})

	t.Run("E2E Expired Flow Token Returns Encrypted EXPIRED Screen", func(t *testing.T) {
		expiredToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    "5511999998888",
			FlowID:       "flow_checkout_01",
			ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		}, []byte(secret))

		flowReq := map[string]interface{}{
			"version":    "3.0",
			"action":     "data_exchange",
			"screen":     "CART",
			"flow_token": expiredToken,
			"data":       map[string]interface{}{},
		}
		reqBytes, _ := json.Marshal(flowReq)
		reqBody := createEncReq(reqBytes)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		decrypted := decryptResp(rec.Body.String())
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
	})

	t.Run("E2E Partner Timeout Returns Encrypted ERROR Screen", func(t *testing.T) {
		timeoutURL := partnerServer.URL + "/timeout"
		wsTimeout := &repository.Workspace{
			ID:             wsID,
			WebhookSecret:  &secret,
			FlowWebhookURL: &timeoutURL,
		}
		wsTimeoutRepo := &mockWsStore{ws: wsTimeout}
		hTimeout := handler.NewFlowDataExchangeHandler(connRepo, wsTimeoutRepo, partnerServer.Client())

		eTimeout := echo.New()
		eTimeout.POST("/api/v1/waba/flows/data-exchange", hTimeout.HandleFlowDataExchange)

		flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    "5511999998888",
			FlowID:       "flow_checkout_01",
			ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		}, []byte(secret))

		flowReq := map[string]interface{}{
			"version":    "3.0",
			"action":     "data_exchange",
			"screen":     "CART",
			"flow_token": flowToken,
			"data":       map[string]interface{}{},
		}
		reqBytes, _ := json.Marshal(flowReq)
		reqBody := createEncReq(reqBytes)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		eTimeout.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with encrypted ERROR screen, got %d: %s", rec.Code, rec.Body.String())
		}

		decrypted := decryptResp(rec.Body.String())
		var resp struct {
			Screen string                 `json:"screen"`
			Data   map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(decrypted, &resp); err != nil {
			t.Fatalf("failed to parse decrypted response: %v", err)
		}
		if resp.Screen != "ERROR" {
			t.Errorf("expected screen 'ERROR' on partner timeout, got %q", resp.Screen)
		}
	})

	t.Run("E2E Partner 5xx Returns Encrypted ERROR Screen", func(t *testing.T) {
		errorURL := partnerServer.URL + "/500"
		wsError := &repository.Workspace{
			ID:             wsID,
			WebhookSecret:  &secret,
			FlowWebhookURL: &errorURL,
		}
		wsErrorRepo := &mockWsStore{ws: wsError}
		hError := handler.NewFlowDataExchangeHandler(connRepo, wsErrorRepo, partnerServer.Client())

		eError := echo.New()
		eError.POST("/api/v1/waba/flows/data-exchange", hError.HandleFlowDataExchange)

		flowToken, _ := crypto.GenerateFlowToken(crypto.FlowTokenPayload{
			WorkspaceID:  wsID,
			ConnectionID: connID,
			ContactID:    "5511999998888",
			FlowID:       "flow_checkout_01",
			ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		}, []byte(secret))

		flowReq := map[string]interface{}{
			"version":    "3.0",
			"action":     "data_exchange",
			"screen":     "CART",
			"flow_token": flowToken,
			"data":       map[string]interface{}{},
		}
		reqBytes, _ := json.Marshal(flowReq)
		reqBody := createEncReq(reqBytes)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange?connection_id="+connID.String(), bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		eError.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with encrypted ERROR screen, got %d: %s", rec.Code, rec.Body.String())
		}

		decrypted := decryptResp(rec.Body.String())
		var resp struct {
			Screen string                 `json:"screen"`
			Data   map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(decrypted, &resp); err != nil {
			t.Fatalf("failed to parse decrypted response: %v", err)
		}
		if resp.Screen != "ERROR" {
			t.Errorf("expected screen 'ERROR' on partner 5xx, got %q", resp.Screen)
		}
	})
}

type mockConnStore struct {
	conn *repository.Connection
}

func (m *mockConnStore) GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error) {
	if m.conn != nil && m.conn.ID == id {
		return m.conn, nil
	}
	return nil, repository.ErrConnectionNotFound
}

type mockWsStore struct {
	ws *repository.Workspace
}

func (m *mockWsStore) GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error) {
	if m.ws != nil && m.ws.ID == id {
		return m.ws, nil
	}
	return nil, repository.ErrWorkspaceNotFound
}
