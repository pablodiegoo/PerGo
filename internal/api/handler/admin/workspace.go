package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// WorkspaceHandler holds dependencies for workspace admin operations.
type WorkspaceHandler struct {
	Repo        *repository.WorkspaceRepository
	APIKeys     *repository.APIKeyRepository
	Credentials *repository.CredentialsRepository
	Templates   *repository.WABATemplateRepository
	ExternalURL string
}

// List renders the workspace list page or HTMX fragment.
func (h *WorkspaceHandler) List(c *echo.Context) error {
	workspaces, err := h.Repo.List(c.Request().Context(), 50)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load workspaces")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.WorkspaceListContent(workspaces))
	}
	return mw.Render(c, http.StatusOK, pages.WorkspaceListPage(workspaces))
}

// Create handles workspace creation via POST form.
func (h *WorkspaceHandler) Create(c *echo.Context) error {
	name := c.FormValue("name")
	if name == "" {
		return c.String(http.StatusBadRequest, "name is required")
	}

	ws, err := h.Repo.Create(c.Request().Context(), name)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create workspace")
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceRow(*ws))
}

// Detail renders the workspace detail page with API keys and channel configuration.
func (h *WorkspaceHandler) Detail(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	var keys []repository.APIKey
	if h.APIKeys != nil {
		keys, err = h.APIKeys.ListByWorkspace(c.Request().Context(), id)
		if err != nil {
			keys = nil // degrade gracefully
		}
	}

	var waba pages.WABAConfig
	var tg pages.TelegramConfig

	if h.Credentials != nil {
		wabaBytes, err := h.Credentials.Get(c.Request().Context(), id, "whatsapp_cloud")
		if err == nil {
			_ = json.Unmarshal(wabaBytes, &waba)
		}
		tgBytes, err := h.Credentials.Get(c.Request().Context(), id, "telegram")
		if err == nil {
			_ = json.Unmarshal(tgBytes, &tg)
		}
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceDetailPage(*ws, keys, waba, tg, h.ExternalURL))
}

// ConfirmDelete returns an HTMX modal fragment for delete confirmation.
func (h *WorkspaceHandler) ConfirmDelete(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceDeleteConfirm(*ws))
}

// Delete removes a workspace and returns empty 200 for HTMX to remove the row.
func (h *WorkspaceHandler) Delete(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	if err := h.Repo.Delete(c.Request().Context(), id); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete workspace")
	}

	return c.NoContent(http.StatusOK)
}

// SaveCredentials handles form submission via POST for channel credentials.
func (h *WorkspaceHandler) SaveCredentials(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	channel, err := echo.PathParam[string](c, "channel")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid channel param")
	}
	if channel != "whatsapp_cloud" && channel != "telegram" {
		return c.String(http.StatusBadRequest, "invalid channel")
	}

	var payload []byte
	var waba pages.WABAConfig
	var tg pages.TelegramConfig

	if channel == "whatsapp_cloud" {
		waba = pages.WABAConfig{
			PhoneNumberID: c.FormValue("phone_number_id"),
			Token:         c.FormValue("token"),
			WABAAccountID: c.FormValue("waba_account_id"),
			VerifyToken:   c.FormValue("verify_token"),
		}
		// Validate credentials synchronously by running template sync
		err = h.syncTemplatesFromMeta(c.Request().Context(), workspaceID, waba)
		if err != nil {
			slog.Warn("WABA credentials validation failed", "error", err, "workspace_id", workspaceID)
			waba.Token = "" // Clear token to render the form again
			return mw.Render(c, http.StatusOK, pages.WABACredentialsCard(idStr, waba, err.Error(), h.ExternalURL))
		}
		payload, err = json.Marshal(waba)
	} else {
		tg = pages.TelegramConfig{
			Token: c.FormValue("token"),
		}
		// Validate Telegram token synchronously
		var botUsername string
		botUsername, err = h.validateTelegramToken(c.Request().Context(), tg.Token)
		if err != nil {
			slog.Warn("Telegram token validation failed", "error", err, "workspace_id", workspaceID)
			tg.Token = "" // Clear token to render the form again
			return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, tg, err.Error(), h.ExternalURL))
		}

		secretToken := ""
		if strings.HasPrefix(h.ExternalURL, "https://") {
			secretToken = uuid.New().String()
			webhookURL := fmt.Sprintf("%s/webhooks/telegram/%s", h.ExternalURL, idStr)
			err = h.registerTelegramWebhook(c.Request().Context(), tg.Token, webhookURL, secretToken)
			if err != nil {
				slog.Warn("failed to register Telegram webhook", "error", err, "workspace_id", workspaceID)
				tg.Token = "" // Clear token to render the form again
				return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, tg, fmt.Sprintf("Bot token is valid, but failed to set webhook: %v", err), h.ExternalURL))
			}
		} else {
			// Generate a predictable fallback secret token for local non-HTTPS development
			secretToken = "pergo_secret_token_" + idStr
		}

		type storedTelegramConfig struct {
			Token       string `json:"token"`
			SecretToken string `json:"secret_token"`
			BotUsername string `json:"bot_username"`
		}

		payload, err = json.Marshal(storedTelegramConfig{
			Token:       tg.Token,
			SecretToken: secretToken,
			BotUsername: botUsername,
		})
	}
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to marshal credentials")
	}

	if err := h.Credentials.Save(c.Request().Context(), workspaceID, channel, payload); err != nil {
		return c.String(http.StatusInternalServerError, "failed to save credentials")
	}

	if channel == "whatsapp_cloud" {
		return mw.Render(c, http.StatusOK, pages.WABACredentialsCard(idStr, waba, "", h.ExternalURL))
	} else {
		return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, tg, "", h.ExternalURL))
	}
}

func (h *WorkspaceHandler) registerTelegramWebhook(ctx context.Context, token, webhookURL, secretToken string) error {
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

func (h *WorkspaceHandler) validateTelegramToken(ctx context.Context, token string) (string, error) {
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
	// Ensure username has "@" prefix for consistency if it doesn't already,
	// but Telegram usernames returned by getMe do NOT have "@" prefix.
	// We want prefix "@" for consistency as connection sender_identity.
	username := tgResp.Result.Username
	if !strings.HasPrefix(username, "@") {
		username = "@" + username
	}
	return username, nil
}

func (h *WorkspaceHandler) syncTemplatesFromMeta(ctx context.Context, workspaceID uuid.UUID, config pages.WABAConfig) error {
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
			MetaTemplateID: t.ID,
			Name:           t.Name,
			Language:       t.Language,
			Status:         t.Status,
			Category:       t.Category,
			Components:     componentsJSON,
		}

		if h.Templates != nil {
			_, err = h.Templates.Upsert(ctx, dbTmpl)
			if err != nil {
				slog.Error("failed to upsert template in local DB", "error", err, "template", t.Name)
			}
		}
	}
	return nil
}

// DeleteCredentials revokes channel credentials.
func (h *WorkspaceHandler) DeleteCredentials(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	channel, err := echo.PathParam[string](c, "channel")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid channel param")
	}
	if channel != "whatsapp_cloud" && channel != "telegram" {
		return c.String(http.StatusBadRequest, "invalid channel")
	}

	if err := h.Credentials.Delete(c.Request().Context(), workspaceID, channel); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete credentials")
	}

	if channel == "whatsapp_cloud" {
		return mw.Render(c, http.StatusOK, pages.WABACredentialsCard(idStr, pages.WABAConfig{}, "", h.ExternalURL))
	} else {
		return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, pages.TelegramConfig{}, "", h.ExternalURL))
	}
}
