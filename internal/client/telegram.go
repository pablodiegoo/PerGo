package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// TelegramBotClient defines operations for interacting with the Telegram Bot API.
type TelegramBotClient interface {
	ValidateToken(ctx context.Context, token string) (string, error)
	RegisterWebhook(ctx context.Context, token, webhookURL, secretToken string) error
}

// HTTPTelegramBotClient provides HTTP interactions with the Telegram Bot API.
type HTTPTelegramBotClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewTelegramBotClient creates a new HTTPTelegramBotClient.
func NewTelegramBotClient(httpClient *http.Client, baseURL string) *HTTPTelegramBotClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &HTTPTelegramBotClient{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// ValidateToken calls /getMe to check if the bot token is valid and returns its username.
func (c *HTTPTelegramBotClient) ValidateToken(ctx context.Context, token string) (string, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", c.baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Telegram API request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return "", fmt.Errorf("failed to parse Telegram response: %w", err)
	}

	if !tgResp.Ok || tgResp.Result.Username == "" {
		return "", errors.New("Telegram API returned OK=false or empty username")
	}

	username := tgResp.Result.Username
	if !strings.HasPrefix(username, "@") {
		username = "@" + username
	}
	return username, nil
}

// RegisterWebhook sets up the webhook URL and secret token on Telegram.
func (c *HTTPTelegramBotClient) RegisterWebhook(ctx context.Context, token, webhookURL, secretToken string) error {
	url := fmt.Sprintf("%s/bot%s/setWebhook?url=%s&secret_token=%s", c.baseURL, token, webhookURL, secretToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create Telegram webhook registration request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
