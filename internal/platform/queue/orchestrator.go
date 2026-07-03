package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// QueueDepthTracker defines the interface for workspace-scoped queue depth tracking.
type QueueDepthTracker interface {
	Decrement(workspaceID uuid.UUID)
}

const (
	orchDefaultMaxRetries  = 5
	orchDefaultMaxBackoff  = 60 * time.Second
	orchDefaultBaseBackoff = 1 * time.Second
)

// DispatchOrchestrator owns all outbound dispatch logic: idempotency,
// TTL enforcement, fallback routing, channel dispatch, audit, and
// webhook events. Tests exercise the full pipeline through Process.
type DispatchOrchestrator struct {
	dispatchers  *channel.Registry
	dispatchRepo *repository.MessageDispatchRepository
	publisher    *JetStreamPublisher
	queueDepth   QueueDepthTracker
	auditWriter  audit.Writer

	maxRetries int
	maxBackoff time.Duration

	// dispatched tracks trace IDs already processed (in-memory dedup fast path).
	dispatched sync.Map // traceID string → struct{}
}

// NewDispatchOrchestrator creates an orchestrator with the given adapters.
func NewDispatchOrchestrator(
	dispatchers *channel.Registry,
	dispatchRepo *repository.MessageDispatchRepository,
	publisher *JetStreamPublisher,
	queueDepth QueueDepthTracker,
	auditWriter audit.Writer,
	maxRetries int,
	maxBackoff time.Duration,
) *DispatchOrchestrator {
	if maxRetries <= 0 {
		maxRetries = orchDefaultMaxRetries
	}
	if maxBackoff <= 0 {
		maxBackoff = orchDefaultMaxBackoff
	}

	return &DispatchOrchestrator{
		dispatchers:  dispatchers,
		dispatchRepo: dispatchRepo,
		publisher:    publisher,
		queueDepth:   queueDepth,
		auditWriter:  auditWriter,
		maxRetries:   maxRetries,
		maxBackoff:   maxBackoff,
	}
}

