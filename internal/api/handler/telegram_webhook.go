package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// TelegramWebhookHandler handles inbound webhooks from Telegram.
type TelegramWebhookHandler struct {
	connectionsRepo     *repository.ConnectionRepository
	telegramContactRepo *repository.TelegramContactRepository
	inboundProcessor    *inbound.InboundProcessor
	client              *http.Client
	telegramBaseURL     string
}

// NewTelegramWebhookHandler creates a new TelegramWebhookHandler.
func NewTelegramWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	telegramContactRepo *repository.TelegramContactRepository,
	inboundProcessor *inbound.InboundProcessor,
) *TelegramWebhookHandler {
	return &TelegramWebhookHandler{
		connectionsRepo:     connectionsRepo,
		telegramContactRepo: telegramContactRepo,
		inboundProcessor:    inboundProcessor,
		client:              &http.Client{Timeout: 30 * time.Second},
		telegramBaseURL:     "https://api.telegram.org",
	}
}

// SetBaseURL overrides the base Telegram API URL (useful for testing).
func (h *TelegramWebhookHandler) SetBaseURL(url string) {
	h.telegramBaseURL = url
}

type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64             `json:"message_id"`
	From      *telegramUser     `json:"from,omitempty"`
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
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
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

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchingConn *repository.Connection
	var botUsername string
	var token string

	for _, conn := range conns {
		if conn.Channel != "telegram" {
			continue
		}

		var config telegramConfig
		if err := json.Unmarshal(conn.Credentials, &config); err == nil {
			if config.SecretToken == receivedToken {
				matchingConn = conn
				botUsername = config.BotUsername
				token = config.Token
				break
			}
		}
	}

	if matchingConn == nil {
		slog.Warn("tg webhook: no matching connection found for secret token", "workspace_id", workspaceID)
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

		if botUsername == "" {
			botUsername = "@bot"
		}

		// Upsert Telegram contact mapping (resolves username/phone -> chat_id)
		if h.telegramContactRepo != nil {
			var usernamePtr *string
			var firstNamePtr *string
			var lastNamePtr *string
			var phonePtr *string

			if update.Message.From != nil {
				if update.Message.From.Username != "" {
					u := update.Message.From.Username
					usernamePtr = &u
				}
				if update.Message.From.FirstName != "" {
					f := update.Message.From.FirstName
					firstNamePtr = &f
				}
				if update.Message.From.LastName != "" {
					l := update.Message.From.LastName
					lastNamePtr = &l
				}
			}

			// Fallback to Chat metadata if From is missing
			if usernamePtr == nil && update.Message.Chat.Username != "" {
				u := update.Message.Chat.Username
				usernamePtr = &u
			}
			if firstNamePtr == nil && update.Message.Chat.FirstName != "" {
				f := update.Message.Chat.FirstName
				firstNamePtr = &f
			}
			if lastNamePtr == nil && update.Message.Chat.LastName != "" {
				l := update.Message.Chat.LastName
				lastNamePtr = &l
			}

			if update.Message.Contact != nil && update.Message.Contact.PhoneNumber != "" {
				p := update.Message.Contact.PhoneNumber
				phonePtr = &p
			}

			_ = h.telegramContactRepo.Upsert(ctx, workspaceID, chatIDStr, usernamePtr, phonePtr, firstNamePtr, lastNamePtr)
		}

		// Parse media type and file ID
		var fileID string
		var mediaType string
		var filename string

		if len(update.Message.Photo) > 0 {
			best := update.Message.Photo[len(update.Message.Photo)-1]
			fileID = best.FileID
			mediaType = "image"
		} else if update.Message.Document != nil {
			fileID = update.Message.Document.FileID
			mediaType = "document"
			filename = update.Message.Document.FileName
		} else if update.Message.Audio != nil {
			fileID = update.Message.Audio.FileID
			mediaType = "audio"
		} else if update.Message.Video != nil {
			fileID = update.Message.Video.FileID
			mediaType = "video"
		}

		var inboundMedia *inbound.InboundMedia
		if fileID != "" && token != "" {
			mediaBytes, err := h.downloadTelegramFile(ctx, fileID, token)
			if err == nil {
				inboundMedia = &inbound.InboundMedia{
					Bytes:     mediaBytes,
					MediaType: mediaType,
					Filename:  filename,
					Caption:   update.Message.Caption,
				}
			} else {
				slog.Error("tg webhook: failed to download media file", "error", err, "file_id", fileID)
			}
		}

		var inboundLocation *inbound.InboundLocation
		if update.Message.Location != nil {
			inboundLocation = &inbound.InboundLocation{
				Latitude:  update.Message.Location.Latitude,
				Longitude: update.Message.Location.Longitude,
			}
		}

		var inboundContacts []inbound.InboundContact
		if update.Message.Contact != nil {
			fullName := update.Message.Contact.FirstName
			if update.Message.Contact.LastName != "" {
				fullName += " " + update.Message.Contact.LastName
			}
			inboundContacts = append(inboundContacts, inbound.InboundContact{
				Name:  fullName,
				Phone: update.Message.Contact.PhoneNumber,
			})
		}

		event := &inbound.InboundEvent{
			WorkspaceID: workspaceID,
			MessageID:   strconv.FormatInt(update.UpdateID, 10), // Use update_id for dedup
			Channel:     "telegram",
			From:        chatIDStr,
			To:          botUsername,
			Body:        update.Message.Text,
			Media:       inboundMedia,
			Location:    inboundLocation,
			Contacts:    inboundContacts,
		}

		// Delegate ingestion to consolidated processor
		if h.inboundProcessor != nil {
			err = h.inboundProcessor.Process(ctx, event)
			if err != nil {
				slog.Error("tg webhook: inbound processor failed", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}

func (h *TelegramWebhookHandler) downloadTelegramFile(ctx context.Context, fileID, token string) ([]byte, error) {
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
