package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat"
	"github.com/pablojhp.pergo/internal/chat/activity"
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
	chatEngine *chat.Engine
}

func NewCloseHandler(contactRepo *repository.ContactRepository) VerbHandler {
	var eng *chat.Engine
	if contactRepo != nil && contactRepo.Pool() != nil {
		chatRepo := repository.NewChatRepository(contactRepo.Pool())
		eng = chat.NewEngine(chatRepo, chat.WithContactRepo(contactRepo))
	}
	return &closeHandler{chatEngine: eng}
}

func NewCloseHandlerWithEngine(eng *chat.Engine) VerbHandler {
	return &closeHandler{chatEngine: eng}
}

func (h *closeHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	if vc.ContactID == uuid.Nil {
		return fmt.Errorf("contact not resolved")
	}
	if h.chatEngine == nil {
		return fmt.Errorf("chat engine not configured")
	}
	ref := chat.ChatRef{ContactID: vc.ContactID}
	if vc.ConnectionID != uuid.Nil {
		connID := vc.ConnectionID
		ref.ConnectionID = &connID
	}
	if _, _, err := h.chatEngine.UpdateStatus(ctx, vc.WorkspaceID, ref, chat.StatusUpdate{Status: "closed"}); err != nil {
		if errors.Is(err, chat.ErrChatNotFound) {
			if _, ensureErr := h.chatEngine.EnsureChatForContact(ctx, vc.WorkspaceID, vc.ContactID, false); ensureErr != nil {
				return fmt.Errorf("db update failed: %w", ensureErr)
			}
			if _, _, retryErr := h.chatEngine.UpdateStatus(ctx, vc.WorkspaceID, chat.ChatRef{ContactID: vc.ContactID}, chat.StatusUpdate{Status: "closed"}); retryErr != nil {
				return fmt.Errorf("db update failed: %w", retryErr)
			}
			return nil
		}
		return fmt.Errorf("db update failed: %w", err)
	}
	return nil
}

type pauseBotHandler struct {
	activity *activity.Recorder
}

func NewPauseBotHandler(contactRepo *repository.ContactRepository) VerbHandler {
	var rec *activity.Recorder
	if contactRepo != nil && contactRepo.Pool() != nil {
		rec = activity.New(contactRepo.Pool(), nil, nil, nil)
	}
	return &pauseBotHandler{activity: rec}
}

func NewPauseBotHandlerWithActivity(rec *activity.Recorder) VerbHandler {
	return &pauseBotHandler{activity: rec}
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
	d := 12 * time.Hour
	if p.Duration != "" {
		parsed, err := time.ParseDuration(p.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration '%s': %w", p.Duration, err)
		}
		d = parsed
	}
	if h.activity == nil {
		return fmt.Errorf("activity recorder not configured")
	}
	until := time.Now().UTC().Add(d)
	if _, err := h.activity.SetAI(ctx, vc.WorkspaceID, activity.ChatOf(vc.ConnectionID, vc.ContactID), activity.AIPauseUntil(until)); err != nil {
		return fmt.Errorf("db update failed: %w", err)
	}
	return nil
}
