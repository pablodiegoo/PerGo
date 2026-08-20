package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/breaker"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/repository"
)

// SubscriptionStore defines the database abstraction for webhook subscription retrieval.
type SubscriptionStore interface {
	Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error)
}

// DLQStore defines the database abstraction for DLQ persistence.
type DLQStore interface {
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

func (e *HTTPError) Terminal() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != 429
}

var (
	ErrSubscriptionInactive = errors.New("webhook subscription is inactive")
	ErrSubscriptionNotFound = errors.New("webhook subscription not found")
)

type WebhookDeliveryTask struct {
	ID             uuid.UUID       `json:"id"`
	SubscriptionID uuid.UUID       `json:"subscription_id"`
	WorkspaceID    uuid.UUID       `json:"workspace_id"`
	Event          string          `json:"event"`
	TraceID        string          `json:"trace_id"`
	MessageID      string          `json:"message_id"`
	Payload        []byte          `json:"payload"`
	Mode           string          `json:"mode"` // "inbound" | "outbound"
}

// WebhookDispatcher defines the interface for webhook payload processing and delivery.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, task WebhookDeliveryTask) error
	WriteToDLQ(ctx context.Context, workspaceID uuid.UUID, subscriptionID uuid.UUID, traceID, messageID, event string, rawEvent []byte, attempts int, failReason string) error
}

// DefaultDispatcher is the concrete implementation of WebhookDispatcher.
type DefaultDispatcher struct {
	subStore    SubscriptionStore
	dlqStore    DLQStore
	wsStore     WorkspaceStore
	client      HTTPClient
	verbsEngine *VerbsEngine
	breaker     *breaker.CircuitBreaker
}

// NewDefaultDispatcher creates a new DefaultDispatcher instance.
func NewDefaultDispatcher(subStore SubscriptionStore, dlqStore DLQStore, wsStore WorkspaceStore, client HTTPClient, verbsEngine *VerbsEngine) *DefaultDispatcher {
	if client == nil {
		client = netpolicy.NewPublicHTTPClient(netpolicy.WithTimeout(10 * time.Second))
	}
	return &DefaultDispatcher{
		subStore:    subStore,
		dlqStore:    dlqStore,
		wsStore:     wsStore,
		client:      client,
		verbsEngine: verbsEngine,
		breaker:     breaker.NewCircuitBreaker(5, 5*time.Minute),
	}
}

