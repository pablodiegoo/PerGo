package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// ChatRepo defines the chat data access methods required by ChatAPIHandler.
type ChatRepo interface {
	GetChat(ctx context.Context, workspaceID, chatID uuid.UUID) (*domain.Chat, error)
	ListChats(ctx context.Context, workspaceID uuid.UUID, filter domain.ChatFilter, limit, offset int) ([]domain.Chat, error)
	ListChatMessages(ctx context.Context, workspaceID, chatID uuid.UUID, beforeUID, afterUID string, limit int) ([]domain.ChatMessage, error)
	AddChatMessage(ctx context.Context, msg *domain.ChatMessage) error
	CreateInternalNote(ctx context.Context, workspaceID, chatID uuid.UUID, authorName, senderType, body string) (*domain.ChatMessage, error)
	AssignChat(ctx context.Context, workspaceID, chatID uuid.UUID, memberID *uuid.UUID, email *string) error
	SetChatTags(ctx context.Context, workspaceID, chatID uuid.UUID, tags []string) error
	UpdateChatStatus(ctx context.Context, workspaceID, chatID uuid.UUID, status string) error
	SnoozeChat(ctx context.Context, workspaceID, chatID uuid.UUID, snoozedUntil *time.Time) error
	SetAIDisabled(ctx context.Context, workspaceID, chatID uuid.UUID, disabled bool) error
	TouchLastMessageAt(ctx context.Context, workspaceID, chatID uuid.UUID, t time.Time) error
	GetChatMessageByUID(ctx context.Context, workspaceID uuid.UUID, uid string) (*domain.ChatMessage, error)
	GetReactions(ctx context.Context, workspaceID uuid.UUID, messageUID string) ([]domain.Reaction, error)
	AddReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, reaction domain.Reaction) ([]domain.Reaction, error)
	RemoveReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, emoji, sender string) ([]domain.Reaction, error)
}

// ChatContactRepo defines contact operations required by ChatAPIHandler.
type ChatContactRepo interface {
	GetByID(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Contact, error)
	SetTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error
	CloseThread(ctx context.Context, workspaceID, contactID uuid.UUID) error
	ReopenThread(ctx context.Context, workspaceID, contactID uuid.UUID) error
	UpdateBotState(ctx context.Context, workspaceID, contactID uuid.UUID, botActive bool, pausedAt *time.Time) error
}

// ChatConnectionRepo defines connection operations required by ChatAPIHandler.
type ChatConnectionRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error)
}

// ChatWorkspaceRepo defines workspace member validation operations.
type ChatWorkspaceRepo interface {
	GetMemberByEmail(ctx context.Context, workspaceID uuid.UUID, email string) (*repository.WorkspaceMember, error)
}

// ChatAuditRepo defines audit logging operations.
type ChatAuditRepo interface {
	InsertAuditLog(ctx context.Context, entry *repository.AuditEntry) error
}

// ChatMessagePublisher defines outbound message dispatch operations.
type ChatMessagePublisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ChatAPIHandler exposes the public REST API for conversation management.
type ChatAPIHandler struct {
	chatRepo    ChatRepo
	contactRepo ChatContactRepo
	connRepo    ChatConnectionRepo
	wsRepo      ChatWorkspaceRepo
	auditRepo   ChatAuditRepo
	publisher   ChatMessagePublisher
}

// ChatAPIOption configures optional dependencies for ChatAPIHandler.
type ChatAPIOption func(*ChatAPIHandler)

// WithChatContactRepo sets the contact repository.
func WithChatContactRepo(r ChatContactRepo) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.contactRepo = r }
}

// WithChatConnectionRepo sets the connection repository.
func WithChatConnectionRepo(r ChatConnectionRepo) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.connRepo = r }
}

// WithChatWorkspaceRepo sets the workspace repository.
func WithChatWorkspaceRepo(r ChatWorkspaceRepo) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.wsRepo = r }
}

// WithChatAuditRepo sets the audit repository.
func WithChatAuditRepo(r ChatAuditRepo) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.auditRepo = r }
}

// WithChatMessagePublisher sets the message publisher.
func WithChatMessagePublisher(p ChatMessagePublisher) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.publisher = p }
}

