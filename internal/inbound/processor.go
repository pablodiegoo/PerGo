package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// InboundMedia carries media bytes and metadata downloaded by the caller/adapter.
type InboundMedia struct {
	Bytes     []byte `json:"-"`
	MediaType string `json:"media_type"` // "image", "document", "audio", "video"
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

// InboundEvent is the channel-agnostic inbound payload.
type InboundEvent struct {
	WorkspaceID uuid.UUID
	MessageID   string // Provider-specific unique message/update ID
	Channel     string // "whatsapp", "whatsapp_cloud", "telegram"
	From        string // Sender JID/phone/chat ID
	To          string // Recipient identity (our bot/phone)
	Body        string
	Media       *InboundMedia
	Location    *InboundLocation
	Contacts    []InboundContact
	SenderName  string
	Metadata    map[string]string
}

// InboundEventPayload is the standard format published to NATS and webhooks.
type InboundEventPayload struct {
	Event       string           `json:"event"`
	TraceID     string           `json:"trace_id"`
	MessageID   string           `json:"message_id"`
	Channel     string           `json:"channel"`
	Timestamp   string           `json:"timestamp"`
	WorkspaceID string           `json:"workspace_id"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	Body        string           `json:"body,omitempty"`
	Media       *EventMedia      `json:"media,omitempty"`
	Location    *InboundLocation `json:"location,omitempty"`
	Contacts    []InboundContact `json:"contacts,omitempty"`
}

type EventMedia struct {
	MediaURL  string `json:"media_url"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
}

// Publisher defines the port for publishing event payloads to a messaging queue.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// InboundProcessor handles workspace verification, deduplication, PII checking,
// S3 uploading, NATS publishing, and audit logging for all messaging channels.
type InboundProcessor struct {
	dedupRepo            *repository.InboundDedupRepository
	wsRepo               *repository.WorkspaceRepository
	mediaEngine          media.Engine
	publisher            Publisher
	auditWriter          audit.Writer
	recipientSessionRepo *repository.RecipientSessionRepository
	contactRepo          *repository.ContactRepository
}

// NewInboundProcessor creates a new InboundProcessor.
func NewInboundProcessor(
	dedupRepo *repository.InboundDedupRepository,
	wsRepo *repository.WorkspaceRepository,
	mediaEngine media.Engine,
	publisher Publisher,
	auditWriter audit.Writer,
	recipientSessionRepo *repository.RecipientSessionRepository,
	contactRepo *repository.ContactRepository,
) *InboundProcessor {
	return &InboundProcessor{
		dedupRepo:            dedupRepo,
		wsRepo:               wsRepo,
		mediaEngine:          mediaEngine,
		publisher:            publisher,
		auditWriter:          auditWriter,
		recipientSessionRepo: recipientSessionRepo,
		contactRepo:          contactRepo,
	}
}

// Process executes the ingestion pipeline for an inbound event.
func (p *InboundProcessor) Process(ctx context.Context, ev *InboundEvent) error {
	if ev.WorkspaceID == uuid.Nil {
		return fmt.Errorf("inbound: workspace ID is required")
	}

	// Resolve/Create Contact Profile
	if p.contactRepo != nil {
		var username, phone string
		if ev.Metadata != nil {
			username = ev.Metadata["username"]
			phone = ev.Metadata["phone_number"]
		}
		if ev.Channel == "whatsapp" || ev.Channel == "whatsapp_cloud" {
			phone = ev.From
		}
		_, err := p.contactRepo.ResolveContact(ctx, ev.WorkspaceID, ev.Channel, ev.From, ev.SenderName, username, phone)
		if err != nil {
			slog.Error("inbound processor: failed to resolve contact profile", "error", err, "from", ev.From)
		}
	}

	// 1. Recipient Session Tracking
	if p.recipientSessionRepo != nil {
		err := p.recipientSessionRepo.Upsert(ctx, ev.WorkspaceID, ev.From, ev.Channel, ev.To, time.Now().UTC())
		if err != nil {
			slog.Error("inbound processor: failed to upsert recipient session", "error", err, "from", ev.From)
		}
	}

	// 2. Deduplication check
	if p.dedupRepo != nil && ev.MessageID != "" {
		unique, err := p.dedupRepo.InsertAndCheck(ctx, ev.WorkspaceID, ev.Channel, ev.MessageID)
		if err != nil {
			return fmt.Errorf("inbound: deduplication check failed: %w", err)
		}
		if !unique {
			slog.Info("inbound processor: duplicate message ignored", "message_id", ev.MessageID, "channel", ev.Channel)
			return nil
		}
	}

	// 3. Retrieve Workspace PII Opt-In
	var piiOptIn bool
	if p.wsRepo != nil {
		if ws, err := p.wsRepo.GetByID(ctx, ev.WorkspaceID); err == nil && ws != nil {
			piiOptIn = ws.PIIOptIn
		}
	}

	// 4. Construct base event payload
	traceID := uuid.New().String()
	payload := InboundEventPayload{
		Event:       "inbound_message",
		TraceID:     traceID,
		MessageID:   ev.MessageID,
		Channel:     ev.Channel,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: ev.WorkspaceID.String(),
		From:        ev.From,
		To:          ev.To,
		Body:        ev.Body,
	}

	// 5. Upload media to S3 if present
	if ev.Media != nil && len(ev.Media.Bytes) > 0 {
		if p.mediaEngine == nil {
			slog.Error("inbound processor: skipped S3 upload; S3 client/media engine is not configured")
		} else {
			mediaURL, err := p.mediaEngine.ProcessInbound(ctx, ev.WorkspaceID, ev.Media.MediaType, ev.Media.Bytes)
			if err != nil {
				slog.Error("inbound processor: media upload/process failed", "error", err)
			} else {
				payload.Media = &EventMedia{
					MediaURL:  mediaURL,
					MediaType: ev.Media.MediaType,
					Filename:  ev.Media.Filename,
					Caption:   ev.Media.Caption,
				}
			}
		}
	}

	// 6. PII Opt-In check (Locations and Contacts)
	if piiOptIn {
		payload.Location = ev.Location
		payload.Contacts = ev.Contacts
	}

	// 7. Drop event if it's completely empty
	if payload.Body == "" && payload.Media == nil && payload.Location == nil && len(payload.Contacts) == 0 {
		slog.Debug("inbound processor: ignoring empty inbound event payload")
		return nil
	}

	// 8. Publish to NATS JetStream and Audit Log
	if p.publisher != nil {
		eventData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("inbound: failed to marshal event payload: %w", err)
		}

		subject := fmt.Sprintf("inbound.events.%s", ev.WorkspaceID.String())
		err = p.publisher.Publish(ctx, subject, eventData, traceID)
		if err != nil {
			return fmt.Errorf("inbound: failed to publish event to NATS: %w", err)
		}

		if p.auditWriter != nil {
			err = p.auditWriter.Write(audit.NewEvent(ev.WorkspaceID, traceID, "inbound_message", eventData))
			if err != nil {
				slog.Error("inbound processor: failed to write audit log", "error", err, "trace_id", traceID)
			}
		}
	}

	return nil
}
