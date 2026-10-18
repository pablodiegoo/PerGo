package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
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

// DebounceCanceller cancels active inbound debounce buffers on human takeover.
type DebounceCanceller interface {
	Cancel(ctx context.Context, workspaceID, chatID uuid.UUID) error
}

// Processor is the concrete implementation of outbound message ingestion.
type Processor struct {
	tracker          QueueDepthChecker
	resolver         RouteResolver
	publisher        Publisher
	mediaEngine      media.Engine
	windowChecker    *session.WindowChecker
	templateRepo     *repository.WABATemplateRepository
	chatRepo         *repository.ChatRepository
	contactRepo      *repository.ContactRepository
	debouncer        DebounceCanceller
	activity         *activity.Recorder
	explicitActivity bool
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

// SetActivity configures the Chat Activity recorder used to record outbound turns.
func (p *Processor) SetActivity(rec *activity.Recorder) *Processor {
	p.activity = rec
	p.explicitActivity = rec != nil
	return p
}

func (p *Processor) rebuildDefaultActivity() {
	if p.explicitActivity {
		return
	}
	if p.chatRepo != nil && p.chatRepo.Pool() != nil {
		p.activity = activity.New(p.chatRepo.Pool(), p.publisher, p.debouncer, nil)
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
	p.rebuildDefaultActivity()
	return p
}

// SetContactRepository configures the ContactRepository for recipient profile resolution.
func (p *Processor) SetContactRepository(r *repository.ContactRepository) *Processor {
	p.contactRepo = r
	return p
}

// SetDebounceCanceller configures the DebounceCanceller for human takeover debounce cancellation.
func (p *Processor) SetDebounceCanceller(dc DebounceCanceller) *Processor {
	p.debouncer = dc
	p.rebuildDefaultActivity()
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
	var windowStatus *session.WindowStatus
	if req.TemplateName == "" && conn.Channel == "whatsapp_cloud" && p.windowChecker != nil {
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
			windowStatus = s
		}
	}

	if req.TemplateName == "" && conn.Channel == "whatsapp_cloud" && windowStatus != nil && !windowStatus.Open {
		if req.StrictServiceWindow && p.activity == nil {
			return nil, &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "SESSION_WINDOW_EXPIRED",
					Message: "free-form message blocked: 24h customer service window is closed; use an approved WABA template",
				},
				Err: &session.SessionWindowError{
					Status: windowStatus,
					Source: "ingestion",
				},
			}
		} else if !req.StrictServiceWindow {
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
					Status: windowStatus,
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

	// 5. Construct QueueMessage, record Chat Activity (single outbound writer), and Publish
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

	if p.activity != nil && conn != nil {
		if err := p.recordOutboundTurn(ctx, workspaceID, traceID, req, conn, qMsg, windowStatus); err != nil {
			return nil, err
		}
	}

	if p.publisher != nil {
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("failed to marshal message", "error", err, "trace_id", traceID)
			if p.activity != nil {
				_ = p.activity.DispatchFailed(ctx, workspaceID, traceID)
			}
			return nil, err
		}

		if err := p.publisher.Publish(ctx, "messages.outbound", payload, traceID); err != nil {
			slog.Error("failed to publish message", "error", err, "trace_id", traceID)
			if p.activity != nil {
				_ = p.activity.DispatchFailed(ctx, workspaceID, traceID)
			}
			return nil, err
		}
	}

	// 6. Increment queue depth counter
	if p.tracker != nil && workspaceID != uuid.Nil {
		p.tracker.Increment(workspaceID)
	}

	return qMsg, nil
}

func (p *Processor) recordOutboundTurn(
	ctx context.Context,
	workspaceID uuid.UUID,
	traceID string,
	req *domain.CreateMessageRequest,
	conn *repository.Connection,
	qMsg *domain.QueueMessage,
	windowStatus *session.WindowStatus,
) error {
	senderName := strings.TrimSpace(req.SenderName)
	if senderName == "" && req.Metadata != nil {
		senderName = strings.TrimSpace(req.Metadata["sender_name"])
	}

	rawSenderType := strings.TrimSpace(req.SenderType)
	if rawSenderType == "" && req.Metadata != nil {
		rawSenderType = strings.TrimSpace(req.Metadata["sender_type"])
	}

	senderType := domain.NormalizeSenderType(rawSenderType)
	if senderType == "" {
		senderType = domain.SenderTypeAIAgent
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

	var windowOpen *bool
	if windowStatus != nil && (!windowStatus.ExpiresAt.IsZero() || windowStatus.Open) {
		open := windowStatus.Open
		windowOpen = &open
	}

	var ref activity.ChatRef
	if req.ChatID != nil && *req.ChatID != uuid.Nil {
		ref = activity.ChatID(*req.ChatID).WithChannel(conn.Channel)
	} else {
		ref = activity.ChatAt(conn.ID, qMsg.To).WithChannel(conn.Channel)
	}

	turn := activity.OutboundTurn{
		Chat:                ref,
		UID:                 traceID,
		SenderType:          senderType,
		SenderName:          senderName,
		SenderID:            conn.SenderIdentity,
		ReplyToUID:          replyToUID,
		Body:                qMsg.Body,
		Media:               qMsg.Media,
		Product:             qMsg.Product,
		TemplateName:        qMsg.TemplateName,
		Language:            qMsg.Language,
		MessageType:         qMsg.Type,
		Channel:             conn.Channel,
		To:                  qMsg.To,
		Metadata:            req.Metadata,
		StrictServiceWindow: req.StrictServiceWindow,
		WindowOpen:          windowOpen,
		At:                  qMsg.QueuedAt,
	}

	_, err := p.activity.Outbound(ctx, workspaceID, turn)
	if errors.Is(err, activity.ErrChatNotFound) && req.ChatID != nil && qMsg.To != "" {
		turn.Chat = activity.ChatAt(conn.ID, qMsg.To).WithChannel(conn.Channel)
		_, err = p.activity.Outbound(ctx, workspaceID, turn)
	}
	if err != nil {
		if errors.Is(err, activity.ErrServiceWindowClosed) {
			return &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "SESSION_WINDOW_EXPIRED",
					Message: "free-form message blocked: 24h customer service window is closed; use an approved WABA template",
				},
				Err: &session.SessionWindowError{
					Status: windowStatus,
					Source: "ingestion",
				},
			}
		}
		if req.StrictServiceWindow {
			return err
		}
		slog.Error("outbound processor: failed to record chat activity", "error", err, "trace_id", traceID)
	}
	return nil
}

