package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

func (s *Server) registerChatTools() {
	s.MCPServer.AddTool(mcp.Tool{
		Name:        "workspace_quotas",
		Description: "Retrieve workspace subscription quotas, plan tier, seat allocations, active channels, and monthly message usage.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace (optional if authenticated).",
				},
			},
		},
	}, s.handleWorkspaceQuotas)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "workspace_team",
		Description: "List all teammates, their roles, and accessible WhatsApp accounts in the workspace.",
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
	}, s.handleWorkspaceTeam)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_assign",
		Description: "Assign a chat thread to a verified teammate by email. Strictly validates that the teammate belongs to the workspace team.",
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
				"email": map[string]interface{}{
					"type":        "string",
					"description": "The email address of the teammate to assign the chat to.",
				},
			},
			Required: []string{"workspace_id", "chat_id", "email"},
		},
	}, s.handleChatAssign)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_unassign",
		Description: "Remove teammate assignment from a chat thread.",
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
			},
			Required: []string{"workspace_id", "chat_id"},
		},
	}, s.handleChatUnassign)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_set_label",
		Description: "Idempotently attach a label/tag to a chat thread.",
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
				"label": map[string]interface{}{
					"type":        "string",
					"description": "The label/tag name to attach (e.g. 'vip', 'follow-up', 'support').",
				},
			},
			Required: []string{"workspace_id", "chat_id", "label"},
		},
	}, s.handleChatSetLabel)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_remove_label",
		Description: "Idempotently remove a label/tag from a chat thread.",
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
				"label": map[string]interface{}{
					"type":        "string",
					"description": "The label/tag name to remove.",
				},
			},
			Required: []string{"workspace_id", "chat_id", "label"},
		},
	}, s.handleChatRemoveLabel)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_open",
		Description: "Reopen a closed or archived chat thread.",
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
			},
			Required: []string{"workspace_id", "chat_id"},
		},
	}, s.handleChatOpen)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_close",
		Description: "Close/resolve/archive an active chat thread.",
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
			},
			Required: []string{"workspace_id", "chat_id"},
		},
	}, s.handleChatClose)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_chats",
		Description: "List conversational chats in a workspace with optional filtering by status (open/closed), read/unread status, contact phone number, assignee, and tags.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the workspace.",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status: 'open', 'closed', or empty for all.",
				},
				"unread": map[string]interface{}{
					"type":        "boolean",
					"description": "Filter by unread state: true for unread only, false for read only, omit for all.",
				},
				"phone": map[string]interface{}{
					"type":        "string",
					"description": "Filter by contact phone number substring.",
				},
				"assigned_email": map[string]interface{}{
					"type":        "string",
					"description": "Filter by assigned teammate email address.",
				},
				"unassigned": map[string]interface{}{
					"type":        "boolean",
					"description": "Filter by unassigned chats (true for only unassigned chats).",
				},
				"tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag/label.",
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
	}, s.handleListChats)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_details",
		Description: "Retrieve detailed information for a specific chat thread, including contact CRM attributes, tags, assignment, and customer service window status.",
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
			},
			Required: []string{"workspace_id", "chat_id"},
		},
	}, s.handleChatDetails)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_history",
		Description: "Retrieve chronological messages in a chat thread with deterministic cursor pagination (before_uid / after_uid) and message limit.",
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
				"before_uid": map[string]interface{}{
					"type":        "string",
					"description": "Message UID cursor to fetch messages before (older than) this message.",
				},
				"after_uid": map[string]interface{}{
					"type":        "string",
					"description": "Message UID cursor to fetch messages after (newer than) this message.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of messages to return (default: 50, max: 100).",
				},
			},
			Required: []string{"workspace_id", "chat_id"},
		},
	}, s.handleChatHistory)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_enable_ai",
		Description: "Enable or disable automated AI responses for a specific chat thread.",
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
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether automated AI responses should be enabled (true) or disabled (false).",
				},
			},
			Required: []string{"workspace_id", "chat_id", "enabled"},
		},
	}, s.handleChatEnableAI)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_create_draft_note",
		Description: "Create a private internal draft note attached to a chat thread. Internal notes are invisible to external contacts and used for teammate collaboration or AI draft suggestions before human dispatch.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread.",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "The content of the internal note or draft response.",
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
				"author_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional display name of the note author (defaults to 'AI Assistant').",
				},
			},
			Required: []string{"chat_id", "body"},
		},
	}, s.handleChatCreateDraftNote)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "message_react",
		Description: "Add or remove an emoji reaction on a specific message UID, persisting reactions in PostgreSQL JSONB, broadcasting NATS events, and triggering signed webhooks.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"message_uid": map[string]interface{}{
					"type":        "string",
					"description": "The unique message identifier (UID) to react to.",
				},
				"emoji": map[string]interface{}{
					"type":        "string",
					"description": "The emoji symbol to add or remove (e.g. '👍', '❤️', '🔥').",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Reaction action: 'add' or 'remove' (default: 'add').",
					"enum":        []string{"add", "remove"},
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional workspace UUID if known. If omitted, resolved automatically from the message record.",
				},
				"sender": map[string]interface{}{
					"type":        "string",
					"description": "Optional sender identifier for the reaction (default: 'ai_agent').",
				},
			},
			Required: []string{"message_uid", "emoji"},
		},
	}, s.handleMessageReact)
}


