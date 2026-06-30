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
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	defaultMaxRetries  = 5
	defaultMaxBackoff  = 60 * time.Second
	defaultBaseBackoff = 1 * time.Second
)

// QueueDepthTracker defines the interface needed to decrement queue depth.
type QueueDepthTracker interface {
	Decrement(workspaceID uuid.UUID)
}

// Worker reads messages from a JetStream consumer and dispatches them.
// It supports retry with exponential backoff, TTL enforcement, and
// delivery deduplication.
type Worker struct {
	consumer   jetstream.Consumer
	cancel     context.CancelFunc
	done       chan struct{}

	maxRetries int
	maxBackoff time.Duration

	// dispatchers provides channel-specific dispatchers.
	// When nil or a channel has no registered dispatcher, messages
	// are logged and acked (no-op).
	dispatchers *channel.Registry

	// dispatchRepo manages database status tracking and fallbacks.
	dispatchRepo *repository.MessageDispatchRepository

	// publisher is used to publish status updates to webhooks.events.
	publisher *JetStreamPublisher

	// queueDepth tracks workspace-scoped queue depth.
	queueDepth QueueDepthTracker

	// auditWriter is the writer for audit events.
	auditWriter audit.Writer

	// dispatched tracks message IDs already processed (in-memory dedup).
	dispatched sync.Map // message_id string → struct{}
}

// NewWorker starts a goroutine that reads messages from consumer and processes them.
// The goroutine stops when ctx is cancelled. Call Stop() to initiate shutdown.
// dispatchers may be nil for log-only mode (useful in tests).
func NewWorker(ctx context.Context, consumer jetstream.Consumer, maxRetries int, maxBackoff time.Duration, dispatchers *channel.Registry, dispatchRepo *repository.MessageDispatchRepository, publisher *JetStreamPublisher, queueDepth QueueDepthTracker, auditWriter audit.Writer) *Worker {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxBackoff
	}

	ctx, cancel := context.WithCancel(ctx)
	w := &Worker{
		consumer:     consumer,
		cancel:       cancel,
		done:         make(chan struct{}),
		maxRetries:   maxRetries,
		maxBackoff:   maxBackoff,
		dispatchers:  dispatchers,
		dispatchRepo: dispatchRepo,
		publisher:    publisher,
		queueDepth:   queueDepth,
		auditWriter:  auditWriter,
	}

	go w.run(ctx)
	return w
}

// run is the main consumer loop. It reads messages from the consumer,
// checks TTL and dedup, then dispatches. On failure, NAKs with
// exponential backoff up to maxRetries.
func (w *Worker) run(ctx context.Context) {
	defer close(w.done)

	msgCtx, err := w.consumer.Messages()
	if err != nil {
		slog.Error("worker: failed to create messages context", "error", err)
		return
	}
	defer msgCtx.Stop()

	slog.Info("message worker started",
		"consumer", w.consumer.CachedInfo().Config.Name,
		"max_retries", w.maxRetries,
		"max_backoff", w.maxBackoff,
	)

	for {
		msg, err := msgCtx.Next()
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("message worker stopped")
				return
			}
			slog.Error("worker: failed to get next message, recreating messages context", "error", err)
			msgCtx.Stop()

			// Sleep to prevent tight reconnect loops
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}

			newMsgCtx, err := w.consumer.Messages()
			if err != nil {
				slog.Error("worker: failed to recreate messages context", "error", err)
				continue
			}
			msgCtx = newMsgCtx
			continue
		}

		w.processMessage(ctx, msg)
	}
}

// ackMessage ACKs the JetStream message and decrements the workspace queue depth.
func (w *Worker) ackMessage(msg jetstream.Msg, workspaceID uuid.UUID) {
	_ = msg.Ack()
	if w.queueDepth != nil && workspaceID != (uuid.UUID{}) {
		w.queueDepth.Decrement(workspaceID)
	}
}

