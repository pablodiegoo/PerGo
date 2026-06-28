package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/queue"
	"github.com/pablojhp.omnigo/internal/repository"
	"github.com/pablojhp.omnigo/templates/pages"
)

// PlaygroundHandler handles routes for the Developer Playground screen.
type PlaygroundHandler struct {
	wsRepo    *repository.WorkspaceRepository
	publisher *queue.JetStreamPublisher
	nc        *nats.Conn
}

// NewPlaygroundHandler creates a new instance of PlaygroundHandler.
func NewPlaygroundHandler(wsRepo *repository.WorkspaceRepository, publisher *queue.JetStreamPublisher, nc *nats.Conn) *PlaygroundHandler {
	return &PlaygroundHandler{
		wsRepo:    wsRepo,
		publisher: publisher,
		nc:        nc,
	}
}

// Page renders the Developer Playground interface.
func (h *PlaygroundHandler) Page(c *echo.Context) error {
	workspaces, err := h.wsRepo.List(c.Request().Context(), 100)
	if err != nil {
		slog.Error("failed to list workspaces in playground", "error", err)
		return c.String(http.StatusInternalServerError, "failed to load workspaces")
	}

	return middleware.Render(c, http.StatusOK, pages.PlaygroundPage(workspaces))
}

// Send handles testing message enqueuing.
func (h *PlaygroundHandler) Send(c *echo.Context) error {
	workspaceIDStr := c.FormValue("workspace_id")
	channel := c.FormValue("channel")
	to := c.FormValue("to")
	body := c.FormValue("body")

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.HTML(http.StatusOK, `<div class="alert alert-error">Invalid Workspace ID</div>`)
	}

	traceID := "playground-" + uuid.New().String()
	qMsg := domain.QueueMessage{
		WorkspaceID: workspaceID,
		TraceID:     traceID,
		To:          to,
		Channel:     channel,
		Body:        body,
		QueuedAt:    time.Now().UTC(),
	}

	if channel == "whatsapp_cloud" {
		templateName := c.FormValue("template_name")
		templateLanguage := c.FormValue("template_language")
		templateComponentsRaw := c.FormValue("template_components")

		if templateName != "" {
			qMsg.TemplateName = templateName
			if templateLanguage != "" {
				qMsg.Language = templateLanguage
			} else {
				qMsg.Language = "en_US"
			}

			if templateComponentsRaw != "" {
				var components []domain.TemplateComponent
				if err := json.Unmarshal([]byte(templateComponentsRaw), &components); err != nil {
					return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="alert alert-error" style="background: #fef2f2; color: var(--color-error); border: 1px solid #fecaca; padding: var(--spacing-md); border-radius: var(--radius); margin-bottom: var(--spacing-md);"><strong>Error:</strong> Invalid Template Parameters JSON: %v</div>`, err))
				}
				qMsg.Components = components
			}
		}
	}

	payload, err := json.Marshal(qMsg)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="alert alert-error" style="background: #fef2f2; color: var(--color-error); border: 1px solid #fecaca; padding: var(--spacing-md); border-radius: var(--radius); margin-bottom: var(--spacing-md);"><strong>Error:</strong> Failed to marshal message: %v</div>`, err))
	}

	err = h.publisher.Publish(c.Request().Context(), "messages.outbound", payload, traceID)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="alert alert-error" style="background: #fef2f2; color: var(--color-error); border: 1px solid #fecaca; padding: var(--spacing-md); border-radius: var(--radius); margin-bottom: var(--spacing-md);"><strong>Error:</strong> Failed to publish to NATS: %v</div>`, err))
	}

	return c.HTML(http.StatusOK, fmt.Sprintf(`
		<div class="alert alert-success" style="background: #f0fdf4; color: var(--color-success); border: 1px solid #bbf7d0; padding: var(--spacing-md); border-radius: var(--radius); margin-bottom: var(--spacing-md);">
			<strong>Success!</strong> Message successfully enqueued.<br/>
			<span style="font-size: 0.875rem; font-family: monospace;">Trace ID: %s</span>
		</div>
	`, traceID))
}

// WS upgrades the connection to WebSocket and streams NATS events live to the client.
func (h *PlaygroundHandler) WS(c *echo.Context) error {
	ws, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("websocket accept failed in playground", "error", err)
		return err
	}
	defer ws.Close(websocket.StatusInternalError, "closed")

	ctx := c.Request().Context()

	// Channel to receive NATS messages
	ch := make(chan *nats.Msg, 128)

	// Subscribe to outgoing messages
	sub1, err := h.nc.ChanSubscribe("messages.>", ch)
	if err != nil {
		slog.Error("nats subscribe messages.> failed", "error", err)
		return err
	}
	defer sub1.Unsubscribe()

	// Subscribe to incoming webhook events
	sub2, err := h.nc.ChanSubscribe("inbound.events.>", ch)
	if err != nil {
		slog.Error("nats subscribe inbound.events.> failed", "error", err)
		return err
	}
	defer sub2.Unsubscribe()

	// Subscribe to webhook delivery events
	sub3, err := h.nc.ChanSubscribe("webhooks.events", ch)
	if err != nil {
		slog.Error("nats subscribe webhooks.events failed", "error", err)
		return err
	}
	defer sub3.Unsubscribe()

	slog.Info("developer playground websocket connection established")

	// Message read loop in separate goroutine to detect client disconnecting
	errChan := make(chan error, 1)
	go func() {
		for {
			_, _, err := ws.Read(ctx)
			if err != nil {
				errChan <- err
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errChan:
			slog.Info("playground websocket closed by client", "error", err)
			return nil
		case m := <-ch:
			var eventType, badgeClass, title string
			var prettyJSON bytes.Buffer

			subject := m.Subject
			rawPayload := m.Data

			if err := json.Indent(&prettyJSON, rawPayload, "", "  "); err != nil {
				prettyJSON.Reset()
				prettyJSON.Write(rawPayload)
			}

			if subject == "messages.outbound" {
				eventType = "outbound"
				badgeClass = "badge-secondary"
				title = "Outbound Message Enqueued"
			} else if subject == "webhooks.events" {
				eventType = "webhook"
				badgeClass = "badge-danger" // Style overrides style class
				title = "Webhook Status Dispatched"
			} else { // inbound.events.<workspace_id>
				eventType = "inbound"
				badgeClass = "badge-success"
				title = "Inbound Message Received"
			}

			timeStr := time.Now().Format("15:04:05")

			var buf bytes.Buffer
			err := pages.PlaygroundEventRow(eventType, badgeClass, title, timeStr, prettyJSON.String()).Render(ctx, &buf)
			if err != nil {
				slog.Error("failed to render playground event row", "error", err)
				continue
			}

			err = ws.Write(ctx, websocket.MessageText, buf.Bytes())
			if err != nil {
				slog.Error("websocket write failed", "error", err)
				return err
			}
		}
	}
}