// NewChatAPIHandler creates a new ChatAPIHandler.
func NewChatAPIHandler(chatRepo ChatRepo, opts ...ChatAPIOption) *ChatAPIHandler {
	h := &ChatAPIHandler{
		chatRepo: chatRepo,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes registers the public chat REST API routes.
func (h *ChatAPIHandler) RegisterRoutes(e *echo.Echo, m ...echo.MiddlewareFunc) {
	// Canonical REST routes (/api/v1/chats)
	chatGroup := e.Group("/api/v1/chats", m...)
	chatGroup.GET("", h.List)
	chatGroup.GET("/", h.List)
	chatGroup.GET("/:id", h.Get)
	chatGroup.GET("/:id/", h.Get)
	chatGroup.GET("/:id/messages", h.ListMessages)
	chatGroup.GET("/:id/messages/", h.ListMessages)
	chatGroup.POST("/:id/messages", h.SendMessage)
	chatGroup.POST("/:id/messages/", h.SendMessage)
	chatGroup.POST("/:id/notes", h.CreateNote)
	chatGroup.POST("/:id/notes/", h.CreateNote)
	chatGroup.POST("/:id/react", h.React)
	chatGroup.POST("/:id/react/", h.React)
	chatGroup.PATCH("/:id/assign", h.Assign)
	chatGroup.PATCH("/:id/assign/", h.Assign)
	chatGroup.PATCH("/:id/labels", h.UpdateLabels)
	chatGroup.PATCH("/:id/labels/", h.UpdateLabels)
	chatGroup.PATCH("/:id/status", h.UpdateStatus)
	chatGroup.PATCH("/:id/status/", h.UpdateStatus)

	// Workspace-scoped canonical routes (/api/v1/workspaces/:workspace_id/chats)
	wsChatGroup := e.Group("/api/v1/workspaces/:workspace_id/chats", m...)
	wsChatGroup.GET("", h.List)
	wsChatGroup.GET("/", h.List)
	wsChatGroup.GET("/:id", h.Get)
	wsChatGroup.GET("/:id/", h.Get)
	wsChatGroup.GET("/:id/messages", h.ListMessages)
	wsChatGroup.GET("/:id/messages/", h.ListMessages)
	wsChatGroup.POST("/:id/messages", h.SendMessage)
	wsChatGroup.POST("/:id/messages/", h.SendMessage)
	wsChatGroup.POST("/:id/notes", h.CreateNote)
	wsChatGroup.POST("/:id/notes/", h.CreateNote)
	wsChatGroup.POST("/:id/react", h.React)
	wsChatGroup.POST("/:id/react/", h.React)
	wsChatGroup.PATCH("/:id/assign", h.Assign)
	wsChatGroup.PATCH("/:id/assign/", h.Assign)
	wsChatGroup.PATCH("/:id/labels", h.UpdateLabels)
	wsChatGroup.PATCH("/:id/labels/", h.UpdateLabels)
	wsChatGroup.PATCH("/:id/status", h.UpdateStatus)
	wsChatGroup.PATCH("/:id/status/", h.UpdateStatus)
}

// resolveWorkspaceID resolves the workspace UUID from route parameter, tenant context, or domain scope,
// enforcing strict tenant scope isolation if a route parameter is supplied.
func (h *ChatAPIHandler) resolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	ctx := c.Request().Context()
	var authWSID uuid.UUID
	if scope, err := domain.Require(ctx); err == nil && scope.WorkspaceID() != uuid.Nil {
		authWSID = scope.WorkspaceID()
	} else if id, err := tenant.RequireWorkspaceID(ctx); err == nil && id != uuid.Nil {
		authWSID = id
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		authWSID = id
	}

	wsParam := strings.TrimSpace(c.Param("workspace_id"))
	if wsParam != "" {
		paramID, err := uuid.Parse(wsParam)
		if err != nil || paramID == uuid.Nil {
			return uuid.Nil, errors.New("invalid workspace_id parameter")
		}
		if authWSID != uuid.Nil && authWSID != paramID {
			return uuid.Nil, errors.New("workspace mismatch: route parameter does not match authenticated token scope")
		}
		if authWSID != uuid.Nil {
			return authWSID, nil
		}
		return uuid.Nil, errors.New("unauthorized: missing workspace authentication context")
	}

	if authWSID != uuid.Nil {
		return authWSID, nil
	}
	return uuid.Nil, errors.New("workspace context required")
}

// handleWorkspaceError maps workspace context errors to the appropriate HTTP status code.
func (h *ChatAPIHandler) handleWorkspaceError(c *echo.Context, err error) error {
	status := http.StatusUnauthorized
	code := "UNAUTHORIZED"
	if strings.Contains(err.Error(), "mismatch") {
		status = http.StatusForbidden
		code = "FORBIDDEN"
	}
	return c.JSON(status, map[string]string{
		"code":    code,
		"message": err.Error(),
	})
}

// chatIDFromParam extracts and validates the chat ID UUID from the path parameter.
func chatIDFromParam(c *echo.Context) (uuid.UUID, error) {
	idStr := strings.TrimSpace(c.Param("id"))
	if idStr == "" {
		idStr = strings.TrimSpace(c.Param("chat_id"))
	}
	if idStr == "" {
		return uuid.Nil, errors.New("chat_id path parameter is required")
	}
	return uuid.Parse(idStr)
}

// ChatSummaryDTO wraps domain.Chat with enriched contact attributes for REST API clients.
type ChatSummaryDTO struct {
	domain.Chat
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
	Identities   []string `json:"identities,omitempty"`
}

// List handles GET /api/v1/chats — lists conversational chats with filtering and pagination.
func (h *ChatAPIHandler) List(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	if h.chatRepo == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()

	// Query parameters
	status := strings.ToLower(strings.TrimSpace(c.QueryParam("status")))
	if status == "all" {
		status = ""
	}

	channel := strings.ToLower(strings.TrimSpace(c.QueryParam("channel")))

	var unreadPtr *bool
	if unreadStr := strings.TrimSpace(c.QueryParam("unread")); unreadStr != "" {
		if b, err := strconv.ParseBool(unreadStr); err == nil {
			unreadPtr = &b
		}
	}

	phone := strings.TrimSpace(c.QueryParam("phone"))

	var assignedEmailPtr *string
	if emailVal := strings.TrimSpace(c.QueryParam("assigned")); emailVal != "" {
		assignedEmailPtr = &emailVal
	} else if emailVal := strings.TrimSpace(c.QueryParam("assigned_email")); emailVal != "" {
		assignedEmailPtr = &emailVal
	}

	var unassignedPtr *bool
	if unassignedStr := strings.TrimSpace(c.QueryParam("unassigned")); unassignedStr != "" {
		if b, err := strconv.ParseBool(unassignedStr); err == nil {
			unassignedPtr = &b
		}
	}

	tag := strings.TrimSpace(c.QueryParam("tag"))
	if tag == "" {
		tag = strings.TrimSpace(c.QueryParam("label"))
	}

	limit := 20
	if limitStr := strings.TrimSpace(c.QueryParam("limit")); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if limit > 100 {
		limit = 100
	}

	offset := 0
	if offsetStr := strings.TrimSpace(c.QueryParam("offset")); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	filter := domain.ChatFilter{
		Status:        status,
		Channel:       channel,
		Unread:        unreadPtr,
		Phone:         phone,
		AssignedEmail: assignedEmailPtr,
		Unassigned:    unassignedPtr,
		Tag:           tag,
	}

	chats, err := h.chatRepo.ListChats(ctx, wsID, filter, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list chats", "workspace_id", wsID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to list chats: " + err.Error(),
		})
	}

	results := make([]ChatSummaryDTO, 0, len(chats))
	now := time.Now().UTC()

	for _, chat := range chats {
		// Auto-reopen snoozed chat if snooze timer expired
		if chat.Status == domain.ChatStatusSnoozed && chat.SnoozedUntil != nil && chat.SnoozedUntil.Before(now) {
			if uErr := h.chatRepo.UpdateChatStatus(ctx, wsID, chat.ID, string(domain.ChatStatusOpen)); uErr != nil {
				slog.WarnContext(ctx, "failed to auto un-snooze chat in list", "error", uErr, "chat_id", chat.ID)
			}
			chat.Status = domain.ChatStatusOpen
			chat.SnoozedUntil = nil
		}

		dto := ChatSummaryDTO{Chat: chat}
		if h.contactRepo != nil {
			if contact, cErr := h.contactRepo.GetByID(ctx, wsID, chat.ContactID); cErr == nil && contact != nil {
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

	return c.JSON(http.StatusOK, map[string]interface{}{
		"chats":  results,
		"count":  len(results),
		"limit":  limit,
		"offset": offset,
	})
}

// Get handles GET /api/v1/chats/:id — retrieves a single chat thread by ID with contact details.
func (h *ChatAPIHandler) Get(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()
	chat, err := h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	// Auto-reopen if snoozed timer expired
	if chat.Status == domain.ChatStatusSnoozed && chat.SnoozedUntil != nil && chat.SnoozedUntil.Before(time.Now().UTC()) {
		if uErr := h.chatRepo.UpdateChatStatus(ctx, wsID, chat.ID, string(domain.ChatStatusOpen)); uErr != nil {
			slog.WarnContext(ctx, "failed to auto un-snooze chat in get", "error", uErr, "chat_id", chat.ID)
		}
		chat.Status = domain.ChatStatusOpen
		chat.SnoozedUntil = nil
	}

	resp := map[string]interface{}{
		"chat": chat,
	}

	if h.contactRepo != nil {
		if contact, cErr := h.contactRepo.GetByID(ctx, wsID, chat.ContactID); cErr == nil && contact != nil {
			resp["contact"] = contact
		}
	}

	if h.connRepo != nil && chat.ConnectionID != nil {
		if conn, cErr := h.connRepo.GetByID(ctx, *chat.ConnectionID); cErr == nil && conn != nil {
			resp["connection"] = map[string]interface{}{
				"id":              conn.ID,
				"name":            conn.Name,
				"channel":         conn.Channel,
				"sender_identity": conn.SenderIdentity,
				"status":          conn.Status,
			}
		}
	}

	return c.JSON(http.StatusOK, resp)
}

// ListMessages handles GET /api/v1/chats/:id/messages — retrieves chronological chat messages with cursor pagination.
func (h *ChatAPIHandler) ListMessages(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()

	// Ensure chat exists
	_, err = h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	beforeUID := strings.TrimSpace(c.QueryParam("before_uid"))
	afterUID := strings.TrimSpace(c.QueryParam("after_uid"))

	limit := 50
	if limitStr := strings.TrimSpace(c.QueryParam("limit")); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if limit > 100 {
		limit = 100
	}

	messages, err := h.chatRepo.ListChatMessages(ctx, wsID, chatID, beforeUID, afterUID, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to list messages: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"chat_id":  chatID,
		"messages": messages,
		"count":    len(messages),
	})
}

// SendMessageRequest defines the JSON payload for dispatching an outbound message to a chat thread.
type SendMessageRequest struct {
	Body         string                     `json:"body"`
	Message      string                     `json:"message"` // alias for body
	SenderName   string                     `json:"sender_name,omitempty"`
	SenderType   string                     `json:"sender_type,omitempty"`
	DisableAI    *bool                      `json:"disable_ai,omitempty"`
	TemplateName string                     `json:"template_name,omitempty"`
	Language     string                     `json:"language,omitempty"`
	Components   []domain.TemplateComponent `json:"components,omitempty"`
}

// SendMessage handles POST /api/v1/chats/:id/messages — enqueues and dispatches an outbound message to a chat thread.
func (h *ChatAPIHandler) SendMessage(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()
	chat, err := h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	var req SendMessageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = strings.TrimSpace(req.Message)
	}

	if body == "" && req.TemplateName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "either body or template_name must be provided",
		})
	}

	displayBody := body
	if displayBody == "" && req.TemplateName != "" {
		displayBody = fmt.Sprintf("📋 [Template: %s]", req.TemplateName)
	}

	// Resolve contact recipient info
	if h.contactRepo == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "contact repository not configured",
		})
	}

	contact, err := h.contactRepo.GetByID(ctx, wsID, chat.ContactID)
	if err != nil || contact == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "CONTACT_NOT_FOUND",
			"message": "associated contact could not be resolved",
		})
	}

	// Resolve channel & connection precedence:
	// If chat has an explicit ConnectionID, inspect it first to determine channel
	var channel string
	var connID uuid.UUID
	var senderIdentity string
	if chat.ConnectionID != nil && *chat.ConnectionID != uuid.Nil && h.connRepo != nil {
		if conn, cErr := h.connRepo.GetByID(ctx, *chat.ConnectionID); cErr == nil && conn != nil {
			connID = conn.ID
			senderIdentity = conn.SenderIdentity
			channel = conn.Channel
		}
	}

	// Resolve destination recipient 'to' for the resolved channel if available
	var to string
	if channel != "" {
		for _, id := range contact.Identities {
			if id.Channel == channel {
				to = id.SenderIdentity
				break
			}
		}
	}

	// Fallback to inspecting contact identities if channel or recipient was not resolved
	if to == "" {
		for _, id := range contact.Identities {
			if id.Channel == "whatsapp_cloud" || id.Channel == "whatsapp" || id.Channel == "telegram" {
				if channel == "" {
					channel = id.Channel
				}
				to = id.SenderIdentity
				break
			}
		}
	}
	if to == "" && len(contact.Identities) > 0 {
		if channel == "" {
			channel = contact.Identities[0].Channel
		}
		to = contact.Identities[0].SenderIdentity
	}

	// If connection was not resolved from chat.ConnectionID, look up default for the resolved channel
	if connID == uuid.Nil && h.connRepo != nil && channel != "" {
		if defaultConn, cErr := h.connRepo.GetDefaultChannelConnection(ctx, wsID, channel); cErr == nil && defaultConn != nil {
			connID = defaultConn.ID
			senderIdentity = defaultConn.SenderIdentity
		}
	}

	// Enforce WABA 24h service window if whatsapp_cloud free-form text
	if channel == "whatsapp_cloud" && req.TemplateName == "" {
		if !chat.IsServiceWindowOpen() {
			return c.JSON(http.StatusForbidden, map[string]string{
				"code":    "SERVICE_WINDOW_EXPIRED",
				"message": "Meta WhatsApp 24-hour customer service window expired. A pre-approved template message is required.",
			})
		}
	}

	traceID := fmt.Sprintf("api-chat-%s", uuid.New().String())
	qMsg := domain.QueueMessage{
		WorkspaceID:    wsID,
		ConnectionID:   connID,
		SenderIdentity: senderIdentity,
		TraceID:        traceID,
		To:             to,
		Channel:        channel,
		Body:           body,
		TemplateName:   req.TemplateName,
		Language:       req.Language,
		Components:     req.Components,
		QueuedAt:       time.Now().UTC(),
	}

	if h.publisher != nil {
		data, mErr := json.Marshal(qMsg)
		if mErr != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "SERIALIZATION_ERROR",
				"message": mErr.Error(),
			})
		}
		if pubErr := h.publisher.Publish(ctx, "messages.outbound", data, traceID); pubErr != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "DISPATCH_ERROR",
				"message": pubErr.Error(),
			})
		}
	}

	senderName := strings.TrimSpace(req.SenderName)
	if senderName == "" {
		senderName = "Agent"
	}

	senderType := domain.SenderType(strings.TrimSpace(req.SenderType))
	if senderType == "" {
		senderType = domain.SenderTypeHumanAgent
	}

	disableAI := true
	if req.DisableAI != nil {
		disableAI = *req.DisableAI
	}

	now := time.Now().UTC()
	chatMsg := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: wsID,
		UID:         traceID,
		Direction:   domain.DirectionOutbound,
		SenderType:  senderType,
		SenderName:  senderName,
		SenderID:    senderIdentity,
		Body:        displayBody,
		CreatedAt:   now,
	}
	if err := h.chatRepo.AddChatMessage(ctx, chatMsg); err != nil {
		slog.ErrorContext(ctx, "failed to persist outbound chat message", "error", err, "chat_id", chat.ID, "trace_id", traceID)
	}
	if disableAI {
		if err := h.chatRepo.SetAIDisabled(ctx, wsID, chat.ID, true); err != nil {
			slog.WarnContext(ctx, "failed to disable AI on chat", "error", err, "chat_id", chat.ID)
		}
		if err := h.contactRepo.UpdateBotState(ctx, wsID, chat.ContactID, false, &now); err != nil {
			slog.WarnContext(ctx, "failed to update bot state on contact", "error", err, "contact_id", chat.ContactID)
		}
	}
	if err := h.chatRepo.TouchLastMessageAt(ctx, wsID, chat.ID, now); err != nil {
		slog.WarnContext(ctx, "failed to touch last message at", "error", err, "chat_id", chat.ID)
	}

	if h.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":         chat.ID.String(),
			"contact_id":      chat.ContactID.String(),
			"to":              to,
			"channel":         channel,
			"sender_identity": senderIdentity,
			"body":            displayBody,
			"sender_type":     string(senderType),
		})
		if err := h.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "chat.message.sent",
			Payload:     auditPayload,
			CreatedAt:   now,
		}); err != nil {
			slog.WarnContext(ctx, "failed to insert audit log for outbound message", "error", err, "trace_id", traceID)
		}
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status":     "queued",
		"trace_id":   traceID,
		"chat_id":    chat.ID,
		"message_id": chatMsg.ID,
	})
}

