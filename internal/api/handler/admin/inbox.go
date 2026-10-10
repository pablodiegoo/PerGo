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
	"github.com/pablojhp.pergo/internal/domain"
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
	Sessions          *repository.RecipientSessionRepository
	Workspaces        *repository.WorkspaceRepository
	Connections       *repository.ConnectionRepository
	Publisher         MessagePublisher
	Templates         *repository.WABATemplateRepository
	ContactRepo       *repository.ContactRepository
	UserActionLogs    *repository.UserActionLogRepository
	ChatRepo          *repository.ChatRepository
	Presence          *presence.Tracker
	WebhookSubRepo    *repository.WebhookSubscriptionRepository
	WebhookDispatcher webhook.WebhookDispatcher
}

// loadConversations fetches conversations and computes unread state.
// Returns conversations, unreadMap keyed by contact ID, and total unread count.
func (h *InboxHandler) loadConversations(c *echo.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, map[string]bool, int, error) {
	ctx := c.Request().Context()

	conversations, err := h.Repo.ListConversations(ctx, workspaceID, channelFilter)
	if err != nil {
		return nil, nil, 0, err
	}

	statusFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("status")))
	assignedFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("assigned")))
	tagFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("tag")))
	searchFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("search")))

	unreadMap := make(map[string]bool, len(conversations))
	unreadCount := 0
	filtered := make([]repository.ConversationSummary, 0, len(conversations))

	for i := range conversations {
		conv := &conversations[i]

		if h.ChatRepo != nil && workspaceID != uuid.Nil {
			if chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, conv.ContactID); cErr == nil && chat != nil {
				conv.Status = string(chat.Status)
				conv.AssignedEmail = chat.AssignedEmail
				conv.Tags = chat.Tags
			}
		}
		if conv.Status == "" {
			conv.Status = "open"
		}

		// Apply status filter
		switch statusFilter {
		case "closed":
			if conv.Status != "closed" {
				continue
			}
		case "unassigned":
			if conv.Status == "closed" || (conv.AssignedEmail != nil && *conv.AssignedEmail != "") {
				continue
			}
		case "all":
			// no status filtering
		default: // "open" or empty
			if conv.Status == "closed" {
				continue
			}
		}

		// Apply assigned filter
		if assignedFilter != "" {
			if conv.AssignedEmail == nil || strings.ToLower(*conv.AssignedEmail) != assignedFilter {
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

		isUnread := false
		if h.ContactRepo != nil {
			isUnread, _ = h.ContactRepo.HasUnread(ctx, workspaceID, conv.ContactID)
		}
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

	inboxPage := pages.InboxPage(conversations, unreadMap, connectionFilter, unreadCount, nil, connections, statusFilter)

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.InboxContent(conversations, unreadMap, connectionFilter, unreadCount, nil, connections, statusFilter))
	}
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

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, connectionFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations")
	}

	return mw.Render(c, http.StatusOK, components.ConvList(conversations, unreadMap, connectionFilter, unreadCount, statusFilter))
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

	// Mark all conversation sessions for this contact as read
	if h.Sessions != nil && workspaceID != uuid.Nil {
		_ = h.Sessions.UpdateLastReadAtByContact(ctx, workspaceID, contactID, time.Now().UTC())
	}

	// Reset unread count on the parent Chat if ChatRepo is present
	var currentChat *domain.Chat
	if h.ChatRepo != nil && workspaceID != uuid.Nil {
		if chat, err := h.ChatRepo.GetChatByContact(ctx, workspaceID, contactID); err == nil && chat != nil {
			_ = h.ChatRepo.UpdateChatUnreadCount(ctx, workspaceID, chat.ID, 0)
			currentChat = chat
		} else if chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID); cErr == nil && chat != nil {
			_ = h.ChatRepo.UpdateChatUnreadCount(ctx, workspaceID, chat.ID, 0)
			currentChat = chat
		}
	}

	// Load the thread messages (full history — no cursor)
	messages, err := h.Repo.ListThreadByContact(ctx, workspaceID, contactID, nil)
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

	isWabaBlocked := false
	var wabaIdentity *domain.ContactIdentity
	for _, identity := range contact.Identities {
		if identity.Channel == "whatsapp_cloud" {
			wabaIdentity = &identity
			break
		}
	}
	if wabaIdentity != nil && h.Sessions != nil && workspaceID != uuid.Nil {
		var hasOpenWindow bool
		var checkedAny bool
		for _, conn := range connections {
			if conn.Channel == "whatsapp_cloud" && (conn.Status == "connected" || conn.Status == "active" || conn.Status == "") {
				checkedAny = true
				sessKey := domain.SessionKey{
					WorkspaceID:       workspaceID,
					RecipientPhone:    wabaIdentity.SenderIdentity,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: conn.SenderIdentity,
				}
				session, sErr := h.Sessions.Get(ctx, sessKey)
				if sErr == nil && session != nil {
					windowDuration := 24 * time.Hour
					if session.EntryPointType == "ctwa" {
						windowDuration = 72 * time.Hour
					}
					if !session.LastInboundAt.IsZero() && time.Since(session.LastInboundAt) <= windowDuration {
						hasOpenWindow = true
						break
					}
				}
			}
		}
		if !checkedAny {
			wabaSender := defaultSenders["whatsapp_cloud"]
			if wabaSender != "" {
				sessKey := domain.SessionKey{
					WorkspaceID:       workspaceID,
					RecipientPhone:    wabaIdentity.SenderIdentity,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: wabaSender,
				}
				session, sErr := h.Sessions.Get(ctx, sessKey)
				if sErr == nil && session != nil {
					windowDuration := 24 * time.Hour
					if session.EntryPointType == "ctwa" {
						windowDuration = 72 * time.Hour
					}
					if !session.LastInboundAt.IsZero() && time.Since(session.LastInboundAt) <= windowDuration {
						hasOpenWindow = true
					}
				}
			}
		}
		isWabaBlocked = !hasOpenWindow
	}

	var chat *domain.Chat
	if h.ChatRepo != nil && workspaceID != uuid.Nil {
		chat, _ = h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
	}
	var members []repository.WorkspaceMember
	if h.Workspaces != nil && workspaceID != uuid.Nil {
		members, _ = h.Workspaces.ListMembers(ctx, workspaceID)
	}

	if currentChat == nil {
		currentChat = chat
	}

	var windowExpiresAt *time.Time
	if currentChat != nil && currentChat.ServiceWindowExpiresAt != nil {
		windowExpiresAt = currentChat.ServiceWindowExpiresAt
		isWabaBlocked = !currentChat.IsServiceWindowOpen()
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

	messages, err := h.Repo.ListThreadByContact(ctx, workspaceID, contactID, afterID)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-2 text-red-500 text-xs">Erro ao buscar mensagens.</div>`)
	}

	if len(messages) == 0 {
		if workspaceID != uuid.Nil {
			h.checkBackgroundMessages(c, ctx, workspaceID, contactID)
		}
		return c.NoContent(http.StatusNoContent)
	}

	// Update last_read_at since operator is actively viewing this conversation
	if h.Sessions != nil && workspaceID != uuid.Nil {
		_ = h.Sessions.UpdateLastReadAtByContact(ctx, workspaceID, contactID, time.Now().UTC())
	}

	newLastID := messages[len(messages)-1].ID.String()

	// Render new message bubbles and updated OOB poll anchor
	return mw.Render(c, http.StatusOK, components.PollMessagesResponse(contactID.String(), newLastID, messages))
}

