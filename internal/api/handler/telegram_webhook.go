package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// TelegramWebhookHandler handles inbound webhooks from Telegram.
type TelegramWebhookHandler struct {
	pool                 *repository.WorkspaceRepository
	credsRepo            *repository.CredentialsRepository
	recipientSessionRepo *repository.RecipientSessionRepository
	dedupRepo            *repository.InboundDedupRepository
	s3Client             *storage.S3Client
	publisher            *queue.JetStreamPublisher
	auditWriter          audit.Writer
	client               *http.Client
	telegramBaseURL      string
}

// NewTelegramWebhookHandler creates a new TelegramWebhookHandler.
func NewTelegramWebhookHandler(
	pool *repository.WorkspaceRepository,
	credsRepo *repository.CredentialsRepository,
	recipientSessionRepo *repository.RecipientSessionRepository,
	dedupRepo *repository.InboundDedupRepository,
	s3Client *storage.S3Client,
	publisher *queue.JetStreamPublisher,
	auditWriter audit.Writer,
) *TelegramWebhookHandler {
	return &TelegramWebhookHandler{
		pool:                 pool,
		credsRepo:            credsRepo,
		recipientSessionRepo: recipientSessionRepo,
		dedupRepo:            dedupRepo,
		s3Client:             s3Client,
		publisher:            publisher,
		auditWriter:          auditWriter,
		client:               &http.Client{Timeout: 30 * time.Second},
		telegramBaseURL:      "https://api.telegram.org",
	}
}

// SetBaseURL overrides the base Telegram API URL (useful for testing).
func (h *TelegramWebhookHandler) SetBaseURL(url string) {
	h.telegramBaseURL = url
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64             `json:"message_id"`
	Chat      *telegramChat     `json:"chat"`
	Text      string            `json:"text,omitempty"`
	Caption   string            `json:"caption,omitempty"`
	Photo     []telegramPhoto   `json:"photo,omitempty"`
	Document  *telegramDocument `json:"document,omitempty"`
	Audio     *telegramAudio    `json:"audio,omitempty"`
	Video     *telegramVideo    `json:"video,omitempty"`
	Location  *telegramLocation `json:"location,omitempty"`
	Contact   *telegramContact  `json:"contact,omitempty"`
}

type telegramPhoto struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
}

type telegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
}

type telegramAudio struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
}

type telegramVideo struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
}

type telegramLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type telegramContact struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramConfig struct {
	Token       string `json:"token"`
	SecretToken string `json:"secret_token"`
	BotUsername string `json:"bot_username"`
}