// ChatSummaryDTO enriches domain.Chat with resolved contact summary for API/MCP readability.
type ChatSummaryDTO struct {
	domain.Chat
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
	Identities   []string `json:"identities,omitempty"`
}

func (s *Server) handleListChats(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	var wsID uuid.UUID
	if wsIDStr != "" {
		var err error
		wsID, err = uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	} else {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
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

	var assignedEmailPtr *string
	if emailVal := strings.TrimSpace(request.GetString("assigned_email", "")); emailVal != "" {
		assignedEmailPtr = &emailVal
	}

	var unassignedPtr *bool
	if unassignedVal, ok := args["unassigned"]; ok && unassignedVal != nil {
		if b, ok := unassignedVal.(bool); ok {
			unassignedPtr = &b
		}
	}

	tag := strings.TrimSpace(request.GetString("tag", ""))
	if tag == "" {
		tag = strings.TrimSpace(request.GetString("label", ""))
	}

	filter := domain.ChatFilter{
		Status:        status,
		Unread:        unreadPtr,
		Phone:         phone,
		AssignedEmail: assignedEmailPtr,
		Unassigned:    unassignedPtr,
		Tag:           tag,
	}

	chats, err := s.chatRepo.ListChats(ctx, wsID, filter, limit, offset)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list chats: %v", err)), nil
	}

	results := make([]ChatSummaryDTO, 0, len(chats))
	for _, c := range chats {
		dto := ChatSummaryDTO{Chat: c}
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

func (s *Server) handleChatDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if err == repository.ErrChatNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}

	response := map[string]interface{}{
		"chat": chat,
	}

	if s.contactRepo != nil {
		if contact, cErr := s.contactRepo.GetByID(ctx, wsID, chat.ContactID); cErr == nil && contact != nil {
			response["contact"] = contact
		}
	}

	if s.connectionRepo != nil && chat.ConnectionID != nil {
		if conn, connErr := s.connectionRepo.GetByID(ctx, *chat.ConnectionID); connErr == nil && conn != nil {
			response["connection"] = map[string]interface{}{
				"id":              conn.ID,
				"name":            conn.Name,
				"channel":         conn.Channel,
				"sender_identity": conn.SenderIdentity,
				"status":          conn.Status,
			}
		}
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatHistory(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	beforeUID := strings.TrimSpace(request.GetString("before_uid", ""))
	afterUID := strings.TrimSpace(request.GetString("after_uid", ""))
	limit := request.GetInt("limit", 50)

	messages, err := s.chatRepo.ListChatMessages(ctx, wsID, chatID, beforeUID, afterUID, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list chat messages: %v", err)), nil
	}

	var firstUID, lastUID string
	if len(messages) > 0 {
		firstUID = messages[0].UID
		lastUID = messages[len(messages)-1].UID
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"chat_id":    chatID,
		"messages":   messages,
		"count":      len(messages),
		"limit":      limit,
		"first_uid":  firstUID,
		"last_uid":   lastUID,
		"before_uid": beforeUID,
		"after_uid":  afterUID,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatEnableAI(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	enabled, err := request.RequireBool("enabled")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid enabled parameter: must be a boolean"), nil
	}

	// Update chat ai_disabled = !enabled
	if err := s.chatRepo.SetAIDisabled(ctx, wsID, chatID, !enabled); err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to update chat AI status: %v", err)), nil
	}

	// Also update contact bot active state if contactRepo is configured
	if s.contactRepo != nil {
		chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
		if err == nil && chat != nil {
			var pausedAt *time.Time
			if !enabled {
				now := time.Now().UTC()
				pausedAt = &now
			}
			_ = s.contactRepo.UpdateBotState(ctx, wsID, chat.ContactID, enabled, pausedAt)
		}
	}

	res := map[string]interface{}{
		"chat_id":     chatID.String(),
		"enabled":     enabled,
		"ai_disabled": !enabled,
		"status":      "success",
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatCreateDraftNote(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("missing body parameter"), nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return mcp.NewToolResultError("body parameter cannot be empty"), nil
	}

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	}

	authorName := strings.TrimSpace(request.GetString("author_name", "AI Assistant"))
	if authorName == "" {
		authorName = "AI Assistant"
	}

	note, err := s.chatRepo.CreateInternalNote(ctx, wsID, chatID, authorName, "ai_agent", body)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to create draft note: %v", err)), nil
	}

	data, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// TeammateDTO represents a workspace member enriched with accessible connection accounts.
type TeammateDTO struct {
	ID                    uuid.UUID                 `json:"id"`
	WorkspaceID           uuid.UUID                 `json:"workspace_id"`
	Name                  string                    `json:"name"`
	Email                 string                    `json:"email"`
	Role                  string                    `json:"role"`
	AccessibleAccounts    []AccessibleConnectionDTO `json:"accessible_whatsapp_accounts"`
	AssignedConnectionIDs []uuid.UUID               `json:"assigned_connection_ids"`
}

// AccessibleConnectionDTO represents a communication channel accessible to a teammate.
type AccessibleConnectionDTO struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Channel        string    `json:"channel"`
	SenderIdentity string    `json:"sender_identity"`
	Status         string    `json:"status"`
}

// WorkspaceQuotasDTO represents the subscription and usage quotas of a workspace.
type WorkspaceQuotasDTO struct {
	WorkspaceID           uuid.UUID `json:"workspace_id"`
	WorkspaceName         string    `json:"workspace_name"`
	Plan                  string    `json:"plan"`
	SeatsLimit            int       `json:"seats_limit"`
	SeatsUsed             int       `json:"seats_used"`
	MessagesLimit         int       `json:"messages_limit"`
	MessagesUsed          int       `json:"messages_used"`
	ActiveConnections     int       `json:"active_connections"`
	PIIOptIn              bool      `json:"pii_opt_in"`
	ServiceWindowEnforced bool      `json:"service_window_enforced"`
}

func (s *Server) handleWorkspaceQuotas(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.wsRepo == nil {
		return mcp.NewToolResultError("workspace repository is not configured on this server"), nil
	}

	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	var wsID uuid.UUID
	if wsIDStr != "" {
		var err error
		wsID, err = uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	} else {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}

	ws, err := s.wsRepo.GetByID(ctx, wsID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get workspace: %v", err)), nil
	}

	seatsUsed := 1
	members, err := s.wsRepo.ListMembers(ctx, wsID)
	if err == nil && len(members) > 0 {
		seatsUsed = len(members)
	}

	activeConnections := 0
	if s.connectionRepo != nil {
		conns, err := s.connectionRepo.ListByWorkspace(ctx, wsID)
		if err == nil {
			activeConnections = len(conns)
		}
	}

	quotas := WorkspaceQuotasDTO{
		WorkspaceID:           ws.ID,
		WorkspaceName:         ws.Name,
		Plan:                  "pro",
		SeatsLimit:            10,
		SeatsUsed:             seatsUsed,
		MessagesLimit:         50000,
		MessagesUsed:          0,
		ActiveConnections:     activeConnections,
		PIIOptIn:              ws.PIIOptIn,
		ServiceWindowEnforced: true,
	}

	resBytes, err := json.MarshalIndent(quotas, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal workspace quotas: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}

func (s *Server) handleWorkspaceTeam(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.wsRepo == nil {
		return mcp.NewToolResultError("workspace repository is not configured on this server"), nil
	}

	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	var wsID uuid.UUID
	if wsIDStr != "" {
		var err error
		wsID, err = uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	} else {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}

	members, err := s.wsRepo.ListMembers(ctx, wsID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list workspace members: %v", err)), nil
	}

	// Auto-seed default admin if workspace has zero members yet
	if len(members) == 0 {
		defaultMember := &repository.WorkspaceMember{
			WorkspaceID: wsID,
			Name:        "Workspace Admin",
			Email:       "admin@example.com",
			Role:        "admin",
		}
		_ = s.wsRepo.AddMember(ctx, defaultMember)
		members = []repository.WorkspaceMember{*defaultMember}
	}

	var allConns []*repository.Connection
	if s.connectionRepo != nil {
		allConns, _ = s.connectionRepo.ListByWorkspace(ctx, wsID)
	}

	connMap := make(map[uuid.UUID]*repository.Connection, len(allConns))
	for _, c := range allConns {
		connMap[c.ID] = c
	}

	teammates := make([]TeammateDTO, 0, len(members))
	for _, m := range members {
		dto := TeammateDTO{
			ID:                    m.ID,
			WorkspaceID:           m.WorkspaceID,
			Name:                  m.Name,
			Email:                 m.Email,
			Role:                  m.Role,
			AssignedConnectionIDs: m.AssignedConnectionIDs,
			AccessibleAccounts:    []AccessibleConnectionDTO{},
		}

		if len(m.AssignedConnectionIDs) > 0 {
			for _, cid := range m.AssignedConnectionIDs {
				if conn, ok := connMap[cid]; ok {
					dto.AccessibleAccounts = append(dto.AccessibleAccounts, AccessibleConnectionDTO{
						ID:             conn.ID,
						Name:           conn.Name,
						Channel:        conn.Channel,
						SenderIdentity: conn.SenderIdentity,
						Status:         conn.Status,
					})
				}
			}
		} else {
			for _, conn := range allConns {
				dto.AccessibleAccounts = append(dto.AccessibleAccounts, AccessibleConnectionDTO{
					ID:             conn.ID,
					Name:           conn.Name,
					Channel:        conn.Channel,
					SenderIdentity: conn.SenderIdentity,
					Status:         conn.Status,
				})
			}
		}
		teammates = append(teammates, dto)
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"workspace_id": wsID,
		"teammates":    teammates,
		"count":        len(teammates),
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatAssign(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}
	if s.wsRepo == nil {
		return mcp.NewToolResultError("workspace repository is not configured on this server"), nil
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

	emailStr, err := request.RequireString("email")
	if err != nil {
		return mcp.NewToolResultError("missing email parameter"), nil
	}
	email := strings.TrimSpace(emailStr)
	if email == "" {
		return mcp.NewToolResultError("email cannot be empty"), nil
	}

	// Strictly validate that assigned email exists in workspace team
	member, err := s.wsRepo.GetMemberByEmail(ctx, wsID, email)
	if err != nil || member == nil {
		return mcp.NewToolResultError(fmt.Sprintf("teammate with email %q not found in workspace %s. Call workspace_team to list valid teammates.", email, wsID)), nil
	}

	if err := s.chatRepo.AssignChat(ctx, wsID, chatID, &member.ID, &member.Email); err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to assign chat: %v", err)), nil
	}

	chat, _ := s.chatRepo.GetChat(ctx, wsID, chatID)

	data, err := json.MarshalIndent(map[string]interface{}{
		"success":          true,
		"message":          "Chat successfully assigned",
		"chat_id":          chatID,
		"assigned_user_id": member.ID,
		"assigned_email":   member.Email,
		"chat":             chat,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatUnassign(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	if err := s.chatRepo.AssignChat(ctx, wsID, chatID, nil, nil); err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to unassign chat: %v", err)), nil
	}

	chat, _ := s.chatRepo.GetChat(ctx, wsID, chatID)

	data, err := json.MarshalIndent(map[string]interface{}{
		"success": true,
		"message": "Chat successfully unassigned",
		"chat_id": chatID,
		"chat":    chat,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatSetLabel(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	label := strings.TrimSpace(request.GetString("label", ""))
	if label == "" {
		label = strings.TrimSpace(request.GetString("tag", ""))
	}
	if label == "" {
		return mcp.NewToolResultError("missing label parameter"), nil
	}

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}

	alreadyExists := false
	for _, t := range chat.Tags {
		if strings.EqualFold(t, label) {
			alreadyExists = true
			break
		}
	}

	newTags := chat.Tags
	if !alreadyExists {
		newTags = append(newTags, label)
		if err := s.chatRepo.SetChatTags(ctx, wsID, chatID, newTags); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to set chat tags: %v", err)), nil
		}
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success": true,
		"chat_id": chatID,
		"label":   label,
		"tags":    newTags,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatRemoveLabel(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	label := strings.TrimSpace(request.GetString("label", ""))
	if label == "" {
		label = strings.TrimSpace(request.GetString("tag", ""))
	}
	if label == "" {
		return mcp.NewToolResultError("missing label parameter"), nil
	}

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}

	newTags := make([]string, 0, len(chat.Tags))
	for _, t := range chat.Tags {
		if !strings.EqualFold(t, label) {
			newTags = append(newTags, t)
		}
	}

	if len(newTags) != len(chat.Tags) {
		if err := s.chatRepo.SetChatTags(ctx, wsID, chatID, newTags); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to set chat tags: %v", err)), nil
		}
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success":       true,
		"chat_id":       chatID,
		"removed_label": label,
		"tags":          newTags,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatOpen(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	if err := s.chatRepo.UpdateChatStatus(ctx, wsID, chatID, string(domain.ChatStatusOpen)); err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to open chat: %v", err)), nil
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success": true,
		"chat_id": chatID,
		"status":  "open",
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatClose(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	chatIDStr, err := request.RequireString("chat_id")
	if err != nil {
		return mcp.NewToolResultError("missing chat_id parameter"), nil
	}
	chatID, err := uuid.Parse(strings.TrimSpace(chatIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
	}

	if err := s.chatRepo.UpdateChatStatus(ctx, wsID, chatID, string(domain.ChatStatusClosed)); err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to close chat: %v", err)), nil
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success": true,
		"chat_id": chatID,
		"status":  "closed",
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMessageReact(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	messageUID, err := request.RequireString("message_uid")
	if err != nil || strings.TrimSpace(messageUID) == "" {
		return mcp.NewToolResultError("missing or invalid message_uid parameter"), nil
	}
	messageUID = strings.TrimSpace(messageUID)

	emoji, err := request.RequireString("emoji")
	if err != nil || strings.TrimSpace(emoji) == "" {
		return mcp.NewToolResultError("missing or invalid emoji parameter"), nil
	}
	emoji = strings.TrimSpace(emoji)

	action := strings.ToLower(strings.TrimSpace(request.GetString("action", "add")))
	if action == "" {
		action = "add"
	}
	if action != "add" && action != "remove" {
		return mcp.NewToolResultError("invalid action: must be 'add' or 'remove'"), nil
	}

	sender := strings.TrimSpace(request.GetString("sender", "ai_agent"))
	if sender == "" {
		sender = "ai_agent"
	}

	var wsID uuid.UUID
	var chatIDStr string
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWSID, pErr := uuid.Parse(wsIDStr)
		if pErr != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWSID
		if msg, mErr := s.chatRepo.GetChatMessageByUID(ctx, wsID, messageUID); mErr == nil && msg != nil {
			chatIDStr = msg.ChatID.String()
		}
	} else {
		msg, mErr := s.chatRepo.FindMessageByUID(ctx, messageUID)
		if mErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("message not found: %s", messageUID)), nil
		}
		wsID = msg.WorkspaceID
		chatIDStr = msg.ChatID.String()
	}

	var updatedReactions []domain.Reaction
	if action == "add" {
		updatedReactions, err = s.chatRepo.AddReaction(ctx, wsID, messageUID, domain.Reaction{
			Emoji:     emoji,
			Sender:    sender,
			CreatedAt: time.Now().UTC(),
		})
	} else {
		updatedReactions, err = s.chatRepo.RemoveReaction(ctx, wsID, messageUID, emoji, sender)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update message reaction: %v", err)), nil
	}

	traceID := uuid.New().String()
	rxEvent := domain.ReactionUpdatedPayload{
		Event:       "message.reaction.updated",
		WorkspaceID: wsID.String(),
		ChatID:      chatIDStr,
		MessageUID:  messageUID,
		Emoji:       emoji,
		Sender:      sender,
		Action:      action,
		Reactions:   updatedReactions,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	payloadBytes, err := json.Marshal(rxEvent)
	if err == nil {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, "messages.events.reaction_updated", payloadBytes, traceID)
		}
		if s.webhookDispatcher != nil && s.webhookSubRepo != nil {
			subs, sErr := s.webhookSubRepo.ListByWorkspace(ctx, wsID)
			if sErr == nil {
				for _, sub := range subs {
					if !sub.Active {
						continue
					}
					if webhook.MatchesAny(sub.EventTypes, "message.reaction.updated") {
						task := webhook.WebhookDeliveryTask{
							ID:             uuid.New(),
							SubscriptionID: sub.ID,
							WorkspaceID:    wsID,
							Event:          "message.reaction.updated",
							TraceID:        traceID,
							MessageID:      messageUID,
							Payload:        payloadBytes,
							Mode:           "outbound",
						}
						_ = s.webhookDispatcher.Dispatch(ctx, task)
					}
				}
			}
		}
	}

	response := map[string]interface{}{
		"message_uid": messageUID,
		"action":      action,
		"emoji":       emoji,
		"sender":      sender,
		"reactions":   updatedReactions,
		"count":       len(updatedReactions),
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// CallTool is a convenience helper for invoking a registered MCP tool by name.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	st := s.MCPServer.GetTool(name)
	if st == nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return st.Handler(ctx, req)
}