// processMessage handles a single message: dedup check, TTL check, dispatch.
func (w *Worker) processMessage(ctx context.Context, msg jetstream.Msg) {
	// Parse payload as domain.QueueMessage
	var qMsg domain.QueueMessage
	if err := json.Unmarshal(msg.Data(), &qMsg); err != nil {
		slog.Error("worker: failed to unmarshal payload", "error", err)
		_ = msg.Ack()
		return
	}

	traceID := qMsg.TraceID
	if traceID == "" {
		if headers := msg.Headers(); headers != nil {
			traceID = headers.Get("Nats-Msg-Id")
		}
	}
	workspaceID := qMsg.WorkspaceID
	ctx = tenant.WithWorkspaceID(ctx, workspaceID)

	// Extract retry attempt from header
	attempt := w.retryAttempt(msg)

	data := msg.Data()
	preview := string(data)
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}

	// --- Database State Check / Idempotency ---
	var dispatch *repository.MessageDispatch
	if w.dispatchRepo != nil && traceID != "" {
		var err error
		dispatch, err = w.dispatchRepo.GetOrCreateDispatch(ctx, workspaceID, traceID, qMsg.Channel)
		if err != nil {
			slog.Error("worker: failed to get/create dispatch state", "error", err, "trace_id", traceID)
			w.handleFailure(msg, workspaceID, traceID, attempt)
			return
		}

		if dispatch.Status == "sent" {
			slog.Info("worker: duplicate delivery prevented (status already sent)",
				"trace_id", traceID,
				"subject", msg.Subject(),
			)
			_ = msg.Ack()
			return
		}

		// If it was just created (status "queued"), publish the "queued" status webhook event
		if dispatch.Status == "queued" {
			w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "queued", qMsg.Channel, nil)
		}
	}

	// --- In-memory dedup fast path ---
	if traceID != "" {
		if _, loaded := w.dispatched.LoadOrStore(traceID, struct{}{}); loaded {
			slog.Info("worker: duplicate delivery prevented (in-memory)",
				"trace_id", traceID,
				"subject", msg.Subject(),
			)
			_ = msg.Ack()
			return
		}
	}

	// --- TTL enforcement: drop expired messages ---
	if w.isExpired(msg) {
		slog.Warn("worker: message expired (TTL), dropping",
			"trace_id", traceID,
			"subject", msg.Subject(),
			"payload_preview", preview,
		)
		if w.dispatchRepo != nil && dispatch != nil {
			errMsg := "message expired (TTL)"
			_ = w.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed", dispatch.CurrentChannel, dispatch.FallbackIndex, &errMsg)
			w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed", dispatch.CurrentChannel, &errMsg)
		}
		w.ackMessage(msg, workspaceID)
		return
	}

	// --- Fallback Loop ---
	startIndex := 0
	if dispatch != nil {
		startIndex = dispatch.FallbackIndex
	}

	allChannels := append([]string{qMsg.Channel}, qMsg.FallbackChannels...)
	if startIndex < 0 || startIndex >= len(allChannels) {
		slog.Error("worker: fallback index out of bounds", "index", startIndex, "channels_count", len(allChannels), "trace_id", traceID)
		w.ackMessage(msg, workspaceID)
		return
	}

	var finalErr error
	var lastChannel string
	currentIndex := startIndex

	for i := startIndex; i < len(allChannels); i++ {
		channelName := allChannels[i]
		lastChannel = channelName
		currentIndex = i

		// Update DB status to 'sending'
		if w.dispatchRepo != nil && dispatch != nil {
			err := w.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "sending", channelName, i, nil)
			if err != nil {
				slog.Error("worker: failed to update status to sending", "error", err, "trace_id", traceID)
				w.handleFailure(msg, workspaceID, traceID, attempt)
				return
			}
			w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "sending", channelName, nil)
		}

		slog.Info("worker: attempting dispatch", "channel", channelName, "trace_id", traceID, "index", i, "attempt", attempt)
		respStr, err := w.dispatchToChannel(ctx, channelName, &qMsg)
		if err == nil {
			// Success! Update DB status to 'sent'
			if w.dispatchRepo != nil && dispatch != nil {
				_ = w.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "sent", channelName, i, nil)
				w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "sent", channelName, nil)
			}
			slog.Info("worker: message dispatched successfully", "channel", channelName, "trace_id", traceID)

			// Write outbound audit log
			if w.auditWriter != nil {
				auditPayload := map[string]any{
					"request":  qMsg,
					"response": respStr,
					"status":   "sent",
				}
				payloadBytes, _ := json.Marshal(auditPayload)
				_ = w.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
			}

			w.ackMessage(msg, workspaceID)
			return
		}

		finalErr = err
		if channel.IsTerminal(err) {
			slog.Warn("worker: terminal error on channel, triggering fallback",
				"channel", channelName,
				"error", err,
				"trace_id", traceID,
			)

			// Write outbound audit log (failure attempt)
			if w.auditWriter != nil {
				auditPayload := map[string]any{
					"request":  qMsg,
					"response": respStr,
					"status":   "failed",
					"error":    err.Error(),
				}
				payloadBytes, _ := json.Marshal(auditPayload)
				_ = w.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
			}

			// Loop continues to next fallback channel
			continue
		} else {
			// Transient error! Update DB status to 'failed_transient' and NAK with delay (trigger JetStream retry)
			errMsg := err.Error()
			if w.dispatchRepo != nil && dispatch != nil {
				_ = w.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed_transient", channelName, i, &errMsg)
				w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed_transient", channelName, &errMsg)
			}
			slog.Warn("worker: transient error on channel, triggering NATS retry",
				"channel", channelName,
				"error", err,
				"trace_id", traceID,
			)

			// Write outbound audit log (failure attempt)
			if w.auditWriter != nil {
				auditPayload := map[string]any{
					"request":  qMsg,
					"response": respStr,
					"status":   "failed_transient",
					"error":    err.Error(),
				}
				payloadBytes, _ := json.Marshal(auditPayload)
				_ = w.auditWriter.Write(audit.NewEvent(workspaceID, traceID, "outbound_message", payloadBytes))
			}

			w.handleFailure(msg, workspaceID, traceID, attempt)
			return
		}
	}

	// Tried all channels and they all failed terminally
	errMsg := "all channels failed: " + finalErr.Error()
	if w.dispatchRepo != nil && dispatch != nil {
		_ = w.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, "failed", lastChannel, currentIndex, &errMsg)
		w.publishWebhookEvent(ctx, workspaceID, traceID, dispatch.ID, "failed", lastChannel, &errMsg)
	}
	slog.Error("worker: all fallback channels exhausted (terminal failure)", "error", finalErr, "trace_id", traceID)
	w.ackMessage(msg, workspaceID)
}

