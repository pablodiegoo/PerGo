# Phase 19: Webhook Messaging Verbs Engine - Research

This document outlines the research, technical findings, and implementation plan for Phase 19: Webhook Messaging Verbs Engine.

---

## 1. Database Schema Migrations

To support the `tag` and `close` messaging verbs, we need to extend the contact profile model with `tags` and `closed_at` columns.

### Migration File
A new migration file should be created at:
`[025_webhook_verbs_engine.sql](file:///home/pablo/Coding/OmniGo/internal/platform/postgres/migrations/025_webhook_verbs_engine.sql)`

#### Up Migration SQL
```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN closed_at TIMESTAMPTZ;
CREATE INDEX idx_contacts_tags ON contacts USING gin(tags);
-- +goose StatementEnd
```

#### Down Migration SQL
```sql
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_contacts_tags;
ALTER TABLE contacts DROP COLUMN IF EXISTS tags;
ALTER TABLE contacts DROP COLUMN IF EXISTS closed_at;
-- +goose StatementEnd
```

### Data Model Updates
We must update:
1. **Domain Struct**: Update `Contact` in `[contact.go](file:///home/pablo/Coding/OmniGo/internal/domain/contact.go)`:
   ```go
   type Contact struct {
       ID          uuid.UUID         `json:"id"`
       WorkspaceID uuid.UUID         `json:"workspace_id"`
       Name        string            `json:"name"`
       Email       *string           `json:"email,omitempty"`
       Tags        []string          `json:"tags"`
       ClosedAt    *time.Time        `json:"closed_at,omitempty"`
       CreatedAt   time.Time         `json:"created_at"`
       UpdatedAt   time.Time         `json:"updated_at"`
       Identities  []ContactIdentity `json:"identities,omitempty"`
   }
   ```

2. **Repository Queries**: Update `[contact.go](file:///home/pablo/Coding/OmniGo/internal/repository/contact.go)`:
   - **`GetByID`**: Select `tags` and `closed_at` from the `contacts` table and scan them into the domain model.
   - **`SearchContacts`**: Select `tags` and `closed_at` and scan them.
   - **`ResolveContact`**: 
     - Reset `closed_at` to `NULL` whenever a contact is resolved or matched (which signifies opening/sending a new message).
     - SQL Update to run on contact match/resolve:
       ```sql
       UPDATE contacts SET closed_at = NULL, updated_at = NOW() WHERE id = $1 AND closed_at IS NOT NULL
       ```
     - Add this update inside both the first-read success block and the transaction post-creation/cross-linking block.

3. **New Repository Methods**:
   - **`AddTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error`**:
     Appends tags to the contact's tag list while preserving uniqueness using PostgreSQL array operations.
     ```sql
     UPDATE contacts 
     SET tags = ARRAY(
         SELECT DISTINCT val 
         FROM unnest(array_cat(tags, $3)) val 
         WHERE val IS NOT NULL
     ), 
     updated_at = NOW() 
     WHERE workspace_id = $1 AND id = $2
     ```
   - **`CloseThread(ctx context.Context, workspaceID, contactID uuid.UUID) error`**:
     Sets `closed_at` to the current timestamp.
     ```sql
     UPDATE contacts 
     SET closed_at = NOW(), updated_at = NOW() 
     WHERE workspace_id = $1 AND id = $2
     ```

---

## 2. Verb Type Structures

The Messaging Verbs Engine will parse and execute sequential actions. We define the following payloads:

```go
package webhook

import (
	"encoding/json"
)

// Verb represents a single executable action returned by a webhook flow response.
type Verb struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
}

// ReplyParams defines the payload structure for the 'reply' action.
type ReplyParams struct {
	Body string `json:"body"`
}

// WaitParams defines the payload structure for the 'wait' action.
type WaitParams struct {
	Duration string `json:"duration"` // e.g., "500ms", "2s"
}

// ForwardParams defines the payload structure for the 'forward' action.
type ForwardParams struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
}

// TagParams defines the payload structure for the 'tag' action.
type TagParams struct {
	Tags []string `json:"tags"`
}

// CloseParams defines the payload structure for the 'close' action.
type CloseParams struct {
	// Currently empty, but reserved for future options
}
```

