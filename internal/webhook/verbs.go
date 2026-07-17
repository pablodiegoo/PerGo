package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
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

// PauseBotParams defines the payload structure for the 'pause_bot' action.
type PauseBotParams struct {
	Duration string `json:"duration,omitempty"`
}

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

func (e *VerbsEngine) Execute(ctx context.Context, task WebhookDeliveryTask, verbs []Verb) error {
	// 1. Establish context execution timeout (30 seconds)
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 2. Unmarshal original inbound event payload (needs to be unredacted payload from task.Payload)
	var evt inbound.InboundEventPayload
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

		case "pause_bot":
			if contactID == uuid.Nil {
				log.Status, log.Error = "failed", "contact not resolved"
				execErr = fmt.Errorf("verb %d (pause_bot) contact resolution failed", i)
				executedLogs = append(executedLogs, log)
				break
			}
			var p PauseBotParams
			if len(verb.Params) > 0 && string(verb.Params) != "null" {
				if err := json.Unmarshal(verb.Params, &p); err != nil {
					log.Status, log.Error = "failed", err.Error()
					execErr = fmt.Errorf("verb %d (pause_bot) invalid params: %w", i, err)
					executedLogs = append(executedLogs, log)
					break
				}
			}

			pausedAt := time.Now().UTC()
			if p.Duration != "" {
				d, err := time.ParseDuration(p.Duration)
				if err != nil {
					log.Status, log.Error = "failed", err.Error()
					execErr = fmt.Errorf("verb %d (pause_bot) invalid duration '%s': %w", i, p.Duration, err)
					executedLogs = append(executedLogs, log)
					break
				}
				pausedAt = time.Now().UTC().Add(-12 * time.Hour).Add(d)
			}

			if err := e.contactRepo.UpdateBotState(execCtx, wsID, contactID, false, &pausedAt); err != nil {
				log.Status, log.Error = "failed", err.Error()
				execErr = fmt.Errorf("verb %d (pause_bot) db update failed: %w", i, err)
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

func (e *VerbsEngine) executeReply(
	ctx context.Context,
	workspaceID uuid.UUID,
	recipient string,
	channel string,
	inboundTo string,
	body string,
	traceID string,
) error {
	if e.publisher == nil || e.resolver == nil {
		// If mock environment or parts not wired, skip publishing
		return nil
	}

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
	if e.publisher == nil || e.resolver == nil {
		// If mock environment or parts not wired, skip publishing
		return nil
	}

	// Resolve connection for target channel
	conn, err := e.resolver.GetDefaultChannelConnection(ctx, workspaceID, channel)
	if err != nil {
		return fmt.Errorf("cannot resolve default connection for forward channel '%s': %w", channel, err)
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
		if err := e.logsRepo.Insert(context.Background(), logEntry); err != nil {
			slog.Error("failed to insert webhook verbs action log", "error", err, "trace_id", traceID)
		}
	}()
}
