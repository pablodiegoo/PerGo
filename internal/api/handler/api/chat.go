package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/chat"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// ChatRepo defines the chat data access methods required by ChatAPIHandler.
type ChatRepo = chat.ChatRepository

// ChatContactRepo defines contact operations required by ChatAPIHandler.
type ChatContactRepo = chat.ContactRepository

// ChatConnectionRepo defines connection operations required by ChatAPIHandler.
type ChatConnectionRepo = chat.ConnectionRepository

// ChatWorkspaceRepo defines workspace member validation operations.
type ChatWorkspaceRepo = chat.WorkspaceRepository

// ChatAuditRepo defines audit logging operations.
type ChatAuditRepo = chat.AuditRepository

// ChatMessagePublisher defines outbound message dispatch operations.
type ChatMessagePublisher = chat.MessagePublisher

// ChatReactionWebhookDispatcher defines reaction webhook dispatch operations.
type ChatReactionWebhookDispatcher = chat.ReactionWebhookDispatcher

// ChatAPIHandler exposes the public REST API for conversation management,
// delegating domain rules and persistence to chat.Engine.
type ChatAPIHandler struct {
	chatRepo          ChatRepo
	contactRepo       ChatContactRepo
	connRepo          ChatConnectionRepo
	wsRepo            ChatWorkspaceRepo
	auditRepo         ChatAuditRepo
	publisher         ChatMessagePublisher
	webhookDispatcher ChatReactionWebhookDispatcher
	ingestor          outbound.OutboundProcessor
	debouncer         chat.DebounceCanceller
	activity          *activity.Recorder
	eng               *chat.Engine
}

// ChatAPIOption configures optional dependencies for ChatAPIHandler.
type ChatAPIOption func(*ChatAPIHandler)

// WithChatEngine configures a pre-built chat.Engine instance.
func WithChatEngine(eng *chat.Engine) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.eng = eng }
}

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

// WithChatWebhookDispatcher sets the reaction webhook dispatcher.
func WithChatWebhookDispatcher(d ChatReactionWebhookDispatcher) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.webhookDispatcher = d }
}

// WithChatMessageIngestor sets the unified outbound message ingestor.
func WithChatMessageIngestor(i outbound.OutboundProcessor) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.ingestor = i }
}

// WithChatDebounceCanceller sets the inbound debounce buffer canceller.
func WithChatDebounceCanceller(d chat.DebounceCanceller) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.debouncer = d }
}

// WithChatActivity sets the Chat Activity recorder.
func WithChatActivity(rec *activity.Recorder) ChatAPIOption {
	return func(h *ChatAPIHandler) { h.activity = rec }
}

// SetWebhookDispatcher configures the reaction webhook dispatcher on ChatAPIHandler.
func (h *ChatAPIHandler) SetWebhookDispatcher(d ChatReactionWebhookDispatcher) *ChatAPIHandler {
	h.webhookDispatcher = d
	if h.eng != nil {
		h.eng.SetWebhookDispatcher(d)
	}
	return h
}

