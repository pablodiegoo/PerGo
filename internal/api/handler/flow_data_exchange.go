package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

// ConnectionStore defines the data access methods required for connection resolution.
type ConnectionStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error)
}

// WorkspaceStore defines the data access methods required for workspace configuration resolution.
type WorkspaceStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error)
}

// HTTPClient represents an interface for executing outbound HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// FlowDataExchangeHandler terminates Meta Flow encryption and delegates screen transitions to partner webhooks.
type FlowDataExchangeHandler struct {
	connectionsRepo ConnectionStore
	workspacesRepo  WorkspaceStore
	httpClient      HTTPClient
	timeout         time.Duration
}

// NewFlowDataExchangeHandler creates a new FlowDataExchangeHandler.
func NewFlowDataExchangeHandler(connectionsRepo ConnectionStore, workspacesRepo WorkspaceStore, httpClient ...HTTPClient) *FlowDataExchangeHandler {
	var client HTTPClient = &http.Client{Timeout: 3 * time.Second}
	if len(httpClient) > 0 && httpClient[0] != nil {
		client = httpClient[0]
	}
	return &FlowDataExchangeHandler{
		connectionsRepo: connectionsRepo,
		workspacesRepo:  workspacesRepo,
		httpClient:      client,
		timeout:         2500 * time.Millisecond,
	}
}

// FlowDataExchangeRequest represents the encrypted envelope sent by Meta to the data exchange endpoint.
type FlowDataExchangeRequest struct {
	EncryptedFlowData string `json:"encrypted_flow_data"`
	EncryptedAESKey   string `json:"encrypted_aes_key"`
	InitialVector     string `json:"initial_vector"`
}

