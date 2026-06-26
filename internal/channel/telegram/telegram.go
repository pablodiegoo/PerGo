package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/repository"
)

// TelegramAdapter implements channel.Dispatcher for Telegram Bot API.
type TelegramAdapter struct {
	credentialsRepo *repository.CredentialsRepository
	client          *http.Client
	baseURL         string
}

// TelegramConfig represents the Telegram credentials JSON structure.
type TelegramConfig struct {
	Token string `json:"token"`
}

type telegramMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// TelegramErrorResponse represents the Telegram Bot API error body.
type TelegramErrorResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// NewTelegramAdapter creates a new TelegramAdapter.
func NewTelegramAdapter(credentialsRepo *repository.CredentialsRepository, client *http.Client) *TelegramAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return &TelegramAdapter{
		credentialsRepo: credentialsRepo,
		client:          client,
		baseURL:         "https://api.telegram.org",
	}
}

// SetBaseURL overrides the base API URL (useful for testing).
func (a *TelegramAdapter) SetBaseURL(url string) {
	a.baseURL = url
}

// Dispatch sends a message through the Telegram Bot API.
func (a *TelegramAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) error {
	workspaceID, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		return channel.NewTerminalError(err)
	}

	credsBytes, err := a.credentialsRepo.Get(ctx, workspaceID, "telegram")
	if err != nil {
		if errors.Is(err, repository.ErrCredentialsNotFound) {
			return channel.NewTerminalError(fmt.Errorf("credentials not found: %w", err))
		}
		return err
	}

	var config TelegramConfig
	if err := json.Unmarshal(credsBytes, &config); err != nil {
		return channel.NewTerminalError(fmt.Errorf("invalid credentials format: %w", err))
	}

	if config.Token == "" {
		return channel.NewTerminalError(errors.New("missing bot token in credentials"))
	}

	reqPayload := telegramMessageRequest{
		ChatID: m.To,
		Text:   m.Body,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return channel.NewTerminalError(fmt.Errorf("marshal request: %w", err))
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", a.baseURL, config.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return channel.NewTerminalError(fmt.Errorf("create HTTP request: %w", err))
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBytes, _ := io.ReadAll(resp.Body)
	var errorResp TelegramErrorResponse
	if err := json.Unmarshal(respBytes, &errorResp); err != nil {
		// Fallback status code classification
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return channel.NewTerminalError(fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes)))
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes))
	}

	return a.classifyError(resp.StatusCode, &errorResp)
}

func (a *TelegramAdapter) classifyError(statusCode int, errResp *TelegramErrorResponse) error {
	err := fmt.Errorf("Telegram API error (code: %d): %s", errResp.ErrorCode, errResp.Description)

	// Explicit check based on known Telegram error codes
	// 400: Bad Request (chat not found, etc.)
	// 401: Unauthorized (invalid token)
	// 403: Forbidden (bot blocked by user, etc.)
	if errResp.ErrorCode == 400 || errResp.ErrorCode == 401 || errResp.ErrorCode == 403 {
		return channel.NewTerminalError(err)
	}

	// 429: Too Many Requests (Rate limit hit)
	if errResp.ErrorCode == 429 {
		return err
	}

	// General HTTP Status classification
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return err
	}
	if statusCode >= 400 && statusCode < 500 {
		return channel.NewTerminalError(err)
	}

	return err
}