// CreateNoteRequest defines the JSON payload for creating an internal note.
type CreateNoteRequest struct {
	Body       string `json:"body"`
	AuthorName string `json:"author_name,omitempty"`
	SenderType string `json:"sender_type,omitempty"`
}

// CreateNote handles POST /api/v1/chats/:id/notes — adds a private internal note or draft to a chat thread.
func (h *ChatAPIHandler) CreateNote(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	var req CreateNoteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "body cannot be empty",
		})
	}

	authorName := strings.TrimSpace(req.AuthorName)
	if authorName == "" {
		authorName = "Agent"
	}

	senderType := strings.TrimSpace(req.SenderType)
	if senderType == "" {
		senderType = "human_agent"
	}

	ctx := c.Request().Context()
	note, err := h.chatRepo.CreateInternalNote(ctx, wsID, chatID, authorName, senderType, body)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	if h.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     chatID.String(),
			"note_id":     note.ID.String(),
			"author_name": authorName,
			"body":        body,
			"sender_type": senderType,
			"is_private":  true,
		})
		if err := h.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     note.UID,
			EventType:   "chat.note.created",
			Payload:     auditPayload,
			CreatedAt:   note.CreatedAt,
		}); err != nil {
			slog.WarnContext(ctx, "failed to insert audit log for note", "error", err, "chat_id", chatID)
		}
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status": "created",
		"note":   note,
	})
}