// HandleFlowDataExchange decrypts Meta screen transition requests, validates flow tokens, delegates to flow_webhook_url, and returns encrypted responses.
func (h *FlowDataExchangeHandler) HandleFlowDataExchange(c *echo.Context) error {
	ctx := c.Request().Context()

	connectionIDStr := c.QueryParam("connection_id")
	if connectionIDStr == "" {
		if id, err := echo.PathParam[string](c, "connection_id"); err == nil && id != "" {
			connectionIDStr = id
		}
	}
	if connectionIDStr == "" {
		if id, err := echo.PathParam[string](c, "id"); err == nil && id != "" {
			connectionIDStr = id
		}
	}
	if connectionIDStr == "" {
		return c.String(http.StatusBadRequest, "missing connection_id")
	}

	connectionID, err := uuid.Parse(connectionIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection_id")
	}

	conn, err := h.connectionsRepo.GetByID(ctx, connectionID)
	if err != nil {
		return c.String(http.StatusNotFound, "connection not found")
	}

	var ws *repository.Workspace
	if h.workspacesRepo != nil && conn.WorkspaceID != uuid.Nil {
		var wsErr error
		ws, wsErr = h.workspacesRepo.GetByID(ctx, conn.WorkspaceID)
		if wsErr != nil && !errors.Is(wsErr, repository.ErrWorkspaceNotFound) {
			slog.Error("failed to retrieve workspace for flow data exchange", "error", wsErr, "workspace_id", conn.WorkspaceID)
			return c.String(http.StatusInternalServerError, "internal error")
		}
	}

	privKey, err := crypto.LoadRSAPrivateKey(conn.Credentials, nil)
	if err != nil {
		slog.Error("failed to load RSA private key for connection", "error", err, "connection_id", connectionID)
		return c.String(http.StatusInternalServerError, "internal error")
	}

	var req FlowDataExchangeRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return c.String(http.StatusBadRequest, "invalid json")
	}

	encAESKey, err := base64.StdEncoding.DecodeString(req.EncryptedAESKey)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid encrypted_aes_key")
	}

	iv, err := base64.StdEncoding.DecodeString(req.InitialVector)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid initial_vector")
	}

	encFlowData, err := base64.StdEncoding.DecodeString(req.EncryptedFlowData)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid encrypted_flow_data")
	}

	// 1. Decrypt AES key using RSA private key
	aesKey, err := crypto.DecryptRSA(privKey, encAESKey)
	if err != nil {
		slog.Error("failed to decrypt AES key", "error", err)
		return c.String(http.StatusBadRequest, "decryption failed")
	}

	// 2. Decrypt flow data with AES-128-GCM
	if len(encFlowData) < 16 {
		return c.String(http.StatusBadRequest, "flow data too short")
	}
	tagSize := 16
	ciphertext := encFlowData[:len(encFlowData)-tagSize]
	tag := encFlowData[len(encFlowData)-tagSize:]

	decryptedFlowData, err := crypto.DecryptAES128GCM(aesKey, iv, ciphertext, tag)
	if err != nil {
		slog.Error("failed to decrypt flow data", "error", err)
		return c.String(http.StatusBadRequest, "decryption failed")
	}

	// 3. Inspect decrypted payload
	var metaReq struct {
		Version   string          `json:"version"`
		Action    string          `json:"action"`
		Screen    string          `json:"screen"`
		FlowToken string          `json:"flow_token"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(decryptedFlowData, &metaReq); err != nil {
		slog.Error("failed to parse decrypted Meta flow payload", "error", err)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Invalid flow request payload.")
	}

	// Handle Meta health-check ping action
	if metaReq.Action == "ping" {
		pingResp := []byte(`{"version": "3.0", "data": {"status": "active"}}`)
		return h.encryptAndReturn(c, aesKey, iv, pingResp)
	}

	// Determine workspace signing secret for HMAC verification & outbound signature
	signingSecret := []byte(conn.WorkspaceID.String())
	if ws != nil && ws.WebhookSecret != nil && *ws.WebhookSecret != "" {
		signingSecret = []byte(*ws.WebhookSecret)
	}

	// Validate flow_token (mandatory for non-ping screen exchange)
	if metaReq.FlowToken == "" {
		slog.Warn("missing flow_token in screen exchange request", "action", metaReq.Action)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Missing flow token.")
	}

	var parseErr error
	if ws != nil && ws.WebhookSecret != nil && *ws.WebhookSecret != "" {
		_, parseErr = crypto.ParseAndValidateFlowToken(metaReq.FlowToken, []byte(*ws.WebhookSecret))
	}
	if parseErr != nil || ws == nil || ws.WebhookSecret == nil || *ws.WebhookSecret == "" {
		_, err2 := crypto.ParseAndValidateFlowToken(metaReq.FlowToken, []byte(conn.WorkspaceID.String()))
		if err2 == nil {
			parseErr = nil
		} else if parseErr == nil {
			parseErr = err2
		}
	}
	if parseErr != nil {
		if errors.Is(parseErr, crypto.ErrFlowTokenExpired) {
			slog.Warn("expired flow token received", "flow_token", metaReq.FlowToken)
			return h.returnScreen(c, aesKey, iv, "EXPIRED", "This flow has expired.")
		}
		slog.Warn("invalid flow token signature or structure", "flow_token", metaReq.FlowToken, "error", parseErr)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Invalid flow token.")
	}

	// Check if workspace has a configured flow_webhook_url
	if ws == nil || ws.FlowWebhookURL == nil || *ws.FlowWebhookURL == "" {
		slog.Warn("workspace flow_webhook_url is not configured", "workspace_id", conn.WorkspaceID)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Flow webhook URL not configured.")
	}

	// 4. Synchronously delegate decrypted request to flow_webhook_url with 2500ms timeout budget
	reqCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, *ws.FlowWebhookURL, bytes.NewReader(decryptedFlowData))
	if err != nil {
		slog.Error("failed to create downstream flow webhook request", "error", err)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Service temporarily unavailable. Please try again later.")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if len(signingSecret) > 0 {
		sigHeader := webhook.SignPayload(decryptedFlowData, signingSecret, fmt.Sprintf("%d", time.Now().Unix()))
		httpReq.Header.Set("X-PerGo-Signature", sigHeader)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("downstream flow webhook request failed or timed out", "error", err, "url", *ws.FlowWebhookURL)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Service temporarily unavailable. Please try again later.")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		slog.Error("downstream flow webhook returned 5xx server error", "status", resp.StatusCode, "url", *ws.FlowWebhookURL)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Service temporarily unavailable. Please try again later.")
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil || len(respBody) == 0 {
		slog.Error("downstream flow webhook returned empty response", "error", err)
		return h.returnScreen(c, aesKey, iv, "ERROR", "Invalid response from partner server.")
	}

	return h.encryptAndReturn(c, aesKey, iv, respBody)
}

func (h *FlowDataExchangeHandler) encryptAndReturn(c *echo.Context, aesKey, iv, payload []byte) error {
	invIV := crypto.InvertIV(iv)
	respCiphertext, respTag, err := crypto.EncryptAES128GCM(aesKey, invIV, payload)
	if err != nil {
		slog.Error("failed to encrypt response for Meta", "error", err)
		return c.String(http.StatusInternalServerError, "encryption failed")
	}

	finalEnc := append(respCiphertext, respTag...)
	encodedResp := base64.StdEncoding.EncodeToString(finalEnc)

	c.Response().Header().Set("Content-Type", "text/plain")
	return c.String(http.StatusOK, encodedResp)
}

func (h *FlowDataExchangeHandler) returnScreen(c *echo.Context, aesKey, iv []byte, screen, errorMessage string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"screen": screen,
		"data": map[string]string{
			"error_message": errorMessage,
		},
	})
	return h.encryptAndReturn(c, aesKey, iv, payload)
}
