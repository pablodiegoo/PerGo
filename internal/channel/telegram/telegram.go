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
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// ErrTelegramMediaRetryable is returned for retryable media storage or download failures.
var ErrTelegramMediaRetryable = errors.New("telegram: retryable media storage failure")

const maxTelegramResponseBodySize = 5 * 1024 * 1024 // 5MB limit

// TelegramAdapter implements channel.Dispatcher for Telegram Bot API.
type TelegramAdapter struct {
	connectionsRepo *repository.ConnectionRepository
	client          *http.Client
	baseURL         string
	s3Client        *storage.S3Client
}

// TelegramConfig represents the Telegram credentials JSON structure.
type TelegramConfig struct {
	Token string `json:"token"`
}

type inlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type inlineKeyboardMarkup struct {
	InlineKeyboard [][]inlineKeyboardButton `json:"inline_keyboard"`
}

type telegramMessageRequest struct {
	ChatID          string                `json:"chat_id"`
	MessageThreadID string                `json:"message_thread_id,omitempty"`
	Text            string                `json:"text"`
	ReplyMarkup     *inlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// TelegramErrorResponse represents the Telegram Bot API error body.
type TelegramErrorResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// NewTelegramAdapter creates a new TelegramAdapter.
func NewTelegramAdapter(connectionsRepo *repository.ConnectionRepository, client *http.Client, s3Client *storage.S3Client) *TelegramAdapter {
	if client == nil {
		client = netpolicy.NewPublicHTTPClient()
	}
	return &TelegramAdapter{
		connectionsRepo: connectionsRepo,
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
	_, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		return "", channel.NewTerminalError(err)
	}

	credsBytes, err := a.connectionsRepo.GetCredentials(ctx, m.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) {
			return "", channel.NewTerminalError(fmt.Errorf("connection credentials not found: %w", err))
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
			return "", fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, err)
		}
		defer bodyRC.Close()

		var bodyBuf bytes.Buffer
		writer := multipart.NewWriter(&bodyBuf)

		// Set chat_id
		if err := writer.WriteField("chat_id", m.To); err != nil {
			return "", err
		}

		// Set message_thread_id
		if m.Metadata != nil && m.Metadata["thread_id"] != "" {
			if err := writer.WriteField("message_thread_id", m.Metadata["thread_id"]); err != nil {
				return "", err
			}
		}

		// Set reply_markup and caption from interactive / media
		text, replyMarkup, interErr := buildTelegramInteractive(m)
		if interErr != nil {
			return "", interErr
		}

		if replyMarkup != nil {
			rmBytes, _ := json.Marshal(replyMarkup)
			if err := writer.WriteField("reply_markup", string(rmBytes)); err != nil {
				return "", err
			}
		}

		// Set caption
		caption := m.Media.Caption
		if caption == "" && text != "" {
			caption = text
		}
		if caption != "" {
			if err := writer.WriteField("caption", caption); err != nil {
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
		case "audio", "voice":
			if m.Media.MediaType == "voice" || m.Media.PTT {
				fieldName = "voice"
				endpoint = "sendVoice"
			} else {
				fieldName = "audio"
				endpoint = "sendAudio"
			}
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

	text, replyMarkup, interErr := buildTelegramInteractive(m)
	if interErr != nil {
		return "", interErr
	}

	var messageThreadID string
	if m.Metadata != nil {
		messageThreadID = m.Metadata["thread_id"]
	}

	reqPayload := telegramMessageRequest{
		ChatID:          m.To,
		MessageThreadID: messageThreadID,
		Text:            text,
		ReplyMarkup:     replyMarkup,
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

	lr := io.LimitReader(resp.Body, maxTelegramResponseBodySize+1)
	respBytes, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read Telegram API response: %w", err)
	}

	if int64(len(respBytes)) > maxTelegramResponseBodySize {
		return "", fmt.Errorf("telegram response body exceeds 5MB limit")
	}

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

func buildTelegramInteractive(m *channel.MessagePayload) (string, *inlineKeyboardMarkup, error) {
	if m.Product != nil || m.Type == domain.MessageTypeProduct || m.Type == domain.MessageTypeProductList {
		if m.FallbackBehavior == "fail" {
			return "", nil, channel.NewTerminalError(fmt.Errorf("telegram: product catalog is not natively supported and fallback_behavior is fail"))
		}
		var sb strings.Builder
		if m.Product != nil && m.Product.Header != "" {
			sb.WriteString(m.Product.Header)
			sb.WriteString("\n\n")
		}
		if m.Product != nil && m.Product.Body != "" {
			sb.WriteString(m.Product.Body)
		} else if m.Body != "" {
			sb.WriteString(m.Body)
		}
		if m.Product != nil {
			if m.Product.ProductRetailerID != "" {
				sb.WriteString(fmt.Sprintf("\n- Product: %s", m.Product.ProductRetailerID))
			}
			for _, s := range m.Product.Sections {
				if s.Title != "" {
					sb.WriteString(fmt.Sprintf("\n\n*%s*", s.Title))
				}
				for _, item := range s.ProductItems {
					sb.WriteString(fmt.Sprintf("\n- %s", item.ProductRetailerID))
				}
			}
			if m.Product.Footer != "" {
				sb.WriteString("\n\n")
				sb.WriteString(m.Product.Footer)
			}
		}
		return sb.String(), nil, nil
	}

	if m.Interactive == nil {
		return m.Body, nil, nil
	}

	switch m.Interactive.Type {
	case "button":
		var keyboard [][]inlineKeyboardButton
		for _, b := range m.Interactive.Action.Buttons {
			keyboard = append(keyboard, []inlineKeyboardButton{
				{
					Text:         b.Reply.Title,
					CallbackData: b.Reply.ID,
				},
			})
		}
		var replyMarkup *inlineKeyboardMarkup
		if len(keyboard) > 0 {
			replyMarkup = &inlineKeyboardMarkup{InlineKeyboard: keyboard}
		}

		var sb strings.Builder
		if m.Interactive.Header != nil && m.Interactive.Header.Text != "" {
			sb.WriteString(m.Interactive.Header.Text)
			sb.WriteString("\n\n")
		}
		if m.Interactive.Body.Text != "" {
			sb.WriteString(m.Interactive.Body.Text)
		} else if m.Body != "" {
			sb.WriteString(m.Body)
		}
		if m.Interactive.Footer != nil && m.Interactive.Footer.Text != "" {
			sb.WriteString("\n\n")
			sb.WriteString(m.Interactive.Footer.Text)
		}
		return sb.String(), replyMarkup, nil

	case "list":
		if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
			return "", nil, channel.NewTerminalError(fmt.Errorf("telegram: interactive list is not natively supported and fallback_behavior is fail"))
		}
		var keyboard [][]inlineKeyboardButton
		var sb strings.Builder
		if m.Interactive.Header != nil && m.Interactive.Header.Text != "" {
			sb.WriteString(m.Interactive.Header.Text)
			sb.WriteString("\n\n")
		}
		if m.Interactive.Body.Text != "" {
			sb.WriteString(m.Interactive.Body.Text)
		} else if m.Body != "" {
			sb.WriteString(m.Body)
		}

		for _, s := range m.Interactive.Action.Sections {
			if s.Title != "" {
				sb.WriteString(fmt.Sprintf("\n\n*%s*", s.Title))
			}
			for _, r := range s.Rows {
				btnText := r.Title
				keyboard = append(keyboard, []inlineKeyboardButton{
					{
						Text:         btnText,
						CallbackData: r.ID,
					},
				})
			}
		}

		if m.Interactive.Footer != nil && m.Interactive.Footer.Text != "" {
			sb.WriteString("\n\n")
			sb.WriteString(m.Interactive.Footer.Text)
		}

		var replyMarkup *inlineKeyboardMarkup
		if len(keyboard) > 0 {
			replyMarkup = &inlineKeyboardMarkup{InlineKeyboard: keyboard}
		}
		return sb.String(), replyMarkup, nil

	case "flow":
		if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
			return "", nil, channel.NewTerminalError(fmt.Errorf("telegram: interactive flow is not supported on telegram and fallback_behavior is fail"))
		}
		return m.Interactive.DegradeToText(), nil, nil

	default:
		if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
			return "", nil, channel.NewTerminalError(fmt.Errorf("telegram: interactive type %q is not supported on telegram and fallback_behavior is fail", m.Interactive.Type))
		}
		return m.Interactive.DegradeToText(), nil, nil
	}
}
