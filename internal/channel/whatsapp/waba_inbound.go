package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABAInboundAdapter implements channel.InboundAdapter for Meta's WhatsApp Cloud API (WABA).
type WABAInboundAdapter struct {
	client       *http.Client
	graphBaseURL string
}

// NewWABAInboundAdapter creates a new WABAInboundAdapter.
func NewWABAInboundAdapter() *WABAInboundAdapter {
	return &WABAInboundAdapter{
		client:       &http.Client{Timeout: 30 * time.Second},
		graphBaseURL: "https://graph.facebook.com/v20.0",
	}
}

// SetBaseURL overrides the base Meta Graph API URL (useful for testing).
func (a *WABAInboundAdapter) SetBaseURL(url string) {
	a.graphBaseURL = url
}

type wabaVerifyCreds struct {
	VerifyToken string `json:"verify_token"`
	Token       string `json:"token"`
}

type wabaWebhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string    `json:"field"`
			Value ValueData `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

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

// Parse decodes the WABA payload, validates credentials, downloads media, and returns unified events.
func (a *WABAInboundAdapter) Parse(
	ctx context.Context,
	payload []byte,
	headers map[string]string,
	conn *repository.Connection,
) ([]*inbound.InboundEvent, error) {
	var wabaPayload wabaWebhookPayload
	if err := json.Unmarshal(payload, &wabaPayload); err != nil {
		return nil, fmt.Errorf("failed to parse WABA payload: %w", err)
	}

	var creds wabaVerifyCreds
	if err := json.Unmarshal(conn.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	var events []*inbound.InboundEvent

	for _, entry := range wabaPayload.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Statuses) > 0 {
				continue // Skip status updates (sent, delivered, read receipts)
			}

			for _, msg := range change.Value.Messages {
				recipientIdentity := change.Value.Metadata.DisplayPhoneNumber
				if recipientIdentity == "" {
					recipientIdentity = change.Value.Metadata.PhoneNumberID
				}

				// Resolve media details
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
					dlURL, err := a.fetchWABADownloadURL(ctx, mediaObj.ID, creds.Token)
					if err == nil {
						mediaBytes, err := a.downloadWABAFile(ctx, dlURL, creds.Token)
						if err == nil {
							inboundMedia = &inbound.InboundMedia{
								Bytes:     mediaBytes,
								MediaType: mediaType,
								Filename:  mediaObj.Filename,
								Caption:   mediaObj.Caption,
							}
						} else {
							slog.Error("waba inbound: failed to download media file", "error", err, "media_id", mediaObj.ID)
						}
					} else {
						slog.Error("waba inbound: failed to fetch media URL from Meta", "error", err, "media_id", mediaObj.ID)
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

				events = append(events, &inbound.InboundEvent{
					WorkspaceID: conn.WorkspaceID,
					MessageID:   msg.ID,
					Channel:     "whatsapp_cloud",
					From:        msg.From,
					To:          recipientIdentity,
					Body:        body,
					Media:       inboundMedia,
					Location:    inboundLocation,
					Contacts:    inboundContacts,
				})
			}
		}
	}

	return events, nil
}

func (a *WABAInboundAdapter) fetchWABADownloadURL(ctx context.Context, mediaID, token string) (string, error) {
	reqURL := fmt.Sprintf("%s/%s", a.graphBaseURL, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
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

func (a *WABAInboundAdapter) downloadWABAFile(ctx context.Context, downloadURL, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
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
