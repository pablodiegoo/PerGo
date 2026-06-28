package queue

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.omnigo/internal/repository"
)

type WebhookEvent struct {
	Event       string `json:"event"`
	TraceID     string `json:"trace_id"`
	MessageID   string `json:"message_id"`
	Channel     string `json:"channel"`
	Timestamp   string `json:"timestamp"`
	WorkspaceID string `json:"workspace_id"`
	Error       string `json:"error,omitempty"`
}

type WebhookWorker struct {
	nc              *nats.Conn
	js              jetstream.JetStream
	consumer        jetstream.Consumer
	inboundConsumer jetstream.Consumer
	dlqRepo         *repository.WebhookDLQRepository
	wsRepo          *repository.WorkspaceRepository
	client          *http.Client
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	done            chan struct{}
}

func NewWebhookWorker(ctx context.Context, nc *nats.Conn, dlqRepo *repository.WebhookDLQRepository) (*WebhookWorker, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	// Ensure WEBHOOKS stream exists
	stream, err := EnsureWebhookStream(ctx, nc)
	if err != nil {
		return nil, fmt.Errorf("ensure webhook stream: %w", err)
	}

	// Create pull consumer
	consumer, err := createConsumerWithRetry(ctx, stream, jetstream.ConsumerConfig{
		Durable:       "webhooks-consumer",
		Description:   "Webhook delivery worker consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       15 * time.Second,
		MaxDeliver:    10,
		FilterSubject: "webhooks.events",
	})
	if err != nil {
		return nil, fmt.Errorf("create webhook consumer: %w", err)
	}

	// Ensure INBOUND stream exists
	inboundStream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "INBOUND",
		Subjects:  []string{"inbound.events.>"},
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   10000,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure inbound stream: %w", err)
	}

	// Create inbound consumer
	inboundConsumer, err := createConsumerWithRetry(ctx, inboundStream, jetstream.ConsumerConfig{
		Durable:       "inbound-webhooks-consumer",
		Description:   "Inbound webhook delivery worker consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       15 * time.Second,
		MaxDeliver:    10,
		FilterSubject: "inbound.events.>",
	})
	if err != nil {
		return nil, fmt.Errorf("create inbound webhook consumer: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	w := &WebhookWorker{
		nc:              nc,
		js:              js,
		consumer:        consumer,
		inboundConsumer: inboundConsumer,
		dlqRepo:         dlqRepo,
		client:          &http.Client{Timeout: 10 * time.Second},
		cancel:          cancel,
		done:            make(chan struct{}),
	}

	w.wg.Add(2)
	go w.run(ctx, w.consumer, "outbound")
	go w.run(ctx, w.inboundConsumer, "inbound")
	return w, nil
}

func (w *WebhookWorker) Stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *WebhookWorker) run(ctx context.Context, cons jetstream.Consumer, mode string) {
	defer w.wg.Done()
	slog.Info("webhook worker thread started", "mode", mode)

	for {
		select {
		case <-ctx.Done():
			slog.Info("webhook worker thread stopping", "mode", mode)
			return
		default:
			msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					continue
				}
				slog.Error("webhook worker: fetch error", "error", err, "mode", mode)
				time.Sleep(1 * time.Second)
				continue
			}

			for msg := range msgs.Messages() {
				w.processEvent(ctx, msg, mode)
			}
		}
	}
}

func (w *WebhookWorker) SetWorkspaceRepository(repo *repository.WorkspaceRepository) {
	w.wsRepo = repo
}

