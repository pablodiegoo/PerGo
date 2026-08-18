package api

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

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/pkg/slug"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

// ConnectionRepo defines the repository methods required by ConnectionAPIHandler.
type ConnectionRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error)
	GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error)
	Create(ctx context.Context, c *repository.Connection) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// WABAMetaClient defines operations for interacting with Meta Graph API for WABA.
type WABAMetaClient interface {
	FetchPhoneNumberDetails(ctx context.Context, phoneNumberID, token string) (*client.WABAPhoneNumberDetails, error)
	SyncTemplates(ctx context.Context, connectionID uuid.UUID, wabaAccountID, token string, workspaceID uuid.UUID, repo *repository.WABATemplateRepository, bypassRateLimit bool) ([]repository.WABATemplate, error)
}

// TelegramBotClient defines operations for interacting with Telegram Bot API.
type TelegramBotClient interface {
	ValidateToken(ctx context.Context, token string) (string, error)
	RegisterWebhook(ctx context.Context, token, webhookURL, secretToken string) error
}

// SessionManager defines the pairing and session methods required by ConnectionAPIHandler.
type SessionManager interface {
	StartPairingSession(ctx context.Context, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) (*session.PairingSession, error)
	GetPairingState(key string) (*session.QREvent, bool)
	GetPairingStateForWorkspace(wsID uuid.UUID, key string) (*session.QREvent, bool)
	SubscribeQR(key string) (<-chan session.QREvent, func())
	SubscribeQRForWorkspace(wsID uuid.UUID, key string) (<-chan session.QREvent, func(), bool)
	CancelPairing(connectionID uuid.UUID)
	CancelPairingByPhone(phone string)
	EmitStatusEvent(ctx context.Context, wsID uuid.UUID, connID uuid.UUID, channelName, senderIdentity, status string) error
}

// ActiveSessions defines the active session registry methods required by ConnectionAPIHandler.
type ActiveSessions interface {
	DisconnectByJID(jid string)
	GetClient(jid string) *whatsapp.WhatsAppClient
}

// ConnectionAPIOption configures optional dependencies on ConnectionAPIHandler.
type ConnectionAPIOption func(*ConnectionAPIHandler)

// WithWABAMetaClient sets the WABA Meta client.
func WithWABAMetaClient(metaClient WABAMetaClient) ConnectionAPIOption {
	return func(h *ConnectionAPIHandler) {
		h.metaClient = metaClient
	}
}

// WithWABATemplateRepo sets the WABA template repository.
func WithWABATemplateRepo(repo *repository.WABATemplateRepository) ConnectionAPIOption {
	return func(h *ConnectionAPIHandler) {
		h.templatesRepo = repo
	}
}

// WithTelegramClient sets the Telegram Bot client.
func WithTelegramClient(tgClient TelegramBotClient) ConnectionAPIOption {
	return func(h *ConnectionAPIHandler) {
		h.telegramClient = tgClient
	}
}

// WithExternalURL sets the external URL for webhook registrations.
func WithExternalURL(externalURL string) ConnectionAPIOption {
	return func(h *ConnectionAPIHandler) {
		h.externalURL = externalURL
	}
}

// ConnectionAPIHandler handles headless REST APIs for connection lifecycle and QR streaming.
type ConnectionAPIHandler struct {
	repo           ConnectionRepo
	manager        SessionManager
	sessions       ActiveSessions
	templatesRepo  *repository.WABATemplateRepository
	metaClient     WABAMetaClient
	telegramClient TelegramBotClient
	externalURL    string
}

