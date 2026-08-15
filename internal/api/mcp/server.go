package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/pkg/slug"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/internal/webhook"
)

// Server encapsulates the MCP Server instance and its service dependencies.
type Server struct {
	MCPServer *server.MCPServer
	SSEServer *server.SSEServer

	wsRepo            *repository.WorkspaceRepository
	connectionRepo    *repository.ConnectionRepository
	contactRepo       *repository.ContactRepository
	auditRepo         *repository.AuditRepository
	ingestor          outbound.OutboundProcessor
	apiKeyRepo        *repository.APIKeyRepository
	webhookSubRepo    *repository.WebhookSubscriptionRepository
	sessionManager    *session.Manager
	webhookDispatcher webhook.WebhookDispatcher
	ssoSecret         []byte
	externalURL       string
}

// NewServer creates and configures a new PerGo MCP server.
func NewServer(
	wsRepo *repository.WorkspaceRepository,
	connectionRepo *repository.ConnectionRepository,
	contactRepo *repository.ContactRepository,
	auditRepo *repository.AuditRepository,
	ingestor outbound.OutboundProcessor,
	apiKeyRepo *repository.APIKeyRepository,
	webhookSubRepo *repository.WebhookSubscriptionRepository,
	sessionManager *session.Manager,
	webhookDispatcher webhook.WebhookDispatcher,
	ssoSecret []byte,
	externalURL string,
) *Server {
	mcpSrv := server.NewMCPServer("PerGo CPaaS Gateway", "1.2.0")

	s := &Server{
		MCPServer:         mcpSrv,
		wsRepo:            wsRepo,
		connectionRepo:    connectionRepo,
		contactRepo:       contactRepo,
		auditRepo:         auditRepo,
		ingestor:          ingestor,
		apiKeyRepo:        apiKeyRepo,
		webhookSubRepo:    webhookSubRepo,
		sessionManager:    sessionManager,
		webhookDispatcher: webhookDispatcher,
		ssoSecret:         ssoSecret,
		externalURL:       externalURL,
	}

	s.registerTools()

	// Create SSE transport server mounted on base path "/api/mcp"
	s.SSEServer = server.NewSSEServer(mcpSrv, server.WithBasePath("/api/mcp"))

	return s
}

