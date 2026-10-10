package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// ParseUnixTimestamp parses a string representation of a Unix timestamp in seconds to time.Time in UTC.
// Returns a zero time.Time if the string is empty, unparseable, or <= 0.
func ParseUnixTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// ParseUnixMillis parses an int64 Unix timestamp in milliseconds to time.Time in UTC.
// Returns a zero time.Time if ms <= 0.
func ParseUnixMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// ChatwootSyncer defines the interface to sync inbound customer messages into Chatwoot.
type ChatwootSyncer interface {
	SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}

// TypebotForwarder defines the interface to forward inbound customer messages to Typebot.
type TypebotForwarder interface {
	SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}

// InboundRouter defines the interface for routing unified inbound events to integration syncers.
type InboundRouter interface {
	Route(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
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

// InboundButtonReply represents a button interaction reply.
type InboundButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// InboundListReply represents a list item selection reply.
type InboundListReply struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// InboundNFMReply represents a Meta Flow form submission (native flow message reply).
type InboundNFMReply struct {
	Name         string                 `json:"name,omitempty"`
	Body         string                 `json:"body,omitempty"`
	ResponseJSON string                 `json:"response_json,omitempty"`
	FlowToken    string                 `json:"flow_token,omitempty"`
	Screen       string                 `json:"screen,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

// InboundOrder represents a WhatsApp catalog checkout order.
type InboundOrder struct {
	CatalogID    string                    `json:"catalog_id"`
	Text         string                    `json:"text,omitempty"`
	ProductItems []domain.OrderProductItem `json:"product_items"`
	TotalPrice   float64                   `json:"total_price,omitempty"`
	Currency     string                    `json:"currency,omitempty"`
}

// InboundInteractive represents the unified inbound interactive payload.
type InboundInteractive struct {
	Type        string              `json:"type"` // e.g. "button_reply", "list_reply", "nfm_reply", "order"
	ButtonReply *InboundButtonReply `json:"button_reply,omitempty"`
	ListReply   *InboundListReply   `json:"list_reply,omitempty"`
	NFMReply    *InboundNFMReply    `json:"nfm_reply,omitempty"`
	Order       *InboundOrder       `json:"order,omitempty"`
}

// InboundStoryEvent represents an Instagram story mention or reply.
type InboundStoryEvent struct {
	Subtype  string `json:"subtype"`
	MediaURL string `json:"media_url,omitempty"`
}

// InboundReaction represents an inbound message reaction (add or remove).
type InboundReaction struct {
	MessageUID string `json:"message_uid"`
	Emoji      string `json:"emoji"`
	Action     string `json:"action"` // "add" | "remove"
}

// InboundEvent is the channel-agnostic inbound payload.
type InboundEvent struct {
	WorkspaceID  uuid.UUID
	ConnectionID uuid.UUID
	MessageID    string // Provider-specific unique message/update ID
	TraceID      string
	Channel      string // "whatsapp", "whatsapp_cloud", "telegram"
	From         string // Sender JID/phone/chat ID
	To           string // Recipient identity (our bot/phone)
	Body         string
	Media        *InboundMedia
	Location     *InboundLocation
	Contacts     []InboundContact
	Interactive  *InboundInteractive
	Story        *InboundStoryEvent
	Reaction     *InboundReaction
	SenderName   string
	OccurredAt   time.Time
	Metadata     map[string]string
}

// IsGroup reports whether the inbound event represents a group chat.
func (e *InboundEvent) IsGroup() bool {
	if e == nil {
		return false
	}
	if e.Metadata != nil && e.Metadata[domain.MetaIsGroup] == "true" {
		return true
	}
	return strings.HasSuffix(e.From, "@g.us")
}

// SenderDisplayName resolves the most specific sender name available for the event.
func (e *InboundEvent) SenderDisplayName() string {
	if e == nil {
		return ""
	}
	if e.SenderName != "" {
		return e.SenderName
	}
	if e.Metadata != nil {
		if pushName := e.Metadata[domain.MetaSenderPushName]; pushName != "" {
			return pushName
		}
		if participant := e.Metadata[domain.MetaParticipant]; participant != "" {
			return participant
		}
	}
	return ""
}

// InboundEventPayload is the standard format published to NATS and webhooks.
type InboundEventPayload struct {
	Event       string              `json:"event"`
	TraceID     string              `json:"trace_id"`
	MessageID   string              `json:"message_id"`
	Channel     string              `json:"channel"`
	Timestamp   string              `json:"timestamp"`
	WorkspaceID string              `json:"workspace_id"`
	From        string              `json:"from"`
	To          string              `json:"to"`
	Body        string              `json:"body,omitempty"`
	Media       *EventMedia         `json:"media,omitempty"`
	Location    *InboundLocation    `json:"location,omitempty"`
	Contacts    []InboundContact    `json:"contacts,omitempty"`
	Interactive *InboundInteractive `json:"interactive,omitempty"`
	Story       *InboundStoryEvent  `json:"story_event,omitempty"`
	Timing      *EventTiming        `json:"timing,omitempty"`
	SenderName  string              `json:"sender_name,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
}

// EventTiming carries contact response latency and timing telemetry.
type EventTiming struct {
	ResponseLatencyMS *int64 `json:"response_latency_ms,omitempty"`
	LastOutboundAt    string `json:"last_outbound_at,omitempty"`
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
	MediaURL   string                `json:"media_url"`
	MediaType  string                `json:"media_type"`
	Filename   string                `json:"filename,omitempty"`
	Caption    string                `json:"caption,omitempty"`
	DurationMS int                   `json:"duration_ms,omitempty"`
	RMSEnergy  *float64              `json:"rms_energy,omitempty"`
	Telemetry  *media.AudioTelemetry `json:"telemetry,omitempty"`
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
	dispatchRepo              *repository.MessageDispatchRepository
	router                    InboundRouter
	chatRepo                  *repository.ChatRepository
	debouncer                 *Debouncer
	reactionWebhookDispatcher ReactionWebhookDispatcher
}

// ReactionWebhookDispatcher defines the interface for dispatching signed reaction webhooks.
type ReactionWebhookDispatcher interface {
	DispatchReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, payload []byte, traceID string) error
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
	router InboundRouter,
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
		router:               router,
	}
}