// Dispatch processes compliance, signs the payload, and posts it to the subscription's configured webhook URL.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, task WebhookDeliveryTask) error {
	// 1. Fetch Subscription by SubscriptionID
	sub, err := d.subStore.Get(ctx, task.SubscriptionID)
	if err != nil {
		if errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
			return ErrSubscriptionNotFound
		}
		return err
	}

	if !sub.Active {
		return ErrSubscriptionInactive
	}

	if d.breaker != nil {
		if err := d.breaker.Allow(sub.URL); err != nil {
			return err
		}
	}

	payloadBytes := task.Payload

	// 2. Compliance PII Redaction for inbound events
	if task.Mode == "inbound" {
		var wsOptIn bool
		if d.wsStore != nil {
			if ws, err := d.wsStore.GetByID(ctx, task.WorkspaceID); err == nil && ws != nil {
				wsOptIn = ws.PIIOptIn
			}
		}

		if !wsOptIn {
			var inboundPayload struct {
				Event       string            `json:"event"`
				TraceID     string            `json:"trace_id"`
				MessageID   string            `json:"message_id"`
				Channel     string            `json:"channel"`
				Timestamp   string            `json:"timestamp"`
				WorkspaceID string            `json:"workspace_id"`
				From        string            `json:"from"`
				To          string            `json:"to,omitempty"`
				Body        string            `json:"body,omitempty"`
				Media       any               `json:"media,omitempty"`
				Location    any               `json:"location,omitempty"`
				Contacts    any               `json:"contacts,omitempty"`
				Interactive any               `json:"interactive,omitempty"`
				Story       any               `json:"story_event,omitempty"`
				SenderName  string            `json:"sender_name,omitempty"`
				Metadata    map[string]string `json:"metadata,omitempty"`
			}
			if err := json.Unmarshal(task.Payload, &inboundPayload); err == nil {
				// Hash from field
				hasher := sha256.New()
				hasher.Write([]byte(inboundPayload.From))
				inboundPayload.From = hex.EncodeToString(hasher.Sum(nil))

				// Strip location and contacts
				inboundPayload.Location = nil
				inboundPayload.Contacts = nil

				// Hash participant JID in metadata if present to ensure GDPR/LGPD compliance
				if inboundPayload.Metadata != nil && inboundPayload.Metadata["participant"] != "" {
					pHasher := sha256.New()
					pHasher.Write([]byte(inboundPayload.Metadata["participant"]))
					inboundPayload.Metadata["participant"] = hex.EncodeToString(pHasher.Sum(nil))
				}

				payloadBytes, _ = json.Marshal(inboundPayload)
			}
		}
	}

	// 3. Resolve Secret and Dispatch HTTP Post request
	secret := sub.Secret
	if len(secret) == 0 && d.wsStore != nil {
		if ws, err := d.wsStore.GetByID(ctx, task.WorkspaceID); err == nil && ws != nil && ws.WebhookSecret != nil {
			secret = []byte(*ws.WebhookSecret)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if len(secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature := SignPayload(payloadBytes, secret, timestamp)
		req.Header.Set("X-PerGo-Signature", signature)
	}
	if task.TraceID != "" {
		req.Header.Set("X-Trace-ID", task.TraceID)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		if d.breaker != nil {
			d.breaker.RecordFailure(sub.URL)
		}
		return fmt.Errorf("http dispatch error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if d.breaker != nil {
			d.breaker.RecordFailure(sub.URL)
		}
		return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	if d.breaker != nil {
		d.breaker.RecordSuccess(sub.URL)
	}

	// Read response body to extract potential messaging verbs
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read webhook response body", "error", err, "trace_id", task.TraceID)
		return nil
	}

	var verbs []Verb
	if err := json.Unmarshal(bodyBytes, &verbs); err == nil && len(verbs) > 0 {
		if d.verbsEngine != nil {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("recovered panic in verbs engine execution", "panic", r, "trace_id", task.TraceID)
					}
				}()
				execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := d.verbsEngine.Execute(execCtx, task, verbs); err != nil {
					slog.Error("verbs engine execution failed", "error", err, "trace_id", task.TraceID)
				}
			}()
		}
	}

	return nil
}

// WriteToDLQ writes a permanently failed webhook event to the database DLQ repository.
func (d *DefaultDispatcher) WriteToDLQ(
	ctx context.Context,
	workspaceID uuid.UUID,
	subscriptionID uuid.UUID,
	traceID, messageID, event string,
	rawEvent []byte,
	attempts int,
	failReason string,
) error {
	// Retrieve subscription to get webhook URL for archiving
	sub, err := d.subStore.Get(ctx, subscriptionID)
	if err != nil {
		return fmt.Errorf("failed to retrieve subscription for DLQ: %w", err)
	}

	return d.dlqStore.InsertDLQ(
		ctx,
		workspaceID,
		subscriptionID,
		traceID,
		messageID,
		event,
		rawEvent,
		sub.URL,
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

// VerifyPerGoSignature validates a webhook HMAC-SHA256 signature header against raw payload bytes and a secret.
// It verifies that the signature header is well-formed (t=<ts>,v1=<sig>), checks replay tolerance (5-minute window),
// and performs constant-time comparison of the computed HMAC with the expected HMAC.
func VerifyPerGoSignature(rawBody []byte, signatureHeader string, secret string) bool {
	return VerifySignatureWithTolerance(rawBody, signatureHeader, []byte(secret), 5*time.Minute)
}

// VerifySignatureWithTolerance checks the webhook HMAC signature with custom replay tolerance.
func VerifySignatureWithTolerance(rawBody []byte, signatureHeader string, secret []byte, tolerance time.Duration) bool {
	if signatureHeader == "" || len(secret) == 0 {
		return false
	}

	var timestamp, expectedSig string
	parts := strings.Split(signatureHeader, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			switch strings.TrimSpace(kv[0]) {
			case "t":
				timestamp = strings.TrimSpace(kv[1])
			case "v1":
				expectedSig = strings.TrimSpace(kv[1])
			}
		}
	}

	if timestamp == "" || expectedSig == "" {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	if tolerance > 0 {
		diff := time.Now().Unix() - ts
		if diff < 0 {
			diff = -diff
		}
		if diff > int64(tolerance.Seconds()) {
			return false // Replay attack protection
		}
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	computedSig := hex.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(computedSig), []byte(expectedSig)) == 1
}

