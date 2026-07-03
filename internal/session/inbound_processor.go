// Package session provides inbound message processing for WhatsApp Web.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// InboundProcessor handles incoming WhatsApp messages: text extraction,
// S3 upload, deduplication, PII opt-in, payload construction, NATS
// publish, and audit. The caller is responsible for downloading media
// from the WhatsApp CDN and passing raw bytes via to the processor.
type InboundProcessor struct {
	dedupRepo            *repository.InboundDedupRepository
	wsRepo               *repository.WorkspaceRepository
	s3Client             *storage.S3Client
	publisher            *queue.JetStreamPublisher
	auditWriter          audit.Writer
	recipientSessionRepo *repository.RecipientSessionRepository
}

// InboundMedia carries media bytes and metadata downloaded by the caller.
type InboundMedia struct {
	MediaURL  string `json:"media_url"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
}

// InboundLocation carries location data.
type InboundLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

// InboundContact carries contact data.
type InboundContact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

// NewInboundProcessor creates a processor for inbound WhatsApp messages.
func NewInboundProcessor(
	dedupRepo *repository.InboundDedupRepository,
	wsRepo *repository.WorkspaceRepository,
	s3Client *storage.S3Client,
	publisher *queue.JetStreamPublisher,
	auditWriter audit.Writer,
	recipientSessionRepo *repository.RecipientSessionRepository,
) *InboundProcessor {
	return &InboundProcessor{
		dedupRepo:            dedupRepo,
		wsRepo:               wsRepo,
		s3Client:             s3Client,
		publisher:            publisher,
		auditWriter:          auditWriter,
		recipientSessionRepo: recipientSessionRepo,
	}
}

// DownloadFunc is the interface for downloading media from WhatsApp CDN.
// Implementations use an active whatsmeow client.
type DownloadFunc func(ctx context.Context, downloadable interface{}) ([]byte, error)

// Handle processes an incoming WhatsApp message. mediaBytes and mediaMeta
// are nil for text-only messages. The caller is responsible for downloading
// media from WhatsApp CDN before calling Handle.
func (p *InboundProcessor) Handle(
	ctx context.Context,
	v *waEvents.Message,
	mediaBytes []byte,
	mediaMeta *MediaMeta,
	workspaceID uuid.UUID,
	senderJID string,
	recipientIdentity string,
) {
	messageID := v.Info.ID

	// 1. Recipient Session Tracking
	if p.recipientSessionRepo != nil {
		_ = p.recipientSessionRepo.Upsert(ctx, workspaceID, senderJID, "whatsapp", recipientIdentity, time.Now().UTC())
	}

	// 2. Deduplication check
	if p.dedupRepo != nil {
		unique, err := p.dedupRepo.InsertAndCheck(ctx, workspaceID, "whatsapp", messageID)
		if err != nil {
			slog.Error("inbound processor: dedup check failed", "error", err)
			return
		}
		if !unique {
			slog.Info("inbound processor: duplicate message ignored", "message_id", messageID)
			return
		}
	}

	// 3. Retrieve Workspace PII Opt-In
	var wsOptIn bool
	if p.wsRepo != nil {
		if ws, err := p.wsRepo.GetByID(ctx, workspaceID); err == nil && ws != nil {
			wsOptIn = ws.PIIOptIn
		}
	}

	// 4. Populate payload
	traceID := uuid.New().String()
	inboundEvt := struct {
		Event       string           `json:"event"`
		TraceID     string           `json:"trace_id"`
		MessageID   string           `json:"message_id"`
		Channel     string           `json:"channel"`
		Timestamp   string           `json:"timestamp"`
		WorkspaceID string           `json:"workspace_id"`
		From        string           `json:"from"`
		To          string           `json:"to"`
		Body        string           `json:"body,omitempty"`
		Media       *InboundMedia    `json:"media,omitempty"`
		Location    *InboundLocation `json:"location,omitempty"`
		Contacts    []InboundContact `json:"contacts,omitempty"`
	}{
		Event:       "inbound_message",
		TraceID:     traceID,
		MessageID:   messageID,
		Channel:     "whatsapp",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: workspaceID.String(),
		From:        senderJID,
		To:          recipientIdentity,
	}

	// Extract Text/Body
	inboundEvt.Body = p.extractBody(v)

	// 5. Upload media to S3 if present
	if len(mediaBytes) > 0 && p.s3Client != nil && mediaMeta != nil {
		if len(mediaBytes) <= 25*1024*1024 {
			hashKey := hashBytes(mediaBytes)
			ext := "bin"
			if mediaMeta.MediaType == "image" {
				ext = "jpg"
			}
			s3Key := fmt.Sprintf("%s/%s.%s", workspaceID.String(), hashKey, ext)
			mime := "application/octet-stream"
			if mediaMeta.MediaType == "image" {
				mime = "image/jpeg"
			}
			if err := p.s3Client.Upload(ctx, s3Key, mediaBytes, mime); err == nil {
				inboundEvt.Media = &InboundMedia{
					MediaURL:  fmt.Sprintf("/media/%s/%s.%s", workspaceID.String(), hashKey, ext),
					MediaType: mediaMeta.MediaType,
					Filename:  mediaMeta.Filename,
					Caption:   mediaMeta.Caption,
				}
			}
		}
	}

	// 6. PII Opt-In Check (Location & Contacts)
	if wsOptIn {
		if locMsg := v.Message.GetLocationMessage(); locMsg != nil {
			inboundEvt.Location = &InboundLocation{
				Latitude:  *locMsg.DegreesLatitude,
				Longitude: *locMsg.DegreesLongitude,
				Name:      locMsg.GetName(),
				Address:   locMsg.GetAddress(),
			}
		}
		if contactMsg := v.Message.GetContactMessage(); contactMsg != nil {
			inboundEvt.Contacts = []InboundContact{
				{
					Name:  contactMsg.GetDisplayName(),
					Phone: contactMsg.GetVcard(),
				},
			}
		}
	}

	// 7. Drop if empty
	if inboundEvt.Body == "" && inboundEvt.Media == nil && inboundEvt.Location == nil && len(inboundEvt.Contacts) == 0 {
		return
	}

	// 8. Publish to NATS and Audit Log
	if p.publisher != nil {
		eventData, _ := json.Marshal(inboundEvt)
		subject := fmt.Sprintf("inbound.events.%s", workspaceID.String())
		_ = p.publisher.Publish(ctx, subject, eventData, traceID)

		if p.auditWriter != nil {
			_ = p.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "inbound_message", eventData))
		}
	}
}

// extractBody pulls the human-readable text from a WhatsApp message.
func (p *InboundProcessor) extractBody(v *waEvents.Message) string {
	if msgText := v.Message.GetConversation(); msgText != "" {
		return msgText
	}
	if extText := v.Message.GetExtendedTextMessage().GetText(); extText != "" {
		return extText
	}
	if imageMsg := v.Message.GetImageMessage(); imageMsg != nil && imageMsg.Caption != nil {
		return *imageMsg.Caption
	}
	if documentMsg := v.Message.GetDocumentMessage(); documentMsg != nil && documentMsg.Caption != nil {
		return *documentMsg.Caption
	}
	if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil && videoMsg.Caption != nil {
		return *videoMsg.Caption
	}
	return ""
}

// MediaMeta describes the media downloaded by the caller.
type MediaMeta struct {
	MediaType string
	Filename  string
	Caption   string
}

func hashBytes(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
