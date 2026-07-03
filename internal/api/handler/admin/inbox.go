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
	Repo        *repository.AuditRepository
	Sessions    *repository.RecipientSessionRepository
	Workspaces  *repository.WorkspaceRepository
	Connections *repository.ConnectionRepository
	Publisher   *queue.JetStreamPublisher
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
// Returns conversations, unreadMap keyed by "from|channel|recipientIdentity", and total unread count.
func (h *InboxHandler) loadConversations(c *echo.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, map[string]bool, int, error) {
	ctx := c.Request().Context()

	conversations, err := h.Repo.ListConversations(ctx, workspaceID, channelFilter)
	if err != nil {
		return nil, nil, 0, err
	}

	unreadMap := make(map[string]bool, len(conversations))
	unreadCount := 0

	for _, conv := range conversations {
		isUnread := false
		if h.Sessions != nil {
			session, sErr := h.Sessions.Get(ctx, workspaceID, conv.From, conv.Channel, conv.RecipientIdentity)
			if sErr == nil {
				// Unread if session has never been read, or if last message is after last read
				if session.LastReadAt == nil || conv.LastMessageTime.After(*session.LastReadAt) {
					isUnread = true
				}
			} else {
				// Session doesn't exist yet — treat as unread
				isUnread = true
			}
		}
		key := conv.From + "|" + conv.Channel + "|" + conv.RecipientIdentity
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
	channelFilter := c.QueryParam("channel")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, channelFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations: "+err.Error())
	}

	inboxPage := pages.InboxPage(conversations, unreadMap, channelFilter, unreadCount)

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.InboxContent(conversations, unreadMap, channelFilter, unreadCount))
	}
	return mw.Render(c, http.StatusOK, inboxPage)
}

// PollConversations handles GET /admin/inbox/conversations/poll — returns the conversation list fragment for 5s polling.
// The response includes the conv-list fragment plus an OOB badge update for the sidebar.
func (h *InboxHandler) PollConversations(c *echo.Context) error {
	workspaceID := resolveWorkspaceID(c)
	channelFilter := c.QueryParam("channel")

	conversations, unreadMap, unreadCount, err := h.loadConversations(c, workspaceID, channelFilter)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load conversations")
	}

	return mw.Render(c, http.StatusOK, components.ConvList(conversations, unreadMap, channelFilter, unreadCount))
}

// ChatPanel handles GET /admin/inbox/chat — returns the chat history panel for a contact.
// Query params: from, channel, to (recipient identity).
func (h *InboxHandler) ChatPanel(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)

	from := c.QueryParam("from")
	channel := c.QueryParam("channel")
	to := c.QueryParam("to")

	if from == "" || channel == "" {
		return c.HTML(http.StatusBadRequest, `<div class="p-4 text-red-500">Parâmetros inválidos: from e channel são obrigatórios.</div>`)
	}

	// Mark conversation as read
	if h.Sessions != nil && workspaceID != uuid.Nil {
		_ = h.Sessions.UpdateLastReadAt(ctx, workspaceID, from, channel, to, time.Now().UTC())
	}

	// Load the thread messages (full history — no cursor)
	messages, err := h.Repo.ListThread(ctx, workspaceID, from, channel, to, nil)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-4 text-red-500">Erro ao carregar conversa.</div>`)
	}

	return mw.Render(c, http.StatusOK, components.ChatPanel(from, channel, to, messages))
}

// PollMessages handles GET /admin/inbox/messages — returns new messages for incremental chat polling.
// Uses a UUID cursor (after_id) to return only messages newer than the last rendered one.
// If new messages belong to a different conversation than the open one (different from/channel/to),
// sets HX-Trigger to showToast so the page can display a notification.
func (h *InboxHandler) PollMessages(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)

	from := c.QueryParam("from")
	channel := c.QueryParam("channel")
	to := c.QueryParam("to")
	afterIDStr := c.QueryParam("after_id")

	var afterID *uuid.UUID
	if afterIDStr != "" && afterIDStr != "LAST_ID" {
		id, err := uuid.Parse(afterIDStr)
		if err == nil {
			afterID = &id
		}
	}

	messages, err := h.Repo.ListThread(ctx, workspaceID, from, channel, to, afterID)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, `<div class="p-2 text-red-500 text-xs">Erro ao buscar mensagens.</div>`)
	}

	if len(messages) == 0 {
		// Check if there are new messages from OTHER conversations — trigger toast if so.
		if from != "" && workspaceID != uuid.Nil {
			h.checkBackgroundMessages(c, ctx, workspaceID, from, channel, to)
		}
		return c.String(http.StatusNoContent, "")
	}

	// Update last_read_at since operator is actively viewing this conversation
	if h.Sessions != nil && workspaceID != uuid.Nil {
		_ = h.Sessions.UpdateLastReadAt(ctx, workspaceID, from, channel, to, time.Now().UTC())
	}

	// Render new message bubbles
	return mw.Render(c, http.StatusOK, components.MessageBubbleList(messages))
}

// checkBackgroundMessages checks if any OTHER conversation has new unread messages
// and, if so, fires a showToast HX-Trigger header on the response.
func (h *InboxHandler) checkBackgroundMessages(c *echo.Context, ctx context.Context, workspaceID uuid.UUID, openFrom, openChannel, openTo string) {
	if h.Repo == nil {
		return
	}
	conversations, err := h.Repo.ListConversations(ctx, workspaceID, "")
	if err != nil {
		return
	}

	for _, conv := range conversations {
		// Skip the currently open conversation
		if conv.From == openFrom && conv.Channel == openChannel && conv.RecipientIdentity == openTo {
			continue
		}
		// Check unread state for this background conversation
		if h.Sessions == nil {
			continue
		}
		session, sErr := h.Sessions.Get(ctx, workspaceID, conv.From, conv.Channel, conv.RecipientIdentity)
		isUnread := false
		if sErr != nil {
			isUnread = true
		} else if session.LastReadAt == nil || conv.LastMessageTime.After(*session.LastReadAt) {
			isUnread = true
		}
		if isUnread {
			// Fire toast for this background contact
			trigger := fmt.Sprintf(`{"showToast":{"text":"Nova mensagem de %s"}}`, jsonEscape(conv.From))
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