func (s *Server) registerTools() {
	s.MCPServer.AddTool(mcp.Tool{
		Name:        "create_workspace",
		Description: "Provision a new tenant workspace and generate its default API key and webhook secret atomically.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "The human-readable name of the workspace.",
				},
				"generate_api_key": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to generate a default API key (default: true).",
				},
				"generate_webhook_secret": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to generate a webhook signing secret (default: true).",
				},
			},
			Required: []string{"name"},
		},
	}, s.handleCreateWorkspace)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "get_workspace",
		Description: "Retrieve workspace details and configuration by workspace UUID.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleGetWorkspace)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_workspaces",
		Description: "List all workspaces and their IDs in the system.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
		},
	}, s.handleListWorkspaces)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "create_connection",
		Description: "Provision a new channel connection (whatsapp, whatsapp_cloud, telegram) for a workspace and initialize session/pairing if applicable.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "The human-readable name of the connection.",
				},
				"channel": map[string]interface{}{
					"type":        "string",
					"description": "Channel type: 'whatsapp', 'whatsapp_cloud', or 'telegram'.",
				},
				"sender_identity": map[string]interface{}{
					"type":        "string",
					"description": "Sender identifier: phone number for WhatsApp/WABA or bot username for Telegram.",
				},
				"credentials": map[string]interface{}{
					"type":        "string",
					"description": "Optional raw token or JSON credentials for WABA/Telegram.",
				},
				"proxy_url": map[string]interface{}{
					"type":        "string",
					"description": "Optional HTTP/SOCKS5 proxy URL for WhatsApp connection.",
				},
				"is_default": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether this connection should be the default for its channel (default: false).",
				},
			},
			Required: []string{"workspace_id", "name", "channel"},
		},
	}, s.handleCreateConnection)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "get_connection_status",
		Description: "Get the real-time status, health, and metadata of a communication connection.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the connection.",
				},
			},
			Required: []string{"workspace_id", "connection_id"},
		},
	}, s.handleGetConnectionStatus)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "get_connection_qr_code",
		Description: "Get active base64 PNG QR code and pairing code for a WhatsApp Web connection.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the connection.",
				},
			},
			Required: []string{"workspace_id", "connection_id"},
		},
	}, s.handleGetConnectionQRCode)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "disconnect_connection",
		Description: "Gracefully disconnect and remove a communication channel connection.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the connection.",
				},
			},
			Required: []string{"workspace_id", "connection_id"},
		},
	}, s.handleDisconnectConnection)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_connections",
		Description: "List all configured communication channel connections (WABA, Telegram, WhatsApp Web) for a workspace.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleListConnections)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "search_contacts",
		Description: "Search/list contacts in the workspace database.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Name or identifier query search string.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max results to return (default 10).",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleSearchContacts)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "send_message",
		Description: "Ingest and queue a message to be sent to a contact via a specific channel with fallbacks.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "The recipient identity/phone number.",
				},
				"channel": map[string]interface{}{
					"type":        "string",
					"description": "Primary channel to send the message (whatsapp, whatsapp_cloud, telegram).",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "The message body text.",
				},
				"media": map[string]interface{}{
					"type":        "object",
					"description": "Optional media payload.",
					"properties": map[string]interface{}{
						"media_url": map[string]interface{}{
							"type":        "string",
							"description": "Direct public URL of the media file.",
						},
						"media_type": map[string]interface{}{
							"type":        "string",
							"description": "Type of media: image, document, audio.",
						},
						"filename": map[string]interface{}{
							"type":        "string",
							"description": "Filename of the media (required for documents).",
						},
						"caption": map[string]interface{}{
							"type":        "string",
							"description": "Caption text for the media.",
						},
					},
					"required": []string{"media_url", "media_type"},
				},
				"fallback_channels": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "Optional ordered list of fallback channels.",
				},
			},
			Required: []string{"workspace_id", "to", "channel", "body"},
		},
	}, s.handleSendMessage)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "get_audit_logs",
		Description: "Query latest audit logs for a given workspace.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"event_type": map[string]interface{}{
					"type":        "string",
					"description": "Optional event type filter (e.g. 'message_ingested', 'connection_created').",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max logs to return (default 10).",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleGetAuditLogs)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "create_webhook_subscription",
		Description: "Register a new webhook subscription for a workspace with event filters and signing secret.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The destination HTTPS/HTTP webhook URL.",
				},
				"events": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "List of event names to subscribe to (e.g. 'message.received', 'message.delivered', 'connection.status', or '*' for all).",
				},
				"secret": map[string]interface{}{
					"type":        "string",
					"description": "Optional HMAC-SHA256 signing secret. Auto-generated if not provided.",
				},
				"active": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the webhook subscription is active immediately (default: true).",
				},
			},
			Required: []string{"workspace_id", "url"},
		},
	}, s.handleCreateWebhookSubscription)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_webhook_subscriptions",
		Description: "List all configured webhook subscriptions for a workspace.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleListWebhookSubscriptions)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "delete_webhook_subscription",
		Description: "Delete a webhook subscription by ID.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"subscription_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the webhook subscription to delete.",
				},
			},
			Required: []string{"workspace_id", "subscription_id"},
		},
	}, s.handleDeleteWebhookSubscription)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "rotate_webhook_secret",
		Description: "Rotate the HMAC-SHA256 signing secret for a webhook subscription.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"subscription_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the webhook subscription.",
				},
				"new_secret": map[string]interface{}{
					"type":        "string",
					"description": "Optional new HMAC-SHA256 signing secret. Auto-generated if not provided.",
				},
			},
			Required: []string{"workspace_id", "subscription_id"},
		},
	}, s.handleRotateWebhookSecret)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "test_ping_webhook",
		Description: "Send an immediate test ping payload to a webhook subscription endpoint to verify connectivity and signature validation.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"subscription_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the webhook subscription to test.",
				},
			},
			Required: []string{"workspace_id", "subscription_id"},
		},
	}, s.handleTestPingWebhook)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "generate_admin_sso_url",
		Description: "Generate a signed HMAC-SHA256 Single Sign-On (SSO) URL for seamless human operator hand-off in the Admin UI.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the target workspace to activate in the Admin UI.",
				},
				"subject": map[string]interface{}{
					"type":        "string",
					"description": "Identifier or email for the operator session (default: 'operator@crm.local').",
				},
				"redirect": map[string]interface{}{
					"type":        "string",
					"description": "Internal Admin path to redirect to after authentication (default: '/admin/connections').",
				},
				"ttl_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Validity window for the SSO token in seconds (min 5, max 120, default 60).",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleGenerateAdminSSOURL)
}

