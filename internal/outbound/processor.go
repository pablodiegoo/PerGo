package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

// OutboundProcessor defines the port for outbound message ingestion.
type OutboundProcessor interface {
	Ingest(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error)
}

// QueueDepthChecker defines the port for tracking/checking active queue limits.
type QueueDepthChecker interface {
	Exceeds(workspaceID uuid.UUID, limit int64) bool
	Increment(workspaceID uuid.UUID)
}

// RouteResolver defines the port for connection routing resolution.
type RouteResolver interface {
	GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error)
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
}

// Publisher defines the port for NATS JetStream publishing.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// Processor is the concrete implementation of outbound message ingestion.
type Processor struct {
	tracker       QueueDepthChecker
	resolver      RouteResolver
	publisher     Publisher
	mediaEngine   media.Engine
	windowChecker *session.WindowChecker
	templateRepo  *repository.WABATemplateRepository
	chatRepo      *repository.ChatRepository
	contactRepo   *repository.ContactRepository
}

// NewProcessor creates a new OutboundProcessor implementation.
func NewProcessor(
	tracker QueueDepthChecker,
	mediaEngine media.Engine,
	resolver RouteResolver,
	publisher Publisher,
) *Processor {
	return &Processor{
		tracker:     tracker,
		mediaEngine: mediaEngine,
		resolver:    resolver,
		publisher:   publisher,
	}
}

// SetWindowChecker attaches a WindowChecker for session window verification.
func (p *Processor) SetWindowChecker(w *session.WindowChecker) *Processor {
	p.windowChecker = w
	return p
}

// SetTemplateRepository attaches a WABATemplateRepository for template validation.
func (p *Processor) SetTemplateRepository(r *repository.WABATemplateRepository) *Processor {
	p.templateRepo = r
	return p
}

// SetChatRepository configures the ChatRepository for conversational inbox tracking.
func (p *Processor) SetChatRepository(r *repository.ChatRepository) *Processor {
	p.chatRepo = r
	return p
}

// SetContactRepository configures the ContactRepository for recipient profile resolution.
func (p *Processor) SetContactRepository(r *repository.ContactRepository) *Processor {
	p.contactRepo = r
	return p
}