// checkBackgroundMessages checks if any OTHER conversation has new unread messages
// and, if so, fires a showToast HX-Trigger header on the response.
func (h *InboxHandler) checkBackgroundMessages(c *echo.Context, ctx context.Context, workspaceID uuid.UUID, openContactID uuid.UUID) {
	if h.Repo == nil {
		return
	}
	conversations, err := h.Repo.ListConversations(ctx, workspaceID, "")
	if err != nil {
		return
	}

	for _, conv := range conversations {
		// Skip the currently open conversation
		if conv.ContactID == openContactID {
			continue
		}
		// Check unread state for this background conversation
		if h.ContactRepo == nil {
			continue
		}
		isUnread, _ := h.ContactRepo.HasUnread(ctx, workspaceID, conv.ContactID)
		if isUnread {
			// Fire toast for this background contact
			trigger := fmt.Sprintf(`{"showToast":{"text":"Nova mensagem de %s"}}`, jsonEscape(conv.ContactName))
			c.Response().Header().Set("HX-Trigger", trigger)
			return
		}
	}
}

// SendMessage handles POST /admin/inbox/send — enqueues an outbound reply via NATS JetStream.
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

	// Validate
	if body == "" {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Mensagem não pode ser vazia.</span>`)
	}
	if contact == "" || channel == "" {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Parâmetros inválidos.</span>`)
	}

	if workspaceID == uuid.Nil {
		return c.HTML(http.StatusBadRequest, `<span class="text-red-400">Workspace não selecionado.</span>`)
	}

	// Resolve connection via sender identity
	var connectionID uuid.UUID
	if h.Connections != nil && recipientIdentity != "" {
		conn, err := h.Connections.GetBySenderIdentity(ctx, workspaceID, recipientIdentity)
		if err == nil {
			connectionID = conn.ID
		}
	}

	// Build and publish QueueMessage
	traceID := "inbox-" + uuid.New().String()
	qMsg := domain.QueueMessage{
		WorkspaceID:    workspaceID,
		ConnectionID:   connectionID,
		SenderIdentity: recipientIdentity,
		TraceID:        traceID,
		To:             contact,
		Channel:        channel,
		Body:           body,
		QueuedAt:       time.Now().UTC(),
	}

	if h.Publisher == nil {
		return c.HTML(http.StatusServiceUnavailable, `<span class="text-red-400">Publisher não disponível.</span>`)
	}

	data, err := json.Marshal(qMsg)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro interno ao serializar mensagem.</span>`)
	}

	if err := h.Publisher.Publish(ctx, "messages.outbound", data, traceID); err != nil {
		return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro ao enviar mensagem: `+escapeHTML(err.Error())+`</span>`)
	}

	if h.ContactRepo != nil {
		cProfile, err := h.ContactRepo.ResolveContact(ctx, workspaceID, channel, contact, "", "", "")
		if err == nil && cProfile != nil {
			now := time.Now().UTC()
			_ = h.ContactRepo.UpdateBotState(ctx, workspaceID, cProfile.ID, false, &now)

			if h.ChatRepo != nil {
				var connID *uuid.UUID
				if connectionID != uuid.Nil {
					connID = &connectionID
				}
				chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, connID, cProfile.ID)
				if cErr == nil && chat != nil {
					_ = h.ChatRepo.SetAIDisabled(ctx, workspaceID, chat.ID, true)
					_ = h.ChatRepo.TouchLastMessageAt(ctx, workspaceID, chat.ID, now)

					if h.Publisher != nil {
						handoffPayload, _ := json.Marshal(map[string]interface{}{
							"event":        "chat.handoff.human_takeover",
							"workspace_id": workspaceID.String(),
							"chat_id":      chat.ID.String(),
							"contact_id":   cProfile.ID.String(),
							"timestamp":    now.Format(time.RFC3339),
						})
						_ = h.Publisher.Publish(ctx, "chat.handoff.human_takeover", handoffPayload, traceID)
						_ = h.Publisher.Publish(ctx, fmt.Sprintf("chat.handoff.%s", workspaceID.String()), handoffPayload, traceID)
					}

					chatMsg := &domain.ChatMessage{
						ChatID:      chat.ID,
						WorkspaceID: workspaceID,
						UID:         traceID,
						Direction:   domain.DirectionOutbound,
						SenderType:  domain.SenderTypeHumanAgent,
						SenderName:  "Agent",
						SenderID:    recipientIdentity,
						Body:        body,
						CreatedAt:   now,
					}
					_ = h.ChatRepo.AddChatMessage(ctx, chatMsg)

					if h.Repo != nil {
						auditPayload, _ := json.Marshal(map[string]interface{}{
							"chat_id":         chat.ID.String(),
							"contact_id":      cProfile.ID.String(),
							"to":              contact,
							"channel":         channel,
							"sender_identity": recipientIdentity,
							"body":            body,
							"sender_type":     "human_agent",
						})
						_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
							ID:          uuid.New(),
							WorkspaceID: workspaceID,
							TraceID:     traceID,
							EventType:   "chat.message.sent",
							Payload:     auditPayload,
							CreatedAt:   now,
						})
					}
				}
			}
		}
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

	// Upsert contact profile immediately
	if h.ContactRepo != nil {
		_, _ = h.ContactRepo.ResolveContact(ctx, workspaceID, channel, to, to, "", "")
	}

	traceID := "new-chat-" + uuid.New().String()

	// Create NATS outbound QueueMessage
	qMsg := domain.QueueMessage{
		WorkspaceID:    workspaceID,
		ConnectionID:   connectionID,
		SenderIdentity: senderIdentity,
		TraceID:        traceID,
		To:             to,
		Channel:        channel,
		Body:           body,
		QueuedAt:       time.Now().UTC(),
	}

	if isTemplate {
		qMsg.TemplateName = templateName
		qMsg.Language = language
		qMsg.Components = componentsList
	}

	if h.Publisher == nil {
		return c.String(http.StatusServiceUnavailable, "NATS publisher unavailable")
	}

	data, err := json.Marshal(qMsg)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to serialize message")
	}

	if err := h.Publisher.Publish(ctx, "messages.outbound", data, traceID); err != nil {
		return c.String(http.StatusInternalServerError, "failed to enqueue message: "+err.Error())
	}

	// Upsert session to make sure it exists and registers sending
	if h.Sessions != nil {
		sessKey := domain.SessionKey{
			WorkspaceID:       workspaceID,
			RecipientPhone:    to,
			Channel:           channel,
			RecipientIdentity: senderIdentity,
		}
		_ = h.Sessions.Upsert(ctx, sessKey, time.Now().UTC(), "standard")
		_ = h.Sessions.UpdateLastReadAt(ctx, sessKey, time.Now().UTC())
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

	contactIDStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id parameter")
	}
	contactID, err := uuid.Parse(contactIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid contact_id")
	}

	contact, err := h.ContactRepo.GetByID(ctx, workspaceID, contactID)
	if err != nil {
		return c.String(http.StatusNotFound, "contact not found")
	}

	newActive := !contact.BotActive
	var pausedAt *time.Time
	if !newActive {
		now := time.Now().UTC()
		pausedAt = &now
	}

	err = h.ContactRepo.UpdateBotState(ctx, workspaceID, contactID, newActive, pausedAt)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to update bot state: "+err.Error())
	}

	contact.BotActive = newActive
	contact.BotPausedAt = pausedAt
	return mw.Render(c, http.StatusOK, components.BotStatusBadge(contact))
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

	if h.ChatRepo != nil {
		if err := h.ChatRepo.SetAIDisabled(ctx, workspaceID, chatID, !enabled); err != nil {
			return c.String(http.StatusInternalServerError, "failed to update chat AI status: "+err.Error())
		}

		chat, err := h.ChatRepo.GetChat(ctx, workspaceID, chatID)
		if err != nil {
			return c.String(http.StatusNotFound, "chat not found")
		}

		if h.ContactRepo != nil {
			var pausedAt *time.Time
			if !enabled {
				now := time.Now().UTC()
				pausedAt = &now
			}
			_ = h.ContactRepo.UpdateBotState(ctx, workspaceID, chat.ContactID, enabled, pausedAt)
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

	if h.ChatRepo != nil {
		chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
		if cErr != nil {
			return c.String(http.StatusInternalServerError, "failed to find or create chat: "+cErr.Error())
		}

		if email == "" {
			if err := h.ChatRepo.AssignChat(ctx, workspaceID, chat.ID, nil, nil); err != nil {
				return c.String(http.StatusInternalServerError, "failed to unassign chat: "+err.Error())
			}
		} else {
			var memberID *uuid.UUID
			var memberEmail *string
			if h.Workspaces != nil {
				member, mErr := h.Workspaces.GetMemberByEmail(ctx, workspaceID, email)
				if mErr != nil || member == nil {
					return c.String(http.StatusBadRequest, fmt.Sprintf("teammate with email %q not found in workspace", email))
				}
				memberID = &member.ID
				memberEmail = &member.Email
			} else {
				memberEmail = &email
			}
			if err := h.ChatRepo.AssignChat(ctx, workspaceID, chat.ID, memberID, memberEmail); err != nil {
				return c.String(http.StatusInternalServerError, "failed to assign chat: "+err.Error())
			}
		}

		if h.Repo != nil {
			action := "assign"
			if email == "" {
				action = "unassign"
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"chat_id":        chat.ID.String(),
				"contact_id":     contactID.String(),
				"assigned_email": email,
				"action":         action,
			})
			_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: workspaceID,
				TraceID:     fmt.Sprintf("assign-%s", uuid.New().String()),
				EventType:   "chat.assigned",
				Payload:     auditPayload,
				CreatedAt:   time.Now().UTC(),
			})
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
		if h.ChatRepo != nil {
			chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
			if cErr == nil && chat != nil {
				alreadyExists := false
				for _, t := range chat.Tags {
					if strings.EqualFold(t, tag) {
						alreadyExists = true
						break
					}
				}
				if !alreadyExists {
					newTags := append(chat.Tags, tag)
					_ = h.ChatRepo.SetChatTags(ctx, workspaceID, chat.ID, newTags)
				}
				if h.Repo != nil {
					auditPayload, _ := json.Marshal(map[string]interface{}{
						"chat_id":    chat.ID.String(),
						"contact_id": contactID.String(),
						"tag":        tag,
						"action":     "add",
					})
					_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
						ID:          uuid.New(),
						WorkspaceID: workspaceID,
						TraceID:     fmt.Sprintf("tag-add-%s", uuid.New().String()),
						EventType:   "chat.tag.updated",
						Payload:     auditPayload,
						CreatedAt:   time.Now().UTC(),
					})
				}
			}
		}
		if h.ContactRepo != nil {
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
		if h.ChatRepo != nil {
			chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
			if cErr == nil && chat != nil {
				var newTags []string
				for _, t := range chat.Tags {
					if !strings.EqualFold(t, tag) {
						newTags = append(newTags, t)
					}
				}
				_ = h.ChatRepo.SetChatTags(ctx, workspaceID, chat.ID, newTags)
				if h.Repo != nil {
					auditPayload, _ := json.Marshal(map[string]interface{}{
						"chat_id":    chat.ID.String(),
						"contact_id": contactID.String(),
						"tag":        tag,
						"action":     "remove",
					})
					_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
						ID:          uuid.New(),
						WorkspaceID: workspaceID,
						TraceID:     fmt.Sprintf("tag-remove-%s", uuid.New().String()),
						EventType:   "chat.tag.updated",
						Payload:     auditPayload,
						CreatedAt:   time.Now().UTC(),
					})
				}
			}
		}
		if h.ContactRepo != nil {
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
	if status != "closed" {
		status = "open"
	}

	if h.ChatRepo != nil {
		chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
		if cErr != nil {
			return c.String(http.StatusInternalServerError, "failed to find or create chat: "+cErr.Error())
		}
		if err := h.ChatRepo.UpdateChatStatus(ctx, workspaceID, chat.ID, status); err != nil {
			return c.String(http.StatusInternalServerError, "failed to update chat status: "+err.Error())
		}
		if h.Repo != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"chat_id":    chat.ID.String(),
				"contact_id": contactID.String(),
				"status":     status,
			})
			_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: workspaceID,
				TraceID:     fmt.Sprintf("status-%s", uuid.New().String()),
				EventType:   "chat.status.updated",
				Payload:     auditPayload,
				CreatedAt:   time.Now().UTC(),
			})
		}
	}

	if h.ContactRepo != nil {
		if status == "closed" {
			_ = h.ContactRepo.CloseThread(ctx, workspaceID, contactID)
		} else {
			_ = h.ContactRepo.ReopenThread(ctx, workspaceID, contactID)
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
		contactID, err := uuid.Parse(contactIDStr)
		if err != nil {
			return c.HTML(http.StatusBadRequest, `<span class="text-red-400">ID de contato inválido.</span>`)
		}
		if h.ChatRepo != nil {
			chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
			if cErr != nil {
				return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro ao localizar chat.</span>`)
			}
			chatID = chat.ID
		}
	}

	if h.ChatRepo == nil {
		return c.HTML(http.StatusServiceUnavailable, `<span class="text-red-400">ChatRepo não configurado.</span>`)
	}

	note, err := h.ChatRepo.CreateInternalNote(ctx, workspaceID, chatID, authorName, "human_agent", body)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<span class="text-red-400">Erro ao criar nota: `+escapeHTML(err.Error())+`</span>`)
	}

	if h.Repo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     chatID.String(),
			"note_id":     note.ID.String(),
			"author_name": authorName,
			"body":        body,
			"sender_type": "human_agent",
			"is_private":  true,
		})
		_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: workspaceID,
			TraceID:     note.UID,
			EventType:   "chat.note.created",
			Payload:     auditPayload,
			CreatedAt:   note.CreatedAt,
		})
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