// Handle processes the incoming Telegram webhook POST request.
func (h *TelegramWebhookHandler) Handle(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Retrieve secret token from headers
	receivedToken := c.Request().Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if receivedToken == "" {
		return c.NoContent(http.StatusForbidden)
	}

	// Load registered credentials for the workspace
	credsBytes, err := h.credsRepo.Get(c.Request().Context(), workspaceID, "telegram")
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var config telegramConfig
	if err := json.Unmarshal(credsBytes, &config); err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	// Validate secret token
	if config.SecretToken == "" || receivedToken != config.SecretToken {
		return c.NoContent(http.StatusForbidden)
	}

	// Bind request body
	var update telegramUpdate
	if err := c.Bind(&update); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	ctx := c.Request().Context()

	if update.Message != nil && update.Message.Chat != nil {
		chatIDStr := strconv.FormatInt(update.Message.Chat.ID, 10)

		// Deduplicate using update_id
		if h.dedupRepo != nil {
			unique, err := h.dedupRepo.InsertAndCheck(ctx, workspaceID, "telegram", strconv.FormatInt(update.UpdateID, 10))
			if err != nil {
				slog.Error("tg webhook: dedup check failed", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}
			if !unique {
				slog.Info("tg webhook: duplicate update ignored", "update_id", update.UpdateID)
				return c.NoContent(http.StatusOK)
			}
		}

		botUsername := config.BotUsername
		if botUsername == "" {
			botUsername = "@bot"
		}

		// Upsert recipient session
		err = h.recipientSessionRepo.Upsert(ctx, workspaceID, chatIDStr, "telegram", botUsername, time.Now().UTC())
		if err != nil {
			return err
		}

		ws, err := h.pool.GetByID(ctx, workspaceID)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		// Map to InboundEventPayload
		traceID := uuid.New().String()
		event := InboundEventPayload{
			Event:       "inbound_message",
			TraceID:     traceID,
			MessageID:   strconv.FormatInt(update.Message.MessageID, 10),
			Channel:     "telegram",
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			WorkspaceID: workspaceID.String(),
			From:        chatIDStr,
			To:          botUsername,
		}

		if update.Message.Text != "" {
			event.Body = update.Message.Text
		}

		// Parse media
		var fileID string
		var mediaType string
		var mimeType string
		var filename string

		if len(update.Message.Photo) > 0 {
			// Get highest resolution photo
			best := update.Message.Photo[len(update.Message.Photo)-1]
			fileID = best.FileID
			mediaType = "image"
			mimeType = "image/jpeg"
		} else if update.Message.Document != nil {
			fileID = update.Message.Document.FileID
			mediaType = "document"
			mimeType = update.Message.Document.MimeType
			filename = update.Message.Document.FileName
		} else if update.Message.Audio != nil {
			fileID = update.Message.Audio.FileID
			mediaType = "audio"
			mimeType = update.Message.Audio.MimeType
		} else if update.Message.Video != nil {
			fileID = update.Message.Video.FileID
			mediaType = "video"
			mimeType = update.Message.Video.MimeType
		}

		if fileID != "" && h.s3Client != nil && config.Token != "" {
			mediaBytes, err := h.downloadTelegramFile(ctx, fileID, config.Token)
			if err == nil {
				ext := "bin"
				if mediaType == "image" {
					ext = "jpg"
				} else if mimeType != "" {
					parts := strings.Split(mimeType, "/")
					if len(parts) == 2 {
						ext = parts[1]
					}
				}
				hashKey := sha256Hash(mediaBytes)
				s3Key := fmt.Sprintf("%s/%s.%s", workspaceID.String(), hashKey, ext)
				err = h.s3Client.Upload(ctx, s3Key, mediaBytes, mimeType)
				if err == nil {
					event.Media = &InboundMedia{
						MediaURL:  fmt.Sprintf("/media/%s/%s.%s", workspaceID.String(), hashKey, ext),
						MediaType: mediaType,
						Filename:  filename,
						Caption:   update.Message.Caption,
					}
				}
			}
		}

		// PII opt-in check
		if ws.PIIOptIn {
			if update.Message.Location != nil {
				event.Location = &InboundLocation{
					Latitude:  update.Message.Location.Latitude,
					Longitude: update.Message.Location.Longitude,
				}
			}
			if update.Message.Contact != nil {
				fullName := update.Message.Contact.FirstName
				if update.Message.Contact.LastName != "" {
					fullName += " " + update.Message.Contact.LastName
				}
				event.Contacts = []InboundContact{
					{
						Name:  fullName,
						Phone: update.Message.Contact.PhoneNumber,
					},
				}
			}
		}

		// Silent drop if truly empty
		if event.Body == "" && event.Media == nil && event.Location == nil && len(event.Contacts) == 0 {
			return c.NoContent(http.StatusOK)
		}

		// Publish to NATS
		if h.publisher != nil {
			eventData, _ := json.Marshal(event)
			subject := fmt.Sprintf("inbound.events.%s", workspaceID.String())
			_ = h.publisher.Publish(ctx, subject, eventData, traceID)

			if h.auditWriter != nil {
				_ = h.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "inbound_message", eventData))
			}
		}
	}

	return c.NoContent(http.StatusOK)
}

func (h *TelegramWebhookHandler) downloadTelegramFile(ctx context.Context, fileID, token string) ([]byte, error) {
	// getFile info
	reqURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", h.telegramBaseURL, token, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getFile error status: %d", resp.StatusCode)
	}

	var fileInfo struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&fileInfo); err != nil || !fileInfo.OK {
		return nil, fmt.Errorf("failed to decode getFile: %w", err)
	}

	// Download file
	downloadURL := fmt.Sprintf("%s/file/bot%s/%s", h.telegramBaseURL, token, fileInfo.Result.FilePath)
	reqDl, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}

	respDl, err := h.client.Do(reqDl)
	if err != nil {
		return nil, err
	}
	defer respDl.Body.Close()

	if respDl.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file download status: %d", respDl.StatusCode)
	}

	limitReader := io.LimitReader(respDl.Body, 25*1024*1024+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}

	if len(data) > 25*1024*1024 {
		return nil, errors.New("media_size_exceeded")
	}

	return data, nil
}

func sha256Hash(data []byte) string {
	hasher := sha256.New()
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil))
}
