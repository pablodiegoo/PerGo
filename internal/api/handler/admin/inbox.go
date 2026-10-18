package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	chatpkg "github.com/pablojhp.pergo/internal/chat"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/presence"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
	"github.com/pablojhp.pergo/templates/components"
	"github.com/pablojhp.pergo/templates/pages"
)

// MessagePublisher defines the outbound publisher interface used by InboxHandler.
type MessagePublisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// InboxHandler holds dependencies for the conversational inbox.
type InboxHandler struct {
	Repo              *repository.AuditRepository
	Workspaces        *repository.WorkspaceRepository
	Connections       *repository.ConnectionRepository
	Publisher         MessagePublisher
	Templates         *repository.WABATemplateRepository
	ContactRepo       *repository.ContactRepository
	UserActionLogs    *repository.UserActionLogRepository
	ChatRepo          *repository.ChatRepository
	Presence          *presence.Tracker
	WebhookDispatcher webhook.WebhookDispatcher
	ChatEngine        *chatpkg.Engine
	Ingestor          outbound.OutboundProcessor
	Debouncer         chatpkg.DebounceCanceller
	Activity          *activity.Recorder
}

// NewInboxHandler creates an InboxHandler backed by a pre-configured ChatEngine.
func NewInboxHandler(eng *chatpkg.Engine) *InboxHandler {
	return &InboxHandler{ChatEngine: eng}
}

// engine returns the configured ChatEngine or constructs one from the handler's dependencies.
func (h *InboxHandler) engine() *chatpkg.Engine {
	if h.ChatEngine != nil {
		return h.ChatEngine
	}
	chatRepo := h.ChatRepo
	contactRepo := h.ContactRepo
	if chatRepo == nil {
		if contactRepo != nil && contactRepo.Pool() != nil {
			chatRepo = repository.NewChatRepository(contactRepo.Pool())
		} else if h.Workspaces != nil && h.Workspaces.Pool() != nil {
			chatRepo = repository.NewChatRepository(h.Workspaces.Pool())
		}
	}
	if contactRepo == nil && chatRepo != nil && chatRepo.Pool() != nil {
		contactRepo = repository.NewContactRepository(chatRepo.Pool())
	}
	connRepo := h.Connections
	if connRepo == nil && chatRepo != nil && chatRepo.Pool() != nil {
		connRepo = repository.NewConnectionRepository(chatRepo.Pool(), nil)
	}
	rec := h.Activity
	if rec == nil && chatRepo != nil && chatRepo.Pool() != nil {
		rec = activity.New(chatRepo.Pool(), h.Publisher, h.Debouncer, nil)
	}
	ingestor := h.Ingestor
	if ingestor == nil && h.Publisher != nil && rec != nil {
		ingestor = outbound.NewProcessor(nil, nil, connRepo, h.Publisher).
			SetActivity(rec).
			SetContactRepository(contactRepo).
			SetChatRepository(chatRepo).
			SetDebounceCanceller(h.Debouncer)
	}
	return chatpkg.NewEngineFromConfig(chatpkg.Config{
		ChatRepo:          chatRepo,
		ContactRepo:       contactRepo,
		ConnRepo:          connRepo,
		WorkspaceRepo:     h.Workspaces,
		AuditRepo:         h.Repo,
		Publisher:         h.Publisher,
		WebhookDispatcher: h.WebhookDispatcher,
		Ingestor:          ingestor,
		Debouncer:         h.Debouncer,
		Activity:          rec,
	})
}

// resolveCurrentOperatorEmail resolves the authenticated operator's email from context,
// session cookie, header, or the single workspace member fallback.
func (h *InboxHandler) resolveCurrentOperatorEmail(c *echo.Context, workspaceID uuid.UUID) string {
	if email, ok := c.Get("user_email").(string); ok && strings.TrimSpace(email) != "" {
		return strings.TrimSpace(email)
	}
	if cookie, err := c.Cookie("pergo-user-email"); err == nil && cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}
	if headerEmail := strings.TrimSpace(c.Request().Header.Get("X-User-Email")); headerEmail != "" {
		return headerEmail
	}
	if h.Workspaces != nil && workspaceID != uuid.Nil {
		ctx := c.Request().Context()
		if members, err := h.Workspaces.ListMembers(ctx, workspaceID); err == nil && len(members) == 1 {
			return members[0].Email
		}
	}
	return ""
}

