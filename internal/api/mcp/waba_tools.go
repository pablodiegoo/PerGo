package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

func resolveContactPhone(c *domain.Contact) string {
	if c == nil {
		return ""
	}
	for _, id := range c.Identities {
		if id.Channel == "whatsapp_cloud" {
			return id.SenderIdentity
		}
	}
	for _, id := range c.Identities {
		if id.Channel == "whatsapp" {
			return id.SenderIdentity
		}
	}
	if len(c.Identities) > 0 {
		return c.Identities[0].SenderIdentity
	}
	if c.Attributes != nil {
		if p, ok := c.Attributes["phone"]; ok && p != "" {
			return p
		}
	}
	return ""
}

func (s *Server) registerWABATools() {
	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_accounts",
		Description: "List official Meta WhatsApp Cloud API (WABA) connections in a workspace, including connection IDs, names, phone number IDs, display identities, and operational status.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the tenant workspace.",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleWABAAccounts)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_list_chats",
		Description: "List Meta WhatsApp Cloud (WABA) conversation threads with 24-hour customer service window status (service_window_is_open, expiration time, hours remaining).",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of a specific WABA connection to filter chats.",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by chat status: 'open', 'closed', or empty for all.",
				},
				"unread": map[string]interface{}{
					"type":        "boolean",
					"description": "Filter by unread state: true for unread only, false for read only.",
				},
				"phone": map[string]interface{}{
					"type":        "string",
					"description": "Filter by contact phone number substring.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of chats to return (default: 20, max: 100).",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of chats to skip for pagination (default: 0).",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleWABAListChats)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_templates",
		Description: "List approved Meta WhatsApp message templates (HSM) available in the workspace or for a specific connection, including category, language, and expected variable counts.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional connection UUID to filter templates.",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by template status (default: 'APPROVED'). Use 'ALL' for any status.",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Optional category filter: 'MARKETING', 'UTILITY', or 'AUTHENTICATION'.",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleWABATemplates)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_template_details",
		Description: "Retrieve comprehensive details and component definitions for a specific approved Meta WhatsApp template, including component structure and variable placeholders.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"template_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the template in the database (optional if name, language, and connection_id are provided).",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "Connection UUID (required if template_id is omitted).",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Template name in Meta (required if template_id is omitted).",
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Template language code (e.g. 'pt_BR', 'en_US', required if template_id is omitted).",
				},
			},
			Required: []string{"workspace_id"},
		},
	}, s.handleWABATemplateDetails)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_chat_send_message",
		Description: "Send a freeform text message in a WABA chat. Strictly enforces the Meta 24-hour customer service window: if the window is closed, REJECTS with an explicit error instructing the caller to use waba_chat_send_template.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The freeform text message body to send.",
				},
				"sender_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional human or AI agent display name.",
				},
			},
			Required: []string{"workspace_id", "chat_id", "message"},
		},
	}, s.handleWABAChatSendMessage)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "waba_chat_send_template",
		Description: "Send an approved Meta WhatsApp HSM template message to a contact in a chat thread. Allowed even when the 24-hour customer service window is closed to re-open customer communication.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread.",
				},
				"template_name": map[string]interface{}{
					"type":        "string",
					"description": "The registered name of the Meta approved template.",
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Language code of the template (e.g. 'pt_BR', 'en_US').",
				},
				"components": map[string]interface{}{
					"type":        "array",
					"description": "Optional structured array of template components with parameters.",
				},
				"parameters": map[string]interface{}{
					"type":        "object",
					"description": "Optional map of variable parameters (e.g. {'body': ['John', '12345']} or {'1': 'John', '2': '12345'}).",
				},
				"sender_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional agent display name.",
				},
			},
			Required: []string{"workspace_id", "chat_id", "template_name", "language"},
		},
	}, s.handleWABAChatSendTemplate)
}

