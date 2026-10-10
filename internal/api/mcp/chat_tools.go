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
	"github.com/pablojhp.pergo/internal/repository"
)

func (s *Server) registerChatTools() {
	s.MCPServer.AddTool(mcp.Tool{
		Name:        "list_chats",
		Description: "List conversational chats in a workspace with optional filtering by status (open/closed), read/unread status, and contact phone number.",
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

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}
	wsID, err := uuid.Parse(strings.TrimSpace(wsIDStr))
	if err != nil {
		return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
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