// ReactRequest defines the JSON payload for message reactions.
type ReactRequest struct {
	MessageUID string `json:"message_uid"`
	Emoji      string `json:"emoji"`
	Action     string `json:"action,omitempty"` // "add", "remove", or empty for toggle
	Sender     string `json:"sender,omitempty"`
}

// React handles POST /api/v1/chats/:id/react — adds, removes, or toggles an emoji reaction on a message.
func (h *ChatAPIHandler) React(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	var req ReactRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	messageUID := strings.TrimSpace(req.MessageUID)
	emoji := strings.TrimSpace(req.Emoji)
	if messageUID == "" || emoji == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "message_uid and emoji are required",
		})
	}

	ctx := c.Request().Context()
	msg, err := h.chatRepo.GetChatMessageByUID(ctx, wsID, messageUID)
	if err != nil || msg == nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"code":    "NOT_FOUND",
			"message": fmt.Sprintf("message %s not found", messageUID),
		})
	}
	if msg.ChatID != chatID {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "CHAT_MISMATCH",
			"message": "message does not belong to the specified chat thread",
		})
	}

	sender := strings.TrimSpace(req.Sender)
	if sender == "" {
		sender = "operator"
	}

	existingReactions, err := h.chatRepo.GetReactions(ctx, wsID, messageUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to get reactions: " + err.Error(),
		})
	}

	hasReacted := false
	for _, r := range existingReactions {
		if r.Emoji == emoji && (r.Sender == sender || r.Sender == "operator") {
			hasReacted = true
			break
		}
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	var updatedReactions []domain.Reaction

	if action == "remove" || (action == "" && hasReacted) {
		updatedReactions, err = h.chatRepo.RemoveReaction(ctx, wsID, messageUID, emoji, sender)
		action = "remove"
	} else {
		updatedReactions, err = h.chatRepo.AddReaction(ctx, wsID, messageUID, domain.Reaction{
			Emoji:     emoji,
			Sender:    sender,
			CreatedAt: time.Now().UTC(),
		})
		action = "add"
	}

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to mutate reaction: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":      "success",
		"action":      action,
		"message_uid": messageUID,
		"reactions":   updatedReactions,
	})
}