// Ingest runs the outbound message ingestion pipeline: backpressure, validation, S3 caching, routing, NATS publishing.
func (p *Processor) Ingest(
	ctx context.Context,
	workspaceID uuid.UUID,
	traceID string,
	req *domain.CreateMessageRequest,
) (*domain.QueueMessage, error) {
	// 1. Backpressure: check queue depth tracker limits
	if p.tracker != nil && workspaceID != uuid.Nil {
		if p.tracker.Exceeds(workspaceID, 1000) {
			return nil, ErrQueueFull
		}
	}

	// 2. Validate request payload structure
	if valErr := domain.ValidateMessage(req); valErr != nil {
		return nil, &ValidationError{Response: valErr}
	}

	// 2.5 Safety: Exclude internal notes and private messages from external outbound dispatch
	if req.Metadata != nil {
		if req.Metadata["is_private"] == "true" || req.Metadata["direction"] == string(domain.DirectionInternalNote) {
			return nil, &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "forbidden_dispatch",
					Message: "internal notes and private messages are strictly excluded from external channel dispatch",
				},
			}
		}
	}


	// 3. Process Media if present
	if req.Media != nil {
		if p.mediaEngine == nil {
			slog.Error("media engine is not configured for media processing", "trace_id", traceID)
			return nil, &MediaError{
				Code:    "internal_error",
				Message: "media storage configuration error",
			}
		}

		mediaURL, err := p.mediaEngine.ProcessOutbound(ctx, workspaceID, req.Media.MediaURL)
		if err != nil {
			if errors.Is(err, media.ErrMediaSizeExceeded) {
				return nil, &MediaError{
					Code:    "media_size_exceeded",
					Message: "the downloaded file exceeds the maximum size boundary of 25MB",
					Field:   "media.media_url",
					Err:     err,
				}
			}
			return nil, &MediaError{
				Code:    "media_download_failed",
				Message: "failed to download media from the specified URL",
				Field:   "media.media_url",
				Err:     err,
			}
		}

		// Rewire the message payload's MediaURL to the internal proxy URL
		req.Media.MediaURL = mediaURL
	}

	// 4. Resolve connection routing
	if p.resolver == nil {
		return nil, &RouteError{
			Message: "route resolver is not configured",
		}
	}

	var conn *repository.Connection
	var err error

	if req.From != "" {
		conn, err = p.resolver.GetBySenderIdentity(ctx, workspaceID, req.From)
		if err != nil {
			return nil, &RouteError{
				Message: "no matching connection route resolved for the specified sender identity",
				Err:     err,
			}
		}
		if conn.Channel != req.Channel && conn.Slug != req.Channel {
			return nil, &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "route_not_found",
					Message: "connection channel does not match request channel",
				},
			}
		}
	} else {
		conn, err = p.resolver.GetBySlug(ctx, workspaceID, req.Channel)
		if err != nil {
			if errors.Is(err, repository.ErrConnectionNotFound) {
				conn, err = p.resolver.GetDefaultChannelConnection(ctx, workspaceID, req.Channel)
				if err != nil {
					return nil, &RouteError{
						Message: "no connection route resolved for channel or slug",
						Err:     err,
					}
				}
			} else {
				return nil, &RouteError{
					Message: "failed to resolve connection route by slug",
					Err:     err,
				}
			}
		}
	}

	// 4.4 Smart Session Window Fallback & Strict Service Window Enforcement (WABA freeform messages only)
	var preloadedChat *domain.Chat
	if p.chatRepo != nil {
		if req.ChatID != nil && *req.ChatID != uuid.Nil {
			if workspaceID != uuid.Nil {
				preloadedChat, _ = p.chatRepo.GetChat(ctx, workspaceID, *req.ChatID)
			} else {
				preloadedChat, _ = p.chatRepo.GetChatByID(ctx, *req.ChatID)
			}
		} else if req.StrictServiceWindow && p.contactRepo != nil && req.To != "" {
			if contact, cErr := p.contactRepo.ResolveContact(ctx, workspaceID, conn.Channel, req.To, "", "", req.To); cErr == nil && contact != nil {
				preloadedChat, _ = p.chatRepo.FindOrCreateChat(ctx, workspaceID, &conn.ID, contact.ID)
			}
		}
	}

	if req.TemplateName == "" && conn.Channel == "whatsapp_cloud" && (p.windowChecker != nil || (req.StrictServiceWindow && preloadedChat != nil)) {
		var status *session.WindowStatus
		if p.windowChecker != nil {
			sessKey := domain.SessionKey{
				WorkspaceID:       workspaceID,
				RecipientPhone:    req.To,
				Channel:           "whatsapp_cloud",
				RecipientIdentity: conn.SenderIdentity,
			}
			s, wErr := p.windowChecker.IsWindowOpen(ctx, sessKey, 0)
			if wErr != nil {
				slog.Warn("outbound processor: window checker error", "error", wErr, "trace_id", traceID)
			} else {
				status = s
			}
		}

		windowClosed := false
		if preloadedChat != nil && preloadedChat.ServiceWindowExpiresAt != nil {
			if !preloadedChat.IsServiceWindowOpen(time.Now().UTC()) {
				windowClosed = true
			} else if status != nil && !status.Open && !status.ExpiresAt.IsZero() {
				windowClosed = true
			}
		} else if preloadedChat != nil && req.StrictServiceWindow {
			if status != nil {
				windowClosed = !status.Open
			} else {
				windowClosed = !preloadedChat.IsServiceWindowOpen(time.Now().UTC())
			}
		} else if status != nil && !status.Open {
			windowClosed = true
		}

		if windowClosed {
			if req.StrictServiceWindow {
				return nil, &ValidationError{
					Response: &domain.ErrorResponse{
						Code:    "SESSION_WINDOW_EXPIRED",
						Message: "free-form message blocked: 24h customer service window is closed; use an approved WABA template",
					},
					Err: &session.SessionWindowError{
						Status: status,
						Source: "ingestion",
					},
				}
			}

			var config struct {
				DefaultTemplateName     string `json:"default_template_name"`
				DefaultTemplateLanguage string `json:"default_template_language"`
			}
			_ = json.Unmarshal(conn.Credentials, &config)

			if config.DefaultTemplateName != "" {
				req.TemplateName = config.DefaultTemplateName
				req.Language = config.DefaultTemplateLanguage
				if req.Language == "" {
					req.Language = "en_US"
				}
				req.Components = []domain.TemplateComponent{
					{
						Type: "body",
						Parameters: []domain.TemplateParameter{
							{
								Type: "text",
								Text: req.Body,
							},
						},
					},
				}
				req.Body = ""
			} else {
				return nil, &session.SessionWindowError{
					Status: status,
					Source: "ingestion",
				}
			}
		}
	}

	// 4.5. Template Validation & Parameter Normalization
	if req.TemplateName != "" {
		if p.templateRepo != nil {
			tmpl, err := p.templateRepo.GetByNameAndLanguage(ctx, conn.ID, req.TemplateName, req.Language)
			if err != nil {
				if errors.Is(err, repository.ErrTemplateNotFound) || err.Error() == "no rows in result set" {
					return nil, ErrTemplateNotFound
				}
				slog.Error("outbound processor: template repo lookup error", "error", err, "trace_id", traceID)
				return nil, err
			}

			if tmpl.Status != "APPROVED" {
				return nil, &ErrTemplateNotApproved{
					Status:          tmpl.Status,
					RejectionReason: tmpl.RejectionReason,
				}
			}

			// Normalize parameters
			for i := range req.Components {
				normalized, err := NormalizeTemplateParams(req.Components[i].Parameters)
				if err != nil {
					return nil, &ErrInvalidTemplateParameters{Message: err.Error()}
				}
				req.Components[i].Parameters = normalized
			}

			// Validate variable counts
			expected, err := CountTemplateVariables(tmpl.Components)
			if err != nil {
				slog.Warn("outbound processor: failed to extract expected variable counts", "error", err, "trace_id", traceID)
			} else if len(expected) > 0 {
				if err := ValidateParameterCounts(req.Components, expected); err != nil {
					return nil, &ErrInvalidTemplateParameters{Message: err.Error()}
				}
			}
		}
	}

	// 4.6 Product Catalog Resolution & Validation
	if req.Type == domain.MessageTypeProduct || req.Type == domain.MessageTypeProductList || req.Product != nil {
		catalogID := ""
		if req.Product != nil {
			catalogID = req.Product.CatalogID
		}

		if catalogID == "" && len(conn.Credentials) > 0 {
			var wabaCfg struct {
				DefaultCatalogID string `json:"default_catalog_id"`
			}
			if err := json.Unmarshal(conn.Credentials, &wabaCfg); err == nil {
				if wabaCfg.DefaultCatalogID != "" {
					catalogID = wabaCfg.DefaultCatalogID
				}
			}
		}

		if catalogID == "" {
			return nil, ErrMissingCatalogID
		}

		if req.Product == nil {
			req.Product = &domain.ProductPayload{}
		}
		req.Product.CatalogID = catalogID

		if req.Type == "" {
			if len(req.Product.Sections) > 0 {
				req.Type = domain.MessageTypeProductList
			} else {
				req.Type = domain.MessageTypeProduct
			}
		}

		pDetails := domain.ValidateProductPayload(req.Product, req.Type)
		if len(pDetails) > 0 {
			return nil, &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "invalid_product_payload",
					Message: "request body validation failed",
					Details: pDetails,
				},
			}
		}
	}

	fallbackBehavior := req.FallbackBehavior
	if fallbackBehavior == "" {
		fallbackBehavior = string(domain.FallbackBehaviorDegrade)
	}

	// 5. Construct and Publish QueueMessage
	qMsg := &domain.QueueMessage{
		WorkspaceID:      workspaceID,
		ConnectionID:     conn.ID,
		SenderIdentity:   conn.SenderIdentity,
		TraceID:          traceID,
		To:               req.To,
		Channel:          conn.Channel,
		Body:             req.Body,
		Media:            req.Media,
		Metadata:         req.Metadata,
		TTLSeconds:       req.TTLSeconds,
		QueuedAt:         time.Now().UTC(),
		FallbackChannels: req.FallbackChannels,
		TemplateName:     req.TemplateName,
		Language:         req.Language,
		Components:       req.Components,
		Interactive:      req.Interactive,
		ChannelOverrides: req.ChannelOverrides,
		FallbackBehavior: fallbackBehavior,
		Type:             req.Type,
		Product:          req.Product,
	}

	if p.publisher != nil {
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("failed to marshal message", "error", err, "trace_id", traceID)
			return nil, err
		}

		if err := p.publisher.Publish(ctx, "messages.outbound", payload, traceID); err != nil {
			slog.Error("failed to publish message", "error", err, "trace_id", traceID)
			return nil, err
		}
	}

	// 5.5 Shared Team Inbox Tracking: Upsert Chat & Append Outbound ChatMessage
	if p.chatRepo != nil && conn != nil {
		var contact *domain.Contact
		chat := preloadedChat

		if p.contactRepo != nil {
			if chat != nil && chat.ContactID != uuid.Nil {
				contact, _ = p.contactRepo.GetByID(ctx, workspaceID, chat.ContactID)
			}
			if contact == nil && qMsg.To != "" {
				contact, _ = p.contactRepo.ResolveContact(ctx, workspaceID, conn.Channel, qMsg.To, "", "", qMsg.To)
			}
		}

		if chat == nil && contact != nil {
			chat, _ = p.chatRepo.FindOrCreateChat(ctx, workspaceID, &conn.ID, contact.ID)
		}

		if chat != nil {
			senderName := strings.TrimSpace(req.SenderName)
			if senderName == "" && req.Metadata != nil {
				senderName = strings.TrimSpace(req.Metadata["sender_name"])
			}

			rawSenderType := strings.TrimSpace(req.SenderType)
			if rawSenderType == "" && req.Metadata != nil {
				rawSenderType = strings.TrimSpace(req.Metadata["sender_type"])
			}

			var senderType domain.SenderType
			switch strings.ToLower(rawSenderType) {
			case "human", "human_agent", "operator":
				senderType = domain.SenderTypeHumanAgent
			case "ai", "ai_agent", "bot":
				senderType = domain.SenderTypeAIAgent
			case "system":
				senderType = domain.SenderTypeSystem
			case "contact":
				senderType = domain.SenderTypeContact
			case "":
				if req.DisableAI {
					senderType = domain.SenderTypeHumanAgent
				} else if senderName != "" {
					senderType = domain.SenderTypeAIAgent
				} else {
					senderType = domain.SenderTypeHumanAgent
				}
			default:
				senderType = domain.SenderType(rawSenderType)
			}

			var replyToUID *string
			if req.ReplyToUID != nil && strings.TrimSpace(*req.ReplyToUID) != "" {
				trimmed := strings.TrimSpace(*req.ReplyToUID)
				replyToUID = &trimmed
			} else if req.Metadata != nil {
				if rUID := strings.TrimSpace(req.Metadata["reply_to_uid"]); rUID != "" {
					replyToUID = &rUID
				}
			}

			// Reopen snoozed or closed chat on outbound conversational activity
			if chat.Status == domain.ChatStatusSnoozed || chat.Status == domain.ChatStatusClosed {
				_ = p.chatRepo.UpdateChatStatus(ctx, workspaceID, chat.ID, string(domain.ChatStatusOpen))
				chat.Status = domain.ChatStatusOpen
				chat.SnoozedUntil = nil
				if p.contactRepo != nil && contact != nil {
					_ = p.contactRepo.ReopenThread(ctx, workspaceID, contact.ID)
				}
			}

			shouldDisableAI := req.DisableAI || senderType == domain.SenderTypeHumanAgent
			if shouldDisableAI {
				_ = p.chatRepo.SetAIDisabled(ctx, workspaceID, chat.ID, true)
				if p.contactRepo != nil {
					targetContactID := chat.ContactID
					if contact != nil && contact.ID != uuid.Nil {
						targetContactID = contact.ID
					}
					if targetContactID != uuid.Nil {
						now := qMsg.QueuedAt
						_ = p.contactRepo.UpdateBotState(ctx, workspaceID, targetContactID, false, &now)
					}
				}

				if p.publisher != nil {
					targetContactID := chat.ContactID
					if contact != nil && contact.ID != uuid.Nil {
						targetContactID = contact.ID
					}
					handoffMap := map[string]interface{}{
						"event":        "chat.handoff.human_takeover",
						"workspace_id": workspaceID.String(),
						"chat_id":      chat.ID.String(),
						"contact_id":   targetContactID.String(),
						"trace_id":     traceID,
						"timestamp":    qMsg.QueuedAt.Format(time.RFC3339),
					}
					if senderName != "" {
						handoffMap["sender_name"] = senderName
					}
					handoffPayload, _ := json.Marshal(handoffMap)
					_ = p.publisher.Publish(ctx, "chat.handoff.human_takeover", handoffPayload, traceID)
					_ = p.publisher.Publish(ctx, fmt.Sprintf("chat.handoff.%s", workspaceID.String()), handoffPayload, traceID)
				}
			}

			_ = p.chatRepo.TouchLastMessageAt(ctx, workspaceID, chat.ID, qMsg.QueuedAt)

			bodyText := qMsg.Body
			if bodyText == "" && qMsg.TemplateName != "" {
				if qMsg.Language != "" {
					bodyText = fmt.Sprintf("[Template: %s (%s)]", qMsg.TemplateName, qMsg.Language)
				} else {
					bodyText = fmt.Sprintf("[Template: %s]", qMsg.TemplateName)
				}
			}

			var mediaURL, mediaType *string
			if qMsg.Media != nil {
				if qMsg.Media.MediaURL != "" {
					mediaURL = &qMsg.Media.MediaURL
				}
				if qMsg.Media.MediaType != "" {
					mediaType = &qMsg.Media.MediaType
				}
			}

			msgMetadata := make(map[string]interface{})
			for k, v := range req.Metadata {
				msgMetadata[k] = v
			}
			if conn.Channel != "" {
				if _, ok := msgMetadata["channel"]; !ok {
					msgMetadata["channel"] = conn.Channel
				}
			}
			if conn.SenderIdentity != "" {
				if _, ok := msgMetadata["sender_identity"]; !ok {
					msgMetadata["sender_identity"] = conn.SenderIdentity
				}
			}
			if qMsg.To != "" {
				if _, ok := msgMetadata["to"]; !ok {
					msgMetadata["to"] = qMsg.To
				}
			}
			if qMsg.TemplateName != "" {
				msgMetadata["template_name"] = qMsg.TemplateName
				msgMetadata["language"] = qMsg.Language
				msgMetadata["type"] = "template"
			} else if qMsg.Type != "" {
				if _, ok := msgMetadata["type"]; !ok {
					msgMetadata["type"] = qMsg.Type
				}
			}
			if qMsg.Product != nil {
				if prodBytes, pErr := json.Marshal(qMsg.Product); pErr == nil {
					msgMetadata["product_json"] = string(prodBytes)
				}
			}

			chatMsg := &domain.ChatMessage{
				ChatID:      chat.ID,
				WorkspaceID: workspaceID,
				UID:         traceID,
				Direction:   domain.DirectionOutbound,
				SenderType:  senderType,
				SenderName:  senderName,
				SenderID:    conn.SenderIdentity,
				Body:        bodyText,
				MediaURL:    mediaURL,
				MediaType:   mediaType,
				IsPrivate:   false,
				Reactions:   []domain.Reaction{},
				ReplyToUID:  replyToUID,
				Metadata:    msgMetadata,
				CreatedAt:   qMsg.QueuedAt,
			}
			_ = p.chatRepo.AddChatMessage(ctx, chatMsg)
		}
	}

	// 6. Increment queue depth counter
	if p.tracker != nil && workspaceID != uuid.Nil {
		p.tracker.Increment(workspaceID)
	}

	return qMsg, nil
}
