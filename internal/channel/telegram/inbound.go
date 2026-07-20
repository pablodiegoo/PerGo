package telegram

import (
	"bytes"
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

type telegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from"`
	Message *telegramMessage `json:"message,omitempty"`
	Data    string           `json:"data"`
}

type telegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message,omitempty"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
}

type telegramMessage struct {
	MessageID       int64             `json:"message_id"`
	MessageThreadID int64             `json:"message_thread_id,omitempty"`
	From            *telegramUser     `json:"from,omitempty"`
	Chat            *telegramChat     `json:"chat"`
	Text            string            `json:"text,omitempty"`
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

	var chat *telegramChat
	var fromUser *telegramUser
	var messageThreadIDInt int64
	var text string
	var isCallback bool
	var callbackData string
	var callbackID string
	var caption string
	var photo []telegramPhoto
	var document *telegramDocument
	var audio *telegramAudio
	var video *telegramVideo
	var location *telegramLocation
	var contact *telegramContact

	if update.CallbackQuery != nil {
		isCallback = true
		callbackData = update.CallbackQuery.Data
		callbackID = update.CallbackQuery.ID
		fromUser = update.CallbackQuery.From
		if update.CallbackQuery.Message != nil {
			chat = update.CallbackQuery.Message.Chat
			messageThreadIDInt = update.CallbackQuery.Message.MessageThreadID
		}

		// Acknowledge the callback query in background
		go func(cbID, token string) {
			ackURL := fmt.Sprintf("%s/bot%s/answerCallbackQuery", a.telegramBaseURL, token)
			reqBody, _ := json.Marshal(map[string]string{"callback_query_id": cbID})
			req, err := http.NewRequest(http.MethodPost, ackURL, bytes.NewReader(reqBody))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				resp, err := a.client.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}
		}(callbackID, config.Token)

	} else if update.Message != nil {
		chat = update.Message.Chat
		fromUser = update.Message.From
		messageThreadIDInt = update.Message.MessageThreadID
		text = update.Message.Text
		caption = update.Message.Caption
		photo = update.Message.Photo
		document = update.Message.Document
		audio = update.Message.Audio
		video = update.Message.Video
		location = update.Message.Location
		contact = update.Message.Contact
	}

	if chat == nil {
		return nil, nil // Not a message event we want to ingest
	}

	chatIDStr := strconv.FormatInt(chat.ID, 10)
	botUsername := config.BotUsername
	if botUsername == "" {
		botUsername = "@bot"
	}

	// 5. Extract sender's details and metadata
	var username string
	var firstName string
	var lastName string
	var phone string

	if fromUser != nil {
		username = fromUser.Username
		firstName = fromUser.FirstName
		lastName = fromUser.LastName
	}

	if username == "" && chat.Username != "" {
		username = chat.Username
	}
	if firstName == "" && chat.FirstName != "" {
		firstName = chat.FirstName
	}
	if lastName == "" && chat.LastName != "" {
		lastName = chat.LastName
	}

	if contact != nil && contact.PhoneNumber != "" {
		phone = contact.PhoneNumber
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
	if messageThreadIDInt != 0 {
		metadata["thread_id"] = strconv.FormatInt(messageThreadIDInt, 10)
	}

	// 6. Media parsing and download
	var fileID string
	var mediaType string
	var filename string

	if len(photo) > 0 {
		best := photo[len(photo)-1]
		fileID = best.FileID
		mediaType = "image"
	} else if document != nil {
		fileID = document.FileID
		mediaType = "document"
		filename = document.FileName
	} else if audio != nil {
		fileID = audio.FileID
		mediaType = "audio"
	} else if video != nil {
		fileID = video.FileID
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
				Caption:   caption,
			}
		} else {
			slog.Error("tg inbound: failed to download media file", "error", err, "file_id", fileID)
		}
	}

	var inboundLocation *inbound.InboundLocation
	if location != nil {
		inboundLocation = &inbound.InboundLocation{
			Latitude:  location.Latitude,
			Longitude: location.Longitude,
		}
	}

	var inboundContacts []inbound.InboundContact
	if contact != nil {
		fullName := contact.FirstName
		if contact.LastName != "" {
			fullName += " " + contact.LastName
		}
		inboundContacts = append(inboundContacts, inbound.InboundContact{
			Name:  fullName,
			Phone: contact.PhoneNumber,
		})
	}

	var interactive *inbound.InboundInteractive
	if isCallback {
		interactive = &inbound.InboundInteractive{
			Type: "button_reply",
			ButtonReply: &inbound.InboundButtonReply{
				ID: callbackData,
				// Telegram doesn't send the button title in the callback query, only the data.
				// We populate Title with the same data for now.
				Title: callbackData,
			},
		}
	}

	return []*inbound.InboundEvent{
		{
			WorkspaceID:  conn.WorkspaceID,
			ConnectionID: conn.ID,
			MessageID:    strconv.FormatInt(update.UpdateID, 10),
			Channel:      "telegram",
			From:         chatIDStr,
			To:           botUsername,
			Body:         text,
			Media:        inboundMedia,
			Location:     inboundLocation,
			Contacts:     inboundContacts,
			Interactive:  interactive,
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