// AssignChatRequest defines the JSON payload for assigning or unassigning a chat.
type AssignChatRequest struct {
	Email *string `json:"email"`
}

// Assign handles PATCH /api/v1/chats/:id/assign — assigns a chat thread to a verified teammate or unassigns it.
func (h *ChatAPIHandler) Assign(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()
	chat, err := h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	var req AssignChatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	var targetEmail *string
	if req.Email != nil && strings.TrimSpace(*req.Email) != "" {
		trimmed := strings.TrimSpace(*req.Email)
		targetEmail = &trimmed
	}

	if targetEmail == nil {
		// Unassign
		if err := h.chatRepo.AssignChat(ctx, wsID, chatID, nil, nil); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "INTERNAL_ERROR",
				"message": "failed to unassign chat: " + err.Error(),
			})
		}
	} else {
		var memberID *uuid.UUID
		if h.wsRepo != nil {
			member, mErr := h.wsRepo.GetMemberByEmail(ctx, wsID, *targetEmail)
			if mErr != nil || member == nil {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"code":    "MEMBER_NOT_FOUND",
					"message": fmt.Sprintf("teammate with email %q not found in workspace", *targetEmail),
				})
			}
			memberID = &member.ID
		}
		if err := h.chatRepo.AssignChat(ctx, wsID, chatID, memberID, targetEmail); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "INTERNAL_ERROR",
				"message": "failed to assign chat: " + err.Error(),
			})
		}
	}

	if h.auditRepo != nil {
		action := "assign"
		var emailStr string
		if targetEmail != nil {
			emailStr = *targetEmail
		} else {
			action = "unassign"
		}
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":        chatID.String(),
			"contact_id":     chat.ContactID.String(),
			"assigned_email": emailStr,
			"action":         action,
		})
		if err := h.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("api-assign-%s", uuid.New().String()),
			EventType:   "chat.assigned",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			slog.WarnContext(ctx, "failed to insert audit log for assign", "error", err, "chat_id", chatID)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":         "success",
		"chat_id":        chatID,
		"assigned_email": targetEmail,
	})
}

