package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABAWebhookHandler handles verification and inbound payloads for Meta's WhatsApp Cloud API (WABA).
type WABAWebhookHandler struct {
	connectionsRepo  *repository.ConnectionRepository
	inboundProcessor *inbound.InboundProcessor
	client           *http.Client
	graphBaseURL     string
}

func NewWABAWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	inboundProcessor *inbound.InboundProcessor,
) *WABAWebhookHandler {
	return &WABAWebhookHandler{
		connectionsRepo:  connectionsRepo,
		inboundProcessor: inboundProcessor,
		client:           &http.Client{Timeout: 30 * time.Second},
		graphBaseURL:     "https://graph.facebook.com/v20.0",
	}
}

type wabaVerifyCreds struct {
	VerifyToken string `json:"verify_token"`
	Token       string `json:"token"`
}

// HandleGet verification from Meta
func (h *WABAWebhookHandler) HandleGet(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	verifyToken := c.Request().URL.Query().Get("hub.verify_token")
	challenge := c.Request().URL.Query().Get("hub.challenge")

	expectedVerifyToken := "pergo_verify_token_" + workspaceIDStr

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchFound bool
	for _, conn := range conns {
		if conn.Channel != "whatsapp_cloud" {
			continue
		}

		var creds wabaVerifyCreds
		if err := json.Unmarshal(conn.Credentials, &creds); err == nil {
			if verifyToken != "" && (verifyToken == creds.VerifyToken || verifyToken == expectedVerifyToken) {
				matchFound = true
				break
			}
		}
	}

	if !matchFound {
		return c.NoContent(http.StatusForbidden)
	}

	return c.String(http.StatusOK, challenge)
}

type wabaWebhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field ValueStruct `json:"field"`
			Value ValueData   `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

type ValueStruct string

type ValueData struct {
	MessagingProduct string `json:"messaging_product"`
	Metadata         struct {
		DisplayPhoneNumber string `json:"display_phone_number"`
		PhoneNumberID      string `json:"phone_number_id"`
	} `json:"metadata"`
	Contacts []struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		WaID string `json:"wa_id"`
	} `json:"contacts"`
	Messages []struct {
		From      string `json:"from"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Text      *struct {
			Body string `json:"body"`
		} `json:"text,omitempty"`
		Image    *wabaMediaObj `json:"image,omitempty"`
		Document *wabaMediaObj `json:"document,omitempty"`
		Audio    *wabaMediaObj `json:"audio,omitempty"`
		Video    *wabaMediaObj `json:"video,omitempty"`
		Location *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Address   string  `json:"address"`
		} `json:"location,omitempty"`
		Contacts []struct {
			Name struct {
				FormattedName string `json:"formatted_name"`
			} `json:"name"`
			Phones []struct {
				Phone string `json:"phone"`
			} `json:"phones"`
		} `json:"contacts,omitempty"`
	} `json:"messages,omitempty"`
	Statuses []any `json:"statuses,omitempty"`
}

type wabaMediaObj struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	Sha256   string `json:"sha256"`
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// HandlePost ingests inbound messages from Meta
func (h *WABAWebhookHandler) HandlePost(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	var payload wabaWebhookPayload
	if err := c.Bind(&payload); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel == "whatsapp_cloud" {
			matchingConn = conn
			break
		}
	}

	if matchingConn == nil {
		slog.Warn("waba webhook: no connection found", "workspace_id", workspaceID)
		return c.NoContent(http.StatusForbidden)
	}

	var creds wabaVerifyCreds
	_ = json.Unmarshal(matchingConn.Credentials, &creds)

	ctx := c.Request().Context()

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Statuses) > 0 {
				continue
			}

			for _, msg := range change.Value.Messages {
				recipientIdentity := change.Value.Metadata.DisplayPhoneNumber
				if recipientIdentity == "" {
					recipientIdentity = change.Value.Metadata.PhoneNumberID
				}

				// Resolve media type and media payload
				var mediaObj *wabaMediaObj
				var mediaType string
				switch msg.Type {
				case "image":
					mediaObj = msg.Image
					mediaType = "image"
				case "document":
					mediaObj = msg.Document
					mediaType = "document"
				case "audio":
					mediaObj = msg.Audio
					mediaType = "audio"
				case "video":
					mediaObj = msg.Video
					mediaType = "video"
				}

				var inboundMedia *inbound.InboundMedia
				if mediaObj != nil && creds.Token != "" {
					dlURL, err := h.fetchWABADownloadURL(ctx, mediaObj.ID, creds.Token)
					if err == nil {
						mediaBytes, err := h.downloadWABAFile(ctx, dlURL, creds.Token)
						if err == nil {
							inboundMedia = &inbound.InboundMedia{
								Bytes:     mediaBytes,
								MediaType: mediaType,
								Filename:  mediaObj.Filename,
								Caption:   mediaObj.Caption,
							}
						} else {
							slog.Error("waba webhook: failed to download media file", "error", err, "media_id", mediaObj.ID)
						}
					} else {
						slog.Error("waba webhook: failed to fetch media URL from Meta", "error", err, "media_id", mediaObj.ID)
					}
				}

				var inboundLocation *inbound.InboundLocation
				if msg.Location != nil {
					inboundLocation = &inbound.InboundLocation{
						Latitude:  msg.Location.Latitude,
						Longitude: msg.Location.Longitude,
						Name:      msg.Location.Name,
						Address:   msg.Location.Address,
					}
				}

				var inboundContacts []inbound.InboundContact
				if len(msg.Contacts) > 0 {
					for _, contact := range msg.Contacts {
						for _, phone := range contact.Phones {
							inboundContacts = append(inboundContacts, inbound.InboundContact{
								Name:  contact.Name.FormattedName,
								Phone: phone.Phone,
							})
						}
					}
				}

				var body string
				if msg.Text != nil {
					body = msg.Text.Body
				}

				event := &inbound.InboundEvent{
					WorkspaceID: workspaceID,
					MessageID:   msg.ID, // Used for dedup
					Channel:     "whatsapp_cloud",
					From:        msg.From,
					To:          recipientIdentity,
					Body:        body,
					Media:       inboundMedia,
					Location:    inboundLocation,
					Contacts:    inboundContacts,
				}

				// Delegate ingestion to consolidated processor
				if h.inboundProcessor != nil {
					err := h.inboundProcessor.Process(ctx, event)
					if err != nil {
						slog.Error("waba webhook: inbound processor failed", "error", err, "message_id", msg.ID)
						return c.NoContent(http.StatusInternalServerError)
					}
				}
			}
		}
	}

	return c.NoContent(http.StatusOK)
}

func (h *WABAWebhookHandler) fetchWABADownloadURL(ctx context.Context, mediaID, token string) (string, error) {
	reqURL := fmt.Sprintf("%s/%s", h.graphBaseURL, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status: %d", resp.StatusCode)
	}

	var res struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.URL, nil
}

func (h *WABAWebhookHandler) downloadWABAFile(ctx context.Context, downloadURL, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status: %d", resp.StatusCode)
	}

	limitReader := io.LimitReader(resp.Body, 25*1024*1024+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}

	if len(data) > 25*1024*1024 {
		return nil, errors.New("media_size_exceeded")
	}

	return data, nil
}