// Process handles a single message through the full dispatch pipeline.
// The caller is responsible for JSON deserialization and trace context
// injection. The msg port abstracts JetStream ack/nak operations.
func (o *DispatchOrchestrator) Process(
	ctx context.Context,
	msg DispatchMessage,
	qMsg *domain.QueueMessage,
	attempt int,
) error {
	traceID := qMsg.TraceID
	workspaceID := qMsg.WorkspaceID

	// --- Database State Check / Idempotency ---
	var dispatch *repository.MessageDispatch
	if o.dispatchRepo != nil && traceID != "" {
		var err error
		dispatch, err = o.dispatchRepo.GetOrCreateDispatch(ctx, workspaceID, traceID, qMsg.Channel)
		if err != nil {
			slog.Error("orchestrator: failed to get/create dispatch state", "error", err, "trace_id", traceID)
			o.handleFailure(msg, workspaceID, traceID, attempt)
			return err
		}

		if dispatch.Status == "sent" {
			slog.Info("orchestrator: duplicate delivery prevented (status already sent)",
				"trace_id", traceID,
			)
			o.ack(msg, workspaceID)
			return nil
		}

		if dispatch.Status == "queued" {
			o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "queued", qMsg.Channel, nil)
		}
	}

	// --- In-memory dedup fast path ---
	if traceID != "" {
		if _, loaded := o.dispatched.LoadOrStore(traceID, struct{}{}); loaded {
			slog.Info("orchestrator: duplicate delivery prevented (in-memory)", "trace_id", traceID)
			o.ack(msg, workspaceID)
			return nil
		}
	}

	// --- TTL enforcement ---
	if o.isExpired(qMsg) {
		slog.Warn("orchestrator: message expired (TTL), dropping", "trace_id", traceID)
		if o.dispatchRepo != nil && dispatch != nil {
			errMsg := "message expired (TTL)"
			_ = o.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed", dispatch.CurrentChannel, dispatch.FallbackIndex, &errMsg)
			o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed", dispatch.CurrentChannel, &errMsg)
		}
		o.ack(msg, workspaceID)
		return nil
	}

	// --- Fallback Loop ---
	startIndex := 0
	if dispatch != nil {
		startIndex = dispatch.FallbackIndex
	}

	allChannels := append([]string{qMsg.Channel}, qMsg.FallbackChannels...)
	if startIndex < 0 || startIndex >= len(allChannels) {
		slog.Error("orchestrator: fallback index out of bounds", "index", startIndex, "channels_count", len(allChannels), "trace_id", traceID)
		o.ack(msg, workspaceID)
		return nil
	}

	var finalErr error
	var lastChannel string
	currentIndex := startIndex

	for i := startIndex; i < len(allChannels); i++ {
		channelName := allChannels[i]
		lastChannel = channelName
		currentIndex = i

		if o.dispatchRepo != nil && dispatch != nil {
			err := o.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "sending", channelName, i, nil)
			if err != nil {
				slog.Error("orchestrator: failed to update status to sending", "error", err, "trace_id", traceID)
				o.handleFailure(msg, workspaceID, traceID, attempt)
				return err
			}
			o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "sending", channelName, nil)
		}

		slog.Info("orchestrator: attempting dispatch", "channel", channelName, "trace_id", traceID, "index", i, "attempt", attempt)
		respStr, err := o.dispatchToChannel(ctx, channelName, qMsg)
		if err == nil {
			if o.dispatchRepo != nil && dispatch != nil {
				_ = o.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "sent", channelName, i, nil)
				o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "sent", channelName, nil)
			}
			slog.Info("orchestrator: message dispatched successfully", "channel", channelName, "trace_id", traceID)

			if o.auditWriter != nil {
				auditPayload := map[string]any{
					"request":  qMsg,
					"response": respStr,
					"status":   "sent",
				}
				payloadBytes, _ := json.Marshal(auditPayload)
				_ = o.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
			}

			o.ack(msg, workspaceID)
			return nil
		}

		finalErr = err
		if channel.IsTerminal(err) {
			slog.Warn("orchestrator: terminal error, triggering fallback", "channel", channelName, "error", err, "trace_id", traceID)

			if o.auditWriter != nil {
				auditPayload := map[string]any{
					"request":  qMsg,
					"response": respStr,
					"status":   "failed",
					"error":    err.Error(),
				}
				payloadBytes, _ := json.Marshal(auditPayload)
				_ = o.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
			}
			continue
		}

		// Transient error — NAK with delay, JetStream retries
		errMsg := err.Error()
		if o.dispatchRepo != nil && dispatch != nil {
			_ = o.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed_transient", channelName, i, &errMsg)
			o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed_transient", channelName, &errMsg)
		}
		slog.Warn("orchestrator: transient error, NAK for retry", "channel", channelName, "error", err, "trace_id", traceID)

		if o.auditWriter != nil {
			auditPayload := map[string]any{
				"request":  qMsg,
				"response": respStr,
				"status":   "failed_transient",
				"error":    err.Error(),
			}
			payloadBytes, _ := json.Marshal(auditPayload)
			_ = o.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
		}

		o.handleFailure(msg, workspaceID, traceID, attempt)
		return err
	}

	// All channels exhausted terminally
	errMsg := "all channels failed: " + finalErr.Error()
	if o.dispatchRepo != nil && dispatch != nil {
		_ = o.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed", lastChannel, currentIndex, &errMsg)
		o.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed", lastChannel, &errMsg)
	}
	slog.Error("orchestrator: all fallback channels exhausted (terminal failure)", "error", finalErr, "trace_id", traceID)
	o.ack(msg, workspaceID)
	return finalErr
}

