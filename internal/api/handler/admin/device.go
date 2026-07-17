package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/templates/pages"
)

// DeviceHandler handles admin operations for unified connections management.
type DeviceHandler struct {
	Sessions      *session.ActiveSession
	Manager       *session.Manager
	Connections   *repository.ConnectionRepository
	Publisher     *queue.JetStreamPublisher
	NC            *nats.Conn
	TemplatesRepo *repository.WABATemplateRepository
	ExternalURL   string
}

// pairingState holds the current QR pairing state for a phone number.
type pairingState struct {
	code    string       // raw QR code string (empty if not yet received)
	status  string       // "pending", "paired", "error"
	message string       // human-readable message
	expires time.Time    // when the current QR code expires
	mu      sync.RWMutex // protects fields
}

// pairingSessions holds in-memory pairing state keyed by phone number.
var (
	pairingSessions   = make(map[string]*pairingState)
	pairingSessionsMu sync.Mutex
)

// List renders the unified connection management page or HTMX fragment.
func (h *DeviceHandler) List(c *echo.Context) error {
	workspaceID := resolveWorkspaceID(c)
	connections, err := h.Connections.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load connections: "+err.Error())
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.DeviceListContent(connections))
	}
	return mw.Render(c, http.StatusOK, pages.DeviceListPage(connections))
}

// PairForm renders the unified new connection modal fragment.
// GET /admin/devices/pair-form
func (h *DeviceHandler) PairForm(c *echo.Context) error {
	return mw.Render(c, http.StatusOK, pages.PairForm())
}

// StartPairing begins the QR pairing flow for a new WhatsApp Web connection.
// POST /admin/devices/pair — expects form field "phone" or "connection_id"
func (h *DeviceHandler) StartPairing(c *echo.Context) error {
	phone := c.FormValue("phone")
	proxyURL := c.FormValue("proxy_url")
	var existingConnID *uuid.UUID
	if connIDStr := c.FormValue("connection_id"); connIDStr != "" {
		if u, err := uuid.Parse(connIDStr); err == nil {
			existingConnID = &u
			if phone == "" {
				dev, err := h.Connections.GetByID(c.Request().Context(), u)
				if err == nil && dev != nil {
					phone = dev.SenderIdentity
				}
			}
		}
	}

	if phone == "" {
		return c.String(http.StatusBadRequest, "phone number is required")
	}

	wsID := resolveWorkspaceID(c)

	// Initialize pairing state.
	ps := &pairingState{status: "pending", message: "Waiting for QR code..."}
	pairingSessionsMu.Lock()
	pairingSessions[phone] = ps
	pairingSessionsMu.Unlock()

	// Start pairing in background.
	ch, err := h.Manager.StartPairing(c.Request().Context(), wsID, phone, existingConnID, proxyURL)
	if err != nil {
		ps.mu.Lock()
		ps.status = "error"
		ps.message = err.Error()
		ps.mu.Unlock()
		if errors.Is(err, session.ErrMaxConnectionsExceeded) {
			return mw.Render(c, http.StatusUnprocessableEntity, pages.QRFragment("", phone, "error", err.Error()))
		}
		return mw.Render(c, http.StatusInternalServerError, pages.QRFragment("", phone, "error", err.Error()))
	}

	// Process QR events in background goroutine.
	go func() {
		for evt := range ch {
			ps.mu.Lock()
			switch evt.Type {
			case session.QREventCode:
				ps.code = string(evt.Data)
				ps.status = "pending"
				ps.message = evt.Message
				ps.expires = time.Now().Add(25 * time.Second)
			case session.QREventPaired:
				ps.code = ""
				ps.status = "paired"
				ps.message = evt.Message
			case session.QREventError:
				ps.code = ""
				ps.status = "error"
				ps.message = evt.Message
			}
			ps.mu.Unlock()
		}
		// Channel closed — cleanup after a delay.
		time.AfterFunc(30*time.Second, func() {
			pairingSessionsMu.Lock()
			delete(pairingSessions, phone)
			pairingSessionsMu.Unlock()
		})
	}()

	return mw.Render(c, http.StatusOK, pages.QRFragment("", phone, "pending", "Scan the QR code below to pair your device"))
}

// GetQR returns the current QR code state as an HTMX fragment.
// GET /admin/devices/qr?phone=...
func (h *DeviceHandler) GetQR(c *echo.Context) error {
	phone := c.QueryParam("phone")
	if phone == "" {
		return c.String(http.StatusBadRequest, "phone is required")
	}

	pairingSessionsMu.Lock()
	ps, ok := pairingSessions[phone]
	pairingSessionsMu.Unlock()

	if !ok {
		return mw.Render(c, http.StatusOK, pages.QRFragment("", phone, "error", "No active pairing session for this phone"))
	}

	ps.mu.RLock()
	code, status, message := ps.code, ps.status, ps.message
	ps.mu.RUnlock()

	return mw.Render(c, http.StatusOK, pages.QRFragment(code, phone, status, message))
}

