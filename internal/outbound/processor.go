package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

	// 4.4 Smart Session Window Fallback (WABA freeform messages only)
	if req.TemplateName == "" && conn.Channel == "whatsapp_cloud" && p.windowChecker != nil {
		status, err := p.windowChecker.IsWindowOpen(ctx, workspaceID, req.To, "whatsapp_cloud", conn.SenderIdentity, 0)
		if err != nil {
			slog.Warn("outbound processor: window checker error", "error", err, "trace_id", traceID)
		} else if status != nil && !status.Open {
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

			// Validate and normalize parameters against cached components
			var tmplComponents []domain.TemplateComponent
			if err := json.Unmarshal(tmpl.Components, &tmplComponents); err != nil {
				slog.Error("outbound processor: failed to parse cached template components", "error", err, "trace_id", traceID)
			} else {
				for i := range req.Components {
					// Normalize params
					normalized, err := NormalizeTemplateParams(req.Components[i].Parameters)
					if err != nil {
						return nil, &ErrInvalidTemplateParameters{Message: err.Error()}
					}
					req.Components[i].Parameters = normalized

					// Match against cached components to validate count if applicable
					for _, c := range tmplComponents {
						if c.Type == req.Components[i].Type {
							// If we could extract the expected variable count, we would check it here.
							// For now, we simply trust the normalized parameters.
							_ = c
						}
					}
				}
			}
		}
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
		FallbackBehavior: req.FallbackBehavior,
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

	// 6. Increment queue depth counter
	if p.tracker != nil && workspaceID != uuid.Nil {
		p.tracker.Increment(workspaceID)
	}

	return qMsg, nil
}
