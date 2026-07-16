package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/components"
	"github.com/pablojhp.pergo/templates/pages"
)

// InboxHandler holds dependencies for the conversational inbox.
type InboxHandler struct {
	Repo             *repository.AuditRepository
	Sessions         *repository.RecipientSessionRepository
	Workspaces       *repository.WorkspaceRepository
	Connections      *repository.ConnectionRepository
	Publisher        *queue.JetStreamPublisher
	Templates        *repository.WABATemplateRepository
	ContactRepo      *repository.ContactRepository
	UserActionLogs   *repository.UserActionLogRepository
}

// resolveWorkspaceID reads the active workspace from the cookie.
// Returns uuid.Nil if not set or invalid; callers should handle gracefully.
func resolveWorkspaceID(c *echo.Context) uuid.UUID {
	cookie, err := c.Cookie("pergo-active-workspace")
	if err != nil || cookie == nil || cookie.Value == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// loadConversations fetches conversations and computes unread state.
// Returns conversations, unreadMap keyed by contact ID, and total unread count.
func (h *InboxHandler) loadConversations(c *echo.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, map[string]bool, int, error) {
	ctx := c.Request().Context()

	conversations, err := h.Repo.ListConversations(ctx, workspaceID, channelFilter)
	if err != nil {
		return nil, nil, 0, err
	}

	unreadMap := make(map[string]bool, len(conversations))
	unreadCount := 0

	for i := range conversations {
		conv := &conversations[i]

		isUnread := false
		if h.ContactRepo != nil {
			isUnread, _ = h.ContactRepo.HasUnread(ctx, workspaceID, conv.ContactID)
		}
		key := conv.ContactID.String()
		unreadMap[key] = isUnread
		if isUnread {
			unreadCount++
		}
	}

	return conversations, unreadMap, unreadCount, nil
}

// View handles GET /admin/inbox — renders the full split-pane inbox page.
func (h *InboxHandler) View(c *echo.Context) error {
	workspaceID := resolveWorkspaceID(c)
	connectionFilter := c.QueryParam("connection")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, connectionFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations: "+err.Error())
	}

	var connections []*repository.Connection
	if h.Connections != nil {
		connections, _ = h.Connections.ListByWorkspace(c.Request().Context(), workspaceID)
	}

	inboxPage := pages.InboxPage(conversations, unreadMap, connectionFilter, unreadCount, nil, connections)
 
	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.InboxContent(conversations, unreadMap, connectionFilter, unreadCount, nil, connections))
	}
	return mw.Render(c, http.StatusOK, inboxPage)
}

// PollConversations handles GET /admin/inbox/conversations/poll — returns the conversation list fragment for 5s polling.
// The response includes the conv-list fragment plus an OOB badge update for the sidebar.
func (h *InboxHandler) PollConversations(c *echo.Context) error {
	workspaceID := resolveWorkspaceID(c)
	connectionFilter := c.QueryParam("connection")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, connectionFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations")
	}

	return mw.Render(c, http.StatusOK, components.ConvList(conversations, unreadMap, connectionFilter, unreadCount))
}

// ReplyOption holds reply connection options for picker
type ReplyOption = components.ReplyOption

// ChatPanel handles GET /admin/inbox/chat — returns the chat history panel for a contact.
// Query params: contact_id.
func (h *InboxHandler) ChatPanel(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)

	contactIDStr := c.QueryParam("contact_id")
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

	// Load the thread messages (full history — no cursor)
	messages, err := h.Repo.ListThreadByContact(ctx, workspaceID, contactID, nil)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-4 text-red-500">Erro ao carregar conversa.</div>`)
	}

	// Resolve connections in workspace to find default/active senders
	connections, _ := h.Connections.ListByWorkspace(ctx, workspaceID)
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
		session, sErr := h.Sessions.Get(ctx, workspaceID, wabaIdentity.SenderIdentity, "whatsapp_cloud", wabaIdentity.SenderIdentity)
		if sErr == nil {
			if session.LastInboundAt.IsZero() || time.Since(session.LastInboundAt) > 24*time.Hour {
				isWabaBlocked = true
			}
		} else {
			isWabaBlocked = true
		}
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, components.ChatPanel(contact, replyOptions, messages, isWabaBlocked))
	}

	// Direct page reload -> render the full page with this chat panel pre-opened
	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, "")
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations: "+err.Error())
	}

	chatPanelComp := components.ChatPanel(contact, replyOptions, messages, isWabaBlocked)
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
	workspaceID := resolveWorkspaceID(c)

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
	workspaceID := resolveWorkspaceID(c)

	contact := c.FormValue("contact")           // the recipient phone/chat ID (to field)
	channel := c.FormValue("channel")            // whatsapp / whatsapp_cloud / telegram
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

	// Return 204 so HTMX clears the status and re-polls naturally
	return c.NoContent(http.StatusNoContent)
}

// NewMessageModal renders the new message/template compose modal.
func (h *InboxHandler) NewMessageModal(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)
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
			// Non-blocking log
		}
	}

	return mw.Render(c, http.StatusOK, components.NewChatModal(templates, fromContact, isTemplateOnly, channel, to))
}

// NewMessageSend enqueues template messages or initializes a new chat.
func (h *InboxHandler) NewMessageSend(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	to := c.FormValue("to")
	channel := c.FormValue("channel")
	isTemplate := c.FormValue("is_template") == "true"
	recipientIdentity := c.FormValue("recipient_identity")

	var body string
	var templateName string
	var componentsList []domain.TemplateComponent

	if isTemplate {
		templateName = c.FormValue("template_name")
		if templateName == "" {
			return c.String(http.StatusBadRequest, "template_name is required")
		}
		// Read params
		var params []domain.TemplateParameter
		for i := 1; i <= 3; i++ {
			val := c.FormValue(fmt.Sprintf("param_%d", i))
			if val != "" {
				params = append(params, domain.TemplateParameter{
					Type: "text",
					Text: val,
				})
			}
		}
		componentsList = []domain.TemplateComponent{
			{
				Type:       "body",
				Parameters: params,
			},
		}
		body = fmt.Sprintf("[Template: %s] Params: %v", templateName, params)
	} else {
		body = strings.TrimSpace(c.FormValue("body"))
		if body == "" {
			return c.String(http.StatusBadRequest, "body cannot be empty")
		}
	}

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
		qMsg.Language = "pt_BR" // Default language
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
		_ = h.Sessions.Upsert(ctx, workspaceID, to, channel, senderIdentity, time.Now().UTC())
		_ = h.Sessions.UpdateLastReadAt(ctx, workspaceID, to, channel, senderIdentity, time.Now().UTC())
	}

	c.Response().Header().Set("HX-Trigger", `{"showToast":{"text":"Nova mensagem/template enviada com sucesso!"}}`)
	return c.NoContent(http.StatusOK)
}

// SearchContacts handles GET /admin/contacts/search
func (h *InboxHandler) SearchContacts(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)
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
	workspaceID := resolveWorkspaceID(c)
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

// safeInitial returns the first character of a string, uppercased, safely.
func safeInitial(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return "?"
	}
	r := runes[0]
	if r >= 'a' && r <= 'z' {
		r -= 32
	}
	return string(r)
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