// NewConnectionAPIHandler creates a new ConnectionAPIHandler with optional configurations.
func NewConnectionAPIHandler(repo ConnectionRepo, manager SessionManager, sessions ActiveSessions, opts ...ConnectionAPIOption) *ConnectionAPIHandler {
	h := &ConnectionAPIHandler{
		repo:     repo,
		manager:  manager,
		sessions: sessions,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// SetTemplateRepo sets the WABA template repository used for template sync.
func (h *ConnectionAPIHandler) SetTemplateRepo(repo *repository.WABATemplateRepository) {
	h.templatesRepo = repo
}

// SetMetaClient sets the WABA Meta client used for Meta API interactions.
func (h *ConnectionAPIHandler) SetMetaClient(metaClient WABAMetaClient) {
	h.metaClient = metaClient
}

// SetTelegramClient sets the Telegram client used for Telegram Bot API interactions.
func (h *ConnectionAPIHandler) SetTelegramClient(tgClient TelegramBotClient) {
	h.telegramClient = tgClient
}

// SetExternalURL sets the base external URL for webhook callbacks.
func (h *ConnectionAPIHandler) SetExternalURL(externalURL string) {
	h.externalURL = externalURL
}

// RegisterRoutes registers the connection endpoints on both /api/v1/connections and /api/v1/devices.
func (h *ConnectionAPIHandler) RegisterRoutes(e *echo.Echo) {
	// Canonical routes
	connGroup := e.Group("/api/v1/connections")
	connGroup.POST("/pair", h.StartPairing)
	connGroup.POST("/waba", h.CreateWABA)
	connGroup.POST("/waba/", h.CreateWABA)
	connGroup.POST("/telegram", h.CreateTelegram)
	connGroup.POST("/telegram/", h.CreateTelegram)
	connGroup.GET("", h.List)
	connGroup.GET("/", h.List)
	connGroup.GET("/:id/qr/stream", h.StreamQR)
	connGroup.GET("/:id/qr", h.GetQR)
	connGroup.DELETE("/:id", h.Disconnect)

	// Workspace-scoped canonical routes
	wsConnGroup := e.Group("/api/v1/workspaces/:workspace_id/connections")
	wsConnGroup.POST("/waba", h.CreateWABA)
	wsConnGroup.POST("/waba/", h.CreateWABA)
	wsConnGroup.POST("/telegram", h.CreateTelegram)
	wsConnGroup.POST("/telegram/", h.CreateTelegram)

	// Retrocompatible aliases
	devGroup := e.Group("/api/v1/devices")
	devGroup.POST("/pair", h.StartPairing)
	devGroup.POST("/waba", h.CreateWABA)
	devGroup.POST("/waba/", h.CreateWABA)
	devGroup.POST("/telegram", h.CreateTelegram)
	devGroup.POST("/telegram/", h.CreateTelegram)
	devGroup.GET("", h.List)
	devGroup.GET("/", h.List)
	devGroup.GET("/:id/qr/stream", h.StreamQR)
	devGroup.GET("/:id/qr", h.GetQR)
	devGroup.DELETE("/:id", h.Disconnect)

	// Workspace-scoped aliases
	wsDevGroup := e.Group("/api/v1/workspaces/:workspace_id/devices")
	wsDevGroup.POST("/waba", h.CreateWABA)
	wsDevGroup.POST("/waba/", h.CreateWABA)
	wsDevGroup.POST("/telegram", h.CreateTelegram)
	wsDevGroup.POST("/telegram/", h.CreateTelegram)
}

func (h *ConnectionAPIHandler) resolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	ctxID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || ctxID == uuid.Nil {
		return uuid.Nil, errors.New("workspace context required")
	}

	if paramIDStr, err := echo.PathParam[string](c, "workspace_id"); err == nil && paramIDStr != "" {
		parsed, err := uuid.Parse(paramIDStr)
		if err != nil || parsed == uuid.Nil {
			return uuid.Nil, errors.New("invalid workspace_id parameter")
		}
		if parsed != ctxID {
			return uuid.Nil, errors.New("workspace ID mismatch: authenticated workspace does not match URL")
		}
	}

	return ctxID, nil
}

func sanitizePhoneIdentity(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	if clean, valid := domain.SanitizePhone(phone); valid {
		return clean
	}
	return phone
}

func (h *ConnectionAPIHandler) generateUniqueSlug(ctx context.Context, wsID uuid.UUID, name, defaultSlug string) string {
	baseSlug := slug.Generate(name)
	if baseSlug == "" {
		baseSlug = defaultSlug
	}
	candidateSlug := baseSlug
	if h.repo != nil {
		counter := 1
		for {
			existing, err := h.repo.GetBySlug(ctx, wsID, candidateSlug)
			if errors.Is(err, repository.ErrConnectionNotFound) || existing == nil {
				break
			}
			counter++
			candidateSlug = fmt.Sprintf("%s-%d", baseSlug, counter)
		}
	}
	return candidateSlug
}

// StoredWABAConfig represents the credentials stored in the database for a WhatsApp Cloud API connection.
type StoredWABAConfig struct {
	PhoneNumberID string `json:"phone_number_id"`
	Token         string `json:"token"`
	WABAAccountID string `json:"waba_account_id"`
	VerifyToken   string `json:"verify_token,omitempty"`
	AppSecret     string `json:"app_secret,omitempty"`
}

// StoredTelegramConfig represents the credentials stored in the database for a Telegram Bot connection.
type StoredTelegramConfig struct {
	Token       string `json:"token"`
	SecretToken string `json:"secret_token"`
	BotUsername string `json:"bot_username"`
}

// CreateWABAConnectionRequest defines the JSON payload for registering a WhatsApp Cloud API connection.
type CreateWABAConnectionRequest struct {
	Name               string `json:"name"`
	PhoneNumberID      string `json:"phone_number_id"`
	WABAAccountID      string `json:"waba_account_id"`
	Token              string `json:"token"`
	VerifyToken        string `json:"verify_token,omitempty"`
	AppSecret          string `json:"app_secret,omitempty"`
	DisplayPhoneNumber string `json:"display_phone_number,omitempty"`
	VerifiedName       string `json:"verified_name,omitempty"`
}

// CreateWABA registers a new WhatsApp Cloud (WABA) connection headless via REST API.
// POST /api/v1/connections/waba & POST /api/v1/workspaces/:workspace_id/connections/waba
func (h *ConnectionAPIHandler) CreateWABA(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		if strings.Contains(err.Error(), "mismatch") {
			return c.JSON(http.StatusForbidden, map[string]string{
				"code":    "forbidden",
				"message": err.Error(),
			})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": err.Error(),
		})
	}

	var req CreateWABAConnectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	phoneNumberID := strings.TrimSpace(req.PhoneNumberID)
	wabaAccountID := strings.TrimSpace(req.WABAAccountID)
	token := strings.TrimSpace(req.Token)

	if phoneNumberID == "" || wabaAccountID == "" || token == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "phone_number_id, waba_account_id, and token are required",
		})
	}

	ctx := c.Request().Context()

	var senderIdentity string
	var verifiedName string = strings.TrimSpace(req.VerifiedName)

	if displayPhone := sanitizePhoneIdentity(req.DisplayPhoneNumber); displayPhone != "" {
		senderIdentity = displayPhone
	} else if h.metaClient != nil {
		if details, err := h.metaClient.FetchPhoneNumberDetails(ctx, phoneNumberID, token); err == nil && details != nil {
			if details.DisplayPhoneNumber != "" {
				senderIdentity = sanitizePhoneIdentity(details.DisplayPhoneNumber)
			}
			if verifiedName == "" && details.VerifiedName != "" {
				verifiedName = strings.TrimSpace(details.VerifiedName)
			}
		}
	}

	if senderIdentity == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "display_phone_number is required when Meta phone number details cannot be fetched automatically",
		})
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		if verifiedName != "" {
			name = verifiedName
		} else if req.DisplayPhoneNumber != "" {
			name = req.DisplayPhoneNumber
		} else {
			name = "WABA " + senderIdentity
		}
	}

	candidateSlug := h.generateUniqueSlug(ctx, wsID, name, "waba")

	wabaCfg := StoredWABAConfig{
		PhoneNumberID: phoneNumberID,
		Token:         token,
		WABAAccountID: wabaAccountID,
		VerifyToken:   strings.TrimSpace(req.VerifyToken),
		AppSecret:     strings.TrimSpace(req.AppSecret),
	}

	credentialsJSON, err := json.Marshal(wabaCfg)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to encode credentials",
		})
	}

	if h.repo == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "connection repository not configured",
		})
	}

	now := time.Now().UTC()
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           name,
		Slug:           candidateSlug,
		Channel:        "whatsapp_cloud",
		SenderIdentity: senderIdentity,
		Status:         "connected",
		Credentials:    credentialsJSON,
		ConnectedSince: &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := h.repo.Create(ctx, conn); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": fmt.Sprintf("failed to save connection: %v", err),
		})
	}

	if h.metaClient != nil {
		if _, err := h.metaClient.SyncTemplates(ctx, conn.ID, wabaAccountID, token, wsID, h.templatesRepo, true); err != nil {
			slog.WarnContext(ctx, "failed to run initial template sync on waba connection creation", "error", err, "connection_id", conn.ID)
		}
	}

	if h.manager != nil {
		_ = h.manager.EmitStatusEvent(ctx, wsID, conn.ID, conn.Channel, conn.SenderIdentity, "connected")
	}

	resp := ConnectionItem{
		ID:             conn.ID,
		Name:           conn.Name,
		Slug:           conn.Slug,
		Channel:        conn.Channel,
		SenderIdentity: conn.SenderIdentity,
		Status:         conn.Status,
		IsDefault:      conn.IsDefault,
		ConnectedSince: conn.ConnectedSince,
		CreatedAt:      conn.CreatedAt,
		UpdatedAt:      conn.UpdatedAt,
	}

	return c.JSON(http.StatusCreated, resp)
}

