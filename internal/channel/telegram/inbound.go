package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
)

// TelegramInboundAdapter implements channel.InboundAdapter for Telegram.
type TelegramInboundAdapter struct {
	downloader          media.Downloader
	client              *http.Client
	telegramBaseURL     string
}

// NewTelegramInboundAdapter creates a new TelegramInboundAdapter.
func NewTelegramInboundAdapter(
	downloader media.Downloader,
) *TelegramInboundAdapter {
	return &TelegramInboundAdapter{
		downloader:          downloader,
		client:              &http.Client{Timeout: 30 * time.Second},
		telegramBaseURL:     "https://api.telegram.org",
	}
}

// SetBaseURL overrides the base Telegram API URL (useful for testing).
func (a *TelegramInboundAdapter) SetBaseURL(url string) {
	a.telegramBaseURL = url
}

// Private json structures mapping to Telegram webhook updates
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

// Parse decodes the Telegram payload, validates the token, maps contacts, downloads media, and returns an InboundEvent.
func (a *TelegramInboundAdapter) Parse(
	ctx context.Context,
	payload []byte,
	headers map[string]string,
	conn *repository.Connection,
) ([]*inbound.InboundEvent, error) {
	// 1. Retrieve secret token from headers
	receivedToken := headers["X-Telegram-Bot-Api-Secret-Token"]
	if receivedToken == "" {
		return nil, fmt.Errorf("missing secret token header")
	}

	// 2. Unmarshal connection credentials
	var config telegramConfig
	if err := json.Unmarshal(conn.Credentials, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal connection credentials: %w", err)
	}

	// 3. Verify secret token
	if config.SecretToken != receivedToken {
		return nil, fmt.Errorf("secret token mismatch")
	}

	// 4. Unmarshal payload
	var update telegramUpdate
	if err := json.Unmarshal(payload, &update); err != nil {
		return nil, fmt.Errorf("failed to parse telegram update: %w", err)
	}

	if update.Message == nil || update.Message.Chat == nil {
		return nil, nil // Not a message event we want to ingest (could be edit, inline query, status update)
	}

	chatIDStr := strconv.FormatInt(update.Message.Chat.ID, 10)
	botUsername := config.BotUsername
	if botUsername == "" {
		botUsername = "@bot"
	}

	// 5. Extract sender's details and metadata
	var username string
	var firstName string
	var lastName string
	var phone string

	if update.Message.From != nil {
		username = update.Message.From.Username
		firstName = update.Message.From.FirstName
		lastName = update.Message.From.LastName
	}

	if username == "" && update.Message.Chat.Username != "" {
		username = update.Message.Chat.Username
	}
	if firstName == "" && update.Message.Chat.FirstName != "" {
		firstName = update.Message.Chat.FirstName
	}
	if lastName == "" && update.Message.Chat.LastName != "" {
		lastName = update.Message.Chat.LastName
	}

	if update.Message.Contact != nil && update.Message.Contact.PhoneNumber != "" {
		phone = update.Message.Contact.PhoneNumber
	}

	var nameParts []string
	if firstName != "" {
		nameParts = append(nameParts, firstName)
	}
	if lastName != "" {
		nameParts = append(nameParts, lastName)
	}
	senderName := strings.TrimSpace(strings.Join(nameParts, " "))
	if senderName == "" {
		senderName = username
	}
	if senderName == "" {
		senderName = chatIDStr
	}

	metadata := make(map[string]string)
	if username != "" {
		metadata["username"] = username
	}
	if phone != "" {
		metadata["phone_number"] = phone
	}

	// 6. Media parsing and download
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
	if fileID != "" && config.Token != "" {
		mediaBytes, err := a.downloadTelegramFile(ctx, fileID, config.Token)
		if err == nil {
			inboundMedia = &inbound.InboundMedia{
				Bytes:     mediaBytes,
				MediaType: mediaType,
				Filename:  filename,
				Caption:   update.Message.Caption,
			}
		} else {
			slog.Error("tg inbound: failed to download media file", "error", err, "file_id", fileID)
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

	return []*inbound.InboundEvent{
		{
			WorkspaceID:  conn.WorkspaceID,
			ConnectionID: conn.ID,
			MessageID:    strconv.FormatInt(update.UpdateID, 10),
			Channel:      "telegram",
			From:         chatIDStr,
			To:           botUsername,
			Body:         update.Message.Text,
			Media:        inboundMedia,
			Location:     inboundLocation,
			Contacts:     inboundContacts,
			SenderName:   senderName,
			Metadata:     metadata,
		},
	}, nil
}

func (a *TelegramInboundAdapter) downloadTelegramFile(ctx context.Context, fileID, token string) ([]byte, error) {
	reqURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", a.telegramBaseURL, token, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
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

	downloadURL := fmt.Sprintf("%s/file/bot%s/%s", a.telegramBaseURL, token, fileInfo.Result.FilePath)
	
	if a.downloader == nil {
		return nil, fmt.Errorf("downloader is not configured")
	}

	res, err := a.downloader.Download(ctx, downloadURL, nil, 25*1024*1024)
	if err != nil {
		return nil, err
	}

	return res.Bytes, nil
}