// dispatchToChannel looks up the channel dispatcher and sends.
func (o *DispatchOrchestrator) dispatchToChannel(ctx context.Context, channelName string, qMsg *domain.QueueMessage) (string, error) {
	if o.dispatchers == nil {
		return "", nil
	}

	dispatcher, ok := o.dispatchers.Get(channelName)
	if !ok {
		slog.Warn("orchestrator: no dispatcher for channel, skipping", "channel", channelName, "trace_id", qMsg.TraceID)
		return "", nil
	}

	return dispatcher.Dispatch(ctx, &channel.MessagePayload{
		MessageID:      qMsg.TraceID,
		ConnectionID:   qMsg.ConnectionID,
		SenderIdentity: qMsg.SenderIdentity,
		TraceID:        qMsg.TraceID,
		To:             qMsg.To,
		Channel:        channelName,
		Body:           qMsg.Body,
		Media:          qMsg.Media,
		Metadata:       qMsg.Metadata,
		TemplateName:   qMsg.TemplateName,
		Language:       qMsg.Language,
		Components:     qMsg.Components,
	})
}

// ack ACKs the message and decrements workspace queue depth.
func (o *DispatchOrchestrator) ack(msg DispatchMessage, workspaceID uuid.UUID) {
	_ = msg.Ack()
	if o.queueDepth != nil && workspaceID != (uuid.UUID{}) {
		o.queueDepth.Decrement(workspaceID)
	}
}

// handleFailure NAKs with exponential backoff or acks as terminal.
func (o *DispatchOrchestrator) handleFailure(msg DispatchMessage, workspaceID uuid.UUID, traceID string, attempt int) {
	if attempt >= o.maxRetries {
		slog.Error("orchestrator: terminal failure after max retries", "trace_id", traceID, "attempts", attempt, "max_retries", o.maxRetries)
		o.ack(msg, workspaceID)
		return
	}

	delay := time.Duration(math.Pow(2, float64(attempt))) * orchDefaultBaseBackoff
	if delay > o.maxBackoff {
		delay = o.maxBackoff
	}

	slog.Warn("orchestrator: retrying with backoff", "trace_id", traceID, "attempt", attempt+1, "backoff", delay)

	if nakErr := msg.NakWithDelay(delay); nakErr != nil {
		slog.Error("orchestrator: failed to NAK message", "error", nakErr, "trace_id", traceID)
		o.ack(msg, workspaceID)
	}
}

// isExpired checks if the message TTL has elapsed using the already-parsed QueueMessage.
func (o *DispatchOrchestrator) isExpired(qMsg *domain.QueueMessage) bool {
	if qMsg.TTLSeconds == nil || *qMsg.TTLSeconds <= 0 {
		return false
	}
	if qMsg.QueuedAt.IsZero() {
		return false
	}
	expiry := qMsg.QueuedAt.Add(time.Duration(*qMsg.TTLSeconds) * time.Second)
	return time.Now().UTC().After(expiry)
}

// retryAttempt extracts the retry attempt count from message headers.
func retryAttempt(msg DispatchMessage) int {
	headers := msg.Headers()
	if headers == nil {
		return 0
	}
	val := headers["X-Retry-Attempt"]
	if val == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
		return n
	}
	return 0
}

// publishWebhookEvent creates and publishes a status event to NATS.
func (o *DispatchOrchestrator) publishWebhookEvent(ctx context.Context, workspaceID uuid.UUID, traceID string, dispatchID uuid.UUID, event string, channelName string, errMsg *string) {
	if o.publisher == nil {
		return
	}

	evt := struct {
		Event       string `json:"event"`
		TraceID     string `json:"trace_id"`
		MessageID   string `json:"message_id"`
		Channel     string `json:"channel"`
		Timestamp   string `json:"timestamp"`
		WorkspaceID string `json:"workspace_id"`
		Error       string `json:"error,omitempty"`
	}{
		Event:       event,
		TraceID:     traceID,
		MessageID:   dispatchID.String(),
		Channel:     channelName,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: workspaceID.String(),
	}
	if errMsg != nil {
		evt.Error = *errMsg
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("orchestrator: failed to marshal webhook event", "error", err, "trace_id", traceID)
		return
	}

	if err := o.publisher.Publish(ctx, "webhooks.events", payload, traceID); err != nil {
		slog.Error("orchestrator: failed to publish webhook event", "error", err, "trace_id", traceID)
	}
}