// CreateTelegramConnectionRequest defines the JSON payload for registering a Telegram Bot connection.
type CreateTelegramConnectionRequest struct {
	Name        string `json:"name,omitempty"`
	Token       string `json:"token"`
	SecretToken string `json:"secret_token,omitempty"`
}

// CreateTelegram registers a new Telegram bot connection headless via REST API.
// POST /api/v1/connections/telegram & POST /api/v1/workspaces/:workspace_id/connections/telegram
func (h *ConnectionAPIHandler) CreateTelegram(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		if strings.Contains(err.Error(), "mismatch") {
			return c.JSON(http.StatusForbidden, map[string]string{
				"code":    "forbidden",
				"message": err.Error(),
			})
		}
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": err.Error(),
		})
	}

	var req CreateTelegramConnectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "token is required for Telegram bot connection",
		})
	}

	ctx := c.Request().Context()

	tgClient := h.telegramClient
	if tgClient == nil {
		tgClient = client.NewTelegramBotClient(nil, "")
	}

	botUsername, err := tgClient.ValidateToken(ctx, token)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "invalid_telegram_token",
			"message": fmt.Sprintf("failed to validate Telegram token: %v", err),
		})
	}

	secretToken := strings.TrimSpace(req.SecretToken)
	if strings.HasPrefix(h.externalURL, "https://") {
		if secretToken == "" {
			secretToken = uuid.New().String()
		}
		webhookURL := fmt.Sprintf("%s/webhooks/telegram/%s", h.externalURL, wsID.String())
		if err := tgClient.RegisterWebhook(ctx, token, webhookURL, secretToken); err != nil {
			slog.WarnContext(ctx, "failed to register Telegram webhook on connection creation", "error", err, "workspace_id", wsID)
		}
	} else if secretToken == "" {
		secretToken = "pergo_secret_token_" + wsID.String()
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Telegram " + botUsername
	}

	candidateSlug := h.generateUniqueSlug(ctx, wsID, name, "telegram")

	creds := StoredTelegramConfig{
		Token:       token,
		SecretToken: secretToken,
		BotUsername: botUsername,
	}

	credentialsJSON, err := json.Marshal(creds)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to encode credentials",
		})
	}

	if h.repo == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "connection repository not configured",
		})
	}

	now := time.Now().UTC()
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           name,
		Slug:           candidateSlug,
		Channel:        "telegram",
		SenderIdentity: botUsername,
		Status:         "connected",
		Credentials:    credentialsJSON,
		ConnectedSince: &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := h.repo.Create(ctx, conn); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": fmt.Sprintf("failed to save connection: %v", err),
		})
	}

	if h.manager != nil {
		_ = h.manager.EmitStatusEvent(ctx, wsID, conn.ID, conn.Channel, conn.SenderIdentity, "connected")
	}

	resp := ConnectionItem{
		ID:             conn.ID,
		Name:           conn.Name,
		Slug:           conn.Slug,
		Channel:        conn.Channel,
		SenderIdentity: conn.SenderIdentity,
		Status:         conn.Status,
		IsDefault:      conn.IsDefault,
		ConnectedSince: conn.ConnectedSince,
		CreatedAt:      conn.CreatedAt,
		UpdatedAt:      conn.UpdatedAt,
	}

	return c.JSON(http.StatusCreated, resp)
}