// WABAAccountDTO summarizes a Meta WhatsApp Cloud connection.
type WABAAccountDTO struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Channel        string    `json:"channel"`
	SenderIdentity string    `json:"sender_identity"`
	PhoneNumberID  string    `json:"phone_number_id,omitempty"`
	WABAAccountID  string    `json:"waba_account_id,omitempty"`
	Status         string    `json:"status"`
	IsDefault      bool      `json:"is_default"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Server) handleWABAAccounts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.connectionRepo == nil {
		return mcp.NewToolResultError("connection repository is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	conns, err := s.connectionRepo.ListByWorkspace(ctx, wsID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list connections: %v", err)), nil
	}

	var wabaAccounts []WABAAccountDTO
	for _, c := range conns {
		if c.Channel != "whatsapp_cloud" {
			continue
		}

		dto := WABAAccountDTO{
			ID:             c.ID,
			WorkspaceID:    c.WorkspaceID,
			Name:           c.Name,
			Slug:           c.Slug,
			Channel:        c.Channel,
			SenderIdentity: c.SenderIdentity,
			Status:         c.Status,
			IsDefault:      c.IsDefault,
			CreatedAt:      c.CreatedAt,
		}

		if len(c.Credentials) > 0 {
			var creds struct {
				PhoneNumberID string `json:"phone_number_id"`
				WABAAccountID string `json:"waba_account_id"`
				WABAID        string `json:"waba_id"`
			}
			if err := json.Unmarshal(c.Credentials, &creds); err == nil {
				dto.PhoneNumberID = creds.PhoneNumberID
				if creds.WABAAccountID != "" {
					dto.WABAAccountID = creds.WABAAccountID
				} else {
					dto.WABAAccountID = creds.WABAID
				}
			}
		}

		wabaAccounts = append(wabaAccounts, dto)
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"accounts": wabaAccounts,
		"count":    len(wabaAccounts),
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// WABAChatSummaryDTO wraps a chat summary with service window compliance data.
type WABAChatSummaryDTO struct {
	ChatSummaryDTO
	ServiceWindowIsOpen       bool       `json:"service_window_is_open"`
	ServiceWindowExpiresAt    *time.Time `json:"service_window_expires_at,omitempty"`
	ServiceWindowRemainingSec int64      `json:"service_window_remaining_sec,omitempty"`
	ServiceWindowStatusBadge  string     `json:"service_window_status_badge"`
}

func (s *Server) handleWABAListChats(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	var connIDFilter *uuid.UUID
	connIDStr := strings.TrimSpace(request.GetString("connection_id", ""))
	if connIDStr != "" {
		parsed, cErr := uuid.Parse(connIDStr)
		if cErr != nil {
			return mcp.NewToolResultError("invalid connection_id: must be a valid UUID"), nil
		}
		connIDFilter = &parsed
	}

	status := strings.TrimSpace(request.GetString("status", ""))
	if status == "all" {
		status = ""
	}

	var unreadPtr *bool
	args := request.GetArguments()
	if unreadVal, ok := args["unread"]; ok && unreadVal != nil {
		if b, ok := unreadVal.(bool); ok {
			unreadPtr = &b
		}
	}

	phone := strings.TrimSpace(request.GetString("phone", ""))
	limit := request.GetInt("limit", 20)
	offset := request.GetInt("offset", 0)

	filter := domain.ChatFilter{
		Status: status,
		Unread: unreadPtr,
		Phone:  phone,
	}

	chats, err := s.chatRepo.ListChats(ctx, wsID, filter, limit, offset)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list chats: %v", err)), nil
	}

	// Cache WABA connections to filter relevant chats
	wabaConnMap := make(map[uuid.UUID]bool)
	if s.connectionRepo != nil {
		conns, cErr := s.connectionRepo.ListByWorkspace(ctx, wsID)
		if cErr == nil {
			for _, conn := range conns {
				if conn.Channel == "whatsapp_cloud" {
					wabaConnMap[conn.ID] = true
				}
			}
		}
	}

	now := time.Now().UTC()
	var results []WABAChatSummaryDTO

	for _, c := range chats {
		// Filter out non-WABA chats
		isWABA := false
		if c.ConnectionID != nil && wabaConnMap[*c.ConnectionID] {
			isWABA = true
		} else if c.ServiceWindowExpiresAt != nil {
			isWABA = true
		} else if connIDFilter != nil && c.ConnectionID != nil && *c.ConnectionID == *connIDFilter {
			isWABA = true
		}

		if !isWABA {
			continue
		}

		if connIDFilter != nil && (c.ConnectionID == nil || *c.ConnectionID != *connIDFilter) {
			continue
		}

		isOpen := c.IsServiceWindowOpen()
		var remSec int64
		var badge string

		if isOpen && c.ServiceWindowExpiresAt != nil {
			remSec = int64(c.ServiceWindowExpiresAt.Sub(now).Seconds())
			hours := remSec / 3600
			mins := (remSec % 3600) / 60
			if hours < 4 {
				badge = fmt.Sprintf("🟡 %dh %02dm remaining", hours, mins)
			} else {
				badge = fmt.Sprintf("🟢 %dh %02dm remaining", hours, mins)
			}
		} else {
			badge = "🔴 Expired"
		}

		dto := WABAChatSummaryDTO{
			ChatSummaryDTO:            ChatSummaryDTO{Chat: c},
			ServiceWindowIsOpen:       isOpen,
			ServiceWindowExpiresAt:    c.ServiceWindowExpiresAt,
			ServiceWindowRemainingSec: remSec,
			ServiceWindowStatusBadge:  badge,
		}

		if s.contactRepo != nil {
			if contact, cErr := s.contactRepo.GetByID(ctx, wsID, c.ContactID); cErr == nil && contact != nil {
				dto.ContactName = contact.Name
				for _, id := range contact.Identities {
					dto.Identities = append(dto.Identities, fmt.Sprintf("%s:%s", id.Channel, id.SenderIdentity))
					if dto.ContactPhone == "" && (id.Channel == "whatsapp" || id.Channel == "whatsapp_cloud") {
						dto.ContactPhone = id.SenderIdentity
					}
				}
			}
		}

		results = append(results, dto)
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"chats":  results,
		"count":  len(results),
		"limit":  limit,
		"offset": offset,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// WABATemplateSummaryDTO summarizes an approved Meta template.
type WABATemplateSummaryDTO struct {
	ID                uuid.UUID      `json:"id"`
	WorkspaceID       uuid.UUID      `json:"workspace_id"`
	ConnectionID      uuid.UUID      `json:"connection_id"`
	MetaTemplateID    string         `json:"meta_template_id"`
	Name              string         `json:"name"`
	Language          string         `json:"language"`
	Status            string         `json:"status"`
	Category          string         `json:"category"`
	QualityScore      *string        `json:"quality_score,omitempty"`
	ExpectedVariables map[string]int `json:"expected_variables"`
	CreatedAt         time.Time      `json:"created_at"`
}

func (s *Server) handleWABATemplates(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.wabaTemplateRepo == nil {
		return mcp.NewToolResultError("WABA template repository is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	var connIDFilter *uuid.UUID
	connIDStr := strings.TrimSpace(request.GetString("connection_id", ""))
	if connIDStr != "" {
		parsed, cErr := uuid.Parse(connIDStr)
		if cErr != nil {
			return mcp.NewToolResultError("invalid connection_id: must be a valid UUID"), nil
		}
		connIDFilter = &parsed
	}

	statusFilter := strings.TrimSpace(request.GetString("status", "APPROVED"))
	categoryFilter := strings.TrimSpace(request.GetString("category", ""))

	var rawTemplates []repository.WABATemplate
	if connIDFilter != nil {
		rawTemplates, err = s.wabaTemplateRepo.ListByConnection(ctx, *connIDFilter)
	} else {
		rawTemplates, err = s.wabaTemplateRepo.ListByWorkspace(ctx, wsID)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list WABA templates: %v", err)), nil
	}

	var results []WABATemplateSummaryDTO
	for _, tmpl := range rawTemplates {
		if tmpl.WorkspaceID != wsID {
			continue
		}
		if statusFilter != "" && statusFilter != "ALL" && !strings.EqualFold(tmpl.Status, statusFilter) {
			continue
		}
		if categoryFilter != "" && !strings.EqualFold(tmpl.Category, categoryFilter) {
			continue
		}

		vars, _ := outbound.CountTemplateVariables(tmpl.Components)
		if vars == nil {
			vars = make(map[string]int)
		}

		results = append(results, WABATemplateSummaryDTO{
			ID:                tmpl.ID,
			WorkspaceID:       tmpl.WorkspaceID,
			ConnectionID:      tmpl.ConnectionID,
			MetaTemplateID:    tmpl.MetaTemplateID,
			Name:              tmpl.Name,
			Language:          tmpl.Language,
			Status:            tmpl.Status,
			Category:          tmpl.Category,
			QualityScore:      tmpl.QualityScore,
			ExpectedVariables: vars,
			CreatedAt:         tmpl.CreatedAt,
		})
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"templates": results,
		"count":     len(results),
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWABATemplateDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.wabaTemplateRepo == nil {
		return mcp.NewToolResultError("WABA template repository is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	templateIDStr := strings.TrimSpace(request.GetString("template_id", ""))
	name := strings.TrimSpace(request.GetString("name", ""))
	language := strings.TrimSpace(request.GetString("language", ""))
	connIDStr := strings.TrimSpace(request.GetString("connection_id", ""))

	var tmpl *repository.WABATemplate
	if templateIDStr != "" {
		tmplID, pErr := uuid.Parse(templateIDStr)
		if pErr != nil {
			return mcp.NewToolResultError("invalid template_id: must be a valid UUID"), nil
		}
		tmpl, err = s.wabaTemplateRepo.GetByID(ctx, tmplID)
	} else if name != "" && language != "" && connIDStr != "" {
		connID, pErr := uuid.Parse(connIDStr)
		if pErr != nil {
			return mcp.NewToolResultError("invalid connection_id: must be a valid UUID"), nil
		}
		tmpl, err = s.wabaTemplateRepo.GetByNameAndLanguage(ctx, connID, name, language)
	} else {
		return mcp.NewToolResultError("must provide either template_id OR (connection_id, name, and language)"), nil
	}

	if err != nil {
		if errors.Is(err, repository.ErrTemplateNotFound) || err.Error() == "no rows in result set" {
			return mcp.NewToolResultError("template not found"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get template: %v", err)), nil
	}
	if tmpl.WorkspaceID != wsID {
		return mcp.NewToolResultError("template not found in specified workspace"), nil
	}

	expectedVars, _ := outbound.CountTemplateVariables(tmpl.Components)
	if expectedVars == nil {
		expectedVars = make(map[string]int)
	}

	var parsedComponents []interface{}
	if len(tmpl.Components) > 0 {
		_ = json.Unmarshal(tmpl.Components, &parsedComponents)
	}

	response := map[string]interface{}{
		"template":           tmpl,
		"parsed_components":  parsedComponents,
		"expected_variables": expectedVars,
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWABAChatSendMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}
	if s.ingestor == nil {
		return mcp.NewToolResultError("outbound ingestor is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	messageText, err := request.RequireString("message")
	if err != nil || strings.TrimSpace(messageText) == "" {
		return mcp.NewToolResultError("missing or empty message parameter"), nil
	}
	messageText = strings.TrimSpace(messageText)
	senderName := strings.TrimSpace(request.GetString("sender_name", "AI Assistant"))

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}

	// 24-HOUR CUSTOMER SERVICE WINDOW ENFORCEMENT:
	// Freeform text messages are strictly prohibited when the service window is closed.
	if !chat.IsServiceWindowOpen() {
		return mcp.NewToolResultError("Meta WhatsApp 24-hour customer service window is closed for this chat. Freeform text messages cannot be sent. You must use 'waba_chat_send_template' to initiate contact with an approved HSM template."), nil
	}

	// Resolve contact phone
	if s.contactRepo == nil {
		return mcp.NewToolResultError("contact repository is not configured on this server"), nil
	}
	contact, err := s.contactRepo.GetByID(ctx, wsID, chat.ContactID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get contact: %v", err)), nil
	}

	recipientPhone := resolveContactPhone(contact)
	if recipientPhone == "" {
		return mcp.NewToolResultError("contact has no phone number or whatsapp_cloud identity"), nil
	}

	// Resolve sender identity
	var senderIdentity string
	if s.connectionRepo != nil && chat.ConnectionID != nil {
		if conn, cErr := s.connectionRepo.GetByID(ctx, *chat.ConnectionID); cErr == nil && conn != nil {
			senderIdentity = conn.SenderIdentity
		}
	}

	traceID := fmt.Sprintf("mcp-waba-%s", uuid.New().String())
	req := &domain.CreateMessageRequest{
		To:       recipientPhone,
		Channel:  "whatsapp_cloud",
		From:     senderIdentity,
		Body:     messageText,
		Type:     "text",
		Metadata: map[string]string{
			"source":      "mcp_gateway",
			"sender_type": string(domain.SenderTypeAIAgent),
			"sender_name": senderName,
		},
	}

	qMsg, err := s.ingestor.Ingest(ctx, wsID, traceID, req)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to dispatch message: %v", err)), nil
	}

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":         chat.ID.String(),
			"to":              recipientPhone,
			"channel":         "whatsapp_cloud",
			"sender_identity": senderIdentity,
			"body":            messageText,
			"sender_name":     senderName,
			"sender_type":     string(domain.SenderTypeAIAgent),
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "chat.message.sent",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success":                true,
		"chat_id":                chat.ID,
		"trace_id":               traceID,
		"to":                     recipientPhone,
		"body":                   messageText,
		"service_window_is_open": true,
		"queued_at":              qMsg.QueuedAt,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWABAChatSendTemplate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}
	if s.ingestor == nil {
		return mcp.NewToolResultError("outbound ingestor is not configured on this server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
	}

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	templateName, err := request.RequireString("template_name")
	if err != nil || strings.TrimSpace(templateName) == "" {
		return mcp.NewToolResultError("missing template_name parameter"), nil
	}
	templateName = strings.TrimSpace(templateName)

	language, err := request.RequireString("language")
	if err != nil || strings.TrimSpace(language) == "" {
		return mcp.NewToolResultError("missing language parameter"), nil
	}
	language = strings.TrimSpace(language)

	senderName := strings.TrimSpace(request.GetString("sender_name", "AI Assistant"))

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}

	// Resolve contact phone
	if s.contactRepo == nil {
		return mcp.NewToolResultError("contact repository is not configured on this server"), nil
	}
	contact, err := s.contactRepo.GetByID(ctx, wsID, chat.ContactID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get contact: %v", err)), nil
	}

	recipientPhone := resolveContactPhone(contact)
	if recipientPhone == "" {
		return mcp.NewToolResultError("contact has no phone number or whatsapp_cloud identity"), nil
	}

	// Resolve connection
	var conn *repository.Connection
	if s.connectionRepo != nil {
		if chat.ConnectionID != nil {
			conn, _ = s.connectionRepo.GetByID(ctx, *chat.ConnectionID)
		}
		if conn == nil {
			conns, _ := s.connectionRepo.ListByWorkspace(ctx, wsID)
			for _, c := range conns {
				if c.Channel == "whatsapp_cloud" && (c.Status == "active" || c.Status == "connected" || c.IsDefault) {
					conn = c
					break
				}
			}
		}
	}

	var senderIdentity string
	if conn != nil {
		senderIdentity = conn.SenderIdentity
	}

	// Verify template approval if template repo is available
	if s.wabaTemplateRepo != nil && conn != nil {
		tmpl, tErr := s.wabaTemplateRepo.GetByNameAndLanguage(ctx, conn.ID, templateName, language)
		if tErr != nil {
			if errors.Is(tErr, repository.ErrTemplateNotFound) || tErr.Error() == "no rows in result set" {
				return mcp.NewToolResultError(fmt.Sprintf("template '%s' (%s) not found for connection", templateName, language)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to lookup template: %v", tErr)), nil
		}
		if !strings.EqualFold(tmpl.Status, "APPROVED") {
			return mcp.NewToolResultError(fmt.Sprintf("template '%s' is not APPROVED (current status: %s)", templateName, tmpl.Status)), nil
		}
	}

	// Parse template components and parameters
	args := request.GetArguments()
	components, err := parseTemplateComponents(args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid template parameters: %v", err)), nil
	}

	traceID := fmt.Sprintf("mcp-waba-tmpl-%s", uuid.New().String())
	req := &domain.CreateMessageRequest{
		To:           recipientPhone,
		Channel:      "whatsapp_cloud",
		From:         senderIdentity,
		TemplateName: templateName,
		Language:     language,
		Components:   components,
		Type:         "template",
		Metadata: map[string]string{
			"source":      "mcp_gateway",
			"sender_type": string(domain.SenderTypeAIAgent),
			"sender_name": senderName,
		},
	}

	qMsg, err := s.ingestor.Ingest(ctx, wsID, traceID, req)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to dispatch template message: %v", err)), nil
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success":       true,
		"chat_id":       chat.ID,
		"trace_id":      traceID,
		"template_name": templateName,
		"language":      language,
		"to":            recipientPhone,
		"queued_at":     qMsg.QueuedAt,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// parseTemplateComponents converts flexible arguments (components array or parameters map/array)
// into the standard []domain.TemplateComponent required by PerGo outbound processor.
func parseTemplateComponents(args map[string]any) ([]domain.TemplateComponent, error) {
	// Case 1: "components" provided as array of component objects
	if compsVal, ok := args["components"]; ok && compsVal != nil {
		bytes, err := json.Marshal(compsVal)
		if err != nil {
			return nil, err
		}
		var comps []domain.TemplateComponent
		if err := json.Unmarshal(bytes, &comps); err == nil && len(comps) > 0 {
			// Normalize parameters in each component
			for i := range comps {
				if normParams, err := outbound.NormalizeTemplateParams(comps[i].Parameters); err == nil && len(normParams) > 0 {
					comps[i].Parameters = normParams
				}
			}
			return comps, nil
		}
	}

	// Case 2: "parameters" provided as map or array
	if paramsVal, ok := args["parameters"]; ok && paramsVal != nil {
		switch p := paramsVal.(type) {
		case map[string]interface{}:
			// Check if keys are component types ("body", "header") or positional ("1", "2")
			hasNamedComponents := false
			for k := range p {
				lk := strings.ToLower(k)
				if lk == "body" || lk == "header" || lk == "buttons" {
					hasNamedComponents = true
					break
				}
			}

			if hasNamedComponents {
				var comps []domain.TemplateComponent
				for compType, val := range p {
					normParams, err := outbound.NormalizeTemplateParams(val)
					if err != nil {
						return nil, err
					}
					comps = append(comps, domain.TemplateComponent{
						Type:       strings.ToLower(compType),
						Parameters: normParams,
					})
				}
				return comps, nil
			}

			// Positional map like {"1": "Val1", "2": "Val2"}
			normParams, err := outbound.NormalizeTemplateParams(p)
			if err != nil {
				return nil, err
			}
			return []domain.TemplateComponent{
				{
					Type:       "body",
					Parameters: normParams,
				},
			}, nil

		case []interface{}:
			normParams, err := outbound.NormalizeTemplateParams(p)
			if err != nil {
				return nil, err
			}
			return []domain.TemplateComponent{
				{
					Type:       "body",
					Parameters: normParams,
				},
			}, nil
		}
	}

	return nil, nil
}

// Ensure unused math package import does not cause compile error if unused
var _ = math.Max
var _ = strconv.Itoa