// Disconnect deletes a connection from the database and stops its active session if it is WhatsApp Web.
// DELETE /admin/devices/:id
func (h *DeviceHandler) Disconnect(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.String(http.StatusBadRequest, "invalid ID")
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection ID format")
	}

	ctx := c.Request().Context()
	conn, err := h.Connections.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) {
			return c.String(http.StatusNotFound, "connection not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get connection")
	}

	// If WhatsApp Web, stop active session
	if conn.Channel == "whatsapp" && conn.JID != nil && *conn.JID != "" {
		h.Sessions.DisconnectByJID(*conn.JID)
	}

	// Delete from database
	if err := h.Connections.Delete(ctx, id); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete connection")
	}

	workspaceID := resolveWorkspaceID(c)
	connections, err := h.Connections.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to reload connections")
	}

	return mw.Render(c, http.StatusOK, pages.ConnectionTable(connections))
}

// Create handles creation of Telegram and WABA connections.
// POST /admin/devices/create
func (h *DeviceHandler) Create(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceID(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	name := c.FormValue("name")
	channel := c.FormValue("channel")

	if name == "" || channel == "" {
		return c.String(http.StatusBadRequest, "name and channel are required")
	}

	var senderIdentity string
	var credentialsJSON []byte
	var validationErr error
	var connID uuid.UUID
	var wabaCfg pages.WABAConfig

	if channel == "telegram" {
		token := c.FormValue("token")
		if token == "" {
			return c.String(http.StatusBadRequest, "token is required for Telegram bot")
		}

		var botUsername string
		botUsername, validationErr = h.validateTelegramToken(ctx, token)
		if validationErr == nil {
			senderIdentity = botUsername

			secretToken := ""
			if strings.HasPrefix(h.ExternalURL, "https://") {
				secretToken = uuid.New().String()
				webhookURL := fmt.Sprintf("%s/webhooks/telegram/%s", h.ExternalURL, workspaceID.String())
				validationErr = h.registerTelegramWebhook(ctx, token, webhookURL, secretToken)
			} else {
				secretToken = "pergo_secret_token_" + workspaceID.String()
			}

			if validationErr == nil {
				type storedTelegramConfig struct {
					Token       string `json:"token"`
					SecretToken string `json:"secret_token"`
					BotUsername string `json:"bot_username"`
				}
				credentialsJSON, _ = json.Marshal(storedTelegramConfig{
					Token:       token,
					SecretToken: secretToken,
					BotUsername: botUsername,
				})
			}
		}
	} else if channel == "whatsapp_cloud" {
		phoneNumberID := c.FormValue("phone_number_id")
		wabaAccountID := c.FormValue("waba_account_id")
		token := c.FormValue("token")
		verifyToken := c.FormValue("verify_token")

		if phoneNumberID == "" || wabaAccountID == "" || token == "" {
			return c.String(http.StatusBadRequest, "phone_number_id, waba_account_id, and token are required")
		}

		senderIdentity = phoneNumberID

		wabaCfg = pages.WABAConfig{
			PhoneNumberID: phoneNumberID,
			Token:         token,
			WABAAccountID: wabaAccountID,
			VerifyToken:   verifyToken,
		}

		connID = uuid.New()
		validationErr = h.syncTemplatesFromMeta(ctx, workspaceID, connID, wabaCfg, false)
		if validationErr == nil {
			credentialsJSON, _ = json.Marshal(wabaCfg)
		}
	} else {
		return c.String(http.StatusBadRequest, "unsupported channel type for synchronous creation")
	}

	if validationErr != nil {
		c.Response().Header().Set("HX-Retarget", "#modal-error-container")
		return c.HTML(http.StatusOK, fmt.Sprintf(`
			<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">
				<strong>Erro de Validação:</strong> %s
			</div>
		`, validationErr.Error()))
	}

	now := time.Now().UTC()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    workspaceID,
		Name:           name,
		Channel:        channel,
		SenderIdentity: senderIdentity,
		Status:         "connected",
		Credentials:    credentialsJSON,
		ConnectedSince: &now,
	}

	if err := h.Connections.Create(ctx, conn); err != nil {
		c.Response().Header().Set("HX-Retarget", "#modal-error-container")
		return c.HTML(http.StatusOK, fmt.Sprintf(`
			<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">
				<strong>Erro ao salvar conexão:</strong> %s
			</div>
		`, err.Error()))
	}

	if channel == "whatsapp_cloud" {
		if err := h.syncTemplatesFromMeta(ctx, workspaceID, connID, wabaCfg, true); err != nil {
			_ = h.Connections.Delete(ctx, connID)
			c.Response().Header().Set("HX-Retarget", "#modal-error-container")
			return c.HTML(http.StatusOK, fmt.Sprintf(`
				<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">
					<strong>Erro ao sincronizar templates:</strong> %s
				</div>
			`, err.Error()))
		}
	}

	currentURL := c.Request().Header.Get("HX-Current-URL")
	if strings.Contains(currentURL, "/campaigns") {
		c.Response().Header().Set("HX-Trigger", "connection-created")
		return c.NoContent(200)
	}

	connections, err := h.Connections.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to reload connections")
	}

	return mw.Render(c, http.StatusOK, pages.ConnectionTable(connections))
}

