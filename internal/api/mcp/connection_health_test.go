package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"go.mau.fi/whatsmeow"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

func TestConnectionHealthDiagnostic(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, enc)
	contactRepo := repository.NewContactRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "Diagnostic Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	otherWs, err := wsRepo.Create(ctx, "Other Workspace")
	if err != nil {
		t.Fatalf("failed to create other workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, otherWs.ID)
	}()

	sessionRegistry := session.NewActiveSession()
	sessionManager := session.NewManager(nil, connRepo, sessionRegistry, nil, "2.3000.1025000000", nil)
	mockCli := &mockWhatsAppClient{
		qrCh: make(chan whatsmeow.QRChannelItem, 10),
	}
	sessionManager.SetClientFactory(&mockClientFactory{client: mockCli})

	// Mock Telegram API server
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "valid-token") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true, "result": {"id": 999888, "is_bot": true, "first_name": "PerGoDiagnosticBot"}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok": false, "error_code": 401, "description": "Unauthorized"}`))
	}))
	defer telegramServer.Close()

	srv := NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		auditRepo,
		&mockOutboundProcessor{},
		apiKeyRepo,
		webhookSubRepo,
		sessionManager,
		nil,
		[]byte("diagnostic-sso-secret"),
		"http://localhost:8080",
		WithTelegramBaseURL(telegramServer.URL),
		WithHTTPClient(telegramServer.Client()),
	)

	t.Run("ToolRegisteredWithValidSchema", func(t *testing.T) {
		// Verify tool is present in MCPServer
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{}
		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error result for empty arguments, got success")
		}
	})

	t.Run("Validation_ArgumentsAndOwnership", func(t *testing.T) {
		dummyConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    otherWs.ID,
			Name:           "Other Workspace Conn",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990001",
			Status:         "active",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, dummyConn); err != nil {
			t.Fatalf("failed to create other conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, dummyConn.ID) }()

		tests := []struct {
			name        string
			args        map[string]any
			expectedErr string
		}{
			{
				name:        "missing_workspace_id",
				args:        map[string]any{"connection_id": uuid.New().String()},
				expectedErr: "missing or invalid workspace_id argument",
			},
			{
				name:        "invalid_workspace_id_uuid",
				args:        map[string]any{"workspace_id": "not-a-uuid", "connection_id": uuid.New().String()},
				expectedErr: "invalid workspace_id UUID",
			},
			{
				name:        "missing_connection_id",
				args:        map[string]any{"workspace_id": ws.ID.String()},
				expectedErr: "missing or invalid connection_id argument",
			},
			{
				name:        "invalid_connection_id_uuid",
				args:        map[string]any{"workspace_id": ws.ID.String(), "connection_id": "not-a-uuid"},
				expectedErr: "invalid connection_id UUID",
			},
			{
				name:        "workspace_not_found",
				args:        map[string]any{"workspace_id": uuid.New().String(), "connection_id": uuid.New().String()},
				expectedErr: "workspace not found",
			},
			{
				name:        "connection_not_found",
				args:        map[string]any{"workspace_id": ws.ID.String(), "connection_id": uuid.New().String()},
				expectedErr: "connection not found for workspace",
			},
			{
				name:        "connection_belongs_to_different_workspace",
				args:        map[string]any{"workspace_id": ws.ID.String(), "connection_id": dummyConn.ID.String()},
				expectedErr: "connection not found for workspace",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				req := mcp.CallToolRequest{}
				req.Params.Arguments = tc.args
				res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
				if err != nil {
					t.Fatalf("unexpected handler err: %v", err)
				}
				if !res.IsError {
					t.Fatalf("expected error result, got: %+v", res.Content)
				}
				text := extractText(t, res)
				if !strings.Contains(text, tc.expectedErr) {
					t.Errorf("expected error %q, got %q", tc.expectedErr, text)
				}
			})
		}
	})

	t.Run("WhatsApp_OperationalAndConnected", func(t *testing.T) {
		jidVal := "5511999990002@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp Connected",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990002",
			Status:         "connected",
			JID:            &jidVal,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		// Emit connected status in session manager
		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateConnected)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.Status != "healthy" {
			t.Errorf("expected status 'healthy', got %q", diag.Status)
		}
		if diag.SocketStatus != "connected" {
			t.Errorf("expected socket_status 'connected', got %q", diag.SocketStatus)
		}
		if !diag.CredentialsValid {
			t.Errorf("expected credentials_valid true, got false")
		}
		if diag.ProxyHealth == nil || diag.ProxyHealth.Configured {
			t.Errorf("expected proxy_health configured=false, got %+v", diag.ProxyHealth)
		}
		if diag.SelfHealingGuidance != "Connection is healthy and active" {
			t.Errorf("expected guidance 'Connection is healthy and active', got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WhatsApp_DisconnectedAndUnpaired", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp Unpaired",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990003",
			Status:         "pairing",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		// Emit disconnected status
		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateDisconnected)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.Status != "disconnected" {
			t.Errorf("expected status 'disconnected', got %q", diag.Status)
		}
		if diag.SocketStatus != "disconnected" {
			t.Errorf("expected socket_status 'disconnected', got %q", diag.SocketStatus)
		}
		if diag.SelfHealingGuidance != "Trigger pairing via get_connection_qr_code" {
			t.Errorf("expected guidance 'Trigger pairing via get_connection_qr_code', got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WhatsApp_Banned", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp Banned",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990004",
			Status:         "banned",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateDisconnectedBanned)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.Status != "banned" {
			t.Errorf("expected status 'banned', got %q", diag.Status)
		}
		if diag.SocketStatus != "disconnected_banned" {
			t.Errorf("expected socket_status 'disconnected_banned', got %q", diag.SocketStatus)
		}
		if diag.SelfHealingGuidance != "Session expired or banned by provider; halt reconnections and re-authenticate" {
			t.Errorf("expected guidance for banned session, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WhatsApp_ConnectingOrReconnecting", func(t *testing.T) {
		jidVal := "5511999990005@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp Reconnecting",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990005",
			Status:         "reconnecting",
			JID:            &jidVal,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateConnecting)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.Status != "degraded" {
			t.Errorf("expected status 'degraded', got %q", diag.Status)
		}
		if diag.SocketStatus != "connecting" {
			t.Errorf("expected socket_status 'connecting', got %q", diag.SocketStatus)
		}
		if !strings.Contains(diag.SelfHealingGuidance, "wait for socket handshake to complete") {
			t.Errorf("expected connecting guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("ProxyProbe_ReachableWithLocalListener", func(t *testing.T) {
		// Create local TCP listener to simulate reachable proxy host:port
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create local TCP listener: %v", err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()

		proxyURL := "http://" + ln.Addr().String()
		jidVal := "5511999990006@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp with Working Proxy",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990006",
			Status:         "connected",
			JID:            &jidVal,
			ProxyURL:       &proxyURL,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateConnected)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.ProxyHealth == nil {
			t.Fatalf("expected non-nil proxy_health")
		}
		if !diag.ProxyHealth.Configured {
			t.Errorf("expected proxy_health configured true")
		}
		if !diag.ProxyHealth.Reachable {
			t.Errorf("expected proxy_health reachable true, got error: %s", diag.ProxyHealth.Error)
		}
		if diag.ProxyHealth.LatencyMS < 0 {
			t.Errorf("expected positive latency, got %d", diag.ProxyHealth.LatencyMS)
		}
		if diag.Status != "healthy" {
			t.Errorf("expected status 'healthy', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Connection is healthy and active" {
			t.Errorf("expected healthy guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("ProxyProbe_UnreachableClosedPort", func(t *testing.T) {
		// Acquire an ephemeral port and close listener immediately
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen on ephemeral port: %v", err)
		}
		closedAddr := ln.Addr().String()
		_ = ln.Close()

		proxyURL := "http://" + closedAddr
		jidVal := "5511999990007@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp with Dead Proxy",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990007",
			Status:         "connected",
			JID:            &jidVal,
			ProxyURL:       &proxyURL,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success result, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.ProxyHealth == nil {
			t.Fatalf("expected non-nil proxy_health")
		}
		if !diag.ProxyHealth.Configured {
			t.Errorf("expected proxy_health configured true")
		}
		if diag.ProxyHealth.Reachable {
			t.Errorf("expected proxy_health reachable false")
		}
		if diag.ProxyHealth.Error == "" {
			t.Errorf("expected error message in proxy_health")
		}
		if diag.Status != "degraded" {
			t.Errorf("expected status 'degraded', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Inspect dedicated proxy host/port and firewall credentials" {
			t.Errorf("expected proxy unreachable guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("ProxyProbe_SOCKS5HandshakeSuccess", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create local TCP listener: %v", err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					buf := make([]byte, 3)
					_, _ = io.ReadFull(c, buf)
					if buf[0] == 0x05 {
						_, _ = c.Write([]byte{0x05, 0x00})
					}
				}(conn)
			}
		}()

		proxyURL := "socks5://" + ln.Addr().String()
		jidVal := "5511999990008@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp with Working SOCKS5 Proxy",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990008",
			Status:         "connected",
			JID:            &jidVal,
			ProxyURL:       &proxyURL,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		if err := sessionManager.EmitStatusEvent(ctx, ws.ID, conn.ID, "whatsapp", conn.SenderIdentity, string(session.StateConnected)); err != nil {
			t.Fatalf("failed to emit status event: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.ProxyHealth == nil || !diag.ProxyHealth.Configured || !diag.ProxyHealth.Reachable {
			t.Fatalf("expected proxy_health reachable true for socks5 handshake, got %+v", diag.ProxyHealth)
		}
		if diag.Status != "healthy" {
			t.Errorf("expected status 'healthy', got %q", diag.Status)
		}
	})

	t.Run("ProxyProbe_SOCKS5HandshakeRejected", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create local TCP listener: %v", err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					buf := make([]byte, 3)
					_, _ = io.ReadFull(c, buf)
					_, _ = c.Write([]byte{0x05, 0xFF})
				}(conn)
			}
		}()

		proxyURL := "socks5://" + ln.Addr().String()
		jidVal := "5511999990009@s.whatsapp.net"
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WhatsApp with Rejected SOCKS5 Proxy",
			Channel:        "whatsapp",
			SenderIdentity: "+5511999990009",
			Status:         "connected",
			JID:            &jidVal,
			ProxyURL:       &proxyURL,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success result, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.ProxyHealth == nil || !diag.ProxyHealth.Configured || diag.ProxyHealth.Reachable {
			t.Fatalf("expected proxy_health reachable false for rejected socks5 handshake, got %+v", diag.ProxyHealth)
		}
		if !strings.Contains(diag.ProxyHealth.Error, "socks5 handshake rejected") {
			t.Errorf("expected 'socks5 handshake rejected' in error, got %q", diag.ProxyHealth.Error)
		}
		if diag.Status != "degraded" {
			t.Errorf("expected status 'degraded', got %q", diag.Status)
		}
	})

	t.Run("ProxyProbe_TelegramChannelProbeLatencyRecorded", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create local TCP listener: %v", err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()

		tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(40 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":12345,"is_bot":true,"first_name":"TestBot"}}`))
		}))
		defer tgServer.Close()

		srvWithTG := NewServer(
			wsRepo,
			connRepo,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			[]byte("test-sso-secret"),
			"http://localhost:8080",
			WithTelegramBaseURL(tgServer.URL),
		)

		proxyURL := "http://" + ln.Addr().String()
		tgCreds, _ := json.Marshal(map[string]string{"token": "test-bot-token-12345"})
		conn := &repository.Connection{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Name:        "Telegram with Proxy and Latency",
			Channel:     "telegram",
			Status:      "connected",
			Credentials: tgCreds,
			ProxyURL:    &proxyURL,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srvWithTG.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.ProxyHealth == nil || !diag.ProxyHealth.Reachable {
			t.Fatalf("expected proxy to be reachable, got %+v", diag.ProxyHealth)
		}
		if diag.LatencyMS < 30 {
			t.Errorf("expected channel probe latency >= 30ms, got %d ms", diag.LatencyMS)
		}
	})

	t.Run("Telegram_ValidToken", func(t *testing.T) {
		validTokenCreds := `{"token": "valid-token-secret"}`
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Telegram Valid Bot",
			Channel:        "telegram",
			SenderIdentity: "@PerGoDiagnosticBot",
			Status:         "active",
			Credentials:    []byte(validTokenCreds),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if !diag.CredentialsValid {
			t.Errorf("expected credentials_valid true, got false")
		}
		if diag.Status != "healthy" {
			t.Errorf("expected status 'healthy', got %q", diag.Status)
		}
		if diag.SocketStatus != "connected" {
			t.Errorf("expected socket_status 'connected', got %q", diag.SocketStatus)
		}
		if diag.SelfHealingGuidance != "Connection is healthy and active" {
			t.Errorf("expected healthy guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("Telegram_UnauthorizedToken", func(t *testing.T) {
		invalidTokenCreds := `{"token": "wrong-secret-token"}`
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Telegram Invalid Bot",
			Channel:        "telegram",
			SenderIdentity: "@PerGoDiagnosticBot",
			Status:         "active",
			Credentials:    []byte(invalidTokenCreds),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success response with error diagnosis, got: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.CredentialsValid {
			t.Errorf("expected credentials_valid false")
		}
		if diag.Status != "banned" {
			t.Errorf("expected status 'banned', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Session expired or banned by provider; halt reconnections and re-authenticate" {
			t.Errorf("expected auth failure guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("Telegram_MissingToken", func(t *testing.T) {
		emptyCreds := `{}`
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "Telegram Missing Token Bot",
			Channel:        "telegram",
			SenderIdentity: "@PerGoDiagnosticBot",
			Status:         "active",
			Credentials:    []byte(emptyCreds),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success response with error diagnosis, got: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.CredentialsValid {
			t.Errorf("expected credentials_valid false")
		}
		if diag.Status != "banned" {
			t.Errorf("expected status 'banned', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Session expired or banned by provider; halt reconnections and re-authenticate" {
			t.Errorf("expected auth failure guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WABA_ValidCredentials", func(t *testing.T) {
		wabaCreds := `{"phone_number_id": "109876543210123", "token": "EAAG1234567890abcdefghijklmnopqrstuvwxyz"}`
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA Valid Conn",
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+5511988880001",
			Status:         "active",
			Credentials:    []byte(wabaCreds),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if !diag.CredentialsValid {
			t.Errorf("expected credentials_valid true, got false")
		}
		if diag.Status != "healthy" {
			t.Errorf("expected status 'healthy', got %q", diag.Status)
		}
		if diag.SocketStatus != "connected" {
			t.Errorf("expected socket_status 'connected', got %q", diag.SocketStatus)
		}
		if diag.SelfHealingGuidance != "Connection is healthy and active" {
			t.Errorf("expected healthy guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WABA_InvalidPhoneNumberIDStructure", func(t *testing.T) {
		wabaCreds := `{"phone_number_id": "not-numeric-id", "token": "EAAG1234567890abcdef"}`
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA Invalid ID Conn",
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+5511988880002",
			Status:         "active",
			Credentials:    []byte(wabaCreds),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success response with error diagnosis, got: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.CredentialsValid {
			t.Errorf("expected credentials_valid false")
		}
		if diag.Status != "banned" {
			t.Errorf("expected status 'banned', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Session expired or banned by provider; halt reconnections and re-authenticate" {
			t.Errorf("expected auth failure guidance, got %q", diag.SelfHealingGuidance)
		}
	})

	t.Run("WABA_MissingCredentials", func(t *testing.T) {
		conn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA Empty Conn",
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+5511988880003",
			Status:         "active",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := connRepo.Create(ctx, conn); err != nil {
			t.Fatalf("failed to create conn: %v", err)
		}
		defer func() { _ = connRepo.Delete(ctx, conn.ID) }()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": conn.ID.String(),
		}

		res, err := srv.handleDiagnoseConnectionHealth(ctx, req)
		if err != nil {
			t.Fatalf("handleDiagnoseConnectionHealth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success response with error diagnosis, got: %+v", res.Content)
		}

		var diag ConnectionHealthDiagnosticResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &diag); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if diag.CredentialsValid {
			t.Errorf("expected credentials_valid false")
		}
		if diag.Status != "banned" {
			t.Errorf("expected status 'banned', got %q", diag.Status)
		}
		if diag.SelfHealingGuidance != "Session expired or banned by provider; halt reconnections and re-authenticate" {
			t.Errorf("expected auth failure guidance, got %q", diag.SelfHealingGuidance)
		}
	})
}