// NewChatAPIHandler creates a new ChatAPIHandler backed by chat.Engine.
func NewChatAPIHandler(chatRepo ChatRepo, opts ...ChatAPIOption) *ChatAPIHandler {
	h := &ChatAPIHandler{
		chatRepo: chatRepo,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	_ = h.engine()
	return h
}

func (h *ChatAPIHandler) engine() *chat.Engine {
	if h.eng == nil && h.chatRepo != nil {
		h.eng = chat.NewEngineFromConfig(chat.Config{
			ChatRepo:          h.chatRepo,
			ContactRepo:       h.contactRepo,
			ConnRepo:          h.connRepo,
			WorkspaceRepo:     h.wsRepo,
			AuditRepo:         h.auditRepo,
			Publisher:         h.publisher,
			WebhookDispatcher: h.webhookDispatcher,
			Ingestor:          h.ingestor,
			Debouncer:         h.debouncer,
			Activity:          h.activity,
		})
	}
	return h.eng
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
	chatGroup.GET("/:id/summarize", h.Summarize)
	chatGroup.GET("/:id/summarize/", h.Summarize)
	chatGroup.POST("/:id/summarize", h.Summarize)
	chatGroup.POST("/:id/summarize/", h.Summarize)
	chatGroup.PATCH("/:id/assign", h.Assign)
	chatGroup.PATCH("/:id/assign/", h.Assign)
	chatGroup.PATCH("/:id/labels", h.UpdateLabels)
	chatGroup.PATCH("/:id/labels/", h.UpdateLabels)
	chatGroup.PATCH("/:id/status", h.UpdateStatus)
	chatGroup.PATCH("/:id/status/", h.UpdateStatus)
	chatGroup.PATCH("/:id/ai", h.ToggleAI)
	chatGroup.PATCH("/:id/ai/", h.ToggleAI)

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
	wsChatGroup.GET("/:id/summarize", h.Summarize)
	wsChatGroup.GET("/:id/summarize/", h.Summarize)
	wsChatGroup.POST("/:id/summarize", h.Summarize)
	wsChatGroup.POST("/:id/summarize/", h.Summarize)
	wsChatGroup.PATCH("/:id/assign", h.Assign)
	wsChatGroup.PATCH("/:id/assign/", h.Assign)
	wsChatGroup.PATCH("/:id/labels", h.UpdateLabels)
	wsChatGroup.PATCH("/:id/labels/", h.UpdateLabels)
	wsChatGroup.PATCH("/:id/status", h.UpdateStatus)
	wsChatGroup.PATCH("/:id/status/", h.UpdateStatus)
	wsChatGroup.PATCH("/:id/ai", h.ToggleAI)
	wsChatGroup.PATCH("/:id/ai/", h.ToggleAI)
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
type ChatSummaryDTO = chat.ChatSummary

// List handles GET /api/v1/chats — lists conversational chats with filtering and pagination.
func (h *ChatAPIHandler) List(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return h.handleWorkspaceError(c, err)
	}

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()

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

	results, err := eng.List(ctx, wsID, filter, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list chats", "workspace_id", wsID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to list chats: " + err.Error(),
		})
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()
	res, err := eng.Get(ctx, wsID, chat.ChatRef{ChatID: chatID}, 0, false)
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
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

	resp := map[string]interface{}{
		"chat": res.Chat,
	}
	if res.Contact != nil {
		resp["contact"] = res.Contact
	}
	if res.Connection != nil {
		resp["connection"] = map[string]interface{}{
			"id":              res.Connection.ID,
			"name":            res.Connection.Name,
			"channel":         res.Connection.Channel,
			"sender_identity": res.Connection.SenderIdentity,
			"status":          res.Connection.Status,
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
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

	ctx := c.Request().Context()
	_, messages, err := eng.ListMessages(ctx, wsID, chat.ChatRef{ChatID: chatID}, beforeUID, afterUID, limit)
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
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
	ReplyToUID   *string                    `json:"reply_to_uid,omitempty"`
	TemplateName string                     `json:"template_name,omitempty"`
	Language     string                     `json:"language,omitempty"`
	Components   []domain.TemplateComponent `json:"components,omitempty"`
	Media        *domain.Media              `json:"media,omitempty"`
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()
	chatObj, err := eng.ResolveChat(ctx, wsID, chat.ChatRef{ChatID: chatID})
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
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

	if body == "" && req.TemplateName == "" && req.Media == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "either body or template_name must be provided",
		})
	}

	if h.contactRepo == nil && h.eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "contact repository not configured",
		})
	}
	if h.contactRepo != nil {
		contact, err := h.contactRepo.GetByID(ctx, wsID, chatObj.ContactID)
		if err != nil || contact == nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "CONTACT_NOT_FOUND",
				"message": "associated contact could not be resolved",
			})
		}
	}

	senderType := strings.TrimSpace(req.SenderType)
	if senderType == "" {
		senderType = string(domain.SenderTypeAIAgent)
	}

	res, err := eng.SendMessage(ctx, wsID, chat.SendRequest{
		Ref:           chat.ChatRef{ChatID: chatID},
		Body:          body,
		Media:         req.Media,
		SenderName:    req.SenderName,
		SenderType:    senderType,
		ReplyToUID:    req.ReplyToUID,
		TemplateName:  req.TemplateName,
		Language:      req.Language,
		Components:    req.Components,
		TraceIDPrefix: "api-chat",
	})
	if err != nil {
		if errors.Is(err, chat.ErrContactNotFound) || strings.Contains(err.Error(), "no phone number or channel identity") {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "CONTACT_NOT_FOUND",
				"message": "associated contact could not be resolved",
			})
		}
		if errors.Is(err, outbound.ErrQueueFull) {
			return c.JSON(http.StatusTooManyRequests, map[string]string{
				"code":    "QUEUE_FULL",
				"message": err.Error(),
			})
		}
		var tmplErr *domain.TemplateRequiredError
		if errors.Is(err, chat.ErrServiceWindowExpired) || errors.As(err, &tmplErr) {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{
				"code":    "SERVICE_WINDOW_EXPIRED",
				"message": "Meta WhatsApp 24-hour customer service window expired. A pre-approved template message is required.",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "DISPATCH_ERROR",
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status":     res.Status,
		"trace_id":   res.TraceID,
		"chat_id":    res.ChatID,
		"message_id": res.MessageID,
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()
	note, err := eng.CreateNote(ctx, wsID, chat.NoteRequest{
		Ref:        chat.ChatRef{ChatID: chatID},
		Body:       body,
		AuthorName: req.AuthorName,
		SenderType: req.SenderType,
	})
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
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

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "" && action != "add" && action != "remove" && action != "toggle" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "action must be 'add', 'remove', or 'toggle'",
		})
	}

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()
	res, err := eng.React(ctx, wsID, chat.ReactRequest{
		ChatRef:    chat.ChatRef{ChatID: chatID},
		MessageUID: messageUID,
		Emoji:      emoji,
		Action:     action,
		Sender:     req.Sender,
	})
	if err != nil {
		if errors.Is(err, chat.ErrMessageNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("message %s not found", messageUID),
			})
		}
		if errors.Is(err, chat.ErrChatMismatch) {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "CHAT_MISMATCH",
				"message": "message does not belong to the specified chat thread",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to mutate reaction: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":      "success",
		"action":      res.Action,
		"message_uid": res.MessageUID,
		"reactions":   res.Reactions,
	})
}