// loadConversations fetches conversations in a single SQL query against chats and chat_messages.
// Returns conversations, unreadMap keyed by contact ID, and total unread count.
func (h *InboxHandler) loadConversations(c *echo.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, map[string]bool, int, error) {
	ctx := c.Request().Context()

	conversations, err := h.engine().ListConversations(ctx, workspaceID, channelFilter)
	if err != nil {
		return nil, nil, 0, err
	}

	statusFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("status")))
	assignedFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("assigned")))
	tagFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("tag")))
	searchFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("search")))

	var currentOperatorEmail string
	if assignedFilter == "me" {
		currentOperatorEmail = h.resolveCurrentOperatorEmail(c, workspaceID)
	}

	unreadMap := make(map[string]bool, len(conversations))
	unreadCount := 0
	filtered := make([]repository.ConversationSummary, 0, len(conversations))

	for i := range conversations {
		conv := &conversations[i]
		if conv.Status == "" {
			conv.Status = "open"
		}

		// Apply status filter
		switch statusFilter {
		case "closed":
			if conv.Status != "closed" {
				continue
			}
		case "snoozed":
			if conv.Status != "snoozed" {
				continue
			}
		case "unassigned":
			if conv.Status == "closed" || conv.Status == "snoozed" || (conv.AssignedEmail != nil && *conv.AssignedEmail != "") {
				continue
			}
		case "all":
			// no status filtering
		default: // "open" or empty
			if conv.Status == "closed" || conv.Status == "snoozed" {
				continue
			}
		}

		// Apply assigned filter
		if assignedFilter != "" {
			if assignedFilter == "unassigned" {
				if conv.AssignedEmail != nil && *conv.AssignedEmail != "" {
					continue
				}
			} else if assignedFilter == "me" {
				if currentOperatorEmail == "" || conv.AssignedEmail == nil || !strings.EqualFold(*conv.AssignedEmail, currentOperatorEmail) {
					continue
				}
			} else if conv.AssignedEmail == nil || strings.ToLower(*conv.AssignedEmail) != assignedFilter {
				continue
			}
		}

		// Apply tag filter
		if tagFilter != "" {
			hasTag := false
			for _, t := range conv.Tags {
				if strings.ToLower(t) == tagFilter {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		// Apply search query filter
		if searchFilter != "" {
			match := strings.Contains(strings.ToLower(conv.ContactName), searchFilter) ||
				strings.Contains(strings.ToLower(conv.RecipientIdentity), searchFilter) ||
				strings.Contains(strings.ToLower(conv.LastMessageBody), searchFilter)
			if !match {
				continue
			}
		}

		isUnread := conv.Unread || conv.UnreadCount > 0
		key := conv.ContactID.String()
		unreadMap[key] = isUnread
		if isUnread {
			unreadCount++
		}
		filtered = append(filtered, *conv)
	}

	return filtered, unreadMap, unreadCount, nil
}

// View handles GET /admin/inbox — renders the full split-pane inbox page.
func (h *InboxHandler) View(c *echo.Context) error {
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(c.Request().Context()); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(c.Request().Context()); ok && id != uuid.Nil {
		workspaceID = id
	}
	connectionFilter := c.QueryParam("connection")
	statusFilter := c.QueryParam("status")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, connectionFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations: "+err.Error())
	}

	var connections []*repository.Connection
	if h.Connections != nil {
		connections, _ = h.Connections.ListByWorkspace(c.Request().Context(), workspaceID)
	}

	assignedFilter := c.QueryParam("assigned")
	isEmbedded := strings.EqualFold(strings.TrimSpace(c.Request().Header.Get("X-PerGo-Embedded")), "true") ||
		strings.EqualFold(strings.TrimSpace(c.QueryParam("embedded")), "true")

	if mw.IsHTMX(c) || isEmbedded {
		return mw.Render(c, http.StatusOK, pages.InboxContent(conversations, unreadMap, connectionFilter, unreadCount, nil, connections, statusFilter, assignedFilter))
	}
	inboxPage := pages.InboxPage(conversations, unreadMap, connectionFilter, unreadCount, nil, connections, statusFilter, assignedFilter)
	return mw.Render(c, http.StatusOK, inboxPage)
}

// PollConversations handles GET /admin/inbox/conversations/poll — returns the conversation list fragment for 5s polling.
// The response includes the conv-list fragment plus an OOB badge update for the sidebar.
func (h *InboxHandler) PollConversations(c *echo.Context) error {
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(c.Request().Context()); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(c.Request().Context()); ok && id != uuid.Nil {
		workspaceID = id
	}
	connectionFilter := c.QueryParam("connection")
	statusFilter := c.QueryParam("status")
	assignedFilter := c.QueryParam("assigned")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, connectionFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations")
	}

	return mw.Render(c, http.StatusOK, components.ConvList(conversations, unreadMap, connectionFilter, unreadCount, statusFilter, assignedFilter))
}

// ReplyOption holds reply connection options for picker
type ReplyOption = components.ReplyOption

