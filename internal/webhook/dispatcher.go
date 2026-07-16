package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

// ConfigStore defines the database abstraction for webhook configuration and DLQ persistence.
type ConfigStore interface {
	GetConfig(ctx context.Context, workspaceID uuid.UUID) (*repository.WebhookConfig, error)
	InsertDLQ(ctx context.Context, workspaceID uuid.UUID, subscriptionID uuid.UUID, traceID, messageID, eventType string, payload []byte, url string, attempts int, failureReason *string) error
}

// WorkspaceStore defines the database abstraction for workspace settings.
type WorkspaceStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error)
}

// HTTPClient defines the client abstraction for executing HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPError is returned when a webhook endpoint responds with a non-2xx status code.
type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http status %s", e.Status)
}

// WebhookDispatcher defines the interface for webhook payload processing and delivery.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, mode string, rawEvent []byte) error
	WriteToDLQ(ctx context.Context, workspaceID uuid.UUID, traceID, messageID, event string, rawEvent []byte, attempts int, failReason string) error
}

// DefaultDispatcher is the concrete implementation of WebhookDispatcher.
type DefaultDispatcher struct {
	configStore ConfigStore
	wsStore     WorkspaceStore
	client      HTTPClient
}

// NewDefaultDispatcher creates a new DefaultDispatcher instance.
func NewDefaultDispatcher(configStore ConfigStore, wsStore WorkspaceStore, client HTTPClient) *DefaultDispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &DefaultDispatcher{
		configStore: configStore,
		wsStore:     wsStore,
		client:      client,
	}
}

// Dispatch processes compliance, signs the payload, and posts it to the workspace's configured webhook URL.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, mode string, rawEvent []byte) error {
	var evt struct {
		Event       string `json:"event"`
		TraceID     string `json:"trace_id"`
		MessageID   string `json:"message_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(rawEvent, &evt); err != nil {
		return fmt.Errorf("failed to unmarshal event header: %w", err)
	}

	workspaceID, err := uuid.Parse(evt.WorkspaceID)
	if err != nil {
		return fmt.Errorf("invalid workspace ID: %w", err)
	}

	// 1. Fetch Webhook Configuration
	cfg, err := d.configStore.GetConfig(ctx, workspaceID)
	if err != nil {
		return err
	}

	payloadBytes := rawEvent

	// 2. Compliance PII Redaction for inbound events
	if mode == "inbound" {
		var wsOptIn bool
		if d.wsStore != nil {
			if ws, err := d.wsStore.GetByID(ctx, workspaceID); err == nil && ws != nil {
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
			if err := json.Unmarshal(rawEvent, &inboundPayload); err == nil {
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

	// 3. Dispatch HTTP Post request
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := SignPayload(payloadBytes, cfg.Secret, timestamp)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PerGo-Signature", signature)
	if evt.TraceID != "" {
		req.Header.Set("X-Trace-ID", evt.TraceID)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("http dispatch error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	return nil
}

// WriteToDLQ writes a permanently failed webhook event to the database DLQ repository.
func (d *DefaultDispatcher) WriteToDLQ(
	ctx context.Context,
	workspaceID uuid.UUID,
	traceID, messageID, event string,
	rawEvent []byte,
	attempts int,
	failReason string,
) error {
	// Retrieve config to get webhook URL for archiving
	cfg, err := d.configStore.GetConfig(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to retrieve config for DLQ: %w", err)
	}

	return d.configStore.InsertDLQ(
		ctx,
		workspaceID,
		cfg.ID,
		traceID,
		messageID,
		event,
		rawEvent,
		cfg.URL,
		attempts,
		&failReason,
	)
}

// SignPayload computes the HMAC-SHA256 signature for a webhook delivery request.
func SignPayload(payload []byte, secret []byte, timestamp string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%s,v1=%s", timestamp, signature)
}