// PairConnectionRequest defines the JSON payload for initiating a pairing flow.
type PairConnectionRequest struct {
	Channel      string     `json:"channel"`
	Phone        string     `json:"phone"`
	Name         string     `json:"name,omitempty"`
	ProxyURL     string     `json:"proxy_url,omitempty"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
}

// PairConnectionResponse defines the JSON response returned when pairing is initiated.
type PairConnectionResponse struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	Phone        string    `json:"phone"`
	Status       string    `json:"status"`
	Message      string    `json:"message"`
}

// StartPairing initiates QR code pairing for a WhatsApp Web connection.
// POST /api/v1/connections/pair & POST /api/v1/devices/pair
func (h *ConnectionAPIHandler) StartPairing(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || wsID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	var req PairConnectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	if req.Channel != "" && !strings.EqualFold(req.Channel, "whatsapp") {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "unsupported channel for qr pairing: only whatsapp is supported",
		})
	}

	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "phone number is required",
		})
	}

	if h.manager == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "session manager not configured",
		})
	}

	ps, err := h.manager.StartPairingSession(c.Request().Context(), wsID, phone, req.ConnectionID, req.ProxyURL)
	if err != nil {
		if errors.Is(err, session.ErrMaxConnectionsExceeded) {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{
				"code":    "limit_exceeded",
				"message": err.Error(),
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, PairConnectionResponse{
		ConnectionID: ps.ConnectionID(),
		Phone:        phone,
		Status:       "pairing_started",
		Message:      "Sessão iniciada. Obtenha o QR code via polling ou stream SSE.",
	})
}

// GetQR returns the current QR code state for a pairing session or connection.
// GET /api/v1/connections/:id/qr & GET /api/v1/devices/:id/qr
func (h *ConnectionAPIHandler) GetQR(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || wsID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	id, _ := echo.PathParam[string](c, "id")
	if id == "" {
		id = c.QueryParam("phone")
	}
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "connection ID or phone is required",
		})
	}

	if h.manager != nil {
		if evt, ok := h.manager.GetPairingStateForWorkspace(wsID, id); ok && evt != nil {
			return c.JSON(http.StatusOK, evt)
		}
		if phone := c.QueryParam("phone"); phone != "" && phone != id {
			if evt, ok := h.manager.GetPairingStateForWorkspace(wsID, phone); ok && evt != nil {
				return c.JSON(http.StatusOK, evt)
			}
		}
	}

	if parsedID, err := uuid.Parse(id); err == nil && h.repo != nil {
		conn, err := h.repo.GetByID(c.Request().Context(), parsedID)
		if err == nil && conn != nil && conn.WorkspaceID == wsID {
			if conn.Status == "connected" || conn.Status == "active" {
				return c.JSON(http.StatusOK, session.QREvent{
					Status:       "paired",
					Message:      "device is connected",
					ConnectionID: &conn.ID,
				})
			}
			return c.JSON(http.StatusOK, session.QREvent{
				Status:       "disconnected",
				Message:      "No active pairing session",
				ConnectionID: &conn.ID,
			})
		}
	}

	return c.JSON(http.StatusNotFound, map[string]string{
		"code":    "not_found",
		"message": "pairing session or connection not found",
	})
}

// StreamQR streams real-time QR pairing events via Server-Sent Events (SSE).
// GET /api/v1/connections/:id/qr/stream & GET /api/v1/devices/:id/qr/stream
func (h *ConnectionAPIHandler) StreamQR(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || wsID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	id, _ := echo.PathParam[string](c, "id")
	if id == "" {
		id = c.QueryParam("phone")
	}
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "connection ID or phone is required",
		})
	}

	if h.manager == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "session manager not configured",
		})
	}

	qrCh, unsub, ok := h.manager.SubscribeQRForWorkspace(wsID, id)
	if !ok {
		if parsedID, err := uuid.Parse(id); err == nil && h.repo != nil {
			conn, err := h.repo.GetByID(c.Request().Context(), parsedID)
			if err != nil || conn == nil || conn.WorkspaceID != wsID {
				return c.JSON(http.StatusNotFound, map[string]string{
					"code":    "not_found",
					"message": "pairing session or connection not found",
				})
			}
		} else {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "not_found",
				"message": "pairing session not found",
			})
		}
	}
	defer unsub()

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	flusher, ok := c.Response().(http.Flusher)
	if ok {
		flusher.Flush()
	}

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-pingTicker.C:
			if _, err := fmt.Fprintf(c.Response(), ": ping\n\n"); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		case evt, ok := <-qrCh:
			if !ok {
				return nil
			}
			eventName := "qr"
			if evt.Status == "paired" {
				eventName = "paired"
			} else if evt.Status == "error" {
				eventName = "error"
			}
			dataBytes, err := json.Marshal(evt)
			if err != nil {
				return nil
			}
			if _, err := fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", eventName, string(dataBytes)); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
			if evt.Status == "paired" || evt.Status == "error" {
				return nil
			}
		}
	}
}

// ConnectionItem represents a single connection item in the list response.
type ConnectionItem struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Channel        string     `json:"channel"`
	SenderIdentity string     `json:"sender_identity"`
	Status         string     `json:"status"`
	IsDefault      bool       `json:"is_default"`
	BatteryLevel   *int       `json:"battery_level,omitempty"`
	PushName       string     `json:"push_name,omitempty"`
	ConnectedSince *time.Time `json:"connected_since,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ListConnectionsResponse defines the JSON response returned by List.
type ListConnectionsResponse struct {
	Connections []ConnectionItem `json:"connections"`
}

// List returns all connections for the authenticated workspace.
// GET /api/v1/connections & GET /api/v1/devices
func (h *ConnectionAPIHandler) List(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || wsID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	if h.repo == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "connection repository not configured",
		})
	}

	conns, err := h.repo.ListByWorkspace(c.Request().Context(), wsID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to list connections",
		})
	}

	items := make([]ConnectionItem, 0, len(conns))
	for _, conn := range conns {
		var pushName string
		if conn.Channel == "whatsapp" && conn.JID != nil && *conn.JID != "" && h.sessions != nil {
			if client := h.sessions.GetClient(*conn.JID); client != nil {
				if store := client.DeviceStore(); store != nil {
					pushName = store.PushName
				}
			}
		}
		items = append(items, ConnectionItem{
			ID:             conn.ID,
			Name:           conn.Name,
			Slug:           conn.Slug,
			Channel:        conn.Channel,
			SenderIdentity: conn.SenderIdentity,
			Status:         conn.Status,
			IsDefault:      conn.IsDefault,
			PushName:       pushName,
			ConnectedSince: conn.ConnectedSince,
			CreatedAt:      conn.CreatedAt,
			UpdatedAt:      conn.UpdatedAt,
		})
	}

	return c.JSON(http.StatusOK, ListConnectionsResponse{
		Connections: items,
	})
}