// ChatPanel handles GET /admin/inbox/chat — returns the chat history panel for a contact.
// Query params: contact_id.
func (h *InboxHandler) ChatPanel(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
	if contactIDStr == "" {
		contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if contactIDStr == "" {
		return c.HTML(http.StatusBadRequest, `<div class="p-4 text-red-500">Parâmetro inválido: contact_id é obrigatório.</div>`)
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.HTML(http.StatusBadRequest, `<div class="p-4 text-red-500">ID de contato inválido.</div>`)
	}

	contact, err := h.ContactRepo.GetByID(ctx, workspaceID, contactID)
	if err != nil {
		return c.HTML(http.StatusNotFound, `<div class="p-4 text-red-500">Contato não encontrado.</div>`)
	}

	eng := h.engine()
	currentChat, _ := eng.EnsureChatForContact(ctx, workspaceID, contactID, true)

	// Load the thread messages (full history — no cursor) from chat_messages and reset unread count
	messages, err := eng.ListThread(ctx, workspaceID, contactID, nil, true)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-4 text-red-500">Erro ao carregar chat.</div>`)
	}

	// Resolve connections in workspace to find default/active senders
	var connections []*repository.Connection
	if h.Connections != nil {
		connections, _ = h.Connections.ListByWorkspace(ctx, workspaceID)
	}
	defaultSenders := make(map[string]string)
	for _, conn := range connections {
		if conn.IsDefault || defaultSenders[conn.Channel] == "" {
			defaultSenders[conn.Channel] = conn.SenderIdentity
		}
	}

	// Build reply options
	var replyOptions []ReplyOption
	for _, identity := range contact.Identities {
		// Filter out non-dispatch channels
		if identity.Channel == "whatsapp" || identity.Channel == "whatsapp_cloud" || identity.Channel == "telegram" {
			sender := defaultSenders[identity.Channel]
			if sender != "" {
				replyOptions = append(replyOptions, ReplyOption{
					Channel:        identity.Channel,
					RecipientPhone: identity.SenderIdentity,
					SenderIdentity: sender,
					Label:          fmt.Sprintf("%s (%s)", channelLabelStr(identity.Channel), identity.SenderIdentity),
				})
			}
		}
	}

	var members []repository.WorkspaceMember
	if h.Workspaces != nil && workspaceID != uuid.Nil {
		members, _ = h.Workspaces.ListMembers(ctx, workspaceID)
	}

	var hasWabaIdentity bool
	for _, identity := range contact.Identities {
		if identity.Channel == "whatsapp_cloud" {
			hasWabaIdentity = true
			break
		}
	}

	var windowExpiresAt *time.Time
	if currentChat != nil && currentChat.ServiceWindowExpiresAt != nil {
		windowExpiresAt = currentChat.ServiceWindowExpiresAt
	}

	isWabaBlocked := false
	if hasWabaIdentity {
		isWabaBlocked = currentChat == nil || !currentChat.IsServiceWindowOpen()
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, components.ChatPanel(contact, currentChat, replyOptions, messages, isWabaBlocked, members, windowExpiresAt))
	}

	// Direct page reload -> render the full page with this chat panel pre-opened
	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, "")
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations: "+err.Error())
	}

	chatPanelComp := components.ChatPanel(contact, currentChat, replyOptions, messages, isWabaBlocked, members, windowExpiresAt)
	isEmbedded := strings.EqualFold(strings.TrimSpace(c.Request().Header.Get("X-PerGo-Embedded")), "true") ||
		strings.EqualFold(strings.TrimSpace(c.QueryParam("embedded")), "true")
	if isEmbedded {
		return mw.Render(c, http.StatusOK, pages.InboxContent(conversations, unreadMap, "", unreadCount, chatPanelComp, connections))
	}
	return mw.Render(c, http.StatusOK, pages.InboxPage(conversations, unreadMap, "", unreadCount, chatPanelComp, connections))
}

// channelLabelStr maps to human readable labels
func channelLabelStr(channel string) string {
	switch channel {
	case "whatsapp":
		return "WhatsApp Web"
	case "whatsapp_cloud":
		return "WhatsApp Cloud"
	case "telegram":
		return "Telegram"
	default:
		return channel
	}
}

// PollMessages handles GET /admin/inbox/messages — returns new messages for incremental chat polling.
// Uses a UUID cursor (after_id) to return only messages newer than the last rendered one.
func (h *InboxHandler) PollMessages(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	contactIDStr := c.QueryParam("contact_id")
	if contactIDStr == "" {
		return c.HTML(http.StatusBadRequest, `<div class="p-2 text-red-500 text-xs">Parâmetro inválido: contact_id é obrigatório.</div>`)
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.HTML(http.StatusBadRequest, `<div class="p-2 text-red-500 text-xs">ID de contato inválido.</div>`)
	}

	afterIDStr := c.QueryParam("after_id")
	var afterID *uuid.UUID
	if afterIDStr != "" && afterIDStr != "LAST_ID" {
		id, err := uuid.Parse(afterIDStr)
		if err == nil {
			afterID = &id
		}
	}

	messages, err := h.engine().ListThread(ctx, workspaceID, contactID, afterID, true)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-2 text-red-500 text-xs">Erro ao buscar mensagens.</div>`)
	}

	if len(messages) == 0 {
		if workspaceID != uuid.Nil {
			h.checkBackgroundMessages(c, ctx, workspaceID, contactID)
		}
		return c.NoContent(http.StatusNoContent)
	}

	newLastID := messages[len(messages)-1].ID.String()

	// Render new message bubbles and updated OOB poll anchor
	return mw.Render(c, http.StatusOK, components.PollMessagesResponse(contactID.String(), newLastID, messages))
}

// checkBackgroundMessages checks if any OTHER conversation has new unread messages
// and, if so, fires a showToast HX-Trigger header on the response.
func (h *InboxHandler) checkBackgroundMessages(c *echo.Context, ctx context.Context, workspaceID uuid.UUID, openContactID uuid.UUID) {
	conversations, err := h.engine().ListConversations(ctx, workspaceID, "")
	if err != nil {
		return
	}

	for _, conv := range conversations {
		// Skip the currently open conversation
		if conv.ContactID == openContactID {
			continue
		}
		if conv.Unread || conv.UnreadCount > 0 {
			// Fire toast for this background contact
			trigger := fmt.Sprintf(`{"showToast":{"text":"Nova mensagem de %s"}}`, jsonEscape(conv.ContactName))
			c.Response().Header().Set("HX-Trigger", trigger)
			return
		}
	}
}