// SetChatRepository configures the ChatRepository for conversational inbox tracking.
func (p *InboundProcessor) SetChatRepository(r *repository.ChatRepository) *InboundProcessor {
	p.chatRepo = r
	return p
}

// SetDebouncer configures the Debouncer for inbound AI sliding-window coalescing.
func (p *InboundProcessor) SetDebouncer(d *Debouncer) *InboundProcessor {
	p.debouncer = d
	return p
}

// SetReactionWebhookDispatcher configures webhook dispatching for reaction events.
func (p *InboundProcessor) SetReactionWebhookDispatcher(d ReactionWebhookDispatcher) *InboundProcessor {
	p.reactionWebhookDispatcher = d
	return p
}

// Process executes the ingestion pipeline for an inbound event.
func (p *InboundProcessor) Process(ctx context.Context, ev *InboundEvent) error {
	if ev.WorkspaceID == uuid.Nil {
		return fmt.Errorf("inbound: workspace ID is required")
	}

	traceID := ev.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}

	if ev.Reaction != nil {
		occurredAt := ev.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}

		var updatedReactions []domain.Reaction
		var chatID *uuid.UUID
		if p.chatRepo != nil {
			if msg, mErr := p.chatRepo.GetChatMessageByUID(ctx, ev.WorkspaceID, ev.Reaction.MessageUID); mErr == nil && msg != nil {
				chatID = &msg.ChatID
			}
			var err error
			if ev.Reaction.Action == "remove" || ev.Reaction.Emoji == "" {
				updatedReactions, err = p.chatRepo.RemoveReaction(ctx, ev.WorkspaceID, ev.Reaction.MessageUID, ev.Reaction.Emoji, ev.From)
			} else {
				updatedReactions, err = p.chatRepo.AddReaction(ctx, ev.WorkspaceID, ev.Reaction.MessageUID, domain.Reaction{
					Emoji:     ev.Reaction.Emoji,
					Sender:    ev.From,
					CreatedAt: occurredAt,
				})
			}
			if err != nil {
				slog.Error("inbound processor: failed to record reaction", "error", err, "message_uid", ev.Reaction.MessageUID, "trace_id", traceID)
			}
		}

		action := ev.Reaction.Action
		if action == "" {
			if ev.Reaction.Emoji == "" {
				action = "remove"
			} else {
				action = "add"
			}
		}

		var chatIDStr string
		if chatID != nil {
			chatIDStr = chatID.String()
		}

		rxEvent := domain.ReactionUpdatedPayload{
			Event:       "message.reaction.updated",
			WorkspaceID: ev.WorkspaceID.String(),
			ChatID:      chatIDStr,
			MessageUID:  ev.Reaction.MessageUID,
			Emoji:       ev.Reaction.Emoji,
			Sender:      ev.From,
			Action:      action,
			Reactions:   updatedReactions,
			Timestamp:   occurredAt.Format(time.RFC3339),
		}
		payloadBytes, err := json.Marshal(rxEvent)
		if err == nil {
			if p.publisher != nil {
				_ = p.publisher.Publish(ctx, "messages.events.reaction_updated", payloadBytes, traceID)
			}
			if p.reactionWebhookDispatcher != nil {
				_ = p.reactionWebhookDispatcher.DispatchReaction(ctx, ev.WorkspaceID, ev.Reaction.MessageUID, payloadBytes, traceID)
			}
			if p.auditWriter != nil {
				_ = p.auditWriter.Write(audit.NewEvent(ev.WorkspaceID, traceID, "reaction_updated", payloadBytes))
			}
		}
		return nil
	}

	if ev.Metadata != nil && ev.Metadata["type"] == "status_update" {
		if p.dispatchRepo == nil {
			slog.Warn("inbound processor: status_update received but dispatchRepo is nil", "trace_id", traceID)
			return nil
		}
		dispatch, err := p.dispatchRepo.GetByProviderMessageID(ctx, ev.MessageID)
		if err != nil {
			if errors.Is(err, repository.ErrDispatchNotFound) {
				slog.Warn("inbound processor: dispatch not found for status update", "provider_message_id", ev.MessageID, "trace_id", traceID)
				return nil
			}
			return fmt.Errorf("inbound processor: failed to get dispatch by provider message ID: %w", err)
		}

		err = p.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, ev.Body, dispatch.CurrentChannel, dispatch.FallbackIndex, nil)
		if err != nil {
			return fmt.Errorf("inbound processor: failed to update dispatch status: %w", err)
		}

		if p.publisher != nil {
			statusOccurredAt := ev.OccurredAt
			if statusOccurredAt.IsZero() {
				statusOccurredAt = time.Now().UTC()
			}
			payload := MessageStatusUpdatedPayload{
				WorkspaceID: dispatch.WorkspaceID.String(),
				DispatchID:  dispatch.ID.String(),
				MessageID:   ev.MessageID,
				Status:      ev.Body,
				Timestamp:   statusOccurredAt.Format(time.RFC3339),
			}
			eventData, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("inbound processor: failed to marshal status update payload: %w", err)
			}
			statusTraceID := dispatch.TraceID
			if statusTraceID == "" {
				statusTraceID = traceID
			}
			err = p.publisher.Publish(ctx, "messages.status_updated", eventData, statusTraceID)
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
		if !ev.IsGroup() && (ev.Channel == "whatsapp" || ev.Channel == "whatsapp_cloud") {
			phone = ev.From
		}
		var err error
		contact, err = p.contactRepo.ResolveContact(ctx, ev.WorkspaceID, ev.Channel, ev.From, ev.SenderName, username, phone)
		if err != nil {
			slog.Error("inbound processor: failed to resolve contact profile", "error", err, "from", ev.From, "trace_id", traceID)
		}

		if contact != nil && !contact.BotActive && contact.BotPausedAt != nil {
			if time.Since(*contact.BotPausedAt) > 12*time.Hour {
				slog.Info("inbound processor: bot inactive for > 12 hours, auto-resetting to active", "contact_id", contact.ID, "trace_id", traceID)
				err := p.contactRepo.UpdateBotState(ctx, ev.WorkspaceID, contact.ID, true, nil)
				if err != nil {
					slog.Error("inbound processor: failed to reset bot state to active", "error", err, "contact_id", contact.ID, "trace_id", traceID)
				} else {
					contact.BotActive = true
					contact.BotPausedAt = nil
				}
			}
		}
	}

	// 1. Recipient Session Tracking & Timing Telemetry
	occurredAt := ev.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	var timing *EventTiming
	if p.recipientSessionRepo != nil {
		sessKey := domain.SessionKey{
			WorkspaceID:       ev.WorkspaceID,
			RecipientPhone:    ev.From,
			Channel:           ev.Channel,
			RecipientIdentity: ev.To,
		}
		if sess, err := p.recipientSessionRepo.Get(ctx, sessKey); err == nil && sess != nil {
			if sess.LastOutboundAt != nil && !sess.LastOutboundAt.IsZero() {
				diff := occurredAt.Sub(*sess.LastOutboundAt)
				var latencyMS int64
				if diff > 0 {
					latencyMS = diff.Milliseconds()
				}
				timing = &EventTiming{
					ResponseLatencyMS: &latencyMS,
					LastOutboundAt:    sess.LastOutboundAt.UTC().Format(time.RFC3339),
				}
			}
		}

		entryPointType := "standard"
		if ev.Metadata != nil {
			if ept, ok := ev.Metadata["entry_point_type"]; ok && ept != "" {
				entryPointType = ept
			}
		}
		err := p.recipientSessionRepo.Upsert(ctx, sessKey, occurredAt, entryPointType)
		if err != nil {
			slog.Error("inbound processor: failed to upsert recipient session", "error", err, "from", ev.From, "trace_id", traceID)
		}
	}

	// 2. Deduplication check
	if p.dedupRepo != nil && ev.MessageID != "" {
		if ev.Metadata == nil || ev.Metadata["deduplicated"] != "true" {
			unique, err := p.dedupRepo.InsertAndCheck(ctx, ev.WorkspaceID, ev.Channel, ev.MessageID)
			if err != nil {
				return fmt.Errorf("inbound: deduplication check failed: %w", err)
			}
			if !unique {
				slog.Info("inbound processor: duplicate message ignored", "message_id", ev.MessageID, "channel", ev.Channel, "trace_id", traceID)
				return nil
			}
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
	payload := InboundEventPayload{
		Event:       "inbound_message",
		TraceID:     traceID,
		MessageID:   ev.MessageID,
		Channel:     ev.Channel,
		Timestamp:   occurredAt.Format(time.RFC3339),
		WorkspaceID: ev.WorkspaceID.String(),
		From:        ev.From,
		To:          ev.To,
		Body:        ev.Body,
		Interactive: ev.Interactive,
		Story:       ev.Story,
		Timing:      timing,
		SenderName:  ev.SenderName,
		Metadata:    ev.Metadata,
	}

	// 5. Upload media to S3 if present
	if ev.Media != nil && len(ev.Media.Bytes) > 0 {
		if p.mediaEngine == nil {
			slog.Error("inbound processor: skipped S3 upload; S3 client/media engine is not configured", "trace_id", traceID)
		} else {
			mediaURL, err := p.mediaEngine.ProcessInbound(ctx, ev.WorkspaceID, ev.Media.MediaType, ev.Media.Bytes)
			if err != nil {
				slog.Error("inbound processor: media upload/process failed", "error", err, "trace_id", traceID)
			} else {
				payload.Media = &EventMedia{
					MediaURL:  mediaURL,
					MediaType: ev.Media.MediaType,
					Filename:  ev.Media.Filename,
					Caption:   ev.Media.Caption,
				}
				ev.Media.MediaURL = mediaURL

				// If media is audio, extract acoustic telemetry (RMS energy and duration)
				if media.IsAudio(ev.Media.MediaType, ev.Media.Filename) {
					telemetry, err := p.mediaEngine.ExtractAudioTelemetry(ev.Media.Bytes, ev.Media.MediaType)
					if err != nil {
						slog.Warn("inbound processor: failed to extract audio telemetry", "error", err, "trace_id", traceID)
					} else if telemetry != nil {
						payload.Media.DurationMS = telemetry.DurationMS
						payload.Media.RMSEnergy = &telemetry.RMSEnergy
						payload.Media.Telemetry = telemetry
					}
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
	if payload.Body == "" && payload.Media == nil && payload.Location == nil && len(payload.Contacts) == 0 && payload.Interactive == nil && payload.Story == nil {
		slog.Debug("inbound processor: ignoring empty inbound event payload", "trace_id", traceID)
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

		if ev.Interactive != nil {
			if ev.Interactive.Type == "nfm_reply" && ev.Interactive.NFMReply != nil {
				flowEv := &domain.FlowCompletedEvent{
					Screen:    ev.Interactive.NFMReply.Screen,
					Data:      ev.Interactive.NFMReply.Data,
					FlowToken: ev.Interactive.NFMReply.FlowToken,
					ContactID: ev.From,
					Wamid:     ev.MessageID,
					TraceID:   traceID,
				}
				if pubErr := p.PublishFlowCompleted(ctx, ev.WorkspaceID, flowEv); pubErr != nil {
					slog.Error("inbound processor: failed to publish flow.completed event", "error", pubErr, "message_id", ev.MessageID, "trace_id", traceID)
				}
			} else if ev.Interactive.Type == "order" && ev.Interactive.Order != nil {
				orderEv := &domain.OrderCreatedEvent{
					OrderID:    ev.MessageID,
					CatalogID:  ev.Interactive.Order.CatalogID,
					Items:      ev.Interactive.Order.ProductItems,
					TotalPrice: ev.Interactive.Order.TotalPrice,
					Currency:   ev.Interactive.Order.Currency,
					Wamid:      ev.MessageID,
					ContactID:  ev.From,
					TraceID:    traceID,
				}
				if pubErr := p.PublishOrderCreated(ctx, ev.WorkspaceID, orderEv); pubErr != nil {
					slog.Error("inbound processor: failed to publish order.created event", "error", pubErr, "message_id", ev.MessageID, "trace_id", traceID)
				}
			}
		} else if ev.Metadata != nil && ev.Metadata["type"] == "order" && ev.Metadata["order_json"] != "" {
			var orderEv domain.OrderCreatedEvent
			if err := json.Unmarshal([]byte(ev.Metadata["order_json"]), &orderEv); err == nil {
				if orderEv.TraceID == "" {
					orderEv.TraceID = traceID
				}
				_ = p.PublishOrderCreated(ctx, ev.WorkspaceID, &orderEv)
			}
		}
	}

	// 8.5 Shared Team Inbox Tracking: Upsert Chat & Append Inbound ChatMessage
	var chat *domain.Chat
	if p.chatRepo != nil && contact != nil {
		var connID *uuid.UUID
		if ev.ConnectionID != uuid.Nil {
			connID = &ev.ConnectionID
		}

		var cErr error
		chat, cErr = p.chatRepo.FindOrCreateChat(ctx, ev.WorkspaceID, connID, contact.ID)
		if cErr != nil {
			slog.Error("inbound processor: failed to find or create chat", "error", cErr, "contact_id", contact.ID, "trace_id", traceID)
		} else if chat != nil {
			_ = p.chatRepo.IncrementUnreadCount(ctx, ev.WorkspaceID, chat.ID)
			_ = p.chatRepo.TouchLastMessageAt(ctx, ev.WorkspaceID, chat.ID, occurredAt)

			if ev.Channel == "whatsapp_cloud" {
				duration := 24 * time.Hour
				if ev.Metadata != nil && (ev.Metadata["entry_point_type"] == "ctwa" || ev.Metadata["is_referral"] == "true") {
					duration = 72 * time.Hour
				}
				exp := occurredAt.Add(duration)
				_ = p.chatRepo.UpdateServiceWindow(ctx, ev.WorkspaceID, chat.ID, &exp)
			}

			msgUID := ev.MessageID
			if msgUID == "" {
				msgUID = traceID
			}

			msgBody := ev.Body
			if msgBody == "" && ev.Interactive != nil {
				if ev.Interactive.ButtonReply != nil {
					msgBody = ev.Interactive.ButtonReply.Title
				} else if ev.Interactive.ListReply != nil {
					msgBody = ev.Interactive.ListReply.Title
				} else if ev.Interactive.NFMReply != nil {
					if ev.Interactive.NFMReply.Body != "" {
						msgBody = ev.Interactive.NFMReply.Body
					} else {
						msgBody = fmt.Sprintf("[Flow Submission: %s]", ev.Interactive.NFMReply.Name)
					}
				}
			}

			var mediaURL, mediaType *string
			if payload.Media != nil {
				if payload.Media.MediaURL != "" {
					mediaURL = &payload.Media.MediaURL
				}
				if payload.Media.MediaType != "" {
					mediaType = &payload.Media.MediaType
				}
			}

			chatMsg := &domain.ChatMessage{
				ChatID:      chat.ID,
				WorkspaceID: ev.WorkspaceID,
				UID:         msgUID,
				Direction:   string(domain.DirectionInbound),
				SenderType:  string(domain.SenderTypeContact),
				SenderName:  ev.SenderDisplayName(),
				SenderID:    ev.From,
				Body:        msgBody,
				MediaURL:    mediaURL,
				MediaType:   mediaType,
				IsPrivate:   false,
				Reactions:   []domain.Reaction{},
				Metadata:    map[string]interface{}{},
				CreatedAt:   occurredAt,
			}
			if err := p.chatRepo.AddChatMessage(ctx, chatMsg); err != nil {
				slog.Error("inbound processor: failed to add chat message", "error", err, "uid", msgUID, "trace_id", traceID)
			}

			// 8.6 Inbound Debounce Buffer & AI Handoff Gate
			if p.debouncer != nil {
				if chat.AIDisabled {
					slog.Info("inbound processor: chat AI disabled by human takeover, bypassing automated AI dispatch",
						"workspace_id", ev.WorkspaceID,
						"chat_id", chat.ID,
						"contact_id", contact.ID,
						"trace_id", traceID,
					)
				} else {
					debouncedMsg := &DebouncedMessage{
						MessageID:    msgUID,
						TraceID:      traceID,
						WorkspaceID:  ev.WorkspaceID,
						ConnectionID: connID,
						ChatID:       chat.ID,
						ContactID:    contact.ID,
						Channel:      ev.Channel,
						From:         ev.From,
						To:           ev.To,
						SenderName:   ev.SenderDisplayName(),
						Body:         msgBody,
						OccurredAt:   occurredAt,
						Metadata:     map[string]interface{}{},
					}
					if err := p.debouncer.Enqueue(ctx, debouncedMsg); err != nil {
						slog.Error("inbound processor: failed to enqueue message to debounce buffer", "error", err, "chat_id", chat.ID, "trace_id", traceID)
					}
				}
			}
		}
	}

	// 9. Route inbound event via InboundRouter
	if p.router != nil && contact != nil {
		if chat != nil && chat.AIDisabled {
			slog.Info("inbound processor: chat AI is disabled (human takeover), bypassing InboundRouter dispatch", "chat_id", chat.ID, "contact_id", contact.ID)
		} else {
			if err := p.router.Route(ctx, contact, ev); err != nil {
				slog.Error("inbound processor: router failed to route event", "error", err, "contact_id", contact.ID, "trace_id", traceID)
			}
		}
	}

	return nil
}

// PublishFlowCompleted emits a flow.completed event to the webhook system.
func (p *InboundProcessor) PublishFlowCompleted(ctx context.Context, workspaceID uuid.UUID, ev *domain.FlowCompletedEvent) error {
	if p.publisher == nil {
		return nil
	}

	payload := struct {
		Event       string `json:"event"`
		WorkspaceID string `json:"workspace_id"`
		*domain.FlowCompletedEvent
	}{
		Event:              string(domain.EventTypeFlowCompleted),
		WorkspaceID:        workspaceID.String(),
		FlowCompletedEvent: ev,
	}

	eventData, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("inbound.events.%s", workspaceID.String())
	traceID := ev.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}
	return p.publisher.Publish(ctx, subject, eventData, traceID)
}

// DedupRepo returns the InboundDedupRepository associated with the processor.
func (p *InboundProcessor) DedupRepo() *repository.InboundDedupRepository {
	return p.dedupRepo
}

// PublishOrderCreated emits an order.created event to the webhook system.
func (p *InboundProcessor) PublishOrderCreated(ctx context.Context, workspaceID uuid.UUID, ev *domain.OrderCreatedEvent) error {
	if p.publisher == nil {
		return nil
	}

	payload := struct {
		Event       string `json:"event"`
		WorkspaceID string `json:"workspace_id"`
		*domain.OrderCreatedEvent
	}{
		Event:             string(domain.EventTypeOrderCreated),
		WorkspaceID:       workspaceID.String(),
		OrderCreatedEvent: ev,
	}

	eventData, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("inbound.events.%s", workspaceID.String())
	traceID := ev.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}
	return p.publisher.Publish(ctx, subject, eventData, traceID)
}
