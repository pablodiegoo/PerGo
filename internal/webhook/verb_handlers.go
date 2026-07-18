package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

type replyHandler struct {
	publisher outbound.Publisher
	resolver  outbound.RouteResolver
}

func NewReplyHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler {
	return &replyHandler{publisher: publisher, resolver: resolver}
}

func (h *replyHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p ReplyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if h.publisher == nil || h.resolver == nil {
		return nil
	}
	conn, err := h.resolver.GetBySenderIdentity(ctx, vc.WorkspaceID, vc.Event.To)
	if err != nil {
		conn, err = h.resolver.GetDefaultChannelConnection(ctx, vc.WorkspaceID, vc.Event.Channel)
		if err != nil {
			return fmt.Errorf("cannot resolve connection for reply: %w", err)
		}
	}
	qMsg := &domain.QueueMessage{
		WorkspaceID:    vc.WorkspaceID,
		ConnectionID:   conn.ID,
		SenderIdentity: conn.SenderIdentity,
		TraceID:        vc.TraceID,
		To:             vc.Event.From,
		Channel:        vc.Event.Channel,
		Body:           p.Body,
		QueuedAt:       time.Now().UTC(),
	}
	payload, err := json.Marshal(qMsg)
	if err != nil {
		return err
	}
	return h.publisher.Publish(ctx, "messages.outbound", payload, vc.TraceID)
}

type waitHandler struct{}

func NewWaitHandler() VerbHandler {
	return &waitHandler{}
}

func (h *waitHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p WaitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	d, err := time.ParseDuration(p.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration '%s': %w", p.Duration, err)
	}
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	if d < 0 {
		d = 0
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type forwardHandler struct {
	publisher outbound.Publisher
	resolver  outbound.RouteResolver
}

func NewForwardHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler {
	return &forwardHandler{publisher: publisher, resolver: resolver}
}

func (h *forwardHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p ForwardParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if h.publisher == nil || h.resolver == nil {
		return nil
	}
	conn, err := h.resolver.GetDefaultChannelConnection(ctx, vc.WorkspaceID, p.Channel)
	if err != nil {
		return fmt.Errorf("cannot resolve default connection for forward channel '%s': %w", p.Channel, err)
	}
	qMsg := &domain.QueueMessage{
		WorkspaceID:    vc.WorkspaceID,
		ConnectionID:   conn.ID,
		SenderIdentity: conn.SenderIdentity,
		TraceID:        vc.TraceID,
		To:             p.To,
		Channel:        p.Channel,
		Body:           vc.Event.Body,
		QueuedAt:       time.Now().UTC(),
	}
	payload, err := json.Marshal(qMsg)
	if err != nil {
		return err
	}
	return h.publisher.Publish(ctx, "messages.outbound", payload, vc.TraceID)
}

type tagHandler struct {
	contactRepo *repository.ContactRepository
}

func NewTagHandler(contactRepo *repository.ContactRepository) VerbHandler {
	return &tagHandler{contactRepo: contactRepo}
}

func (h *tagHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p TagParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if vc.ContactID == uuid.Nil {
		return fmt.Errorf("contact not resolved")
	}
	if err := h.contactRepo.AddTags(ctx, vc.WorkspaceID, vc.ContactID, p.Tags); err != nil {
		return fmt.Errorf("db update failed: %w", err)
	}
	return nil
}

type closeHandler struct {
	contactRepo *repository.ContactRepository
}

func NewCloseHandler(contactRepo *repository.ContactRepository) VerbHandler {
	return &closeHandler{contactRepo: contactRepo}
}

func (h *closeHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	if vc.ContactID == uuid.Nil {
		return fmt.Errorf("contact not resolved")
	}
	if err := h.contactRepo.CloseThread(ctx, vc.WorkspaceID, vc.ContactID); err != nil {
		return fmt.Errorf("db update failed: %w", err)
	}
	return nil
}

type pauseBotHandler struct {
	contactRepo *repository.ContactRepository
}

func NewPauseBotHandler(contactRepo *repository.ContactRepository) VerbHandler {
	return &pauseBotHandler{contactRepo: contactRepo}
}

func (h *pauseBotHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	if vc.ContactID == uuid.Nil {
		return fmt.Errorf("contact not resolved")
	}
	var p PauseBotParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return fmt.Errorf("invalid params: %w", err)
		}
	}
	pausedAt := time.Now().UTC()
	if p.Duration != "" {
		d, err := time.ParseDuration(p.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration '%s': %w", p.Duration, err)
		}
		pausedAt = time.Now().UTC().Add(-12 * time.Hour).Add(d)
	}
	if err := h.contactRepo.UpdateBotState(ctx, vc.WorkspaceID, vc.ContactID, false, &pausedAt); err != nil {
		return fmt.Errorf("db update failed: %w", err)
	}
	return nil
}