---

## 3. Core Engine Design & Parsing Logic

The Verbs Engine should process a list of verbs sequentially. It must handle time caps, validation errors, and context cancellations at each step.

### Engine Constructor & Dependencies
```go
package webhook

import (
	"context"
	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

type VerbsEngine struct {
	publisher   outbound.Publisher
	contactRepo *repository.ContactRepository
	logsRepo    *repository.UserActionLogRepository
	resolver    outbound.RouteResolver
}

func NewVerbsEngine(
	publisher outbound.Publisher,
	contactRepo *repository.ContactRepository,
	logsRepo *repository.UserActionLogRepository,
	resolver outbound.RouteResolver,
) *VerbsEngine {
	return &VerbsEngine{
		publisher:   publisher,
		contactRepo: contactRepo,
		logsRepo:    logsRepo,
		resolver:    resolver,
	}
}
```

### Parsing and Sequential Execution Loop
```go
func (e *VerbsEngine) Execute(ctx context.Context, task WebhookDeliveryTask, verbs []Verb) error {
	// 1. Establish context execution timeout (30 seconds)
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 2. Unmarshal original inbound event payload (needs to be unredacted payload from task.Payload)
	var evt InboundEventPayload
	if err := json.Unmarshal(task.Payload, &evt); err != nil {
		return fmt.Errorf("failed to unmarshal original task payload: %w", err)
	}

	wsID, err := uuid.Parse(evt.WorkspaceID)
	if err != nil {
		return fmt.Errorf("invalid workspace ID: %w", err)
	}

	// 3. Resolve the Contact ID
	var contactID uuid.UUID
	if e.contactRepo != nil {
		c, err := e.contactRepo.ResolveContact(execCtx, wsID, evt.Channel, evt.From, "", "", "")
		if err == nil && c != nil {
			contactID = c.ID
		}
	}

	// 4. Execution loop
	var executedLogs []ExecutedVerbLog
	var execErr error

	for i, verb := range verbs {
		select {
		case <-execCtx.Done():
			execErr = execCtx.Err()
			break
		default:
		}
		if execErr != nil {
			break
		}

		log := ExecutedVerbLog{Action: verb.Action, Status: "success"}

		switch verb.Action {
		case "reply":
			var p ReplyParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (reply) invalid params: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}
			if err := e.executeReply(execCtx, wsID, evt.From, evt.Channel, evt.To, p.Body, task.TraceID); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (reply) execution failed: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}

		case "wait":
			var p WaitParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (wait) invalid params: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}
			d, err := time.ParseDuration(p.Duration)
			if err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (wait) invalid duration '%s': %w", i, p.Duration, err)
				executedLogs = append(executedLogs, log)
				break
			}
			// Cap duration at 10 seconds, prevent negative values
			if d > 10*time.Second {
				d = 10 * time.Second
			}
			if d < 0 {
				d = 0
			}

			select {
			case <-time.After(d):
			case <-execCtx.Done():
				log.Status, log.Error = "failed", execCtx.Err().Error()
				execErr = execCtx.Err()
			}

		case "forward":
			var p ForwardParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (forward) invalid params: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}
			if err := e.executeForward(execCtx, wsID, p.To, p.Channel, evt.Body, task.TraceID); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (forward) execution failed: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}

		case "tag":
			var p TagParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (tag) invalid params: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}
			if contactID == uuid.Nil {
				log.Status, log.Error = "failed", "contact not resolved"
				execErr = fmt.Errorf("verb %d (tag) contact resolution failed", i)
				executedLogs = append(executedLogs, log)
				break
			}
			if err := e.contactRepo.AddTags(execCtx, wsID, contactID, p.Tags); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (tag) db update failed: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}

		case "close":
			if contactID == uuid.Nil {
				log.Status, log.Error = "failed", "contact not resolved"
				execErr = fmt.Errorf("verb %d (close) contact resolution failed", i)
				executedLogs = append(executedLogs, log)
				break
			}
			if err := e.contactRepo.CloseThread(execCtx, wsID, contactID); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (close) db update failed: %w", i, err)
				executedLogs = append(executedLogs, log)
				break
			}

		default:
			log.Status, log.Error = "failed", "unknown action"
			execErr = fmt.Errorf("verb %d unknown action: %s", i, verb.Action)
			executedLogs = append(executedLogs, log)
		}

		if execErr != nil {
			break
		}
		executedLogs = append(executedLogs, log)
	}

	// 5. Record Execution Results to User Action Logs
	e.logActionResults(ctx, wsID, task.SubscriptionID, contactID, evt.Channel, evt.From, task.TraceID, executedLogs, execErr)

	return execErr
}
```

