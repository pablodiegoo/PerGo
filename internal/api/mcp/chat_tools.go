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
					"description": "Optional workspace UUID (defaults to authenticated workspace context).",
				},
				"sender": map[string]interface{}{
					"type":        "string",
					"description": "Optional sender identifier for the reaction (default: 'ai_agent').",
				},
			},
			Required: []string{"message_uid", "emoji"},
		},
	}, s.handleMessageReact)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_summarize",
		Description: "Generate a concise 3-bullet AI synopsis (Customer Issues, Promises Made, Current Status) of recent conversation messages.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread.",
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
			},
			Required: []string{"chat_id"},
		},
	}, s.handleChatSummarize)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "chat_send_message",
		Description: "Send an outbound message in an existing chat thread via WhatsApp/Telegram with state synchronization and immutable audit logging.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The message body text to send.",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Alias for message.",
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
				"sender_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional sender display name.",
				},
				"reply_to_uid": map[string]interface{}{
					"type":        "string",
					"description": "Optional UID of a previous message to reply to in a threaded conversation.",
				},
			},
			Required: []string{"chat_id"},
		},
	}, s.handleChatSendMessage)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "workspace_whatsapp_accounts",
		Description: "List all connected WhatsApp accounts/sessions for the workspace, showing connection ID, phone number/jid, channel type, and operational status (connected, healthy, disconnected).",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace (defaults to authenticated workspace).",
				},
			},
		},
	}, s.handleWorkspaceWhatsAppAccounts)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "message_details",
		Description: "Retrieve complete details, payload, direction, delivery status, reactions, and failure reason for a specific message.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"message_uid": map[string]interface{}{
					"type":        "string",
					"description": "The unique message UID or trace ID.",
				},
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the chat thread.",
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
			},
			Required: []string{"message_uid"},
		},
	}, s.handleMessageDetails)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "whatsapp_account_send_message",
		Description: "Initiate an outbound message from a specific WhatsApp account to a destination phone number.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the WhatsApp connection to send from.",
				},
				"sender_identity": map[string]interface{}{
					"type":        "string",
					"description": "The sender identity / phone number of the account (alternative to connection_id).",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Destination phone number in international E.164 format.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The message text body to send.",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Alias for message.",
				},
				"sender_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional sender display name (default: 'AI Assistant').",
				},
			},
			Required: []string{"to"},
		},
	}, s.handleWhatsAppAccountSendMessage)

	s.MCPServer.AddTool(mcp.Tool{
		Name:        "message_reply",
		Description: "Send a threaded reply referencing a specific message (reply_to_uid) in the chat.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"reply_to_uid": map[string]interface{}{
					"type":        "string",
					"description": "The UID of the message being replied to.",
				},
				"chat_id": map[string]interface{}{
					"type":        "string",
					"description": "The UUID of the chat thread (optional if resolvable from reply_to_uid).",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The message body text to send.",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Alias for message.",
				},
				"workspace_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional UUID of the workspace.",
				},
				"sender_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional sender display name.",
				},
			},
			Required: []string{"reply_to_uid"},
		},
	}, s.handleMessageReply)
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

	if s.auditRepo != nil {
		targetWs := wsID
		if targetWs == uuid.Nil {
			targetWs = note.WorkspaceID
		}
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     chatID.String(),
			"note_id":     note.ID.String(),
			"author_name": authorName,
			"body":        body,
			"sender_type": "ai_agent",
			"is_private":  true,
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: targetWs,
			TraceID:     note.UID,
			EventType:   "chat.note.created",
			Payload:     auditPayload,
			CreatedAt:   note.CreatedAt,
		})
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

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":          chatID.String(),
			"assigned_user_id": member.ID.String(),
			"assigned_email":   member.Email,
			"action":           "assign",
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("mcp-assign-%s", uuid.New().String()),
			EventType:   "chat.assigned",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
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

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id": chatID.String(),
			"action":  "unassign",
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("mcp-unassign-%s", uuid.New().String()),
			EventType:   "chat.assigned",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
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
		if s.auditRepo != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"chat_id": chatID.String(),
				"label":   label,
				"action":  "add",
			})
			_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: wsID,
				TraceID:     fmt.Sprintf("mcp-label-%s", uuid.New().String()),
				EventType:   "chat.tag.updated",
				Payload:     auditPayload,
				CreatedAt:   time.Now().UTC(),
			})
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
		if s.auditRepo != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"chat_id": chatID.String(),
				"label":   label,
				"action":  "remove",
			})
			_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: wsID,
				TraceID:     fmt.Sprintf("mcp-unlabel-%s", uuid.New().String()),
				EventType:   "chat.tag.updated",
				Payload:     auditPayload,
				CreatedAt:   time.Now().UTC(),
			})
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

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id": chatID.String(),
			"status":  "open",
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("mcp-open-%s", uuid.New().String()),
			EventType:   "chat.status.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
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

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id": chatID.String(),
			"status":  "closed",
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("mcp-close-%s", uuid.New().String()),
			EventType:   "chat.status.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
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
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	var msg *domain.ChatMessage
	if wsID != uuid.Nil {
		var mErr error
		msg, mErr = s.chatRepo.GetChatMessageByUID(ctx, wsID, messageUID)
		if mErr != nil || msg == nil {
			return mcp.NewToolResultError(fmt.Sprintf("message not found: %s", messageUID)), nil
		}
	} else {
		var mErr error
		msg, mErr = s.chatRepo.FindMessageByUID(ctx, messageUID)
		if mErr != nil || msg == nil {
			return mcp.NewToolResultError(fmt.Sprintf("message not found: %s", messageUID)), nil
		}
		wsID = msg.WorkspaceID
	}

	if authWS, ok := tenant.WorkspaceIDFrom(ctx); ok && authWS != uuid.Nil && wsID != authWS {
		return mcp.NewToolResultError("workspace_id does not match authenticated context"), nil
	}
	chatIDStr = msg.ChatID.String()

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