// SendMessage handles POST /admin/inbox/send — enqueues an outbound reply via ChatEngine.
// Form params: contact (maps to to), channel, recipient_identity (maps to sender_identity), body.
func (h *InboxHandler) SendMessage(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	contact := c.FormValue("contact")                      // the recipient phone/chat ID (to field)
	channel := c.FormValue("channel")                      // whatsapp / whatsapp_cloud / telegram
	recipientIdentity := c.FormValue("recipient_identity") // the bot/phone identity (sender_identity)
	body := strings.TrimSpace(c.FormValue("body"))
	draftUID := strings.TrimSpace(c.FormValue("draft_uid"))

	// Validate
	if body == "" && draftUID == "" {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Mensagem não pode ser vazia.</span>`)
	}
	if contact == "" || channel == "" {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Parâmetros inválidos.</span>`)
	}

	if workspaceID == uuid.Nil {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Workspace não selecionado.</span>`)
	}

	if h.Publisher == nil && h.Ingestor == nil {
		return c.HTML(http.StatusServiceUnavailable, `<span class="text-red-400">Publisher não disponível.</span>`)
	}

	var err error
	if draftUID != "" {
		_, err = h.engine().PromoteDraft(ctx, workspaceID, chatpkg.ChatRef{
			Channel: channel,
			Phone:   contact,
		}, draftUID, body)
	} else {
		_, err = h.engine().SendMessage(ctx, workspaceID, chatpkg.SendRequest{
			Ref: chatpkg.ChatRef{
				Channel: channel,
				Phone:   contact,
			},
			Body:           body,
			SenderName:     "Agent",
			SenderType:     string(domain.SenderTypeHumanAgent),
			SenderIdentity: recipientIdentity,
			TraceIDPrefix:  "inbox",
			AuditEventType: "chat.message.sent",
		})
	}
	if err != nil {
		var tmplErr *domain.TemplateRequiredError
		if errors.As(err, &tmplErr) || errors.Is(err, chatpkg.ErrServiceWindowExpired) {
			return c.HTML(http.StatusUnprocessableEntity, `<span class="text-amber-400">Janela de atendimento de 24h expirada. Envie um template aprovado para retomar o contato.</span>`)
		}
		return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro ao enviar mensagem: `+escapeHTML(err.Error())+`</span>`)
	}

	// Return 204 so HTMX clears the status and re-polls naturally
	return c.NoContent(http.StatusNoContent)
}

// NewMessageModal renders the new message/template compose modal.
func (h *InboxHandler) NewMessageModal(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	modalType := c.QueryParam("type")
	fromContact := c.QueryParam("from")
	channel := c.QueryParam("channel")
	to := c.QueryParam("to")

	isTemplateOnly := modalType == "template_only"

	var templates []repository.WABATemplate
	if h.Templates != nil {
		var err error
		if isTemplateOnly && to != "" {
			conn, cErr := h.Connections.GetBySenderIdentity(ctx, workspaceID, to)
			if cErr == nil && conn != nil {
				templates, err = h.Templates.ListByConnection(ctx, conn.ID)
			} else {
				templates, err = h.Templates.ListByWorkspace(ctx, workspaceID)
			}
		} else {
			defaultWABAConn, cErr := h.Connections.GetDefaultChannelConnection(ctx, workspaceID, "whatsapp_cloud")
			if cErr == nil && defaultWABAConn != nil {
				templates, err = h.Templates.ListByConnection(ctx, defaultWABAConn.ID)
			} else {
				templates, err = h.Templates.ListByWorkspace(ctx, workspaceID)
			}
		}
		if err != nil {
			slog.WarnContext(ctx, "failed to list waba templates for modal", "workspace_id", workspaceID, "error", err)
		}
	}

	return mw.Render(c, http.StatusOK, components.NewChatModal(templates, fromContact, isTemplateOnly, channel, to))
}

