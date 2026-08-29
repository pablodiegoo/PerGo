package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/pkg/slug"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/templates/pages"
)

// DeviceHandler handles admin operations for unified connections management.
type DeviceHandler struct {
	Sessions      *session.ActiveSession
	Manager       *session.Manager
	Connections   *repository.ConnectionRepository
	Publisher     MessagePublisher
	NC            *nats.Conn
	TemplatesRepo *repository.WABATemplateRepository
	ExternalURL   string
	ContactRepo   *repository.ContactRepository
}

// List renders the unified connection management page or HTMX fragment.
func (h *DeviceHandler) List(c *echo.Context) error {
	workspaceID := resolveWorkspaceIDOrNil(c)
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
// POST /admin/devices/pair — optional form field "phone" or "connection_id"
func (h *DeviceHandler) StartPairing(c *echo.Context) error {
	phone := c.FormValue("phone")
	proxyURL := c.FormValue("proxy_url")
	var existingConnID *uuid.UUID
	if connIDStr := c.FormValue("connection_id"); connIDStr != "" {
		if u, err := uuid.Parse(connIDStr); err == nil {
			existingConnID = &u
			if phone == "" && h.Connections != nil {
				dev, err := h.Connections.GetByID(c.Request().Context(), u)
				if err == nil && dev != nil {
					phone = dev.SenderIdentity
				}
			}
		}
	}

	wsID := resolveWorkspaceIDOrNil(c)

	ps, err := h.Manager.StartPairingSession(c.Request().Context(), wsID, phone, existingConnID, proxyURL)
	if err != nil {
		ident := phone
		if ident == "" && existingConnID != nil {
			ident = existingConnID.String()
		}
		if errors.Is(err, session.ErrMaxConnectionsExceeded) {
			return mw.Render(c, http.StatusUnprocessableEntity, pages.QRFragment("", "", ident, "error", err.Error()))
		}
		return mw.Render(c, http.StatusInternalServerError, pages.QRFragment("", "", ident, "error", err.Error()))
	}

	ident := ps.ConnectionID().String()
	if phone != "" {
		ident = phone
	}

	return mw.Render(c, http.StatusOK, pages.QRFragment("", "", ident, "pending", "Aponte o WhatsApp do seu celular para escanear o QR Code"))
}

// GetQR returns the current QR code state as an HTMX fragment.
// GET /admin/devices/qr?id=... or GET /admin/devices/qr?phone=...
func (h *DeviceHandler) GetQR(c *echo.Context) error {
	id := c.QueryParam("id")
	if id == "" {
		id = c.QueryParam("phone")
	}
	if id == "" {
		return c.String(http.StatusBadRequest, "id or phone is required")
	}

	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	evt, ok := h.Manager.GetPairingStateForWorkspace(workspaceID, id)
	if !ok {
		return mw.Render(c, http.StatusOK, pages.QRFragment("", "", id, "error", "Nenhuma sessão de pareamento ativa para este identificador"))
	}

	return mw.Render(c, http.StatusOK, pages.QRFragment(evt.Code, evt.QRDataURL, id, evt.Status, evt.Message))
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

	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	ctx := c.Request().Context()
	conn, err := h.Connections.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) {
			return c.String(http.StatusNotFound, "connection not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get connection")
	}

	if conn.WorkspaceID != workspaceID {
		return c.String(http.StatusNotFound, "connection not found")
	}

	// If WhatsApp Web, stop active session
	if conn.Channel == "whatsapp" && conn.JID != nil && *conn.JID != "" {
		h.Sessions.DisconnectByJID(*conn.JID)
	}

	// Delete from database
	if err := h.Connections.Delete(ctx, id); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete connection")
	}

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
	workspaceID := resolveWorkspaceIDOrNil(c)
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
		privateKey := strings.TrimSpace(c.FormValue("private_key"))

		if phoneNumberID == "" || wabaAccountID == "" || token == "" {
			return c.String(http.StatusBadRequest, "phone_number_id, waba_account_id, and token are required")
		}

		if privateKey != "" {
			if _, err := crypto.ParseRSAPrivateKeyFromPEM([]byte(privateKey)); err != nil {
				validationErr = fmt.Errorf("chave privada RSA inválida: %w", err)
			}
		} else {
			privPEM, _, err := crypto.GenerateRSAKeyPair2048()
			if err != nil {
				validationErr = fmt.Errorf("falha ao gerar par de chaves RSA: %w", err)
			} else {
				privateKey = privPEM
			}
		}

		senderIdentity = phoneNumberID
		metaClient := client.NewWABAMetaClient(nil, "")
		if details, err := metaClient.FetchPhoneNumberDetails(ctx, phoneNumberID, token); err == nil && details != nil && details.DisplayPhoneNumber != "" {
			if clean, valid := domain.SanitizePhone(details.DisplayPhoneNumber); valid {
				senderIdentity = clean
			} else {
				senderIdentity = details.DisplayPhoneNumber
			}
		}

		wabaCfg = pages.WABAConfig{
			PhoneNumberID: phoneNumberID,
			Token:         token,
			WABAAccountID: wabaAccountID,
			VerifyToken:   verifyToken,
			PrivateKey:    privateKey,
		}

		connID = uuid.New()
		if validationErr == nil {
			validationErr = h.syncTemplatesFromMeta(ctx, workspaceID, connID, wabaCfg, false)
		}
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

	baseSlug := slug.Generate(name)
	candidateSlug := baseSlug
	counter := 1
	for {
		existing, err := h.Connections.GetBySlug(ctx, workspaceID, candidateSlug)
		if errors.Is(err, repository.ErrConnectionNotFound) || existing == nil {
			break
		}
		counter++
		candidateSlug = fmt.Sprintf("%s-%d", baseSlug, counter)
	}

	now := time.Now().UTC()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    workspaceID,
		Name:           name,
		Slug:           candidateSlug,
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

