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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// InboundEventPayload is the standard format for inbound messages forwarded to consumers.
type InboundEventPayload struct {
	Event       string           `json:"event"`
	TraceID     string           `json:"trace_id"`
	MessageID   string           `json:"message_id"`
	Channel     string           `json:"channel"`
	Timestamp   string           `json:"timestamp"`
	WorkspaceID string           `json:"workspace_id"`
	From        string           `json:"from"`
	Body        string           `json:"body,omitempty"`
	Media       *InboundMedia    `json:"media,omitempty"`
	Location    *InboundLocation `json:"location,omitempty"`
	Contacts    []InboundContact `json:"contacts,omitempty"`
}

type InboundMedia struct {
	MediaURL  string `json:"media_url"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
}

type InboundLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

type InboundContact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

// WABAWebhookHandler handles verification and inbound payloads for Meta's WhatsApp Cloud API (WABA).
type WABAWebhookHandler struct {
	pool          *repository.WorkspaceRepository
	credsRepo     *repository.CredentialsRepository
	sessRepo      *repository.RecipientSessionRepository
	dedupRepo     *repository.InboundDedupRepository
	s3Client      *storage.S3Client
	publisher     *queue.JetStreamPublisher
	auditWriter   audit.Writer
	client        *http.Client
	graphBaseURL  string
}

func NewWABAWebhookHandler(
	pool *repository.WorkspaceRepository,
	credsRepo *repository.CredentialsRepository,
	sessRepo *repository.RecipientSessionRepository,
	dedupRepo *repository.InboundDedupRepository,
	s3Client *storage.S3Client,
	publisher *queue.JetStreamPublisher,
	auditWriter audit.Writer,
) *WABAWebhookHandler {
	return &WABAWebhookHandler{
		pool:         pool,
		credsRepo:    credsRepo,
		sessRepo:     sessRepo,
		dedupRepo:    dedupRepo,
		s3Client:     s3Client,
		publisher:    publisher,
		auditWriter:  auditWriter,
		client:       &http.Client{Timeout: 30 * time.Second},
		graphBaseURL: "https://graph.facebook.com/v20.0",
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

	credsBytes, err := h.credsRepo.Get(c.Request().Context(), workspaceID, "whatsapp_cloud")
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var creds wabaVerifyCreds
	if err := json.Unmarshal(credsBytes, &creds); err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	verifyToken := c.Request().URL.Query().Get("hub.verify_token")
	challenge := c.Request().URL.Query().Get("hub.challenge")

	expectedVerifyToken := "pergo_verify_token_" + workspaceIDStr
	if verifyToken == "" || challenge == "" || (verifyToken != creds.VerifyToken && verifyToken != expectedVerifyToken) {
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

	credsBytes, err := h.credsRepo.Get(c.Request().Context(), workspaceID, "whatsapp_cloud")
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}
	var creds wabaVerifyCreds
	_ = json.Unmarshal(credsBytes, &creds)

	ws, err := h.pool.GetByID(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	ctx := c.Request().Context()

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			// If it's a status message update, ignore it
			if len(change.Value.Statuses) > 0 {
				continue
			}

			for _, msg := range change.Value.Messages {
				// Deduplicate using provider message ID
				unique, err := h.dedupRepo.InsertAndCheck(ctx, workspaceID, "whatsapp_cloud", msg.ID)
				if err != nil {
					slog.Error("waba webhook: dedup check failed", "error", err)
					return c.NoContent(http.StatusInternalServerError)
				}
				if !unique {
					slog.Info("waba webhook: duplicate message ignored", "message_id", msg.ID)
					continue
				}

				// Upsert recipient session for window tracking
				_ = h.sessRepo.Upsert(ctx, workspaceID, msg.From, "whatsapp_cloud", time.Now().UTC())

				// Extract contents
				traceID := uuid.New().String()
				event := InboundEventPayload{
					Event:       "inbound_message",
					TraceID:     traceID,
					MessageID:   msg.ID,
					Channel:     "whatsapp_cloud",
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					WorkspaceID: workspaceID.String(),
					From:        msg.From,
				}

				if msg.Text != nil {
					event.Body = msg.Text.Body
				}

				// Handle Media
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

				if mediaObj != nil && h.s3Client != nil && creds.Token != "" {
					dlURL, err := h.fetchWABADownloadURL(ctx, mediaObj.ID, creds.Token)
					if err == nil {
						mediaBytes, err := h.downloadWABAFile(ctx, dlURL, creds.Token)
						if err == nil {
							ext := h.getExtFromMime(mediaObj.MimeType)
							hashKey := contentHash(mediaBytes)
							s3Key := fmt.Sprintf("%s/%s.%s", workspaceID.String(), hashKey, ext)
							err = h.s3Client.Upload(ctx, s3Key, mediaBytes, mediaObj.MimeType)
							if err == nil {
								proxyURL := fmt.Sprintf("/media/%s/%s.%s", workspaceID.String(), hashKey, ext)
								event.Media = &InboundMedia{
									MediaURL:  proxyURL,
									MediaType: mediaType,
									Filename:  mediaObj.Filename,
									Caption:   mediaObj.Caption,
								}
							}
						}
					}
				}

				// Location & Contacts PII check
				if ws.PIIOptIn {
					if msg.Location != nil {
						event.Location = &InboundLocation{
							Latitude:  msg.Location.Latitude,
							Longitude: msg.Location.Longitude,
							Name:      msg.Location.Name,
							Address:   msg.Location.Address,
						}
					}
					if len(msg.Contacts) > 0 {
						for _, contact := range msg.Contacts {
							for _, phone := range contact.Phones {
								event.Contacts = append(event.Contacts, InboundContact{
									Name:  contact.Name.FormattedName,
									Phone: phone.Phone,
								})
							}
						}
					}
				}

				// Truly empty check
				if event.Body == "" && event.Media == nil && event.Location == nil && len(event.Contacts) == 0 {
					continue
				}

				// Publish to NATS
				eventData, _ := json.Marshal(event)
				subject := fmt.Sprintf("inbound.events.%s", workspaceID.String())
				_ = h.publisher.Publish(ctx, subject, eventData, traceID)

				// Audit logging
				if h.auditWriter != nil {
					_ = h.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "inbound_message", eventData))
				}
			}
		}
	}

	return c.NoContent(http.StatusOK)
}

func contentHash(data []byte) string {
	hasher := sha256.New()
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil))
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

	// Limit to 25MB
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

func (h *WABAWebhookHandler) getExtFromMime(mime string) string {
	parts := strings.Split(mime, "/")
	if len(parts) == 2 {
		ext := parts[1]
		if idx := strings.Index(ext, ";"); idx != -1 {
			ext = ext[:idx]
		}
		return ext
	}
	return "bin"
}