func (s *Server) handleCreateWorkspace(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil || strings.TrimSpace(name) == "" {
		return mcp.NewToolResultError("missing or invalid name argument"), nil
	}
	name = strings.TrimSpace(name)

	ws, err := s.wsRepo.Create(ctx, name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create workspace: %v", err)), nil
	}

	var rawAPIKey *string
	genKey := request.GetBool("generate_api_key", true)
	if genKey && s.apiKeyRepo != nil {
		_, key, err := s.apiKeyRepo.Create(ctx, ws.ID, "Default API Key")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate API key: %v", err)), nil
		}
		rawAPIKey = &key
	}

	var webhookSec *string
	genSecret := request.GetBool("generate_webhook_secret", true)
	if genSecret && s.wsRepo != nil {
		secret, err := s.wsRepo.GenerateWebhookSecret(ctx, ws.ID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate webhook secret: %v", err)), nil
		}
		webhookSec = &secret
	}

	type createWsResult struct {
		ID            uuid.UUID `json:"id"`
		Name          string    `json:"name"`
		APIKey        *string   `json:"api_key,omitempty"`
		WebhookSecret *string   `json:"webhook_secret,omitempty"`
		CreatedAt     time.Time `json:"created_at"`
	}

	res := createWsResult{
		ID:            ws.ID,
		Name:          ws.Name,
		APIKey:        rawAPIKey,
		WebhookSecret: webhookSec,
		CreatedAt:     ws.CreatedAt,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal workspace result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleGetWorkspace(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	ws, err := s.wsRepo.GetByID(ctx, workspaceID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get workspace: %v", err)), nil
	}

	resBytes, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal workspace: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleCreateConnection(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	// Verify workspace exists
	if _, err := s.wsRepo.GetByID(ctx, workspaceID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("workspace not found: %v", err)), nil
	}

	name, err := request.RequireString("name")
	if err != nil || strings.TrimSpace(name) == "" {
		return mcp.NewToolResultError("missing or invalid name argument"), nil
	}
	name = strings.TrimSpace(name)

	channelName, err := request.RequireString("channel")
	if err != nil || strings.TrimSpace(channelName) == "" {
		return mcp.NewToolResultError("missing or invalid channel argument"), nil
	}
	channelName = strings.ToLower(strings.TrimSpace(channelName))

	if channelName != "whatsapp" && channelName != "whatsapp_cloud" && channelName != "telegram" {
		return mcp.NewToolResultError("unsupported channel: must be 'whatsapp', 'whatsapp_cloud', or 'telegram'"), nil
	}

	senderIdentity := strings.TrimSpace(request.GetString("sender_identity", ""))
	credentialsStr := strings.TrimSpace(request.GetString("credentials", ""))
	proxyURL := strings.TrimSpace(request.GetString("proxy_url", ""))
	isDefault := request.GetBool("is_default", false)

	type createConnectionResult struct {
		ID             uuid.UUID `json:"id"`
		WorkspaceID    uuid.UUID `json:"workspace_id"`
		Name           string    `json:"name"`
		Slug           string    `json:"slug"`
		Channel        string    `json:"channel"`
		SenderIdentity string    `json:"sender_identity"`
		Status         string    `json:"status"`
		IsDefault      bool      `json:"is_default"`
		PairingMessage string    `json:"pairing_message,omitempty"`
		CreatedAt      time.Time `json:"created_at"`
	}

	if channelName == "whatsapp" {
		if senderIdentity == "" {
			return mcp.NewToolResultError("sender_identity (phone number) is required for whatsapp connection"), nil
		}

		if s.sessionManager != nil {
			connID := uuid.New()
			conn := &repository.Connection{
				ID:             connID,
				WorkspaceID:    workspaceID,
				Name:           name,
				Slug:           slug.Generate(name),
				Channel:        "whatsapp",
				SenderIdentity: senderIdentity,
				Status:         "pairing",
				IsDefault:      isDefault,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			}
			if proxyURL != "" {
				conn.ProxyURL = &proxyURL
			}
			if err := s.connectionRepo.Create(ctx, conn); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to create connection: %v", err)), nil
			}

			_, err := s.sessionManager.StartPairingSession(ctx, workspaceID, senderIdentity, &connID, proxyURL)
			if err != nil {
				_ = s.connectionRepo.Delete(ctx, connID)
				return mcp.NewToolResultError(fmt.Sprintf("failed to start pairing session: %v", err)), nil
			}

			res := createConnectionResult{
				ID:             connID,
				WorkspaceID:    workspaceID,
				Name:           name,
				Slug:           slug.Generate(name),
				Channel:        "whatsapp",
				SenderIdentity: senderIdentity,
				Status:         "pairing_started",
				IsDefault:      isDefault,
				PairingMessage: "QR code pairing session started. Use get_connection_qr_code tool to fetch QR code.",
				CreatedAt:      time.Now().UTC(),
			}

			resBytes, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal connection result: %v", err)), nil
			}
			return mcp.NewToolResultText(string(resBytes)), nil
		}
	}

	// For non-whatsapp or when sessionManager is not active
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    workspaceID,
		Name:           name,
		Slug:           slug.Generate(name),
		Channel:        channelName,
		SenderIdentity: senderIdentity,
		Status:         "active",
		IsDefault:      isDefault,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if channelName == "whatsapp" {
		conn.Status = "disconnected"
	}
	if proxyURL != "" {
		conn.ProxyURL = &proxyURL
	}
	if credentialsStr != "" {
		conn.Credentials = []byte(credentialsStr)
	}

	if err := s.connectionRepo.Create(ctx, conn); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create connection: %v", err)), nil
	}

	res := createConnectionResult{
		ID:             conn.ID,
		WorkspaceID:    conn.WorkspaceID,
		Name:           conn.Name,
		Slug:           conn.Slug,
		Channel:        conn.Channel,
		SenderIdentity: conn.SenderIdentity,
		Status:         conn.Status,
		IsDefault:      conn.IsDefault,
		CreatedAt:      conn.CreatedAt,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal connection result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleGetConnectionStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	connIDStr, err := request.RequireString("connection_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid connection_id argument"), nil
	}

	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid connection_id UUID: %v", err)), nil
	}

	conn, err := s.connectionRepo.GetByID(ctx, connID)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("connection not found for workspace"), nil
	}

	type connectionStatusResult struct {
		ID             uuid.UUID  `json:"id"`
		WorkspaceID    uuid.UUID  `json:"workspace_id"`
		Name           string     `json:"name"`
		Slug           string     `json:"slug"`
		Channel        string     `json:"channel"`
		SenderIdentity string     `json:"sender_identity"`
		Status         string     `json:"status"`
		IsDefault      bool       `json:"is_default"`
		JID            *string    `json:"jid,omitempty"`
		ConnectedSince *time.Time `json:"connected_since,omitempty"`
		LastSeen       *time.Time `json:"last_seen,omitempty"`
		LastError      string     `json:"last_error,omitempty"`
		PairingStatus  string     `json:"pairing_status,omitempty"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
	}

	res := connectionStatusResult{
		ID:             conn.ID,
		WorkspaceID:    conn.WorkspaceID,
		Name:           conn.Name,
		Slug:           conn.Slug,
		Channel:        conn.Channel,
		SenderIdentity: conn.SenderIdentity,
		Status:         conn.Status,
		IsDefault:      conn.IsDefault,
		JID:            conn.JID,
		ConnectedSince: conn.ConnectedSince,
		CreatedAt:      conn.CreatedAt,
		UpdatedAt:      conn.UpdatedAt,
	}

	if s.sessionManager != nil {
		health, err := s.sessionManager.SessionHealth(conn.ID)
		if err == nil && health != nil {
			if !health.LastSeen.IsZero() {
				res.LastSeen = &health.LastSeen
			}
			res.LastError = health.LastError
			if health.State != "" && health.State != session.StateDisconnected {
				res.Status = string(health.State)
			}
		}

		if evt, ok := s.sessionManager.GetPairingState(conn.ID.String()); ok && evt != nil {
			res.PairingStatus = evt.Status
		} else if conn.SenderIdentity != "" {
			if evt, ok := s.sessionManager.GetPairingState(conn.SenderIdentity); ok && evt != nil {
				res.PairingStatus = evt.Status
			}
		}
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal status: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleGetConnectionQRCode(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	connIDStr, err := request.RequireString("connection_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid connection_id argument"), nil
	}

	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid connection_id UUID: %v", err)), nil
	}

	conn, err := s.connectionRepo.GetByID(ctx, connID)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("connection not found for workspace"), nil
	}

	if conn.Channel != "whatsapp" {
		return mcp.NewToolResultError("QR code pairing is only supported for whatsapp connections"), nil
	}

	if s.sessionManager != nil {
		if evt, ok := s.sessionManager.GetPairingState(conn.ID.String()); ok && evt != nil {
			resBytes, err := json.MarshalIndent(evt, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal qr event: %v", err)), nil
			}
			return mcp.NewToolResultText(string(resBytes)), nil
		}
		if conn.SenderIdentity != "" {
			if evt, ok := s.sessionManager.GetPairingState(conn.SenderIdentity); ok && evt != nil {
				resBytes, err := json.MarshalIndent(evt, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to marshal qr event: %v", err)), nil
				}
				return mcp.NewToolResultText(string(resBytes)), nil
			}
		}
	}

	if conn.Status == "connected" || conn.Status == "active" {
		evt := session.QREvent{
			Status:       "paired",
			Message:      "device is connected",
			ConnectionID: &conn.ID,
		}
		resBytes, _ := json.MarshalIndent(evt, "", "  ")
		return mcp.NewToolResultText(string(resBytes)), nil
	}

	evt := session.QREvent{
		Status:       "disconnected",
		Message:      "No active pairing session",
		ConnectionID: &conn.ID,
	}
	resBytes, _ := json.MarshalIndent(evt, "", "  ")
	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleDisconnectConnection(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	connIDStr, err := request.RequireString("connection_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid connection_id argument"), nil
	}

	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid connection_id UUID: %v", err)), nil
	}

	conn, err := s.connectionRepo.GetByID(ctx, connID)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("connection not found for workspace"), nil
	}

	if s.sessionManager != nil {
		if conn.Channel == "whatsapp" {
			s.sessionManager.CancelPairing(conn.ID)
			if conn.SenderIdentity != "" {
				s.sessionManager.CancelPairingByPhone(conn.SenderIdentity)
			}
		}
		_ = s.sessionManager.EmitStatusEvent(ctx, workspaceID, conn.ID, conn.Channel, conn.SenderIdentity, "disconnected")
	}

	if err := s.connectionRepo.Delete(ctx, conn.ID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete connection: %v", err)), nil
	}

	type disconnectResult struct {
		Status       string    `json:"status"`
		ConnectionID uuid.UUID `json:"connection_id"`
		Message      string    `json:"message"`
	}

	res := disconnectResult{
		Status:       "disconnected",
		ConnectionID: conn.ID,
		Message:      "connection disconnected and removed successfully",
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleListWorkspaces(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaces, err := s.wsRepo.List(ctx, 100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list workspaces: %v", err)), nil
	}

	resBytes, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal workspaces: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleListConnections(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	connections, err := s.connectionRepo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list connections: %v", err)), nil
	}

	type connectionSummary struct {
		ID             uuid.UUID `json:"id"`
		Name           string    `json:"name"`
		Channel        string    `json:"channel"`
		SenderIdentity string    `json:"sender_identity"`
		Status         string    `json:"status"`
	}

	summaries := make([]connectionSummary, 0, len(connections))
	for _, conn := range connections {
		summaries = append(summaries, connectionSummary{
			ID:             conn.ID,
			Name:           conn.Name,
			Channel:        conn.Channel,
			SenderIdentity: conn.SenderIdentity,
			Status:         conn.Status,
		})
	}

	resBytes, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal connections: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleSearchContacts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	query := request.GetString("query", "")
	limit := request.GetInt("limit", 10)

	contacts, err := s.contactRepo.SearchContacts(ctx, workspaceID, query, uuid.Nil, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search contacts: %v", err)), nil
	}

	resBytes, err := json.MarshalIndent(contacts, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal contacts: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleSendMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	to, err := request.RequireString("to")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid to argument"), nil
	}

	channelName, err := request.RequireString("channel")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid channel argument"), nil
	}

	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid body argument"), nil
	}

	reqPayload := &domain.CreateMessageRequest{
		To:      to,
		Channel: channelName,
		Body:    body,
	}

	args := request.GetArguments()
	if rawMedia, exists := args["media"]; exists {
		mediaMap, ok := rawMedia.(map[string]interface{})
		if ok {
			mediaObj := &domain.Media{}
			if val, ok := mediaMap["media_url"].(string); ok {
				mediaObj.MediaURL = val
			}
			if val, ok := mediaMap["media_type"].(string); ok {
				mediaObj.MediaType = val
			}
			if val, ok := mediaMap["filename"].(string); ok {
				mediaObj.Filename = val
			}
			if val, ok := mediaMap["caption"].(string); ok {
				mediaObj.Caption = val
			}
			reqPayload.Media = mediaObj
		}
	}

	if rawFallbacks, exists := args["fallback_channels"]; exists {
		if list, ok := rawFallbacks.([]interface{}); ok {
			for _, item := range list {
				if str, ok := item.(string); ok {
					reqPayload.FallbackChannels = append(reqPayload.FallbackChannels, str)
				}
			}
		}
	}

	traceID := "mcp-" + uuid.New().String()

	// Ingest using outbound processor
	qMsg, err := s.ingestor.Ingest(ctx, workspaceID, traceID, reqPayload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message ingestion failed: %v", err)), nil
	}

	type sendResult struct {
		MessageID uuid.UUID `json:"message_id"`
		Status    string    `json:"status"`
		QueuedAt  time.Time `json:"queued_at"`
		TraceID   string    `json:"trace_id"`
	}

	msgID := uuid.New()

	res := sendResult{
		MessageID: msgID,
		Status:    "queued",
		QueuedAt:  qMsg.QueuedAt,
		TraceID:   traceID,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleGetAuditLogs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	filters := repository.AuditFilters{
		WorkspaceID: &workspaceID,
		Page:        1,
		PageSize:    request.GetInt("limit", 10),
	}

	filters.EventType = request.GetString("event_type", "")

	entries, _, err := s.auditRepo.ListFiltered(ctx, filters)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to query audit logs: %v", err)), nil
	}

	type auditSummary struct {
		ID        uuid.UUID `json:"id"`
		TraceID   string    `json:"trace_id"`
		EventType string    `json:"event_type"`
		Payload   string    `json:"payload"`
		CreatedAt time.Time `json:"created_at"`
	}

	summaries := make([]auditSummary, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, auditSummary{
			ID:        entry.ID,
			TraceID:   entry.TraceID,
			EventType: entry.EventType,
			Payload:   string(entry.Payload),
			CreatedAt: entry.CreatedAt,
		})
	}

	resBytes, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal audit logs: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleCreateWebhookSubscription(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	if s.wsRepo != nil {
		if _, err := s.wsRepo.GetByID(ctx, workspaceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("workspace not found: %v", err)), nil
		}
	}

	urlStr, err := request.RequireString("url")
	if err != nil || strings.TrimSpace(urlStr) == "" {
		return mcp.NewToolResultError("missing or invalid url argument"), nil
	}
	urlStr = strings.TrimSpace(urlStr)

	if err := netpolicy.ValidateURL(urlStr); err != nil {
		if errors.Is(err, netpolicy.ErrRestrictedIP) {
			return mcp.NewToolResultError("destination URL blocked by SSRF netpolicy (private/loopback IPs not allowed)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("invalid webhook URL: %v", err)), nil
	}

	var events []string
	args := request.GetArguments()
	if rawEvents, exists := args["events"]; exists {
		if list, ok := rawEvents.([]interface{}); ok {
			for _, item := range list {
				if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
					events = append(events, strings.TrimSpace(str))
				}
			}
		}
	}
	if len(events) == 0 {
		if rawEventTypes, exists := args["event_types"]; exists {
			if list, ok := rawEventTypes.([]interface{}); ok {
				for _, item := range list {
					if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
						events = append(events, strings.TrimSpace(str))
					}
				}
			}
		}
	}
	if len(events) == 0 {
		events = []string{"*"}
	}

	secretStr := strings.TrimSpace(request.GetString("secret", ""))
	if secretStr == "" {
		randomSecret, err := generateRandomHexSecret(32)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate signing secret: %v", err)), nil
		}
		secretStr = randomSecret
	}

	active := request.GetBool("active", true)

	sub, err := s.webhookSubRepo.Create(ctx, workspaceID, urlStr, events, []byte(secretStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create webhook subscription: %v", err)), nil
	}

	if !active {
		if err := s.webhookSubRepo.Update(ctx, sub.ID, sub.URL, sub.EventTypes, false, nil); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update subscription active status: %v", err)), nil
		}
		sub.Active = false
	}

	type createSubResult struct {
		ID          uuid.UUID `json:"id"`
		WorkspaceID uuid.UUID `json:"workspace_id"`
		URL         string    `json:"url"`
		Events      []string  `json:"events"`
		Secret      string    `json:"secret"`
		IsActive    bool      `json:"is_active"`
		CreatedAt   time.Time `json:"created_at"`
	}

	res := createSubResult{
		ID:          sub.ID,
		WorkspaceID: sub.WorkspaceID,
		URL:         sub.URL,
		Events:      sub.EventTypes,
		Secret:      secretStr,
		IsActive:    sub.Active,
		CreatedAt:   sub.CreatedAt,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal subscription result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleListWebhookSubscriptions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	subs, err := s.webhookSubRepo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list webhook subscriptions: %v", err)), nil
	}

	type subSummary struct {
		ID          uuid.UUID `json:"id"`
		WorkspaceID uuid.UUID `json:"workspace_id"`
		URL         string    `json:"url"`
		Events      []string  `json:"events"`
		IsActive    bool      `json:"is_active"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	summaries := make([]subSummary, 0, len(subs))
	for _, sub := range subs {
		summaries = append(summaries, subSummary{
			ID:          sub.ID,
			WorkspaceID: sub.WorkspaceID,
			URL:         sub.URL,
			Events:      sub.EventTypes,
			IsActive:    sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		})
	}

	resBytes, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal subscriptions: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleDeleteWebhookSubscription(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	subIDStr, err := request.RequireString("subscription_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid subscription_id argument"), nil
	}

	subID, err := uuid.Parse(subIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid subscription_id UUID: %v", err)), nil
	}

	sub, err := s.webhookSubRepo.Get(ctx, subID)
	if err != nil || sub == nil || sub.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("webhook subscription not found for workspace"), nil
	}

	if err := s.webhookSubRepo.Delete(ctx, subID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete webhook subscription: %v", err)), nil
	}

	type deleteSubResult struct {
		Status         string    `json:"status"`
		SubscriptionID uuid.UUID `json:"subscription_id"`
		Message        string    `json:"message"`
	}

	res := deleteSubResult{
		Status:         "deleted",
		SubscriptionID: subID,
		Message:        "webhook subscription deleted successfully",
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleRotateWebhookSecret(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	subIDStr, err := request.RequireString("subscription_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid subscription_id argument"), nil
	}

	subID, err := uuid.Parse(subIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid subscription_id UUID: %v", err)), nil
	}

	sub, err := s.webhookSubRepo.Get(ctx, subID)
	if err != nil || sub == nil || sub.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("webhook subscription not found for workspace"), nil
	}

	newSecret := strings.TrimSpace(request.GetString("new_secret", ""))
	if newSecret == "" {
		randomSecret, err := generateRandomHexSecret(32)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate signing secret: %v", err)), nil
		}
		newSecret = randomSecret
	}

	if err := s.webhookSubRepo.Update(ctx, subID, sub.URL, sub.EventTypes, sub.Active, []byte(newSecret)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to rotate webhook secret: %v", err)), nil
	}

	type rotateResult struct {
		SubscriptionID uuid.UUID `json:"subscription_id"`
		WorkspaceID    uuid.UUID `json:"workspace_id"`
		URL            string    `json:"url"`
		Secret         string    `json:"secret"`
		Message        string    `json:"message"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	res := rotateResult{
		SubscriptionID: sub.ID,
		WorkspaceID:    sub.WorkspaceID,
		URL:            sub.URL,
		Secret:         newSecret,
		Message:        "webhook signing secret rotated successfully",
		UpdatedAt:      time.Now().UTC(),
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleTestPingWebhook(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	subIDStr, err := request.RequireString("subscription_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid subscription_id argument"), nil
	}

	subID, err := uuid.Parse(subIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid subscription_id UUID: %v", err)), nil
	}

	sub, err := s.webhookSubRepo.Get(ctx, subID)
	if err != nil || sub == nil || sub.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("webhook subscription not found for workspace"), nil
	}

	pingPayload, err := json.Marshal(map[string]any{
		"event":           "ping",
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
		"workspace_id":    sub.WorkspaceID.String(),
		"subscription_id": sub.ID.String(),
		"message":         "PerGo Webhook Ping Test via MCP",
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize ping payload: %v", err)), nil
	}

	var signature string
	if len(sub.Secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature = webhook.SignPayload(pingPayload, sub.Secret, timestamp)
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(pingPayload))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create ping request: %v", err)), nil
	}

	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-PerGo-Signature", signature)
	}
	req.Header.Set("X-Trace-ID", "mcp-ping-"+uuid.New().String()[:8])
	req.Header.Set("X-PerGo-Simulated", "true")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, reqErr := client.Do(req)
	latency := time.Since(start).Milliseconds()

	type pingResult struct {
		SubscriptionID uuid.UUID `json:"subscription_id"`
		URL            string    `json:"url"`
		StatusCode     int       `json:"status_code"`
		LatencyMS      int64     `json:"latency_ms"`
		Success        bool      `json:"success"`
		ResponseBody   string    `json:"response_body,omitempty"`
		Error          string    `json:"error,omitempty"`
	}

	res := pingResult{
		SubscriptionID: sub.ID,
		URL:            sub.URL,
		LatencyMS:      latency,
	}

	if reqErr != nil {
		res.Success = false
		res.Error = fmt.Sprintf("connection error: %v", reqErr)
	} else {
		defer resp.Body.Close()
		res.StatusCode = resp.StatusCode
		res.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if len(bodyBytes) > 0 {
			res.ResponseBody = string(bodyBytes)
		}
		if !res.Success {
			res.Error = fmt.Sprintf("HTTP %s", resp.Status)
		}
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal ping result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleGenerateAdminSSOURL(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	if s.wsRepo != nil {
		if _, err := s.wsRepo.GetByID(ctx, workspaceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("workspace not found: %v", err)), nil
		}
	}

	subject := strings.TrimSpace(request.GetString("subject", "operator@crm.local"))
	if subject == "" {
		subject = "operator@crm.local"
	}

	redirect := admin.SanitizeRedirect(request.GetString("redirect", "/admin/connections"))
	ttl := request.GetInt("ttl_seconds", 60)
	if ttl < 5 {
		ttl = 5
	}
	if ttl > 120 {
		ttl = 120
	}

	claims := admin.SSOClaims{
		Sub:         subject,
		WorkspaceID: workspaceID.String(),
		Role:        "admin",
		Iat:         time.Now().Unix(),
		Exp:         time.Now().Unix() + int64(ttl),
	}

	token, err := admin.GenerateSSOToken(claims, s.ssoSecret)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate sso token: %v", err)), nil
	}

	baseURL := strings.TrimRight(s.externalURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	ssoURL := fmt.Sprintf("%s/admin/sso?token=%s&redirect=%s", baseURL, token, url.QueryEscape(redirect))

	type ssoResult struct {
		SSOURL      string    `json:"sso_url"`
		Token       string    `json:"token"`
		WorkspaceID uuid.UUID `json:"workspace_id"`
		Subject     string    `json:"subject"`
		Redirect    string    `json:"redirect"`
		ExpiresAt   time.Time `json:"expires_at"`
		TTLSeconds  int       `json:"ttl_seconds"`
	}

	res := ssoResult{
		SSOURL:      ssoURL,
		Token:       token,
		WorkspaceID: workspaceID,
		Subject:     subject,
		Redirect:    redirect,
		ExpiresAt:   time.Unix(claims.Exp, 0).UTC(),
		TTLSeconds:  ttl,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal sso result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func generateRandomHexSecret(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