// TestForm renders the connectivity test modal.
// GET /admin/devices/test?id={id}
func (h *DeviceHandler) TestForm(c *echo.Context) error {
	idStr := c.QueryParam("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection ID")
	}

	conn, err := h.Connections.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "connection not found")
	}

	var templates []repository.WABATemplate
	if conn.Channel == "whatsapp_cloud" && h.TemplatesRepo != nil {
		var err error
		templates, err = h.TemplatesRepo.ListByConnection(c.Request().Context(), conn.ID)
		if err != nil {
			slog.Warn("failed to list templates for testing", "error", err)
		}
	}

	return mw.Render(c, http.StatusOK, pages.TestConnectionModal(conn, templates))
}

// RunTest publishes a test outbound message to the messages.outbound JetStream subject.
// POST /admin/devices/test
func (h *DeviceHandler) RunTest(c *echo.Context) error {
	connIDStr := c.FormValue("connection_id")
	to := c.FormValue("to")
	body := c.FormValue("body")
	isTemplate := c.FormValue("is_template") == "true"
	templateName := c.FormValue("template_name")

	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return c.HTML(http.StatusOK, `<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">ID de Conexão inválido</div>`)
	}

	conn, err := h.Connections.GetByID(c.Request().Context(), connID)
	if err != nil {
		return c.HTML(http.StatusOK, `<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Conexão não encontrada</div>`)
	}

	var componentsList []domain.TemplateComponent
	if isTemplate && templateName != "" {
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
	}

	traceID := "test-" + uuid.New().String()
	qMsg := domain.QueueMessage{
		WorkspaceID:    conn.WorkspaceID,
		ConnectionID:   conn.ID,
		SenderIdentity: conn.SenderIdentity,
		TraceID:        traceID,
		To:             to,
		Channel:        conn.Channel,
		Body:           body,
		QueuedAt:       time.Now().UTC(),
	}

	if isTemplate {
		qMsg.TemplateName = templateName
		qMsg.Language = "pt_BR" // Default language
		qMsg.Components = componentsList
	}

	payload, err := json.Marshal(qMsg)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Erro ao serializar mensagem: %v</div>`, err))
	}

	err = h.Publisher.Publish(c.Request().Context(), "messages.outbound", payload, traceID)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Erro ao publicar no NATS: %v</div>`, err))
	}

	return c.HTML(http.StatusOK, fmt.Sprintf(`
		<div class="p-3 bg-emerald-50 text-emerald-800 border border-emerald-200 rounded-md text-sm">
			<strong>Sucesso!</strong> Mensagem enviada para a fila de saída.<br/>
			<span class="text-xs font-mono">Trace ID: %s</span>
		</div>
	`, traceID))
}