// Summarize handles GET and POST /api/v1/chats/:id/summarize — generates a structured 3-bullet synopsis of the chat thread.
func (h *ChatAPIHandler) Summarize(c *echo.Context) error {
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	ctx := c.Request().Context()
	synopsis, err := eng.Summarize(ctx, wsID, chat.ChatRef{ChatID: chatID}, 50)
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to summarize chat: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"chat_id":         chatID,
		"customer_issues": synopsis.CustomerIssues,
		"promises_made":   synopsis.PromisesMade,
		"current_status":  synopsis.CurrentStatus,
		"synopsis":        synopsis,
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	var req AssignChatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	var emailStr string
	var targetEmail *string
	if req.Email != nil && strings.TrimSpace(*req.Email) != "" {
		trimmed := strings.TrimSpace(*req.Email)
		emailStr = trimmed
		targetEmail = &trimmed
	}

	ctx := c.Request().Context()
	_, _, err = eng.Assign(ctx, wsID, chat.ChatRef{ChatID: chatID}, emailStr)
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		if errors.Is(err, chat.ErrInvalidMember) {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "MEMBER_NOT_FOUND",
				"message": fmt.Sprintf("teammate with email %q not found in workspace", emailStr),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to assign chat: " + err.Error(),
		})
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	var req UpdateLabelsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	if req.Tag != "" {
		if strings.ToLower(req.Action) == "remove" {
			req.Remove = append(req.Remove, req.Tag)
		} else {
			req.Add = append(req.Add, req.Tag)
		}
	}

	ctx := c.Request().Context()
	newTags, err := eng.UpdateTags(ctx, wsID, chat.ChatRef{ChatID: chatID}, chat.TagMutation{
		Add:    req.Add,
		Remove: req.Remove,
		Set:    req.Tags,
	})
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to update chat tags: " + err.Error(),
		})
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	var req UpdateStatusRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	var snoozedUntil *time.Time
	if req.SnoozedUntil != nil && strings.TrimSpace(*req.SnoozedUntil) != "" {
		parsed, pErr := time.Parse(time.RFC3339, strings.TrimSpace(*req.SnoozedUntil))
		if pErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "BAD_REQUEST",
				"message": "invalid snoozed_until timestamp: must be RFC3339",
			})
		}
		snoozedUntil = &parsed
	}

	ctx := c.Request().Context()
	updatedChat, resolvedUntil, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: chatID}, chat.StatusUpdate{
		Status:       req.Status,
		SnoozedUntil: snoozedUntil,
	})
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		if errors.Is(err, chat.ErrInvalidStatus) {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "BAD_REQUEST",
				"message": err.Error(),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to update chat status: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":        "success",
		"chat_id":       chatID,
		"chat_status":   string(updatedChat.Status),
		"snoozed_until": resolvedUntil,
	})
}