// NewMessageSend enqueues template messages or initializes a new chat.
func (h *InboxHandler) NewMessageSend(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	to := c.FormValue("to")
	channel := c.FormValue("channel")
	isTemplate := c.FormValue("is_template") == "true"
	recipientIdentity := c.FormValue("recipient_identity")

	var body string
	var templateName string
	var language string
	var componentsList []domain.TemplateComponent

	// Resolve connection via sender identity/channel
	var connectionID uuid.UUID
	var senderIdentity string
	if h.Connections != nil {
		if recipientIdentity != "" {
			conn, err := h.Connections.GetBySenderIdentity(ctx, workspaceID, recipientIdentity)
			if err == nil {
				connectionID = conn.ID
				senderIdentity = conn.SenderIdentity
			}
		} else {
			// Find first connected connection for the requested channel in workspace
			conns, err := h.Connections.ListByWorkspace(ctx, workspaceID)
			if err == nil {
				for _, conn := range conns {
					if conn.Channel == channel && conn.Status == "connected" {
						connectionID = conn.ID
						senderIdentity = conn.SenderIdentity
						break
					}
				}
			}
		}
	}

	if isTemplate {
		templateName = c.FormValue("template_name")
		if templateName == "" {
			return c.String(http.StatusBadRequest, "template_name is required")
		}

		language = c.FormValue("language")
		if language == "" && h.Templates != nil {
			// Try to resolve language from registered templates
			var tmpls []repository.WABATemplate
			if connectionID != uuid.Nil {
				tmpls, _ = h.Templates.ListByConnection(ctx, connectionID)
			}
			if len(tmpls) == 0 {
				tmpls, _ = h.Templates.ListByWorkspace(ctx, workspaceID)
			}
			for _, t := range tmpls {
				if t.Name == templateName && t.Language != "" {
					language = t.Language
					break
				}
			}
		}
		if language == "" {
			language = "pt_BR"
		}

		body, componentsList = ExtractFormTemplateParams(c, templateName)
	} else {
		body = strings.TrimSpace(c.FormValue("body"))
		if body == "" {
			return c.String(http.StatusBadRequest, "body cannot be empty")
		}
	}

	if h.Publisher == nil && h.Ingestor == nil {
		return c.String(http.StatusServiceUnavailable, "NATS publisher unavailable")
	}

	var connIDPtr *uuid.UUID
	if connectionID != uuid.Nil {
		connIDPtr = &connectionID
	}
	_, err := h.engine().SendMessage(ctx, workspaceID, chatpkg.SendRequest{
		Ref: chatpkg.ChatRef{
			Channel: channel,
			Phone:   to,
		},
		Body:           body,
		SenderName:     "Agent",
		SenderType:     string(domain.SenderTypeHumanAgent),
		TemplateName:   templateName,
		Language:       language,
		Components:     componentsList,
		ConnectionID:   connIDPtr,
		SenderIdentity: senderIdentity,
		TraceIDPrefix:  "new-chat",
		AuditEventType: "chat.message.sent",
	})
	if err != nil {
		var tmplErr *domain.TemplateRequiredError
		if errors.As(err, &tmplErr) || errors.Is(err, chatpkg.ErrServiceWindowExpired) {
			return c.String(http.StatusUnprocessableEntity, "Customer service window expired. Use a template to reach this contact.")
		}
		return c.String(http.StatusInternalServerError, "failed to enqueue message: "+err.Error())
	}

	c.Response().Header().Set("HX-Trigger", `{"showToast":{"text":"Nova mensagem/template enviada com sucesso!"}}`)
	return c.NoContent(http.StatusOK)
}

// SearchContacts handles GET /admin/contacts/search
func (h *InboxHandler) SearchContacts(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	query := c.QueryParam("q")
	excludeIDStr := c.QueryParam("exclude_id")
	var excludeID uuid.UUID
	if excludeIDStr != "" {
		excludeID, _ = uuid.Parse(excludeIDStr)
	}

	results, err := h.ContactRepo.SearchContacts(ctx, workspaceID, query, excludeID, 10)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return mw.Render(c, http.StatusOK, components.ContactSearchResults(results, excludeID))
}

// MergeContacts handles POST /admin/contacts/merge
func (h *InboxHandler) MergeContacts(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	primaryIDStr := c.QueryParam("primary_id")
	secondaryIDStr := c.QueryParam("secondary_id")

	primaryID, err := uuid.Parse(primaryIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid primary_id")
	}
	secondaryID, err := uuid.Parse(secondaryIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid secondary_id")
	}

	err = h.ContactRepo.MergeContacts(ctx, workspaceID, primaryID, secondaryID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to merge contacts: "+err.Error())
	}

	// Write User Action Log
	if h.UserActionLogs != nil {
		metaBytes, _ := json.Marshal(map[string]string{
			"primary_id":   primaryIDStr,
			"secondary_id": secondaryIDStr,
		})
		logEntry := &repository.UserActionLog{
			WorkspaceID: workspaceID,
			ActorType:   "user",
			ActorID:     "operator",
			ActorName:   "Operator",
			Action:      "contact.merge",
			Source:      "web",
			Metadata:    metaBytes,
		}
		_ = h.UserActionLogs.Insert(ctx, logEntry)
	}

	// Redirect to the newly consolidated primary chat page
	c.Response().Header().Set("HX-Location", fmt.Sprintf("/admin/inbox/chat?contact_id=%s", primaryIDStr))
	return c.NoContent(http.StatusOK)
}

// escapeHTML performs minimal HTML escaping to prevent XSS in string-concatenated HTML.
func escapeHTML(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			result = append(result, []byte("&amp;")...)
		case '<':
			result = append(result, []byte("&lt;")...)
		case '>':
			result = append(result, []byte("&gt;")...)
		case '"':
			result = append(result, []byte("&#34;")...)
		case '\'':
			result = append(result, []byte("&#39;")...)
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}

// jsonEscape escapes a string for safe inclusion in a JSON value (not full JSON encoder,
// but sufficient for simple display names without newlines or unusual chars).
func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// Marshal returns JSON with surrounding quotes; strip them
	return string(b[1 : len(b)-1])
}