// WS upgrades the connection to WebSocket and streams NATS events live to the client.
// GET /admin/devices/test/ws
func (h *DeviceHandler) WS(c *echo.Context) error {
	ws, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("websocket accept failed in device test", "error", err)
		return err
	}
	defer ws.Close(websocket.StatusInternalError, "closed")

	ctx := c.Request().Context()

	// Channel to receive NATS messages
	ch := make(chan *nats.Msg, 128)

	// Subscribe to outgoing messages
	sub1, err := h.NC.ChanSubscribe("messages.>", ch)
	if err != nil {
		slog.Error("nats subscribe messages.> failed", "error", err)
		return err
	}
	defer sub1.Unsubscribe()

	// Subscribe to incoming webhook events
	sub2, err := h.NC.ChanSubscribe("inbound.events.>", ch)
	if err != nil {
		slog.Error("nats subscribe inbound.events.> failed", "error", err)
		return err
	}
	defer sub2.Unsubscribe()

	// Subscribe to webhook delivery events
	sub3, err := h.NC.ChanSubscribe("webhooks.events", ch)
	if err != nil {
		slog.Error("nats subscribe webhooks.events failed", "error", err)
		return err
	}
	defer sub3.Unsubscribe()

	slog.Info("device connectivity tester websocket connection established")

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
			slog.Info("device websocket closed by client", "error", err)
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
				badgeClass = "badge-danger"
				title = "Webhook Status Dispatched"
			} else { // inbound.events.<workspace_id>
				eventType = "inbound"
				badgeClass = "badge-success"
				title = "Inbound Message Received"
			}

			timeStr := time.Now().Format("15:04:05")

			var buf bytes.Buffer
			err := pages.TestEventRow(eventType, badgeClass, title, timeStr, prettyJSON.String()).Render(ctx, &buf)
			if err != nil {
				slog.Error("failed to render test event row", "error", err)
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

// --- helpers ---

func (h *DeviceHandler) registerTelegramWebhook(ctx context.Context, token, webhookURL, secretToken string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook?url=%s&secret_token=%s", token, webhookURL, secretToken)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create Telegram webhook registration request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Telegram API for webhook registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram webhook registration returned HTTP status %d", resp.StatusCode)
	}

	type tgWebhookResponse struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	var tgResp tgWebhookResponse
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return fmt.Errorf("failed to decode Telegram webhook response: %w", err)
	}

	if !tgResp.Ok {
		return fmt.Errorf("Telegram webhook registration failed: %s", tgResp.Description)
	}

	slog.Info("Telegram webhook registered successfully", "url", webhookURL)
	return nil
}

func (h *DeviceHandler) validateTelegramToken(ctx context.Context, token string) (string, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Telegram API request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Telegram API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New("Telegram token is unauthorized/invalid")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Telegram API returned HTTP status %d", resp.StatusCode)
	}

	type tgResponse struct {
		Ok     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	var tgResp tgResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&tgResp); err != nil {
		return "", fmt.Errorf("failed to parse Telegram response: %w", err)
	}

	if !tgResp.Ok {
		return "", errors.New("Telegram API returned OK=false")
	}

	slog.Info("Telegram bot token validated successfully", "username", tgResp.Result.Username)
	username := tgResp.Result.Username
	if !strings.HasPrefix(username, "@") {
		username = "@" + username
	}
	return username, nil
}

func (h *DeviceHandler) syncTemplatesFromMeta(ctx context.Context, workspaceID uuid.UUID, connectionID uuid.UUID, config pages.WABAConfig, saveToDB bool) error {
	baseURL := "https://graph.facebook.com/v18.0"
	metaURL := fmt.Sprintf("%s/%s/message_templates?limit=100", baseURL, config.WABAAccountID)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create Meta API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Meta API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response from Meta: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		type metaError struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		type metaErrorResponse struct {
			Error metaError `json:"error"`
		}
		var metaErr metaErrorResponse
		if err := json.Unmarshal(respBytes, &metaErr); err == nil && metaErr.Error.Message != "" {
			return fmt.Errorf("Meta API error: %s (code %d)", metaErr.Error.Message, metaErr.Error.Code)
		}
		return fmt.Errorf("Meta API returned HTTP status %d", resp.StatusCode)
	}

	type metaTemplate struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Language   string            `json:"language"`
		Status     string            `json:"status"`
		Category   string            `json:"category"`
		Components []json.RawMessage `json:"components"`
	}

	type metaTemplatesResponse struct {
		Data []metaTemplate `json:"data"`
	}

	var metaResp metaTemplatesResponse
	if err := json.Unmarshal(respBytes, &metaResp); err != nil {
		return fmt.Errorf("failed to parse Meta response: %w", err)
	}

	slog.Info("syncing templates from Meta", "count", len(metaResp.Data), "workspace_id", workspaceID)

	for _, t := range metaResp.Data {
		componentsJSON, err := json.Marshal(t.Components)
		if err != nil {
			slog.Error("failed to marshal components", "error", err, "template", t.Name)
			continue
		}

		dbTmpl := &repository.WABATemplate{
			WorkspaceID:    workspaceID,
			ConnectionID:   connectionID,
			MetaTemplateID: t.ID,
			Name:           t.Name,
			Language:       t.Language,
			Status:         t.Status,
			Category:       t.Category,
			Components:     componentsJSON,
		}

		if saveToDB && h.TemplatesRepo != nil {
			_, err = h.TemplatesRepo.Upsert(ctx, dbTmpl)
			if err != nil {
				slog.Error("failed to upsert template in local DB", "error", err, "template", t.Name)
				return fmt.Errorf("failed to save template %s in local DB: %w", t.Name, err)
			}
		}
	}
	return nil
}