---

## 4. Integration into Webhook Dispatcher

When a webhook succeeds (2xx response), the Dispatcher extracts the response body, parses it, and triggers the `VerbsEngine` asynchronously.

### Implementation in `dispatcher.go`

```go
type DefaultDispatcher struct {
	subStore    SubscriptionStore
	dlqStore    DLQStore
	wsStore     WorkspaceStore
	client      HTTPClient
	verbsEngine *VerbsEngine // Injected Verbs Engine
}

// Inside Dispatch(ctx context.Context, task WebhookDeliveryTask) error
resp, err := d.client.Do(req)
if err != nil {
    return fmt.Errorf("http dispatch error: %w", err)
}
defer resp.Body.Close()

if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
}

// 1. Read response body
bodyBytes, err := io.ReadAll(resp.Body)
if err != nil {
    slog.Error("failed to read webhook response body", "error", err, "trace_id", task.TraceID)
    return nil // delivery succeeded, do not retry due to verbs parsing failure
}

// 2. Parse response body for sequential verbs
var verbs []Verb
if err := json.Unmarshal(bodyBytes, &verbs); err == nil && len(verbs) > 0 {
    // 3. Trigger execution asynchronously
    if d.verbsEngine != nil {
        go func() {
            execCtx := context.Background() // decoupling context from HTTP lifecycle
            if err := d.verbsEngine.Execute(execCtx, task, verbs); err != nil {
                slog.Error("verbs engine execution failed", "error", err, "trace_id", task.TraceID)
            }
        }()
    }
}
```

### PII Redaction Strategy
PII Redaction modifies `payloadBytes` sent to the webhook, but **does not mutate `task.Payload`**. Thus, the async engine receives the original `task.Payload` containing the actual sender identities (`from` phone number/JID) instead of hashed tokens, ensuring outbound actions (`reply`/`forward`) and contact updates function correctly even with PII protection enabled.

---

## 5. Outbound Queue Publishing & User Action Logging

### Outbound Queue Publishing
To send messages on behalf of the customer, the Engine resolves the workspace channel connection and publishes `domain.QueueMessage` to NATS on the `messages.outbound` subject.

```go
func (e *VerbsEngine) executeReply(
	ctx context.Context,
	workspaceID uuid.UUID,
	recipient string,
	channel string,
	inboundTo string,
	body string,
	traceID string,
) error {
	// Resolve Connection
	conn, err := e.resolver.GetBySenderIdentity(ctx, workspaceID, inboundTo)
	if err != nil {
		// Fallback to default channel connection
		conn, err = e.resolver.GetDefaultChannelConnection(ctx, workspaceID, channel)
		if err != nil {
			return fmt.Errorf("cannot resolve connection for reply: %w", err)
		}
	}

	qMsg := &domain.QueueMessage{
		WorkspaceID:    workspaceID,
		ConnectionID:   conn.ID,
		SenderIdentity: conn.SenderIdentity,
		TraceID:        traceID,
		To:             recipient,
		Channel:        channel,
		Body:           body,
		QueuedAt:       time.Now().UTC(),
	}

	payload, err := json.Marshal(qMsg)
	if err != nil {
		return err
	}

	return e.publisher.Publish(ctx, "messages.outbound", payload, traceID)
}

func (e *VerbsEngine) executeForward(
	ctx context.Context,
	workspaceID uuid.UUID,
	to string,
	channel string,
	originalBody string,
	traceID string,
) error {
	// Resolve connection for target channel
	conn, err := e.resolver.GetDefaultChannelConnection(ctx, workspaceID, channel)
	if err != nil {
		return fmt.Errorf("cannot resolve default connection for forward channel '%s': %w", err)
	}

	qMsg := &domain.QueueMessage{
		WorkspaceID:    workspaceID,
		ConnectionID:   conn.ID,
		SenderIdentity: conn.SenderIdentity,
		TraceID:        traceID,
		To:             to,
		Channel:        channel,
		Body:           originalBody,
		QueuedAt:       time.Now().UTC(),
	}

	payload, err := json.Marshal(qMsg)
	if err != nil {
		return err
	}

	return e.publisher.Publish(ctx, "messages.outbound", payload, traceID)
}
```