// UpdateSlug updates a connection's slug.
// POST /admin/devices/:id/slug
func (h *DeviceHandler) UpdateSlug(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	connIDStr := c.Param("id")
	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection id")
	}

	rawSlug := c.FormValue("slug")
	sanitizedSlug := slug.Generate(rawSlug)
	if sanitizedSlug == "" {
		return c.String(http.StatusBadRequest, "invalid slug")
	}

	conn, err := h.Connections.GetByID(ctx, connID)
	if err != nil || conn.WorkspaceID != workspaceID {
		return c.String(http.StatusNotFound, "connection not found")
	}

	if conn.Slug != sanitizedSlug {
		existing, err := h.Connections.GetBySlug(ctx, workspaceID, sanitizedSlug)
		if err == nil && existing != nil && existing.ID != connID {
			return c.String(http.StatusConflict, "slug already in use")
		}

		if err := h.Connections.UpdateSlug(ctx, connID, sanitizedSlug); err != nil {
			return c.String(http.StatusInternalServerError, "failed to update slug: "+err.Error())
		}
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
	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	idStr := c.QueryParam("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection ID")
	}

	conn, err := h.Connections.GetByID(c.Request().Context(), id)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
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

// FlowKey renders the modal with the Meta Flows RSA public key.
// GET /admin/devices/flow-key?id={id}
func (h *DeviceHandler) FlowKey(c *echo.Context) error {
	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

	idStr := c.QueryParam("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection ID")
	}

	conn, err := h.Connections.GetByID(c.Request().Context(), id)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return c.String(http.StatusNotFound, "connection not found")
	}

	if conn.Channel != "whatsapp_cloud" {
		return c.String(http.StatusBadRequest, "connection is not a WhatsApp Cloud connection")
	}

	privKey, err := crypto.LoadRSAPrivateKey(conn.Credentials, nil)
	if err != nil {
		return c.String(http.StatusBadRequest, fmt.Sprintf("falha ao carregar chave RSA: %v", err))
	}

	pubPEM, err := crypto.ExportRSAPublicKeyPEM(privKey)
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("falha ao exportar chave pública RSA: %v", err))
	}

	return mw.Render(c, http.StatusOK, pages.FlowKeyModal(conn, pubPEM))
}

// RunTest publishes a test outbound message to the messages.outbound JetStream subject.
// POST /admin/devices/test
func (h *DeviceHandler) RunTest(c *echo.Context) error {
	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.HTML(http.StatusOK, `<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Workspace não selecionado</div>`)
	}

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
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return c.HTML(http.StatusOK, `<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Conexão não encontrada</div>`)
	}

	var language string
	var componentsList []domain.TemplateComponent
	if isTemplate && templateName != "" {
		language = c.FormValue("language")
		if language == "" && h.TemplatesRepo != nil {
			tmpls, err := h.TemplatesRepo.ListByConnection(c.Request().Context(), conn.ID)
			if err == nil {
				for _, t := range tmpls {
					if t.Name == templateName && t.Language != "" {
						language = t.Language
						break
					}
				}
			}
		}
		if language == "" {
			language = "pt_BR" // Default language fallback
		}

		body, componentsList = ExtractFormTemplateParams(c, templateName)
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
		qMsg.Language = language
		qMsg.Components = componentsList
	}

	payload, err := json.Marshal(qMsg)
	if err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Erro ao serializar mensagem: %v</div>`, err))
	}

	if h.Publisher == nil {
		return c.HTML(http.StatusServiceUnavailable, `<div class="p-3 bg-red-50 text-red-800 border border-red-200 rounded-md text-sm mb-4">Publisher não disponível</div>`)
	}

	if h.ContactRepo != nil {
		_, _ = h.ContactRepo.ResolveContact(c.Request().Context(), workspaceID, conn.Channel, to, to, "", "")
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
	workspaceID := resolveWorkspaceIDOrNil(c)
	if workspaceID == uuid.Nil {
		return c.String(http.StatusBadRequest, "workspace not selected")
	}

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

	slog.Info("device connectivity tester websocket connection established", "workspace_id", workspaceID)

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

	type eventWorkspacePayload struct {
		WorkspaceID uuid.UUID `json:"workspace_id"`
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errChan:
			slog.Info("device websocket closed by client", "error", err)
			return nil
		case m := <-ch:
			subject := m.Subject
			rawPayload := m.Data

			// Filter out events not belonging to this workspace
			if strings.HasPrefix(subject, "inbound.events.") {
				subWsStr := strings.TrimPrefix(subject, "inbound.events.")
				if subWsStr != "" && subWsStr != workspaceID.String() {
					continue
				}
			} else {
				var p eventWorkspacePayload
				if err := json.Unmarshal(rawPayload, &p); err == nil && p.WorkspaceID != uuid.Nil {
					if p.WorkspaceID != workspaceID {
						continue
					}
				}
			}

			var eventType, badgeClass, title string
			var prettyJSON bytes.Buffer

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
	metaClient := client.NewWABAMetaClient(nil, "")
	var repo *repository.WABATemplateRepository
	if saveToDB {
		repo = h.TemplatesRepo
	}
	_, err := metaClient.SyncTemplates(ctx, connectionID, config.WABAAccountID, config.Token, workspaceID, repo, true)
	return err
}