// ToggleBot handles POST /admin/contacts/:id/toggle-bot
func (h *InboxHandler) ToggleBot(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	if role, ok := c.Get("actor_role").(string); ok && strings.EqualFold(role, "operator") {
		return c.JSON(http.StatusForbidden, map[string]string{
			"code":    "forbidden",
			"message": "admin role required to toggle bot automation",
		})
	}

	contactIDStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id parameter")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	if h.ContactRepo != nil {
		if _, err := h.ContactRepo.GetByID(ctx, workspaceID, contactID); err != nil {
			return c.String(http.StatusNotFound, "contact not found")
		}
	}

	eng := h.engine()
	currentChat, err := eng.EnsureChatForContact(ctx, workspaceID, contactID, false)
	if err != nil {
		if errors.Is(err, chatpkg.ErrContactNotFound) || errors.Is(err, chatpkg.ErrChatNotFound) {
			return c.String(http.StatusNotFound, "contact not found")
		}
		return c.String(http.StatusInternalServerError, "failed to resolve chat: "+err.Error())
	}

	var change activity.AIChange
	if currentChat != nil && currentChat.EffectiveAI().IsEngaged() {
		change = activity.AIOff()
	} else {
		change = activity.AIResume()
	}

	ref := chatpkg.ChatRef{ContactID: contactID}
	if currentChat != nil && currentChat.ID != uuid.Nil {
		ref.ChatID = currentChat.ID
	}

	updatedChat, err := eng.SetAI(ctx, workspaceID, ref, change)
	if err != nil {
		if errors.Is(err, chatpkg.ErrContactNotFound) || errors.Is(err, chatpkg.ErrChatNotFound) {
			return c.String(http.StatusNotFound, "contact not found")
		}
		return c.String(http.StatusInternalServerError, "failed to update bot state: "+err.Error())
	}

	return mw.Render(c, http.StatusOK, components.BotStatusBadge(updatedChat))
}

// EnableChatAI handles POST /admin/inbox/chats/:id/enable-ai and /admin/inbox/chat_enable_ai
// Query/Form params: enabled (bool, default true), chat_id (if not in URL param).
func (h *InboxHandler) EnableChatAI(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	if role, ok := c.Get("actor_role").(string); ok && strings.EqualFold(role, "operator") {
		return c.JSON(http.StatusForbidden, map[string]string{
			"code":    "forbidden",
			"message": "admin role required to toggle AI automation",
		})
	}

	chatIDStr := c.Param("id")
	if chatIDStr == "" {
		chatIDStr = c.QueryParam("chat_id")
		if chatIDStr == "" {
			chatIDStr = c.FormValue("chat_id")
		}
	}
	if chatIDStr == "" {
		return c.String(http.StatusBadRequest, "chat_id is required")
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid chat_id UUID")
	}

	enabledStr := c.QueryParam("enabled")
	if enabledStr == "" {
		enabledStr = c.FormValue("enabled")
	}
	enabled := true
	if enabledStr != "" {
		enabled = enabledStr == "true" || enabledStr == "1"
	}

	if h.ChatRepo != nil || h.ChatEngine != nil {
		var change activity.AIChange
		if enabled {
			change = activity.AIResume()
		} else {
			change = activity.AIOff()
		}
		chat, err := h.engine().SetAI(ctx, workspaceID, chatpkg.ChatRef{ChatID: chatID}, change)
		if err != nil {
			if errors.Is(err, chatpkg.ErrChatNotFound) {
				return c.String(http.StatusNotFound, "chat not found")
			}
			return c.String(http.StatusInternalServerError, "failed to update chat AI status: "+err.Error())
		}

		if mw.IsHTMX(c) {
			return mw.Render(c, http.StatusOK, components.AIStatusBadge(chat))
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":      "success",
			"chat_id":     chat.ID.String(),
			"ai_disabled": chat.AIDisabled,
			"enabled":     enabled,
		})
	}

	return c.NoContent(http.StatusOK)
}