func (w *WebhookWorker) processEvent(ctx context.Context, msg jetstream.Msg, mode string) {
	var evt WebhookEvent
	if err := json.Unmarshal(msg.Data(), &evt); err != nil {
		slog.Error("webhook worker: failed to unmarshal event", "error", err)
		_ = msg.Ack()
		return
	}

	workspaceID, err := uuid.Parse(evt.WorkspaceID)
	if err != nil {
		slog.Error("webhook worker: invalid workspace ID in event", "error", err, "workspace_id", evt.WorkspaceID)
		_ = msg.Ack()
		return
	}

	// 1. Fetch Webhook Configuration for Workspace
	cfg, err := w.dlqRepo.GetConfig(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, repository.ErrWebhookConfigNotFound) {
			slog.Debug("webhook worker: no webhook config for workspace, dropping event", "workspace_id", workspaceID)
			_ = msg.Ack()
			return
		}
		// DB error, retry later
		slog.Error("webhook worker: failed to fetch webhook config", "error", err, "workspace_id", workspaceID)
		w.handleRetry(msg, fmt.Errorf("failed to fetch config: %w", err), &evt, "")
		return
	}

	payloadBytes := msg.Data()

	// Inbound PII redaction logic
	if mode == "inbound" {
		var wsOptIn bool
		if w.wsRepo != nil {
			if ws, err := w.wsRepo.GetByID(ctx, workspaceID); err == nil && ws != nil {
				wsOptIn = ws.PIIOptIn
			}
		}

		if !wsOptIn {
			var inboundPayload struct {
				Event       string `json:"event"`
				TraceID     string `json:"trace_id"`
				MessageID   string `json:"message_id"`
				Channel     string `json:"channel"`
				Timestamp   string `json:"timestamp"`
				WorkspaceID string `json:"workspace_id"`
				From        string `json:"from"`
				Body        string `json:"body,omitempty"`
				Media       any    `json:"media,omitempty"`
				Location    any    `json:"location,omitempty"`
				Contacts    any    `json:"contacts,omitempty"`
			}
			if err := json.Unmarshal(msg.Data(), &inboundPayload); err == nil {
				// Hash from field
				hasher := sha256.New()
				hasher.Write([]byte(inboundPayload.From))
				inboundPayload.From = hex.EncodeToString(hasher.Sum(nil))

				// Strip location and contacts
				inboundPayload.Location = nil
				inboundPayload.Contacts = nil

				payloadBytes, _ = json.Marshal(inboundPayload)
			}
		}
	}

	// 2. Dispatch Webhook
	err = w.dispatch(ctx, cfg.URL, cfg.Secret, payloadBytes, evt.TraceID)
	if err != nil {
		slog.Warn("webhook worker: dispatch failed", "error", err, "url", cfg.URL, "trace_id", evt.TraceID)
		w.handleRetry(msg, err, &evt, cfg.URL)
		return
	}

	slog.Info("webhook worker: dispatched successfully", "url", cfg.URL, "trace_id", evt.TraceID, "event", evt.Event)
	_ = msg.Ack()
}

func (w *WebhookWorker) dispatch(ctx context.Context, url string, secret []byte, payload []byte, traceID string) error {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := SignPayload(payload, secret, timestamp)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OmniGo-Signature", signature)
	if traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("http dispatch error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	return nil
}

type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http status %s", e.Status)
}

func (w *WebhookWorker) handleRetry(msg jetstream.Msg, err error, evt *WebhookEvent, url string) {
	numDelivered := uint64(1)
	if meta, metadataErr := msg.Metadata(); metadataErr == nil && meta != nil {
		numDelivered = meta.NumDelivered
	}

	// Check if this error is terminal or we have exhausted max retries (10)
	var httpErr *HTTPError
	isTerminalErr := false
	if errors.As(err, &httpErr) {
		// Terminal status codes: 400, 401, 403, 404
		if httpErr.StatusCode == 400 || httpErr.StatusCode == 401 || httpErr.StatusCode == 403 || httpErr.StatusCode == 404 {
			isTerminalErr = true
		}
	}

	if isTerminalErr || numDelivered >= 10 {
		slog.Error("webhook worker: moving webhook to DLQ", "error", err, "trace_id", evt.TraceID, "attempts", numDelivered)
		
		wsID, _ := uuid.Parse(evt.WorkspaceID)
		failReason := err.Error()

		// Write to DLQ in database
		dlqErr := w.dlqRepo.InsertDLQ(
			context.Background(),
			wsID,
			evt.TraceID,
			evt.MessageID,
			evt.Event,
			msg.Data(),
			url,
			int(numDelivered),
			&failReason,
		)
		if dlqErr != nil {
			slog.Error("webhook worker: failed to write to DLQ", "error", dlqErr, "trace_id", evt.TraceID)
			// If we fail to write to the DLQ DB, Nak with short delay so we don't lose the event!
			_ = msg.NakWithDelay(5 * time.Second)
			return
		}

		_ = msg.Ack()
		return
	}

	// Calculate exponential backoff: 2^(numDelivered-1) * 1s
	delay := time.Duration(1<<(numDelivered-1)) * time.Second
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}

	slog.Info("webhook worker: scheduling retry", "trace_id", evt.TraceID, "attempt", numDelivered, "backoff", delay)
	_ = msg.NakWithDelay(delay)
}

// SignPayload computes the HMAC-SHA256 signature for a webhook delivery request.
// Format is: t=<timestamp>,v1=<hex_signature>
// Signed bytes are: <timestamp> + "." + <raw_payload_bytes>
func SignPayload(payload []byte, secret []byte, timestamp string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%s,v1=%s", timestamp, signature)
}