// ToggleAIRequest defines the JSON payload for updating AI engagement on a chat thread.
type ToggleAIRequest struct {
	Disabled    *bool   `json:"disabled,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	PausedUntil *string `json:"paused_until,omitempty"`
}

// ToggleAI handles PATCH /api/v1/chats/:id/ai — updates AI engagement on a chat thread.
func (h *ChatAPIHandler) ToggleAI(c *echo.Context) error {
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

	eng := h.engine()
	if eng == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "chat repository not configured",
		})
	}

	var req ToggleAIRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "invalid request body",
		})
	}

	var change activity.AIChange
	switch {
	case req.Disabled != nil:
		if *req.Disabled {
			change = activity.AIOff()
		} else {
			change = activity.AIResume()
		}
	case req.Enabled != nil:
		if *req.Enabled {
			change = activity.AIResume()
		} else {
			change = activity.AIOff()
		}
	case strings.TrimSpace(req.Mode) != "":
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		switch mode {
		case string(domain.AIModeOff):
			change = activity.AIOff()
		case string(domain.AIModeActive), "engaged":
			change = activity.AIResume()
		case string(domain.AIModePaused):
			if req.PausedUntil == nil || strings.TrimSpace(*req.PausedUntil) == "" {
				change = activity.AIPauseUntil(time.Now().UTC().Add(12 * time.Hour))
			} else {
				parsed, pErr := time.Parse(time.RFC3339, strings.TrimSpace(*req.PausedUntil))
				if pErr != nil {
					return c.JSON(http.StatusBadRequest, map[string]string{
						"code":    "BAD_REQUEST",
						"message": "invalid paused_until timestamp: must be RFC3339",
					})
				}
				change = activity.AIPauseUntil(parsed)
			}
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "BAD_REQUEST",
				"message": "invalid mode: must be engaged, paused, or off",
			})
		}
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "BAD_REQUEST",
			"message": "disabled, enabled, or mode is required",
		})
	}

	ctx := c.Request().Context()
	updatedChat, err := eng.SetAI(ctx, wsID, chat.ChatRef{ChatID: chatID}, change)
	if err != nil {
		if errors.Is(err, chat.ErrChatNotFound) || errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("chat %s not found", chatID),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "failed to update chat AI state: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":      "success",
		"chat_id":     chatID,
		"ai_disabled": updatedChat.AIDisabled,
		"ai":          updatedChat.AI,
	})
}
