package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// Server encapsulates the MCP Server instance and its service dependencies.
type Server struct {
	MCPServer *server.MCPServer
	SSEServer *server.SSEServer

	wsRepo         *repository.WorkspaceRepository
	connectionRepo *repository.ConnectionRepository
	contactRepo    *repository.ContactRepository
	auditRepo      *repository.AuditRepository
	ingestor       outbound.OutboundProcessor
}

// NewServer creates and configures a new PerGo MCP server.
func NewServer(
	wsRepo *repository.WorkspaceRepository,
	connectionRepo *repository.ConnectionRepository,
	contactRepo *repository.ContactRepository,
	auditRepo *repository.AuditRepository,
	ingestor outbound.OutboundProcessor,
) *Server {
	mcpSrv := server.NewMCPServer("PerGo CPaaS Gateway", "1.2.0")

	s := &Server{
		MCPServer:      mcpSrv,
		wsRepo:         wsRepo,
		connectionRepo: connectionRepo,
		contactRepo:    contactRepo,
		auditRepo:      auditRepo,
		ingestor:       ingestor,
	}

	s.registerTools()

	// Create SSE transport server mounted on base path "/api/mcp"
	s.SSEServer = server.NewSSEServer(mcpSrv, server.WithBasePath("/api/mcp"))

	return s
}

func (s *Server) registerTools() {
	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_workspaces",
		Description: "List all workspaces and their IDs in the system.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
		},
	}, s.handleListWorkspaces)

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