// AssignChat handles POST /admin/inbox/assign
func (h *InboxHandler) AssignChat(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
	if contactIDStr == "" {
		contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if contactIDStr == "" {
		return c.String(http.StatusBadRequest, "contact_id is required")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	email := strings.TrimSpace(c.FormValue("email"))
	if email == "" {
		email = strings.TrimSpace(c.QueryParam("email"))
	}

	if h.ChatRepo != nil || h.ChatEngine != nil {
		if _, _, err := h.engine().Assign(ctx, workspaceID, chatpkg.ChatRef{ContactID: contactID}, email); err != nil {
			if errors.Is(err, chatpkg.ErrInvalidMember) {
				return c.String(http.StatusBadRequest, fmt.Sprintf("teammate with email %q not found in workspace", email))
			}
			return c.String(http.StatusInternalServerError, "failed to assign chat: "+err.Error())
		}
	}

	c.Response().Header().Set("HX-Trigger", "refreshChats")
	return h.ChatPanel(c)
}

// AddChatTag handles POST /admin/inbox/tags/add
func (h *InboxHandler) AddChatTag(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
	if contactIDStr == "" {
		contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if contactIDStr == "" {
		return c.String(http.StatusBadRequest, "contact_id is required")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	tag := strings.TrimSpace(c.FormValue("tag"))
	if tag == "" {
		tag = strings.TrimSpace(c.QueryParam("tag"))
	}

	if tag != "" {
		if h.ChatRepo != nil || h.ChatEngine != nil {
			_, _ = h.engine().UpdateTags(ctx, workspaceID, chatpkg.ChatRef{ContactID: contactID}, chatpkg.TagMutation{Add: []string{tag}})
		} else if h.ContactRepo != nil {
			_ = h.ContactRepo.AddTags(ctx, workspaceID, contactID, []string{tag})
		}
	}

	c.Response().Header().Set("HX-Trigger", "refreshChats")
	return h.ChatPanel(c)
}

// RemoveChatTag handles POST /admin/inbox/tags/remove
func (h *InboxHandler) RemoveChatTag(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
	if contactIDStr == "" {
		contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if contactIDStr == "" {
		return c.String(http.StatusBadRequest, "contact_id is required")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	tag := strings.TrimSpace(c.QueryParam("tag"))
	if tag == "" {
		tag = strings.TrimSpace(c.FormValue("tag"))
	}

	if tag != "" {
		if h.ChatRepo != nil || h.ChatEngine != nil {
			_, _ = h.engine().UpdateTags(ctx, workspaceID, chatpkg.ChatRef{ContactID: contactID}, chatpkg.TagMutation{Remove: []string{tag}})
		} else if h.ContactRepo != nil {
			if contact, cErr := h.ContactRepo.GetByID(ctx, workspaceID, contactID); cErr == nil && contact != nil {
				var newTags []string
				for _, t := range contact.Tags {
					if !strings.EqualFold(t, tag) {
						newTags = append(newTags, t)
					}
				}
				_ = h.ContactRepo.SetTags(ctx, workspaceID, contactID, newTags)
			}
		}
	}

	c.Response().Header().Set("HX-Trigger", "refreshChats")
	return h.ChatPanel(c)
}

// UpdateChatStatus handles POST /admin/inbox/status
func (h *InboxHandler) UpdateChatStatus(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
	if contactIDStr == "" {
		contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if contactIDStr == "" {
		return c.String(http.StatusBadRequest, "contact_id is required")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	status := strings.ToLower(strings.TrimSpace(c.QueryParam("status")))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(c.FormValue("status")))
	}
	var snoozedUntil *time.Time
	if status == "snoozed" {
		untilStr := strings.TrimSpace(c.QueryParam("snoozed_until"))
		if untilStr == "" {
			untilStr = strings.TrimSpace(c.FormValue("snoozed_until"))
		}
		if untilStr != "" {
			parsed, pErr := time.Parse(time.RFC3339, untilStr)
			if pErr != nil {
				return c.String(http.StatusBadRequest, "invalid snoozed_until timestamp")
			}
			snoozedUntil = &parsed
		}
		if snoozedUntil == nil {
			defaultUntil := time.Now().UTC().Add(24 * time.Hour)
			snoozedUntil = &defaultUntil
		}
	} else if status != "closed" && status != "pending" {
		status = "open"
	}

	if h.ChatRepo != nil || h.ChatEngine != nil {
		if _, _, err := h.engine().UpdateStatus(ctx, workspaceID, chatpkg.ChatRef{ContactID: contactID}, chatpkg.StatusUpdate{
			Status:       status,
			SnoozedUntil: snoozedUntil,
		}); err != nil {
			if errors.Is(err, chatpkg.ErrInvalidStatus) {
				return c.String(http.StatusBadRequest, err.Error())
			}
			return c.String(http.StatusInternalServerError, "failed to update chat status: "+err.Error())
		}
	}

	c.Response().Header().Set("HX-Trigger", "refreshChats")
	return h.ChatPanel(c)
}

// CreateNote handles POST /admin/inbox/notes — creates a private internal note for a contact/chat.
func (h *InboxHandler) CreateNote(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	if workspaceID == uuid.Nil {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Workspace não selecionado.</span>`)
	}

	body := strings.TrimSpace(c.FormValue("body"))
	if body == "" {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Nota não pode ser vazia.</span>`)
	}

	authorName := strings.TrimSpace(c.FormValue("author_name"))
	if authorName == "" {
		authorName = "Operador"
	}

	var chatID uuid.UUID
	var contactID uuid.UUID
	chatIDStr := c.FormValue("chat_id")
	if chatIDStr != "" {
		var err error
		chatID, err = uuid.Parse(chatIDStr)
		if err != nil {
			return c.HTML(http.StatusBadRequest, `<span class="text-red-400">ID de chat inválido.</span>`)
		}
	} else {
		// Resolve via contact_id
		contactIDStr := c.FormValue("contact_id")
		if contactIDStr == "" {
			return c.HTML(http.StatusBadRequest, `<span class="text-red-400">chat_id ou contact_id obrigatório.</span>`)
		}
		var err error
		contactID, err = uuid.Parse(contactIDStr)
		if err != nil {
			return c.HTML(http.StatusBadRequest, `<span class="text-red-400">ID de contato inválido.</span>`)
		}
	}

	if h.ChatRepo == nil && h.ChatEngine == nil {
		return c.HTML(http.StatusServiceUnavailable, `<span class="text-red-400">ChatRepo não configurado.</span>`)
	}

	note, err := h.engine().CreateNote(ctx, workspaceID, chatpkg.NoteRequest{
		Ref: chatpkg.ChatRef{
			ChatID:    chatID,
			ContactID: contactID,
		},
		Body:       body,
		AuthorName: authorName,
		SenderType: "human_agent",
	})
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro ao criar nota: `+escapeHTML(err.Error())+`</span>`)
	}

	if mw.IsHTMX(c) {
		threadMsg := repository.ThreadMessage{
			ID:        note.ID,
			TraceID:   note.UID,
			Direction: string(domain.DirectionInternalNote),
			Body:      note.Body,
			CreatedAt: note.CreatedAt,
			Metadata:  map[string]string{"author_name": authorName, "is_private": "true"},
		}
		return mw.Render(c, http.StatusOK, components.MessageBubble(threadMsg))
	}

	return c.JSON(http.StatusCreated, note)
}

// ToggleReaction handles POST /admin/inbox/reactions — toggles an emoji reaction on a message.
func (h *InboxHandler) ToggleReaction(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	messageUID := strings.TrimSpace(c.FormValue("message_uid"))
	emoji := strings.TrimSpace(c.FormValue("emoji"))
	if messageUID == "" || emoji == "" {
		return c.String(http.StatusBadRequest, "message_uid and emoji are required")
	}

	if h.ChatRepo == nil && h.ChatEngine == nil {
		return c.String(http.StatusInternalServerError, "chat repository not configured")
	}

	if workspaceID == uuid.Nil {
		return c.String(http.StatusUnauthorized, "workspace context required")
	}

	sender := "operator"
	if opEmail := h.resolveCurrentOperatorEmail(c, workspaceID); opEmail != "" {
		sender = opEmail
	}

	res, err := h.engine().React(ctx, workspaceID, chatpkg.ReactRequest{
		MessageUID: messageUID,
		Emoji:      emoji,
		Action:     "toggle",
		Sender:     sender,
	})
	if err != nil {
		if errors.Is(err, chatpkg.ErrMessageNotFound) {
			return c.String(http.StatusNotFound, "message not found")
		}
		return c.String(http.StatusInternalServerError, "failed to mutate reaction: "+err.Error())
	}

	return mw.Render(c, http.StatusOK, components.ReactionBadgeList(messageUID, res.Reactions))
}

// SummarizeChat handles POST /admin/inbox/summarize and POST /admin/inbox/chat/summarize
func (h *InboxHandler) SummarizeChat(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}
	if wsIDStr := c.QueryParam("workspace_id"); wsIDStr != "" && workspaceID == uuid.Nil {
		workspaceID, _ = uuid.Parse(wsIDStr)
	}
	if wsIDStr := c.FormValue("workspace_id"); wsIDStr != "" && workspaceID == uuid.Nil {
		workspaceID, _ = uuid.Parse(wsIDStr)
	}
	if wsIDStr := c.Param("workspace_id"); wsIDStr != "" && workspaceID == uuid.Nil {
		workspaceID, _ = uuid.Parse(wsIDStr)
	}

	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	var chatID uuid.UUID
	var contactID uuid.UUID
	chatIDStr := strings.TrimSpace(c.QueryParam("chat_id"))
	if chatIDStr == "" {
		chatIDStr = strings.TrimSpace(c.FormValue("chat_id"))
	}
	if chatIDStr == "" {
		chatIDStr = strings.TrimSpace(c.Param("chat_id"))
	}

	if chatIDStr != "" {
		var err error
		chatID, err = uuid.Parse(chatIDStr)
		if err != nil {
			return c.String(http.StatusBadRequest, "invalid chat_id")
		}
	} else {
		// Fallback: resolve chat via contact_id if provided
		contactIDStr := strings.TrimSpace(c.QueryParam("contact_id"))
		if contactIDStr == "" {
			contactIDStr = strings.TrimSpace(c.FormValue("contact_id"))
		}
		if contactIDStr != "" {
			contactID, _ = uuid.Parse(contactIDStr)
		}
	}

	if chatID == uuid.Nil && contactID == uuid.Nil {
		return c.String(http.StatusBadRequest, "chat_id is required")
	}

	if h.ChatRepo == nil && h.ChatEngine == nil {
		return c.String(http.StatusServiceUnavailable, "chat repository not configured")
	}

	synopsis, err := h.engine().Summarize(ctx, workspaceID, chatpkg.ChatRef{
		ChatID:    chatID,
		ContactID: contactID,
	}, 50)
	if err != nil {
		if errors.Is(err, chatpkg.ErrChatNotFound) {
			return c.String(http.StatusNotFound, "chat not found")
		}
		return c.String(http.StatusInternalServerError, "failed to summarize chat: "+err.Error())
	}

	// If API caller requested JSON or non-HTMX request
	if !mw.IsHTMX(c) || strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
		return c.JSON(http.StatusOK, synopsis)
	}

	return mw.Render(c, http.StatusOK, components.ChatSummaryPopover(synopsis))
}