// dispatchToChannel looks up the channel dispatcher and sends.
func (w *Worker) dispatchToChannel(ctx context.Context, channelName string, qMsg *domain.QueueMessage) (string, error) {
	if w.dispatchers == nil {
		return "", nil // no-op mode
	}

	dispatcher, ok := w.dispatchers.Get(channelName)
	if !ok {
		slog.Warn("worker: no dispatcher for channel, skipping",
			"channel", channelName,
			"trace_id", qMsg.TraceID,
		)
		return "", nil // not an error
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
		Components:     convertTemplateComponents(qMsg.Components),
	})
}

func convertTemplateComponents(components []domain.TemplateComponent) []channel.TemplateComponent {
	if components == nil {
		return nil
	}
	res := make([]channel.TemplateComponent, len(components))
	for i, c := range components {
		res[i] = channel.TemplateComponent{
			Type: c.Type,
		}
		if c.Parameters != nil {
			params := make([]channel.TemplateParameter, len(c.Parameters))
			for j, p := range c.Parameters {
				params[j] = channel.TemplateParameter{
					Type: p.Type,
					Text: p.Text,
				}
			}
			res[i].Parameters = params
		}
	}
	return res
}

// handleFailure NAKs the message with exponential backoff or marks as terminal failure.
func (w *Worker) handleFailure(msg jetstream.Msg, workspaceID uuid.UUID, traceID string, attempt int) {
	if attempt >= w.maxRetries {
		// Terminal failure — ack and log
		slog.Error("worker: terminal failure after max retries",
			"trace_id", traceID,
			"subject", msg.Subject(),
			"attempts", attempt,
			"max_retries", w.maxRetries,
		)
		w.ackMessage(msg, workspaceID)
		return
	}

	// Calculate exponential backoff: base * 2^attempt, capped at maxBackoff
	delay := time.Duration(math.Pow(2, float64(attempt))) * defaultBaseBackoff
	if delay > w.maxBackoff {
		delay = w.maxBackoff
	}

	slog.Warn("worker: retrying message with backoff",
		"trace_id", traceID,
		"attempt", attempt+1,
		"backoff", delay,
	)

	// NAK with delay for redelivery
	nakErr := msg.NakWithDelay(delay)
	if nakErr != nil {
		slog.Error("worker: failed to NAK message",
			"error", nakErr,
			"trace_id", traceID,
		)
		// If NAK fails, ack to prevent infinite loop
		w.ackMessage(msg, workspaceID)
	}
}

// retryAttempt extracts the retry attempt count from message headers.
func (w *Worker) retryAttempt(msg jetstream.Msg) int {
	if headers := msg.Headers(); headers != nil {
		val := headers.Get("X-Retry-Attempt")
		if val != "" {
			var n int
			if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

// isExpired checks if the message has exceeded its TTL.
// It looks for queued_at and ttl_seconds in the message metadata.
func (w *Worker) isExpired(msg jetstream.Msg) bool {
	data := msg.Data()

	// Parse minimal fields from the JSON payload to check TTL.
	// The payload is a CreateMessageRequest serialized to JSON.
	type ttlPayload struct {
		TTLSeconds *int `json:"ttl_seconds"`
		QueuedAt   string `json:"queued_at"`
	}

	var p ttlPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return false // can't parse TTL info, don't expire
	}

	if p.TTLSeconds == nil || *p.TTLSeconds <= 0 {
		return false // no TTL set
	}

	queuedAt, err := time.Parse(time.RFC3339Nano, p.QueuedAt)
	if err != nil {
		return false // can't parse queued_at, don't expire
	}

	expiry := queuedAt.Add(time.Duration(*p.TTLSeconds) * time.Second)
	return time.Now().UTC().After(expiry)
}



// Stop cancels the consumer context and waits for the goroutine to finish.
func (w *Worker) Stop() {
	w.cancel()
	<-w.done
}

// publishWebhookEvent creates and publishes a status event to NATS.
func (w *Worker) publishWebhookEvent(ctx context.Context, workspaceID uuid.UUID, traceID string, dispatchID uuid.UUID, event string, channelName string, errMsg *string) {
	if w.publisher == nil {
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
		slog.Error("worker: failed to marshal webhook event", "error", err, "trace_id", traceID)
		return
	}

	// Publish to subject: webhooks.events
	err = w.publisher.Publish(ctx, "webhooks.events", payload, traceID)
	if err != nil {
		slog.Error("worker: failed to publish webhook event to NATS", "error", err, "trace_id", traceID)
	}
}