// Disconnect deletes a connection and disconnects any active session.
// DELETE /api/v1/connections/:id & DELETE /api/v1/devices/:id
func (h *ConnectionAPIHandler) Disconnect(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok || wsID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "connection ID is required",
		})
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid connection ID format",
		})
	}

	if h.repo == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "connection repository not configured",
		})
	}

	conn, err := h.repo.GetByID(c.Request().Context(), id)
	if err != nil || conn == nil || conn.WorkspaceID != wsID {
		return c.JSON(http.StatusNotFound, map[string]string{
			"code":    "not_found",
			"message": "connection not found",
		})
	}

	if conn.Channel == "whatsapp" {
		if h.sessions != nil && conn.JID != nil && *conn.JID != "" {
			h.sessions.DisconnectByJID(*conn.JID)
		}
		if h.manager != nil {
			h.manager.CancelPairing(id)
			if conn.SenderIdentity != "" {
				h.manager.CancelPairingByPhone(conn.SenderIdentity)
			}
		}
	}

	if h.manager != nil {
		_ = h.manager.EmitStatusEvent(c.Request().Context(), wsID, id, conn.Channel, conn.SenderIdentity, "disconnected")
	}

	if err := h.repo.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to delete connection",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":        "disconnected",
		"connection_id": id,
	})
}
