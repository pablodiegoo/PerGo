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
	ingestor outbound.OutboundProcessor
}

func NewReplyHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler {
	var ingestor outbound.OutboundProcessor
	if publisher != nil && resolver != nil {
		ingestor = outbound.NewProcessor(nil, nil, resolver, publisher)
	}
	return &replyHandler{ingestor: ingestor}
}

func NewReplyHandlerWithIngestor(ingestor outbound.OutboundProcessor) VerbHandler {
	return &replyHandler{ingestor: ingestor}
}

func (h *replyHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p ReplyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if h.ingestor == nil {
		return nil
	}
	req := &domain.CreateMessageRequest{
		To:      vc.Event.From,
		Channel: vc.Event.Channel,
		Body:    p.Body,
		ConversationalIngestFields: domain.ConversationalIngestFields{
			SenderType: string(domain.SenderTypeAIAgent),
			SenderName: "Webhook Verb",
		},
	}
	if vc.ConnectionID != uuid.Nil {
		connID := vc.ConnectionID
		req.ConnectionID = &connID
	} else {
		req.From = vc.Event.To
	}
	if vc.ContactID != uuid.Nil {
		contactID := vc.ContactID
		req.ContactID = &contactID
	}
	_, err := h.ingestor.Ingest(ctx, vc.WorkspaceID, vc.TraceID, req)
	return err
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
	ingestor outbound.OutboundProcessor
}

func NewForwardHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler {
	var ingestor outbound.OutboundProcessor
	if publisher != nil && resolver != nil {
		ingestor = outbound.NewProcessor(nil, nil, resolver, publisher)
	}
	return &forwardHandler{ingestor: ingestor}
}

func NewForwardHandlerWithIngestor(ingestor outbound.OutboundProcessor) VerbHandler {
	return &forwardHandler{ingestor: ingestor}
}

func (h *forwardHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p ForwardParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if h.ingestor == nil {
		return nil
	}
	req := &domain.CreateMessageRequest{
		To:      p.To,
		Channel: p.Channel,
		Body:    vc.Event.Body,
		ConversationalIngestFields: domain.ConversationalIngestFields{
			SenderType: string(domain.SenderTypeAIAgent),
			SenderName: "Webhook Verb",
		},
	}
	_, err := h.ingestor.Ingest(ctx, vc.WorkspaceID, vc.TraceID, req)
	return err
}

type tagHandler struct {
	tagRepo *repository.TagRepository
}

func NewTagHandler(contactRepo *repository.ContactRepository) VerbHandler {
	var tagRepo *repository.TagRepository
	if contactRepo != nil && contactRepo.Pool() != nil {
		tagRepo = repository.NewTagRepository(contactRepo.Pool())
	}
	return &tagHandler{tagRepo: tagRepo}
}

func (h *tagHandler) Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error {
	var p TagParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if vc.ContactID == uuid.Nil {
		return fmt.Errorf("contact not resolved")
	}
	if h.tagRepo == nil {
		return fmt.Errorf("tag repository not configured")
	}
	if _, err := h.tagRepo.AddTagsByName(ctx, vc.WorkspaceID, vc.ContactID, p.Tags); err != nil {
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
