package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// ChatwootSyncer defines the interface to sync inbound customer messages into Chatwoot.
type ChatwootSyncer interface {
	SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}

// TypebotForwarder defines the interface to forward inbound customer messages to Typebot.
type TypebotForwarder interface {
	SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}

// InboundMedia carries media bytes and metadata downloaded by the caller/adapter.
type InboundMedia struct {
	Bytes     []byte `json:"-"`
	MediaType string `json:"media_type"` // "image", "document", "audio", "video"
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
	MediaURL  string `json:"media_url,omitempty"`
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
	WorkspaceID  uuid.UUID
	ConnectionID uuid.UUID
	MessageID    string // Provider-specific unique message/update ID
	Channel      string // "whatsapp", "whatsapp_cloud", "telegram"
	From         string // Sender JID/phone/chat ID
	To           string // Recipient identity (our bot/phone)
	Body         string
	Media        *InboundMedia
	Location     *InboundLocation
	Contacts     []InboundContact
	SenderName   string
	Metadata     map[string]string
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

// MessageStatusUpdatedPayload is the structure of the message status update event published to NATS.
type MessageStatusUpdatedPayload struct {
	WorkspaceID string `json:"workspace_id"`
	DispatchID  string `json:"dispatch_id"`
	MessageID   string `json:"message_id"` // Provider-specific unique message ID (e.g. wamid)
	Status      string `json:"status"`     // e.g. "sent", "delivered", "read", "failed"
	Timestamp   string `json:"timestamp"`
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
	dispatchRepo         *repository.MessageDispatchRepository
	chatwootSyncer       ChatwootSyncer
	typebotForwarder     TypebotForwarder
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
	dispatchRepo *repository.MessageDispatchRepository,
) *InboundProcessor {
	return &InboundProcessor{
		dedupRepo:            dedupRepo,
		wsRepo:               wsRepo,
		mediaEngine:          mediaEngine,
		publisher:            publisher,
		auditWriter:          auditWriter,
		recipientSessionRepo: recipientSessionRepo,
		contactRepo:          contactRepo,
		dispatchRepo:         dispatchRepo,
	}
}

// SetChatwootSyncer registers a Chatwoot syncer instance.
func (p *InboundProcessor) SetChatwootSyncer(s ChatwootSyncer) {
	p.chatwootSyncer = s
}

// SetTypebotForwarder registers a Typebot forwarder instance.
func (p *InboundProcessor) SetTypebotForwarder(f TypebotForwarder) {
	p.typebotForwarder = f
}

// Process executes the ingestion pipeline for an inbound event.
func (p *InboundProcessor) Process(ctx context.Context, ev *InboundEvent) error {
	if ev.WorkspaceID == uuid.Nil {
		return fmt.Errorf("inbound: workspace ID is required")
	}

	if ev.Metadata != nil && ev.Metadata["type"] == "status_update" {
		if p.dispatchRepo == nil {
			slog.Warn("inbound processor: status_update received but dispatchRepo is nil")
			return nil
		}
		dispatch, err := p.dispatchRepo.GetByProviderMessageID(ctx, ev.MessageID)
		if err != nil {
			if errors.Is(err, repository.ErrDispatchNotFound) {
				slog.Warn("inbound processor: dispatch not found for status update", "provider_message_id", ev.MessageID)
				return nil
			}
			return fmt.Errorf("inbound processor: failed to get dispatch by provider message ID: %w", err)
		}

		err = p.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, ev.Body, dispatch.CurrentChannel, dispatch.FallbackIndex, nil)
		if err != nil {
			return fmt.Errorf("inbound processor: failed to update dispatch status: %w", err)
		}

		if p.publisher != nil {
			payload := MessageStatusUpdatedPayload{
				WorkspaceID: dispatch.WorkspaceID.String(),
				DispatchID:  dispatch.ID.String(),
				MessageID:   ev.MessageID,
				Status:      ev.Body,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}
			eventData, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("inbound processor: failed to marshal status update payload: %w", err)
			}
			err = p.publisher.Publish(ctx, "messages.status_updated", eventData, dispatch.TraceID)
			if err != nil {
				return fmt.Errorf("inbound processor: failed to publish status update to NATS: %w", err)
			}
		}
		return nil
	}

	// Resolve/Create Contact Profile
	var contact *domain.Contact
	if p.contactRepo != nil {
		var username, phone string
		if ev.Metadata != nil {
			username = ev.Metadata["username"]
			phone = ev.Metadata["phone_number"]
		}
		if ev.Channel == "whatsapp" || ev.Channel == "whatsapp_cloud" {
			phone = ev.From
		}
		var err error
		contact, err = p.contactRepo.ResolveContact(ctx, ev.WorkspaceID, ev.Channel, ev.From, ev.SenderName, username, phone)
		if err != nil {
			slog.Error("inbound processor: failed to resolve contact profile", "error", err, "from", ev.From)
		}

		if contact != nil && !contact.BotActive && contact.BotPausedAt != nil {
			if time.Since(*contact.BotPausedAt) > 12*time.Hour {
				slog.Info("inbound processor: bot inactive for > 12 hours, auto-resetting to active", "contact_id", contact.ID)
				err := p.contactRepo.UpdateBotState(ctx, ev.WorkspaceID, contact.ID, true, nil)
				if err != nil {
					slog.Error("inbound processor: failed to reset bot state to active", "error", err, "contact_id", contact.ID)
				} else {
					contact.BotActive = true
					contact.BotPausedAt = nil
				}
			}
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
				ev.Media.MediaURL = mediaURL
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

	// 9. Sync asynchronously to Chatwoot
	if p.chatwootSyncer != nil && contact != nil {
		go func(c *domain.Contact, e *InboundEvent) {
			ctxBg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := p.chatwootSyncer.SyncInboundMessage(ctxBg, c, e); err != nil {
				slog.Error("inbound processor: failed to sync message to chatwoot", "error", err, "contact_id", c.ID, "workspace_id", e.WorkspaceID)
			}
		}(contact, ev)
	}

	// 10. Sync asynchronously to Typebot
	if p.typebotForwarder != nil && contact != nil {
		go func(c *domain.Contact, e *InboundEvent) {
			ctxBg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := p.typebotForwarder.SyncInboundMessage(ctxBg, c, e); err != nil {
				slog.Error("inbound processor: failed to sync message to typebot", "error", err, "contact_id", c.ID, "workspace_id", e.WorkspaceID)
			}
		}(contact, ev)
	}

	return nil
}