### Action Logging
Logs are persisted to the database under action name `"webhook.verbs"` using `[user_action_log.go](file:///home/pablo/Coding/OmniGo/internal/repository/user_action_log.go)`.

```go
type ExecutedVerbLog struct {
	Action string `json:"action"`
	Status string `json:"status"` // "success" | "failed"
	Error  string `json:"error,omitempty"`
}

type VerbsExecutionMetadata struct {
	SubscriptionID string            `json:"subscription_id"`
	TraceID        string            `json:"trace_id"`
	ContactID      string            `json:"contact_id,omitempty"`
	Channel        string            `json:"channel"`
	From           string            `json:"from"`
	Verbs          []ExecutedVerbLog `json:"verbs"`
	Status         string            `json:"status"` // "success" | "error"
	Error          string            `json:"error,omitempty"`
}

func (e *VerbsEngine) logActionResults(
	ctx context.Context,
	workspaceID uuid.UUID,
	subscriptionID uuid.UUID,
	contactID uuid.UUID,
	channel string,
	from string,
	traceID string,
	verbs []ExecutedVerbLog,
	execErr error,
) {
	if e.logsRepo == nil {
		return
	}

	status := "success"
	errStr := ""
	if execErr != nil {
		status = "error"
		errStr = execErr.Error()
	}

	meta := VerbsExecutionMetadata{
		SubscriptionID: subscriptionID.String(),
		TraceID:        traceID,
		Channel:        channel,
		From:           from,
		Verbs:          verbs,
		Status:         status,
		Error:          errStr,
	}
	if contactID != uuid.Nil {
		meta.ContactID = contactID.String()
	}

	metaBytes, _ := json.Marshal(meta)

	logEntry := &repository.UserActionLog{
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "verbs_engine",
		ActorName:   "Webhook Verbs Engine",
		Action:      "webhook.verbs",
		Source:      "system",
		Metadata:    metaBytes,
	}

	// Insert in background context to prevent holding any resources
	go func() {
		_ = e.logsRepo.Insert(context.Background(), logEntry)
	}()
}
```

---

## 6. Wave Plans

### Wave 1: Migration + Verbs Engine & Unit Tests

#### Objectives
1. Add Postgres schema migration (`025_webhook_verbs_engine.sql`).
2. Update `domain.Contact` and `repository.ContactRepository` to support `tags` and `closed_at`.
3. Reset `closed_at = NULL` inside `ResolveContact` on contact updates/matches.
4. Implement `internal/webhook/verbs.go` and unit tests in `internal/webhook/verbs_test.go` (covering validation, duration caps, execution, context cancellation).

#### Verification Tests
- Run database migration verification:
  ```bash
  go test -v ./internal/repository -run TestContactRepository
  ```
- Run verbs engine standalone unit tests:
  ```bash
  go test -v ./internal/webhook -run TestVerbsEngine
  ```

---

### Wave 2: Dispatcher Integration + Action Logging & Integration Tests

#### Objectives
1. Inject the Verbs Engine into `DefaultDispatcher`.
2. Update `dispatcher.go`'s `Dispatch` function to check 2xx status, extract the body, parse the verbs array, and run the engine asynchronously.
3. Wire NATS publishing for `reply`/`forward` and `UserActionLog` persistence.
4. Update `dispatcher_test.go` to mock HTTP webhook response payloads containing verbs.
5. Verify that PII redaction does not interfere with verbs execution because `task.Payload` remains unredacted.

#### Verification Tests
- Run all webhook tests (including dispatcher mocks):
  ```bash
  go test -v ./internal/webhook -run TestDefaultDispatcher
  ```
- Run full codebase tests with race detector:
  ```bash
  make test-race
  ```
