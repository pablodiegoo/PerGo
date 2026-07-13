package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
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

// MediaUploader defines the port for S3 file storage uploads.
type MediaUploader interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
}

// RouteResolver defines the port for connection routing resolution.
type RouteResolver interface {
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
}

// Publisher defines the port for NATS JetStream publishing.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// MediaDownloader defines the function signature for downloading and validating media.
type MediaDownloader func(ctx context.Context, url string, maxBytes int64) (*storage.DownloadResult, error)

// Processor is the concrete implementation of outbound message ingestion.
type Processor struct {
	tracker    QueueDepthChecker
	uploader   MediaUploader
	resolver   RouteResolver
	publisher  Publisher
	downloadFn MediaDownloader
}

// NewProcessor creates a new OutboundProcessor implementation.
func NewProcessor(
	tracker QueueDepthChecker,
	uploader MediaUploader,
	resolver RouteResolver,
	publisher Publisher,
) *Processor {
	return &Processor{
		tracker:    tracker,
		uploader:   uploader,
		resolver:   resolver,
		publisher:  publisher,
		downloadFn: storage.DownloadAndValidate,
	}
}

// SetDownloader overrides the media downloader function (used in testing).
func (p *Processor) SetDownloader(downloadFn MediaDownloader) {
	p.downloadFn = downloadFn
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
		if p.uploader == nil {
			slog.Error("S3 storage client is not configured for media downloads", "trace_id", traceID)
			return nil, &MediaError{
				Code:    "internal_error",
				Message: "media storage configuration error",
			}
		}

		res, err := p.downloadFn(ctx, req.Media.MediaURL, 25000000)
		if err != nil {
			if errors.Is(err, storage.ErrMediaSizeExceeded) {
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

		// Store media in S3
		key := workspaceID.String() + "/" + res.Hash + "." + res.Extension
		if err := p.uploader.Upload(ctx, key, res.Bytes, res.ContentType); err != nil {
			slog.Error("failed to upload media to S3", "error", err, "trace_id", traceID, "key", key)
			return nil, &MediaError{
				Code:    "internal_error",
				Message: "failed to store media file",
				Err:     err,
			}
		}

		// Rewire the message payload's MediaURL to the internal proxy URL
		req.Media.MediaURL = "/media/" + workspaceID.String() + "/" + res.Hash + "." + res.Extension
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
		if conn.Channel != req.Channel {
			return nil, &ValidationError{
				Response: &domain.ErrorResponse{
					Code:    "route_not_found",
					Message: "connection channel does not match request channel",
				},
			}
		}
	} else {
		conn, err = p.resolver.GetDefaultChannelConnection(ctx, workspaceID, req.Channel)
		if err != nil {
			return nil, &RouteError{
				Message: "no default connection found for channel",
				Err:     err,
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
		Channel:          req.Channel,
		Body:             req.Body,
		Media:            req.Media,
		Metadata:         req.Metadata,
		TTLSeconds:       req.TTLSeconds,
		QueuedAt:         time.Now().UTC(),
		FallbackChannels: req.FallbackChannels,
		TemplateName:     req.TemplateName,
		Language:         req.Language,
		Components:       req.Components,
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
