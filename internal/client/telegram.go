package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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
	escapedURL := url.QueryEscape(webhookURL)
	escapedSecret := url.QueryEscape(secretToken)
	endpoint := fmt.Sprintf("%s/bot%s/setWebhook?url=%s&secret_token=%s", c.baseURL, token, escapedURL, escapedSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
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

	slog.InfoContext(ctx, "Telegram webhook registered successfully", "url", webhookURL)
	return nil
}

// BaseURL returns the configured Telegram Bot API base URL.
func (c *HTTPTelegramBotClient) BaseURL() string {
	if c == nil || c.baseURL == "" {
		return "https://api.telegram.org"
	}
	return c.baseURL
}

// DeleteWebhook removes any configured webhook on Telegram so local long-polling can receive updates.
func (c *HTTPTelegramBotClient) DeleteWebhook(ctx context.Context, token string) error {
	endpoint := fmt.Sprintf("%s/bot%s/deleteWebhook?drop_pending_updates=false", c.baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create Telegram deleteWebhook request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Telegram API for deleteWebhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram deleteWebhook returned HTTP status %d", resp.StatusCode)
	}

	var tgResp struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return fmt.Errorf("failed to decode Telegram deleteWebhook response: %w", err)
	}
	if !tgResp.Ok {
		return fmt.Errorf("Telegram deleteWebhook failed: %s", tgResp.Description)
	}
	return nil
}

// GetUpdates performs long-polling against Telegram's /getUpdates endpoint and returns raw JSON updates
// along with the next offset (highest update_id + 1, or offset when no updates arrived).
func (c *HTTPTelegramBotClient) GetUpdates(ctx context.Context, token string, offset int64, timeoutSec int) ([]json.RawMessage, int64, error) {
	if timeoutSec < 0 {
		timeoutSec = 0
	}
	params := url.Values{}
	if offset > 0 {
		params.Set("offset", strconv.FormatInt(offset, 10))
	}
	params.Set("timeout", strconv.Itoa(timeoutSec))

	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?%s", c.baseURL, token, params.Encode())

	reqTimeout := time.Duration(timeoutSec+10) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, offset, fmt.Errorf("failed to create Telegram getUpdates request: %w", err)
	}

	hc := c.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: reqTimeout}
	} else if hc.Timeout > 0 && hc.Timeout < reqTimeout {
		cloned := *hc
		cloned.Timeout = reqTimeout
		hc = &cloned
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, offset, fmt.Errorf("failed to fetch Telegram updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, offset, fmt.Errorf("Telegram getUpdates returned HTTP status %d", resp.StatusCode)
	}

	var tgResp struct {
		Ok          bool              `json:"ok"`
		Description string            `json:"description"`
		Result      []json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return nil, offset, fmt.Errorf("failed to decode Telegram getUpdates response: %w", err)
	}
	if !tgResp.Ok {
		return nil, offset, fmt.Errorf("Telegram getUpdates failed: %s", tgResp.Description)
	}

	nextOffset := offset
	for _, raw := range tgResp.Result {
		var hdr struct {
			UpdateID int64 `json:"update_id"`
		}
		if err := json.Unmarshal(raw, &hdr); err == nil && hdr.UpdateID >= nextOffset {
			nextOffset = hdr.UpdateID + 1
		}
	}

	return tgResp.Result, nextOffset, nil
}