// UpdateLabelsRequest defines the JSON payload for mutating chat tags/labels.
type UpdateLabelsRequest struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Tag    string   `json:"tag,omitempty"`
	Action string   `json:"action,omitempty"`
}

// UpdateLabels handles PATCH /api/v1/chats/:id/labels — adds, removes, or overwrites tags on a chat thread.
func (h *ChatAPIHandler) UpdateLabels(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()
	chat, err := h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	var req UpdateLabelsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	// Handle single-tag shortcut
	if req.Tag != "" {
		if strings.ToLower(req.Action) == "remove" {
			req.Remove = append(req.Remove, req.Tag)
		} else {
			req.Add = append(req.Add, req.Tag)
		}
	}

	var newTags []string
	if req.Tags != nil {
		// Complete replacement
		newTags = req.Tags
	} else {
		// Incremental add / remove
		tagMap := make(map[string]bool)
		for _, t := range chat.Tags {
			clean := strings.TrimSpace(t)
			if clean != "" {
				tagMap[strings.ToLower(clean)] = true
				newTags = append(newTags, clean)
			}
		}

		for _, addTag := range req.Add {
			clean := strings.TrimSpace(addTag)
			if clean != "" && !tagMap[strings.ToLower(clean)] {
				tagMap[strings.ToLower(clean)] = true
				newTags = append(newTags, clean)
			}
		}

		if len(req.Remove) > 0 {
			removeMap := make(map[string]bool)
			for _, remTag := range req.Remove {
				removeMap[strings.ToLower(strings.TrimSpace(remTag))] = true
			}
			filtered := make([]string, 0, len(newTags))
			for _, t := range newTags {
				if !removeMap[strings.ToLower(t)] {
					filtered = append(filtered, t)
				}
			}
			newTags = filtered
		}
	}

	if err := h.chatRepo.SetChatTags(ctx, wsID, chatID, newTags); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to update chat tags: " + err.Error(),
		})
	}

	if h.contactRepo != nil {
		if sErr := h.contactRepo.SetTags(ctx, wsID, chat.ContactID, newTags); sErr != nil {
			slog.WarnContext(ctx, "failed to sync tags with contact", "error", sErr, "contact_id", chat.ContactID)
		}
	}

	if h.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":    chatID.String(),
			"contact_id": chat.ContactID.String(),
			"tags":       newTags,
		})
		if err := h.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("api-labels-%s", uuid.New().String()),
			EventType:   "chat.tag.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			slog.WarnContext(ctx, "failed to insert audit log for labels", "error", err, "chat_id", chatID)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "success",
		"chat_id": chatID,
		"tags":    newTags,
	})
}

