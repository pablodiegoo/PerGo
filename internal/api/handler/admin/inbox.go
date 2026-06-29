package admin

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

// InboxHandler holds dependencies for channel inboxes.
type InboxHandler struct {
	Repo *repository.AuditRepository
}

type inboundPayload struct {
	From string `json:"from"`
	Body string `json:"body"`
}

// View handles GET /admin/inbox/:channel
func (h *InboxHandler) View(c *echo.Context) error {
	ctx := c.Request().Context()
	channelName, err := echo.PathParam[string](c, "channel")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid channel")
	}

	// Normalize channel name for database matching
	dbChannel := channelName
	displayName := channelName
	switch channelName {
	case "whatsapp":
		displayName = "WhatsApp Web"
	case "whatsapp_cloud":
		displayName = "WhatsApp Cloud"
	case "telegram":
		displayName = "Telegram"
	}

	filters := repository.AuditFilters{
		EventType: "inbound_message",
		Channel:   dbChannel,
		Page:      1,
		PageSize:  100, // Display last 100 messages
	}

	auditEntries, _, err := h.Repo.ListFiltered(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load inbox messages")
	}

	var inboxMessages []pages.InboxMessage
	for _, entry := range auditEntries {
		var p inboundPayload
		_ = json.Unmarshal(entry.Payload, &p)

		bodyText := p.Body
		if bodyText == "" {
			bodyText = "[Media or special message]"
		}

		inboxMessages = append(inboxMessages, pages.InboxMessage{
			TraceID:   entry.TraceID,
			From:      p.From,
			Body:      bodyText,
			CreatedAt: entry.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	inboxPage := pages.InboxPage(displayName, inboxMessages)

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.InboxPageContent(displayName, inboxMessages))
	}
	return mw.Render(c, http.StatusOK, layout.Base("Inbox - "+displayName, inboxPage))
}
