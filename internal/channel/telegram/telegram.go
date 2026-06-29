package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// TelegramAdapter implements channel.Dispatcher for Telegram Bot API.
type TelegramAdapter struct {
	credentialsRepo *repository.CredentialsRepository
	client          *http.Client
	baseURL         string
	s3Client        *storage.S3Client
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
func NewTelegramAdapter(credentialsRepo *repository.CredentialsRepository, client *http.Client, s3Client *storage.S3Client) *TelegramAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return &TelegramAdapter{
		credentialsRepo: credentialsRepo,
		client:          client,
		baseURL:         "https://api.telegram.org",
		s3Client:        s3Client,
	}
}

// SetBaseURL overrides the base API URL (useful for testing).
func (a *TelegramAdapter) SetBaseURL(url string) {
	a.baseURL = url
}

// Dispatch sends a message through the Telegram Bot API.
func (a *TelegramAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
	workspaceID, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		return "", channel.NewTerminalError(err)
	}

	credsBytes, err := a.credentialsRepo.Get(ctx, workspaceID, "telegram")
	if err != nil {
		if errors.Is(err, repository.ErrCredentialsNotFound) {
			return "", channel.NewTerminalError(fmt.Errorf("credentials not found: %w", err))
		}
		return "", err
	}

	var config TelegramConfig
	if err := json.Unmarshal(credsBytes, &config); err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("invalid credentials format: %w", err))
	}

	if config.Token == "" {
		return "", channel.NewTerminalError(errors.New("missing bot token in credentials"))
	}

	if m.Media != nil {
		if a.s3Client == nil {
			return "", channel.NewTerminalError(fmt.Errorf("telegram: media storage client not configured"))
		}

		parts := strings.Split(m.Media.MediaURL, "/")
		if len(parts) < 3 {
			return "", channel.NewTerminalError(fmt.Errorf("telegram: invalid media URL format: %s", m.Media.MediaURL))
		}
		workspaceIDStr := parts[len(parts)-2]
		hashWithExt := parts[len(parts)-1]
		key := workspaceIDStr + "/" + hashWithExt

		bodyRC, _, err := a.s3Client.Download(ctx, key)
		if err != nil {
			return "", fmt.Errorf("telegram media download from S3 failed: %w", err)
		}
		defer bodyRC.Close()

		var bodyBuf bytes.Buffer
		writer := multipart.NewWriter(&bodyBuf)

		// Set chat_id
		if err := writer.WriteField("chat_id", m.To); err != nil {
			return "", err
		}

		// Set caption
		if m.Media.Caption != "" {
			if err := writer.WriteField("caption", m.Media.Caption); err != nil {
				return "", err
			}
		}

		var fieldName string
		var endpoint string
		switch m.Media.MediaType {
		case "image":
			fieldName = "photo"
			endpoint = "sendPhoto"
		case "document":
			fieldName = "document"
			endpoint = "sendDocument"
		case "audio":
			fieldName = "audio"
			endpoint = "sendAudio"
		case "video":
			fieldName = "video"
			endpoint = "sendVideo"
		default:
			return "", channel.NewTerminalError(fmt.Errorf("telegram: unsupported media type %s", m.Media.MediaType))
		}

		filename := m.Media.Filename
		if filename == "" {
			filename = "file"
		}
		part, err := writer.CreateFormFile(fieldName, filename)
		if err != nil {
			return "", err
		}

		if _, err := io.Copy(part, bodyRC); err != nil {
			return "", err
		}

		if err := writer.Close(); err != nil {
			return "", err
		}

		url := fmt.Sprintf("%s/bot%s/%s", a.baseURL, config.Token, endpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &bodyBuf)
		if err != nil {
			return "", channel.NewTerminalError(fmt.Errorf("create HTTP request: %w", err))
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		return a.executeRequest(req)
	}

	reqPayload := telegramMessageRequest{
		ChatID: m.To,
		Text:   m.Body,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("marshal request: %w", err))
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", a.baseURL, config.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("create HTTP request: %w", err))
	}

	req.Header.Set("Content-Type", "application/json")
	return a.executeRequest(req)
}

func (a *TelegramAdapter) executeRequest(req *http.Request) (string, error) {
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return string(respBytes), nil
	}

	var errorResp TelegramErrorResponse
	if err := json.Unmarshal(respBytes, &errorResp); err != nil {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return string(respBytes), channel.NewTerminalError(fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes)))
		}
		return string(respBytes), fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes))
	}

	return string(respBytes), a.classifyError(resp.StatusCode, &errorResp)
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