func (s *Server) handleChatSummarize(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	var chat *domain.Chat
	if wsID != uuid.Nil {
		chat, err = s.chatRepo.GetChat(ctx, wsID, chatID)
	} else {
		chat, err = s.chatRepo.GetChatByID(ctx, chatID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}
	wsID = chat.WorkspaceID

	messages, err := s.chatRepo.ListChatMessages(ctx, wsID, chatID, "", "", 50)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list chat messages: %v", err)), nil
	}

	synopsis := domain.SummarizeMessages(messages, chat)

	data, err := json.MarshalIndent(map[string]interface{}{
		"chat_id":         chat.ID,
		"customer_issues": synopsis.CustomerIssues,
		"promises_made":   synopsis.PromisesMade,
		"current_status":  synopsis.CurrentStatus,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleChatSendMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	messageText := strings.TrimSpace(request.GetString("message", ""))
	if messageText == "" {
		messageText = strings.TrimSpace(request.GetString("body", ""))
	}
	if messageText == "" {
		return mcp.NewToolResultError("missing or empty message parameter"), nil
	}

	senderName := strings.TrimSpace(request.GetString("sender_name", "AI Assistant"))

	var replyToUID *string
	if rUID := strings.TrimSpace(request.GetString("reply_to_uid", "")); rUID != "" {
		replyToUID = &rUID
	}

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	var chat *domain.Chat
	if wsID != uuid.Nil {
		chat, err = s.chatRepo.GetChat(ctx, wsID, chatID)
	} else {
		chat, err = s.chatRepo.GetChatByID(ctx, chatID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get chat: %v", err)), nil
	}
	wsID = chat.WorkspaceID

	var recipientPhone string
	if s.contactRepo != nil {
		if contact, cErr := s.contactRepo.GetByID(ctx, wsID, chat.ContactID); cErr == nil && contact != nil {
			recipientPhone = resolveContactPhone(contact)
		}
	}

	channel := "whatsapp"
	var senderIdentity string
	if s.connectionRepo != nil && chat.ConnectionID != nil {
		if conn, cErr := s.connectionRepo.GetByID(ctx, *chat.ConnectionID); cErr == nil && conn != nil {
			channel = conn.Channel
			senderIdentity = conn.SenderIdentity
		}
	}

	traceID := fmt.Sprintf("mcp-chat-%s", uuid.New().String())
	now := time.Now().UTC()

	chatMsg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chat.ID,
		WorkspaceID: wsID,
		UID:         traceID,
		Direction:   string(domain.DirectionOutbound),
		SenderType:  string(domain.SenderTypeAIAgent),
		SenderName:  senderName,
		SenderID:    senderIdentity,
		Body:        messageText,
		ReplyToUID:  replyToUID,
		CreatedAt:   now,
	}
	_ = s.chatRepo.AddChatMessage(ctx, chatMsg)
	_ = s.chatRepo.TouchLastMessageAt(ctx, wsID, chat.ID, now)

	if s.ingestor != nil && recipientPhone != "" {
		reqMeta := map[string]string{
			"source":      "mcp_gateway",
			"sender_type": string(domain.SenderTypeAIAgent),
			"sender_name": senderName,
		}
		if replyToUID != nil {
			reqMeta["reply_to_uid"] = *replyToUID
		}
		req := &domain.CreateMessageRequest{
			To:       recipientPhone,
			Channel:  channel,
			From:     senderIdentity,
			Body:     messageText,
			Type:     "text",
			Metadata: reqMeta,
		}
		_, _ = s.ingestor.Ingest(ctx, wsID, traceID, req)
	}

	if s.auditRepo != nil {
		auditMap := map[string]interface{}{
			"chat_id":         chat.ID.String(),
			"contact_id":      chat.ContactID.String(),
			"to":              recipientPhone,
			"channel":         channel,
			"sender_identity": senderIdentity,
			"body":            messageText,
			"sender_name":     senderName,
			"sender_type":     string(domain.SenderTypeAIAgent),
		}
		if replyToUID != nil {
			auditMap["reply_to_uid"] = *replyToUID
		}
		auditPayload, _ := json.Marshal(auditMap)
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "chat.message.sent",
			Payload:     auditPayload,
			CreatedAt:   now,
		})
	}

	respMap := map[string]interface{}{
		"success":   true,
		"chat_id":   chat.ID,
		"trace_id":  traceID,
		"to":        recipientPhone,
		"body":      messageText,
		"queued_at": now,
	}
	if replyToUID != nil {
		respMap["reply_to_uid"] = *replyToUID
	}

	data, err := json.MarshalIndent(respMap, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWorkspaceWhatsAppAccounts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.connectionRepo == nil {
		return mcp.NewToolResultError("connection repository is not configured on this server"), nil
	}

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	} else {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}

	if authWS, ok := tenant.WorkspaceIDFrom(ctx); ok && authWS != uuid.Nil && wsID != authWS {
		return mcp.NewToolResultError("workspace_id does not match authenticated context"), nil
	}

	conns, err := s.connectionRepo.ListByWorkspace(ctx, wsID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list connections: %v", err)), nil
	}

	type WhatsAppAccountDTO struct {
		ConnectionID   string     `json:"connection_id"`
		Name           string     `json:"name"`
		Slug           string     `json:"slug"`
		Channel        string     `json:"channel"`
		SenderIdentity string     `json:"sender_identity"`
		JID            string     `json:"jid,omitempty"`
		Status         string     `json:"status"`
		IsDefault      bool       `json:"is_default"`
		ConnectedSince *time.Time `json:"connected_since,omitempty"`
	}

	accounts := make([]WhatsAppAccountDTO, 0)
	for _, c := range conns {
		if c.Channel == "whatsapp" || c.Channel == "whatsapp_cloud" {
			st := c.Status
			if st == "active" || st == "connected" {
				st = "connected"
			} else if st == "" || st == "disconnected" {
				st = "disconnected"
			}
			jid := ""
			if c.JID != nil {
				jid = *c.JID
			}
			accounts = append(accounts, WhatsAppAccountDTO{
				ConnectionID:   c.ID.String(),
				Name:           c.Name,
				Slug:           c.Slug,
				Channel:        c.Channel,
				SenderIdentity: c.SenderIdentity,
				JID:            jid,
				Status:         st,
				IsDefault:      c.IsDefault,
				ConnectedSince: c.ConnectedSince,
			})
		}
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"accounts": accounts,
		"count":    len(accounts),
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMessageDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	messageUID, err := request.RequireString("message_uid")
	if err != nil || strings.TrimSpace(messageUID) == "" {
		return mcp.NewToolResultError("missing or empty message_uid parameter"), nil
	}
	messageUID = strings.TrimSpace(messageUID)

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	chatIDStr := strings.TrimSpace(request.GetString("chat_id", ""))
	var chatID uuid.UUID
	if chatIDStr != "" {
		parsedChat, err := uuid.Parse(chatIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
		}
		chatID = parsedChat
	}

	if wsID == uuid.Nil && chatID != uuid.Nil {
		if chat, cErr := s.chatRepo.GetChatByID(ctx, chatID); cErr == nil && chat != nil {
			wsID = chat.WorkspaceID
		}
	}

	if wsID == uuid.Nil {
		return mcp.NewToolResultError("missing workspace_id parameter"), nil
	}

	if authWS, ok := tenant.WorkspaceIDFrom(ctx); ok && authWS != uuid.Nil && wsID != authWS {
		return mcp.NewToolResultError("workspace_id does not match authenticated context"), nil
	}

	msg, err := s.chatRepo.GetChatMessageByUID(ctx, wsID, messageUID)
	if err != nil || msg == nil {
		return mcp.NewToolResultError(fmt.Sprintf("message not found: %s", messageUID)), nil
	}

	if chatID != uuid.Nil && msg.ChatID != chatID {
		return mcp.NewToolResultError(fmt.Sprintf("message %s does not belong to chat %s", messageUID, chatID)), nil
	}

	status := "sent"
	deliveryStatus := "delivered"
	var failureReason *string

	if msg.Direction == string(domain.DirectionInbound) {
		status = "received"
		deliveryStatus = "received"
	} else if msg.IsPrivate || msg.Direction == "internal_note" {
		status = "internal"
		deliveryStatus = "internal"
	} else {
		if sVal, ok := msg.Metadata["status"].(string); ok && sVal != "" {
			status = sVal
		}
		if dVal, ok := msg.Metadata["delivery_status"].(string); ok && dVal != "" {
			deliveryStatus = dVal
		} else {
			deliveryStatus = status
		}
	}

	if errVal, ok := msg.Metadata["error"].(string); ok && errVal != "" {
		failureReason = &errVal
	} else if fVal, ok := msg.Metadata["failure_reason"].(string); ok && fVal != "" {
		failureReason = &fVal
	}

	dto := map[string]interface{}{
		"id":              msg.ID,
		"chat_id":         msg.ChatID,
		"workspace_id":    msg.WorkspaceID,
		"uid":             msg.UID,
		"direction":       msg.Direction,
		"sender_type":     msg.SenderType,
		"sender_name":     msg.SenderName,
		"sender_id":       msg.SenderID,
		"body":            msg.Body,
		"media_url":       msg.MediaURL,
		"media_type":      msg.MediaType,
		"is_private":      msg.IsPrivate,
		"status":          status,
		"delivery_status": deliveryStatus,
		"failure_reason":  failureReason,
		"reactions":       msg.Reactions,
		"reply_to_uid":    msg.ReplyToUID,
		"metadata":        msg.Metadata,
		"created_at":      msg.CreatedAt,
	}

	data, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWhatsAppAccountSendMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.connectionRepo == nil {
		return mcp.NewToolResultError("connection repository is not configured on this server"), nil
	}

	to := strings.TrimSpace(request.GetString("to", ""))
	if to == "" {
		return mcp.NewToolResultError("missing or empty 'to' destination phone number"), nil
	}

	messageText := strings.TrimSpace(request.GetString("message", ""))
	if messageText == "" {
		messageText = strings.TrimSpace(request.GetString("body", ""))
	}
	if messageText == "" {
		return mcp.NewToolResultError("missing or empty message body"), nil
	}

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	var conn *repository.Connection
	connIDStr := strings.TrimSpace(request.GetString("connection_id", ""))
	if connIDStr != "" {
		connID, err := uuid.Parse(connIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid connection_id: must be a valid UUID"), nil
		}
		c, err := s.connectionRepo.GetByID(ctx, connID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connection not found: %v", err)), nil
		}
		conn = c
		if wsID == uuid.Nil {
			wsID = conn.WorkspaceID
		} else if conn.WorkspaceID != wsID {
			return mcp.NewToolResultError("connection does not belong to specified workspace"), nil
		}
	} else if senderIdent := strings.TrimSpace(request.GetString("sender_identity", "")); senderIdent != "" {
		if wsID == uuid.Nil {
			return mcp.NewToolResultError("workspace_id is required when locating connection by sender_identity"), nil
		}
		c, err := s.connectionRepo.GetBySenderIdentity(ctx, wsID, senderIdent)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connection with sender_identity '%s' not found: %v", senderIdent, err)), nil
		}
		conn = c
	} else if wsID != uuid.Nil {
		c, err := s.connectionRepo.GetDefaultChannelConnection(ctx, wsID, "whatsapp")
		if err != nil {
			c, err = s.connectionRepo.GetDefaultChannelConnection(ctx, wsID, "whatsapp_cloud")
		}
		if err == nil {
			conn = c
		}
	}

	if conn == nil {
		return mcp.NewToolResultError("could not resolve a valid WhatsApp connection; provide connection_id or sender_identity"), nil
	}

	if conn.Channel != "whatsapp" && conn.Channel != "whatsapp_cloud" {
		return mcp.NewToolResultError(fmt.Sprintf("connection %s is channel '%s', expected a WhatsApp channel", conn.ID, conn.Channel)), nil
	}

	if authWS, ok := tenant.WorkspaceIDFrom(ctx); ok && authWS != uuid.Nil && wsID != authWS {
		return mcp.NewToolResultError("workspace_id does not match authenticated context"), nil
	}

	senderName := strings.TrimSpace(request.GetString("sender_name", "AI Assistant"))
	if senderName == "" {
		senderName = "AI Assistant"
	}

	var chatID *uuid.UUID
	if s.contactRepo != nil && s.chatRepo != nil {
		contact, cErr := s.contactRepo.ResolveContact(ctx, wsID, conn.Channel, to, to, "", "")
		if cErr == nil && contact != nil {
			if chat, chErr := s.chatRepo.FindOrCreateChat(ctx, wsID, &conn.ID, contact.ID); chErr == nil && chat != nil {
				chatID = &chat.ID
			}
		}
	}

	traceID := fmt.Sprintf("mcp-wa-%s", uuid.New().String())
	now := time.Now().UTC()

	if chatID != nil && s.chatRepo != nil {
		chatMsg := &domain.ChatMessage{
			ID:          uuid.New(),
			ChatID:      *chatID,
			WorkspaceID: wsID,
			UID:         traceID,
			Direction:   string(domain.DirectionOutbound),
			SenderType:  string(domain.SenderTypeAIAgent),
			SenderName:  senderName,
			SenderID:    conn.SenderIdentity,
			Body:        messageText,
			CreatedAt:   now,
		}
		_ = s.chatRepo.AddChatMessage(ctx, chatMsg)
		_ = s.chatRepo.TouchLastMessageAt(ctx, wsID, *chatID, now)
	}

	if s.ingestor != nil {
		req := &domain.CreateMessageRequest{
			To:       to,
			Channel:  conn.Channel,
			From:     conn.SenderIdentity,
			Body:     messageText,
			Type:     "text",
			Metadata: map[string]string{
				"source":        "mcp_gateway",
				"connection_id": conn.ID.String(),
				"sender_type":   string(domain.SenderTypeAIAgent),
				"sender_name":   senderName,
			},
		}
		_, _ = s.ingestor.Ingest(ctx, wsID, traceID, req)
	}

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"connection_id":   conn.ID.String(),
			"to":              to,
			"channel":         conn.Channel,
			"sender_identity": conn.SenderIdentity,
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
			CreatedAt:   now,
		})
	}

	resp := map[string]interface{}{
		"success":       true,
		"connection_id": conn.ID,
		"channel":       conn.Channel,
		"from":          conn.SenderIdentity,
		"to":            to,
		"trace_id":      traceID,
		"body":          messageText,
		"queued_at":     now,
	}
	if chatID != nil {
		resp["chat_id"] = *chatID
	}

	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to format output: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMessageReply(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.chatRepo == nil {
		return mcp.NewToolResultError("chat repository is not configured on this server"), nil
	}

	replyToUID, err := request.RequireString("reply_to_uid")
	if err != nil || strings.TrimSpace(replyToUID) == "" {
		return mcp.NewToolResultError("missing or empty reply_to_uid parameter"), nil
	}
	replyToUID = strings.TrimSpace(replyToUID)

	messageText := strings.TrimSpace(request.GetString("message", ""))
	if messageText == "" {
		messageText = strings.TrimSpace(request.GetString("body", ""))
	}
	if messageText == "" {
		return mcp.NewToolResultError("missing or empty message parameter"), nil
	}

	senderName := strings.TrimSpace(request.GetString("sender_name", "AI Assistant"))

	var wsID uuid.UUID
	wsIDStr := strings.TrimSpace(request.GetString("workspace_id", ""))
	if wsIDStr != "" {
		parsedWs, err := uuid.Parse(wsIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid workspace_id: must be a valid UUID"), nil
		}
		wsID = parsedWs
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	}

	chatIDStr := strings.TrimSpace(request.GetString("chat_id", ""))
	var chatID uuid.UUID
	if chatIDStr != "" {
		parsedChat, err := uuid.Parse(chatIDStr)
		if err != nil {
			return mcp.NewToolResultError("invalid chat_id: must be a valid UUID"), nil
		}
		chatID = parsedChat
	}

	if chatID == uuid.Nil {
		if wsID == uuid.Nil {
			return mcp.NewToolResultError("missing chat_id or workspace_id parameter to locate message"), nil
		}
		parentMsg, pErr := s.chatRepo.GetChatMessageByUID(ctx, wsID, replyToUID)
		if pErr != nil || parentMsg == nil {
			return mcp.NewToolResultError(fmt.Sprintf("parent message to reply to not found: %s", replyToUID)), nil
		}
		chatID = parentMsg.ChatID
	}

	if wsID == uuid.Nil {
		chat, cErr := s.chatRepo.GetChatByID(ctx, chatID)
		if cErr != nil || chat == nil {
			return mcp.NewToolResultError(fmt.Sprintf("chat not found: %s", chatID)), nil
		}
		wsID = chat.WorkspaceID
	}

	if authWS, ok := tenant.WorkspaceIDFrom(ctx); ok && authWS != uuid.Nil && wsID != authWS {
		return mcp.NewToolResultError("workspace_id does not match authenticated context"), nil
	}

	chat, err := s.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("chat not found: %v", err)), nil
	}

	var recipientPhone string
	if s.contactRepo != nil {
		if contact, cErr := s.contactRepo.GetByID(ctx, wsID, chat.ContactID); cErr == nil && contact != nil {
			recipientPhone = resolveContactPhone(contact)
		}
	}

	channel := "whatsapp"
	var senderIdentity string
	if s.connectionRepo != nil && chat.ConnectionID != nil {
		if conn, cErr := s.connectionRepo.GetByID(ctx, *chat.ConnectionID); cErr == nil && conn != nil {
			channel = conn.Channel
			senderIdentity = conn.SenderIdentity
		}
	}

	traceID := fmt.Sprintf("mcp-reply-%s", uuid.New().String())
	now := time.Now().UTC()

	chatMsg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chat.ID,
		WorkspaceID: wsID,
		UID:         traceID,
		Direction:   string(domain.DirectionOutbound),
		SenderType:  string(domain.SenderTypeAIAgent),
		SenderName:  senderName,
		SenderID:    senderIdentity,
		Body:        messageText,
		ReplyToUID:  &replyToUID,
		CreatedAt:   now,
	}
	_ = s.chatRepo.AddChatMessage(ctx, chatMsg)
	_ = s.chatRepo.TouchLastMessageAt(ctx, wsID, chat.ID, now)

	if s.ingestor != nil && recipientPhone != "" {
		req := &domain.CreateMessageRequest{
			To:       recipientPhone,
			Channel:  channel,
			From:     senderIdentity,
			Body:     messageText,
			Type:     "text",
			Metadata: map[string]string{
				"source":       "mcp_gateway",
				"sender_type":  string(domain.SenderTypeAIAgent),
				"sender_name":  senderName,
				"reply_to_uid": replyToUID,
			},
		}
		_, _ = s.ingestor.Ingest(ctx, wsID, traceID, req)
	}

	if s.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":         chat.ID.String(),
			"contact_id":      chat.ContactID.String(),
			"to":              recipientPhone,
			"channel":         channel,
			"sender_identity": senderIdentity,
			"body":            messageText,
			"sender_name":     senderName,
			"sender_type":     string(domain.SenderTypeAIAgent),
			"reply_to_uid":    replyToUID,
		})
		_ = s.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "chat.message.replied",
			Payload:     auditPayload,
			CreatedAt:   now,
		})
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"success":      true,
		"chat_id":      chat.ID,
		"trace_id":     traceID,
		"to":           recipientPhone,
		"body":         messageText,
		"reply_to_uid": replyToUID,
		"queued_at":    now,
	}, "", "  ")
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
