package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
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

// VerbHandler defines the interface for webhook verb processors.
type VerbHandler interface {
	Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error
}

// VerbContext carries context shared across verbs in the same execution block.
type VerbContext struct {
	WorkspaceID uuid.UUID
	ContactID   uuid.UUID
	TraceID     string
	Event       inbound.InboundEventPayload
}

type VerbsEngine struct {
	handlers    map[string]VerbHandler
	contactRepo *repository.ContactRepository
	logsRepo    *repository.UserActionLogRepository
}

func NewVerbsEngine(
	publisher outbound.Publisher,
	contactRepo *repository.ContactRepository,
	logsRepo *repository.UserActionLogRepository,
	resolver outbound.RouteResolver,
) *VerbsEngine {
	handlers := map[string]VerbHandler{
		"reply":     NewReplyHandler(publisher, resolver),
		"wait":      NewWaitHandler(),
		"forward":   NewForwardHandler(publisher, resolver),
		"tag":       NewTagHandler(contactRepo),
		"close":     NewCloseHandler(contactRepo),
		"pause_bot": NewPauseBotHandler(contactRepo),
	}
	return &VerbsEngine{
		handlers:    handlers,
		contactRepo: contactRepo,
		logsRepo:    logsRepo,
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

	vc := VerbContext{
		WorkspaceID: wsID,
		ContactID:   contactID,
		TraceID:     task.TraceID,
		Event:       evt,
	}

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

		handler, ok := e.handlers[verb.Action]
		if !ok {
			log.Status, log.Error = "failed", "unknown action"
			execErr = fmt.Errorf("verb %d unknown action: %s", i, verb.Action)
			executedLogs = append(executedLogs, log)
			break
		}

		if err := handler.Execute(execCtx, vc, verb.Params); err != nil {
			log.Status, log.Error = "failed", err.Error()
			execErr = fmt.Errorf("verb %d (%s): %w", i, verb.Action, err)
			executedLogs = append(executedLogs, log)
			break
		}

		executedLogs = append(executedLogs, log)
	}

	// 5. Record Execution Results to User Action Logs
	e.logActionResults(ctx, wsID, task.SubscriptionID, contactID, evt.Channel, evt.From, task.TraceID, executedLogs, execErr)

	return execErr
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