// UpdateStatusRequest defines the JSON payload for updating conversation lifecycle status.
type UpdateStatusRequest struct {
	Status       string  `json:"status"` // "open", "snoozed", "closed"
	SnoozedUntil *string `json:"snoozed_until,omitempty"`
}

// UpdateStatus handles PATCH /api/v1/chats/:id/status — transitions chat status between open, snoozed, and closed.
func (h *ChatAPIHandler) UpdateStatus(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	chatID, err := chatIDFromParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": err.Error(),
		})
	}

	ctx := c.Request().Context()
	chat, err := h.chatRepo.GetChat(ctx, wsID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": err.Error(),
		})
	}

	var req UpdateStatusRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != "open" && status != "snoozed" && status != "closed" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "status must be one of: 'open', 'snoozed', 'closed'",
		})
	}

	var snoozedUntil *time.Time
	if status == "snoozed" {
		if req.SnoozedUntil != nil && strings.TrimSpace(*req.SnoozedUntil) != "" {
			if parsed, pErr := time.Parse(time.RFC3339, strings.TrimSpace(*req.SnoozedUntil)); pErr == nil {
				snoozedUntil = &parsed
			}
		}
		if snoozedUntil == nil {
			defaultUntil := time.Now().UTC().Add(24 * time.Hour)
			snoozedUntil = &defaultUntil
		}
		if err := h.chatRepo.SnoozeChat(ctx, wsID, chatID, snoozedUntil); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "INTERNAL_ERROR",
				"message": "failed to snooze chat: " + err.Error(),
			})
		}
	} else {
		if err := h.chatRepo.UpdateChatStatus(ctx, wsID, chatID, status); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "INTERNAL_ERROR",
				"message": "failed to update chat status: " + err.Error(),
			})
		}
	}

	if h.contactRepo != nil {
		if status == "closed" {
			if cErr := h.contactRepo.CloseThread(ctx, wsID, chat.ContactID); cErr != nil {
				slog.WarnContext(ctx, "failed to close thread for contact", "error", cErr, "contact_id", chat.ContactID)
			}
		} else {
			if rErr := h.contactRepo.ReopenThread(ctx, wsID, chat.ContactID); rErr != nil {
				slog.WarnContext(ctx, "failed to reopen thread for contact", "error", rErr, "contact_id", chat.ContactID)
			}
		}
	}

	if h.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":       chatID.String(),
			"contact_id":    chat.ContactID.String(),
			"status":        status,
			"snoozed_until": snoozedUntil,
		})
		if err := h.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     fmt.Sprintf("api-status-%s", uuid.New().String()),
			EventType:   "chat.status.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			slog.WarnContext(ctx, "failed to insert audit log for status update", "error", err, "chat_id", chatID)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":        "success",
		"chat_id":       chatID,
		"chat_status":   status,
		"snoozed_until": snoozedUntil,
	})
}