// APICreateNote handles POST /api/v1/chats/:chat_id/notes
func (h *InboxHandler) APICreateNote(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	chatIDStr := c.Param("chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid chat_id"})
	}

	var req struct {
		Body       string `json:"body"`
		AuthorName string `json:"author_name"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "body cannot be empty"})
	}
	if req.AuthorName == "" {
		req.AuthorName = "AI Assistant"
	}

	if h.ChatRepo == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "chat repository not configured"})
	}

	note, err := h.ChatRepo.CreateInternalNote(ctx, workspaceID, chatID, req.AuthorName, "ai_agent", req.Body)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "chat not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if h.Repo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     chatID.String(),
			"note_id":     note.ID.String(),
			"author_name": req.AuthorName,
			"body":        req.Body,
			"sender_type": "ai_agent",
			"is_private":  true,
		})
		_ = h.Repo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: workspaceID,
			TraceID:     note.UID,
			EventType:   "chat.note.created",
			Payload:     auditPayload,
			CreatedAt:   note.CreatedAt,
		})
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

	if h.ChatRepo == nil {
		return c.String(http.StatusInternalServerError, "chat repository not configured")
	}

	if workspaceID == uuid.Nil {
		return c.String(http.StatusUnauthorized, "workspace context required")
	}

	var chatIDStr string
	msg, err := h.ChatRepo.GetChatMessageByUID(ctx, workspaceID, messageUID)
	if err != nil || msg == nil {
		return c.String(http.StatusNotFound, "message not found")
	}
	chatIDStr = msg.ChatID.String()

	sender := "operator"
	if email, ok := c.Get("user_email").(string); ok && email != "" {
		sender = email
	}

	existingReactions, err := h.ChatRepo.GetReactions(ctx, workspaceID, messageUID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to get reactions: "+err.Error())
	}

	hasReacted := false
	for _, r := range existingReactions {
		if r.Emoji == emoji && (r.Sender == sender || r.Sender == "operator") {
			hasReacted = true
			break
		}
	}

	var updatedReactions []domain.Reaction
	action := "add"
	if hasReacted {
		action = "remove"
		updatedReactions, err = h.ChatRepo.RemoveReaction(ctx, workspaceID, messageUID, emoji, sender)
	} else {
		updatedReactions, err = h.ChatRepo.AddReaction(ctx, workspaceID, messageUID, domain.Reaction{
			Emoji:     emoji,
			Sender:    sender,
			CreatedAt: time.Now().UTC(),
		})
	}
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to mutate reaction: "+err.Error())
	}

	traceID := uuid.New().String()
	rxEvent := domain.ReactionUpdatedPayload{
		Event:       "message.reaction.updated",
		WorkspaceID: workspaceID.String(),
		ChatID:      chatIDStr,
		MessageUID:  messageUID,
		Emoji:       emoji,
		Sender:      sender,
		Action:      action,
		Reactions:   updatedReactions,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if payloadBytes, mErr := json.Marshal(rxEvent); mErr == nil {
		if h.Publisher != nil {
			_ = h.Publisher.Publish(ctx, "messages.events.reaction_updated", payloadBytes, traceID)
		}
		if h.WebhookDispatcher != nil && h.WebhookSubRepo != nil {
			subs, sErr := h.WebhookSubRepo.ListByWorkspace(ctx, workspaceID)
			if sErr == nil {
				for _, sub := range subs {
					if !sub.Active {
						continue
					}
					if webhook.MatchesAny(sub.EventTypes, "message.reaction.updated") {
						task := webhook.WebhookDeliveryTask{
							ID:             uuid.New(),
							SubscriptionID: sub.ID,
							WorkspaceID:    workspaceID,
							Event:          "message.reaction.updated",
							TraceID:        traceID,
							MessageID:      messageUID,
							Payload:        payloadBytes,
							Mode:           "outbound",
						}
						_ = h.WebhookDispatcher.Dispatch(ctx, task)
					}
				}
			}
		}
	}

	return mw.Render(c, http.StatusOK, components.ReactionBadgeList(messageUID, updatedReactions))
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
			contactID, err := uuid.Parse(contactIDStr)
			if err == nil && h.ChatRepo != nil {
				chat, cErr := h.ChatRepo.FindOrCreateChat(ctx, workspaceID, nil, contactID)
				if cErr == nil && chat != nil {
					chatID = chat.ID
				}
			}
		}
	}

	if chatID == uuid.Nil {
		return c.String(http.StatusBadRequest, "chat_id is required")
	}

	if h.ChatRepo == nil {
		return c.String(http.StatusServiceUnavailable, "chat repository not configured")
	}

	chat, err := h.ChatRepo.GetChat(ctx, workspaceID, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) {
			return c.String(http.StatusNotFound, "chat not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get chat: "+err.Error())
	}

	messages, err := h.ChatRepo.ListChatMessages(ctx, workspaceID, chatID, "", "", 50)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list chat messages: "+err.Error())
	}

	synopsis := domain.SummarizeMessages(messages, chat)

	// If API caller requested JSON or non-HTMX request
	if !mw.IsHTMX(c) || strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
		return c.JSON(http.StatusOK, synopsis)
	}

	return mw.Render(c, http.StatusOK, components.ChatSummaryPopover(synopsis))
}

