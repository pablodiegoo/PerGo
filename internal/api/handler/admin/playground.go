package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// PlaygroundHandler handles routes for the Developer Playground screen.
type PlaygroundHandler struct {
	wsRepo        *repository.WorkspaceRepository
	publisher     *queue.JetStreamPublisher
	nc            *nats.Conn
	templatesRepo *repository.WABATemplateRepository
	s3Client      *storage.S3Client
}

// NewPlaygroundHandler creates a new instance of PlaygroundHandler.
func NewPlaygroundHandler(wsRepo *repository.WorkspaceRepository, publisher *queue.JetStreamPublisher, nc *nats.Conn, templatesRepo *repository.WABATemplateRepository, s3Client *storage.S3Client) *PlaygroundHandler {
	return &PlaygroundHandler{
		wsRepo:        wsRepo,
		publisher:     publisher,
		nc:            nc,
		templatesRepo: templatesRepo,
		s3Client:      s3Client,
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

		if templateName != "" {
			qMsg.TemplateName = templateName
			if templateLanguage != "" {
				qMsg.Language = templateLanguage
			} else {
				qMsg.Language = "en_US"
			}

			var components []domain.TemplateComponent

			// Parse header media parameter
			headerType := c.FormValue("param_header_type")
			headerURL := c.FormValue("param_header_url")
			if headerType != "" && headerURL != "" {
				components = append(components, domain.TemplateComponent{
					Type: "header",
					Parameters: []domain.TemplateParameter{
						{
							Type: headerType,
							Text: headerURL,
						},
					},
				})
			}

			// Parse body parameters
			var bodyParams []domain.TemplateParameter
			for i := 1; ; i++ {
				val := c.FormValue(fmt.Sprintf("param_body_%d", i))
				if val == "" {
					break
				}
				bodyParams = append(bodyParams, domain.TemplateParameter{
					Type: "text",
					Text: val,
				})
			}
			if len(bodyParams) > 0 {
				components = append(components, domain.TemplateComponent{
					Type:       "body",
					Parameters: bodyParams,
				})
			}

			qMsg.Components = components
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

// GetTemplates returns a template select dropdown fragment.
func (h *PlaygroundHandler) GetTemplates(c *echo.Context) error {
	workspaceIDStr := c.QueryParam("workspace_id")
	if workspaceIDStr == "" {
		return c.HTML(http.StatusOK, `<p style="color: var(--color-text-muted);">Select a workspace first</p>`)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.HTML(http.StatusOK, `<p style="color: var(--color-text-muted);">Invalid workspace ID</p>`)
	}

	templates, err := h.templatesRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<p style="color: var(--color-error);">Failed to load templates: %v</p>`, err))
	}

	return middleware.Render(c, http.StatusOK, pages.PlaygroundTemplateSelect(templates))
}

// GetTemplateDetails returns variable inputs and meta info for a selected template.
func (h *PlaygroundHandler) GetTemplateDetails(c *echo.Context) error {
	workspaceIDStr := c.QueryParam("workspace_id")
	templateName := c.QueryParam("template_name")

	if templateName == "" || workspaceIDStr == "" {
		return c.HTML(http.StatusOK, "")
	}

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.HTML(http.StatusOK, "Invalid Workspace ID")
	}

	templates, err := h.templatesRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf("Failed to load templates: %v", err))
	}

	var matchedTmpl *repository.WABATemplate
	for _, t := range templates {
		if t.Name == templateName {
			matchedTmpl = &t
			break
		}
	}

	if matchedTmpl == nil {
		return c.HTML(http.StatusOK, "Template not found")
	}

	return middleware.Render(c, http.StatusOK, pages.PlaygroundTemplateDetails(*matchedTmpl))
}

// Upload handles uploading a file to S3 and returning a proxy URL.
func (h *PlaygroundHandler) Upload(c *echo.Context) error {
	workspaceIDStr := c.QueryParam("workspace_id")
	if workspaceIDStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "workspace_id is required"})
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace_id"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no file uploaded"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open uploaded file"})
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read uploaded file"})
	}

	s3Filename := uuid.New().String() + "-" + file.Filename
	s3Key := workspaceID.String() + "/" + s3Filename

	err = h.s3Client.Upload(c.Request().Context(), s3Key, data, file.Header.Get("Content-Type"))
	if err != nil {
		slog.Error("failed to upload playground file to S3", "error", err, "key", s3Key)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save file to S3"})
	}

	proxyURL := fmt.Sprintf("/media/%s/%s", workspaceIDStr, s3Filename)
	return c.JSON(http.StatusOK, map[string]string{"url": proxyURL})
}
