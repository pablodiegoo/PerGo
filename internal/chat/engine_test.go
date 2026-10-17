package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/chat"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// --- Mocks ---

type fakeChatRepo struct {
	mu       sync.Mutex
	chats    map[uuid.UUID]*domain.Chat
	messages map[uuid.UUID][]*domain.ChatMessage
}

func newFakeChatRepo() *fakeChatRepo {
	return &fakeChatRepo{
		chats:    make(map[uuid.UUID]*domain.Chat),
		messages: make(map[uuid.UUID][]*domain.ChatMessage),
	}
}

func (r *fakeChatRepo) FindOrCreateChat(_ context.Context, workspaceID uuid.UUID, connectionID *uuid.UUID, contactID uuid.UUID) (*domain.Chat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.chats {
		if c.WorkspaceID == workspaceID && c.ContactID == contactID {
			cp := *c
			return &cp, nil
		}
	}
	now := time.Now().UTC()
	c := &domain.Chat{
		ID:            uuid.New(),
		WorkspaceID:   workspaceID,
		ConnectionID:  connectionID,
		ContactID:     contactID,
		Status:        domain.ChatStatusOpen,
		Tags:          []string{},
		LastMessageAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	r.chats[c.ID] = c
	cp := *c
	return &cp, nil
}

func (r *fakeChatRepo) GetChat(_ context.Context, workspaceID, chatID uuid.UUID) (*domain.Chat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, repository.ErrChatNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *fakeChatRepo) GetChatByID(_ context.Context, chatID uuid.UUID) (*domain.Chat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok {
		return nil, repository.ErrChatNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *fakeChatRepo) GetChatByContact(_ context.Context, workspaceID, contactID uuid.UUID) (*domain.Chat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.chats {
		if c.WorkspaceID == workspaceID && c.ContactID == contactID {
			cp := *c
			return &cp, nil
		}
	}
	return nil, repository.ErrChatNotFound
}

func (r *fakeChatRepo) ListChats(_ context.Context, workspaceID uuid.UUID, filter domain.ChatFilter, _, _ int) ([]domain.Chat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Chat
	for _, c := range r.chats {
		if c.WorkspaceID != workspaceID {
			continue
		}
		if filter.Status != "" && string(c.Status) != filter.Status {
			if !(filter.Status == string(domain.ChatStatusOpen) &&
				c.Status == domain.ChatStatusSnoozed &&
				c.SnoozedUntil != nil && !c.SnoozedUntil.After(time.Now().UTC())) {
				continue
			}
		}
		if filter.ContactID != nil && c.ContactID != *filter.ContactID {
			continue
		}
		if filter.Unassigned != nil && *filter.Unassigned && c.AssignedEmail != nil && *c.AssignedEmail != "" {
			continue
		}
		if filter.AssignedEmail != nil && (c.AssignedEmail == nil || *c.AssignedEmail != *filter.AssignedEmail) {
			continue
		}
		if filter.Tag != "" {
			hasTag := false
			for _, t := range c.Tags {
				if strings.EqualFold(t, filter.Tag) {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}
		out = append(out, *c)
	}
	return out, nil
}

func (r *fakeChatRepo) UpdateChatStatus(_ context.Context, workspaceID, chatID uuid.UUID, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.Status = domain.ChatStatus(status)
	if status != string(domain.ChatStatusSnoozed) {
		c.SnoozedUntil = nil
	}
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeChatRepo) SnoozeChat(_ context.Context, workspaceID, chatID uuid.UUID, snoozedUntil *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.Status = domain.ChatStatusSnoozed
	c.SnoozedUntil = snoozedUntil
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeChatRepo) AssignChat(_ context.Context, workspaceID, chatID uuid.UUID, userID *uuid.UUID, email *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.AssignedUserID = userID
	c.AssignedEmail = email
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeChatRepo) SetChatTags(_ context.Context, workspaceID, chatID uuid.UUID, tags []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.Tags = tags
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeChatRepo) UpdateChatUnreadCount(_ context.Context, workspaceID, chatID uuid.UUID, unreadCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.UnreadCount = unreadCount
	return nil
}

func (r *fakeChatRepo) TouchLastMessageAt(_ context.Context, workspaceID, chatID uuid.UUID, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.LastMessageAt = t
	return nil
}

func (r *fakeChatRepo) SetAIDisabled(_ context.Context, workspaceID, chatID uuid.UUID, disabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || c.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	c.AIDisabled = disabled
	return nil
}

func (r *fakeChatRepo) AddChatMessage(_ context.Context, msg *domain.ChatMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	cp := *msg
	r.messages[msg.ChatID] = append(r.messages[msg.ChatID], &cp)
	return nil
}

func (r *fakeChatRepo) CreateInternalNote(_ context.Context, workspaceID, chatID uuid.UUID, authorName, senderType, body string) (*domain.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chats[chatID]
	if !ok || (workspaceID != uuid.Nil && c.WorkspaceID != workspaceID) {
		return nil, repository.ErrChatNotFound
	}
	msg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chatID,
		WorkspaceID: c.WorkspaceID,
		UID:         "note-" + uuid.New().String(),
		Direction:   domain.DirectionInternalNote,
		SenderType:  domain.SenderType(senderType),
		SenderName:  authorName,
		Body:        body,
		IsPrivate:   true,
		Reactions:   []domain.Reaction{},
		Metadata:    map[string]interface{}{"type": "internal_note"},
		CreatedAt:   time.Now().UTC(),
	}
	r.messages[chatID] = append(r.messages[chatID], msg)
	cp := *msg
	return &cp, nil
}

func (r *fakeChatRepo) ListChatMessages(_ context.Context, workspaceID, chatID uuid.UUID, _, _ string, limit int) ([]domain.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msgs := r.messages[chatID]
	var out []domain.ChatMessage
	for _, m := range msgs {
		if m.WorkspaceID == workspaceID {
			out = append(out, *m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (r *fakeChatRepo) GetChatMessageByUID(_ context.Context, workspaceID uuid.UUID, uid string) (*domain.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgs := range r.messages {
		for _, m := range msgs {
			if m.WorkspaceID == workspaceID && m.UID == uid {
				cp := *m
				return &cp, nil
			}
		}
	}
	return nil, repository.ErrChatMessageNotFound
}

func (r *fakeChatRepo) FindMessageByUID(_ context.Context, uid string) (*domain.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgs := range r.messages {
		for _, m := range msgs {
			if m.UID == uid {
				cp := *m
				return &cp, nil
			}
		}
	}
	return nil, repository.ErrChatMessageNotFound
}

func (r *fakeChatRepo) UpdateMessageMetadata(_ context.Context, workspaceID uuid.UUID, uid string, metadata map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgs := range r.messages {
		for _, m := range msgs {
			if m.WorkspaceID == workspaceID && m.UID == uid {
				if m.Metadata == nil {
					m.Metadata = make(map[string]interface{})
				}
				for k, v := range metadata {
					m.Metadata[k] = v
				}
				return nil
			}
		}
	}
	return repository.ErrChatMessageNotFound
}

func (r *fakeChatRepo) GetReactions(_ context.Context, workspaceID uuid.UUID, messageUID string) ([]domain.Reaction, error) {
	msg, err := r.GetChatMessageByUID(context.Background(), workspaceID, messageUID)
	if err != nil || msg == nil {
		return nil, repository.ErrChatMessageNotFound
	}
	return msg.Reactions, nil
}

func (r *fakeChatRepo) AddReaction(_ context.Context, workspaceID uuid.UUID, messageUID string, reaction domain.Reaction) ([]domain.Reaction, error) {
	_, _, rx, err := r.MutateReaction(context.Background(), workspaceID, messageUID, reaction.Emoji, reaction.Sender, "add")
	return rx, err
}

func (r *fakeChatRepo) RemoveReaction(_ context.Context, workspaceID uuid.UUID, messageUID string, emoji, sender string) ([]domain.Reaction, error) {
	_, _, rx, err := r.MutateReaction(context.Background(), workspaceID, messageUID, emoji, sender, "remove")
	return rx, err
}

func (r *fakeChatRepo) MutateReaction(_ context.Context, workspaceID uuid.UUID, messageUID, emoji, sender, action string) (uuid.UUID, string, []domain.Reaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgs := range r.messages {
		for _, m := range msgs {
			if m.WorkspaceID == workspaceID && m.UID == messageUID {
				resolvedAction := action
				if resolvedAction == "" || resolvedAction == "toggle" {
					resolvedAction = "add"
				}
				if resolvedAction == "remove" {
					var next []domain.Reaction
					for _, rx := range m.Reactions {
						if !(rx.Emoji == emoji && rx.Sender == sender) {
							next = append(next, rx)
						}
					}
					m.Reactions = next
				} else {
					m.Reactions = append(m.Reactions, domain.Reaction{
						Emoji:     emoji,
						Sender:    sender,
						CreatedAt: time.Now().UTC(),
					})
				}
				return m.ChatID, resolvedAction, m.Reactions, nil
			}
		}
	}
	return uuid.Nil, "", nil, repository.ErrChatMessageNotFound
}

func (r *fakeChatRepo) ResetUnreadByContact(_ context.Context, workspaceID, contactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.chats {
		if c.WorkspaceID == workspaceID && c.ContactID == contactID {
			c.UnreadCount = 0
		}
	}
	return nil
}

func (r *fakeChatRepo) ListConversations(_ context.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repository.ConversationSummary
	for _, c := range r.chats {
		if c.WorkspaceID != workspaceID {
			continue
		}
		msgs := r.messages[c.ID]
		if len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		ch := "whatsapp"
		if last.Metadata != nil {
			if mc, ok := last.Metadata["channel"].(string); ok && mc != "" {
				ch = mc
			}
		}
		if channelFilter != "" && channelFilter != ch {
			continue
		}
		out = append(out, repository.ConversationSummary{
			ChatID:          c.ID,
			ContactID:       c.ContactID,
			Channel:         ch,
			LastMessageBody: last.Body,
			LastMessageTime: c.LastMessageAt,
			Unread:          c.UnreadCount > 0,
			UnreadCount:     c.UnreadCount,
			Status:          string(c.Status),
		})
	}
	return out, nil
}

func (r *fakeChatRepo) ListThreadByContact(_ context.Context, workspaceID, contactID uuid.UUID, afterID *uuid.UUID) ([]repository.ThreadMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repository.ThreadMessage
	for _, c := range r.chats {
		if c.WorkspaceID != workspaceID || c.ContactID != contactID {
			continue
		}
		msgs := r.messages[c.ID]
		seenAfter := afterID == nil
		for _, m := range msgs {
			if !seenAfter {
				if m.ID == *afterID {
					seenAfter = true
				}
				continue
			}
			out = append(out, repository.ThreadMessage{
				ID:        m.ID,
				TraceID:   m.UID,
				Direction: string(m.Direction),
				Body:      m.Body,
				CreatedAt: m.CreatedAt,
			})
		}
	}
	return out, nil
}

type fakeContactRepo struct {
	mu            sync.Mutex
	contacts      map[uuid.UUID]*domain.Contact
	markedRead    []uuid.UUID
	getByIDCalls  int
	getByIDsCalls int
}

func newFakeContactRepo() *fakeContactRepo {
	return &fakeContactRepo{contacts: make(map[uuid.UUID]*domain.Contact)}
}

func (r *fakeContactRepo) MarkAsRead(_ context.Context, workspaceID, contactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.contacts[contactID]; ok && c.WorkspaceID == workspaceID {
		r.markedRead = append(r.markedRead, contactID)
	}
	return nil
}

func (r *fakeContactRepo) GetByID(_ context.Context, workspaceID, contactID uuid.UUID) (*domain.Contact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getByIDCalls++
	c, ok := r.contacts[contactID]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (r *fakeContactRepo) GetByIDs(_ context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*domain.Contact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getByIDsCalls++
	res := make(map[uuid.UUID]*domain.Contact, len(ids))
	for _, id := range ids {
		if c, ok := r.contacts[id]; ok && c.WorkspaceID == workspaceID {
			cp := *c
			res[id] = &cp
		}
	}
	return res, nil
}

func (r *fakeContactRepo) SetTags(_ context.Context, workspaceID, contactID uuid.UUID, tags []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.contacts[contactID]; ok && c.WorkspaceID == workspaceID {
		c.Tags = tags
	}
	return nil
}

func (r *fakeContactRepo) CloseThread(_ context.Context, workspaceID, contactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.contacts[contactID]; ok && c.WorkspaceID == workspaceID {
		now := time.Now().UTC()
		c.ClosedAt = &now
	}
	return nil
}

func (r *fakeContactRepo) ReopenThread(_ context.Context, workspaceID, contactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.contacts[contactID]; ok && c.WorkspaceID == workspaceID {
		c.ClosedAt = nil
	}
	return nil
}

func (r *fakeContactRepo) UpdateBotState(_ context.Context, workspaceID, contactID uuid.UUID, botActive bool, pausedAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.contacts[contactID]; ok && c.WorkspaceID == workspaceID {
		c.BotActive = botActive
		c.BotPausedAt = pausedAt
	}
	return nil
}

type fakeConnRepo struct {
	conns map[uuid.UUID]*repository.Connection
}

func (r *fakeConnRepo) GetByID(_ context.Context, id uuid.UUID) (*repository.Connection, error) {
	if c, ok := r.conns[id]; ok {
		return c, nil
	}
	return nil, nil
}

func (r *fakeConnRepo) GetDefaultChannelConnection(_ context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
	for _, c := range r.conns {
		if c.WorkspaceID == workspaceID && c.Channel == channel {
			return c, nil
		}
	}
	return nil, nil
}

func (r *fakeConnRepo) GetBySenderIdentity(_ context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
	for _, c := range r.conns {
		if c.WorkspaceID == workspaceID && c.SenderIdentity == senderIdentity {
			return c, nil
		}
	}
	return nil, nil
}

func (r *fakeConnRepo) ListByWorkspace(_ context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error) {
	var out []*repository.Connection
	for _, c := range r.conns {
		if c.WorkspaceID == workspaceID {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeWsRepo struct {
	members map[string]*repository.WorkspaceMember
}

func (r *fakeWsRepo) GetMemberByEmail(_ context.Context, workspaceID uuid.UUID, email string) (*repository.WorkspaceMember, error) {
	m, ok := r.members[strings.ToLower(strings.TrimSpace(email))]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, nil
	}
	return m, nil
}

type fakeAuditRepo struct {
	mu      sync.Mutex
	entries []*repository.AuditEntry
}

func (r *fakeAuditRepo) InsertAuditLog(_ context.Context, entry *repository.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return nil
}

type publishedMsg struct {
	subject string
	data    []byte
	traceID string
}

type fakePublisher struct {
	mu        sync.Mutex
	published []publishedMsg
}

func (p *fakePublisher) Publish(_ context.Context, subject string, data []byte, traceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, publishedMsg{subject: subject, data: data, traceID: traceID})
	return nil
}

type fakeDebouncer struct {
	mu        sync.Mutex
	cancelled []struct {
		wsID      uuid.UUID
		contactID uuid.UUID
	}
}

func (d *fakeDebouncer) Cancel(_ context.Context, workspaceID, contactID uuid.UUID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = append(d.cancelled, struct {
		wsID      uuid.UUID
		contactID uuid.UUID
	}{wsID: workspaceID, contactID: contactID})
	return nil
}

type fakeIngestor struct {
	mu       sync.Mutex
	requests []*domain.CreateMessageRequest
	err      error
}

func (i *fakeIngestor) Ingest(_ context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.err != nil {
		return nil, i.err
	}
	i.requests = append(i.requests, req)
	return &domain.QueueMessage{
		WorkspaceID: workspaceID,
		TraceID:     traceID,
		To:          req.To,
		Channel:     req.Channel,
		Body:        req.Body,
		QueuedAt:    time.Now().UTC(),
	}, nil
}

// --- Tests ---

func TestEngine_ResolveChat_PolymorphicAndWorkspaceScope(t *testing.T) {
	ctx := context.Background()
	wsA := uuid.New()
	wsB := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()
	connID := uuid.New()
	connRepo := &fakeConnRepo{
		conns: map[uuid.UUID]*repository.Connection{
			connID: {ID: connID, WorkspaceID: wsA, Channel: "whatsapp", Status: "connected", SenderIdentity: "5511000000"},
		},
	}

	contactA := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsA,
		Name:        "Maria",
		Identities: []domain.ContactIdentity{
			{Channel: "whatsapp", SenderIdentity: "+5511999999999"},
		},
	}
	contactRepo.contacts[contactA.ID] = contactA

	chatA := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsA,
		ConnectionID: &connID,
		ContactID:    contactA.ID,
		Status:       domain.ChatStatusOpen,
	}
	chatRepo.chats[chatA.ID] = chatA

	msgA := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chatA.ID,
		WorkspaceID: wsA,
		UID:         "uid-123",
		Body:        "Hola",
	}
	chatRepo.messages[chatA.ID] = []*domain.ChatMessage{msgA}

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
		chat.WithConnectionRepo(connRepo),
	)

	// 1. Resolve by ChatID
	resolved, err := eng.ResolveChat(ctx, wsA, chat.ChatRef{ChatID: chatA.ID})
	if err != nil || resolved.ID != chatA.ID {
		t.Fatalf("expected chatA by ChatID, got %v, err=%v", resolved, err)
	}

	// 2. Resolve by ContactID
	resolved, err = eng.ResolveChat(ctx, wsA, chat.ChatRef{ContactID: contactA.ID})
	if err != nil || resolved.ID != chatA.ID {
		t.Fatalf("expected chatA by ContactID, got %v, err=%v", resolved, err)
	}

	// 3. Resolve by MessageUID
	resolved, err = eng.ResolveChat(ctx, wsA, chat.ChatRef{MessageUID: "uid-123"})
	if err != nil || resolved.ID != chatA.ID {
		t.Fatalf("expected chatA by MessageUID, got %v, err=%v", resolved, err)
	}

	// 4. Resolve by Channel + Phone
	resolved, err = eng.ResolveChat(ctx, wsA, chat.ChatRef{Channel: "whatsapp", Phone: "+5511999999999"})
	if err != nil || resolved.ID != chatA.ID {
		t.Fatalf("expected chatA by Channel+Phone, got %v, err=%v", resolved, err)
	}

	// 5. Cross-workspace scope enforcement (wsB cannot resolve wsA's chat by any ref)
	if _, err := eng.ResolveChat(ctx, wsB, chat.ChatRef{ChatID: chatA.ID}); !errors.Is(err, chat.ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound for cross-workspace ChatID, got %v", err)
	}
	if _, err := eng.ResolveChat(ctx, wsB, chat.ChatRef{ContactID: contactA.ID}); !errors.Is(err, chat.ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound for cross-workspace ContactID, got %v", err)
	}
	if _, err := eng.ResolveChat(ctx, wsB, chat.ChatRef{MessageUID: "uid-123"}); !errors.Is(err, chat.ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound for cross-workspace MessageUID, got %v", err)
	}
	// Authenticated tenant context mismatch also rejected
	ctxB := tenant.WithWorkspaceID(ctx, wsB)
	if _, err := eng.ResolveChat(ctxB, wsA, chat.ChatRef{ChatID: chatA.ID}); !errors.Is(err, chat.ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound when tenant context mismatches wsA, got %v", err)
	}
}

func TestEngine_LazySnoozeReopen_OnListAndGet(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	closedNow := time.Now().UTC()
	contact1 := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Snoozed Contact 1",
		ClosedAt:    &closedNow,
	}
	contact2 := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Snoozed Contact 2",
		ClosedAt:    &closedNow,
	}
	contact3 := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Snoozed Contact 3",
		ClosedAt:    &closedNow,
	}
	contact4 := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Snoozed Contact 4",
		ClosedAt:    &closedNow,
	}
	contactRepo.contacts[contact1.ID] = contact1
	contactRepo.contacts[contact2.ID] = contact2
	contactRepo.contacts[contact3.ID] = contact3
	contactRepo.contacts[contact4.ID] = contact4

	past := time.Now().UTC().Add(-10 * time.Minute)
	chat1 := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ContactID:    contact1.ID,
		Status:       domain.ChatStatusSnoozed,
		SnoozedUntil: &past,
		UnreadCount:  3,
	}
	chat2 := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ContactID:    contact2.ID,
		Status:       domain.ChatStatusSnoozed,
		SnoozedUntil: &past,
	}
	chat3 := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ContactID:    contact3.ID,
		Status:       domain.ChatStatusSnoozed,
		SnoozedUntil: &past,
	}
	chat4 := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ContactID:    contact4.ID,
		Status:       domain.ChatStatusSnoozed,
		SnoozedUntil: &past,
	}
	chatRepo.chats[chat1.ID] = chat1
	chatRepo.chats[chat2.ID] = chat2

	eng := chat.NewEngine(chatRepo, chat.WithContactRepo(contactRepo))

	// Get with markRead=true lazily reopens expired snooze AND resets unread count
	res, err := eng.Get(ctx, wsID, chat.ChatRef{ChatID: chat1.ID}, 50, true)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if res.Chat.Status != domain.ChatStatusOpen || res.Chat.SnoozedUntil != nil {
		t.Fatalf("expected chat1 to be reopened with nil SnoozedUntil, got status=%s snoozedUntil=%v", res.Chat.Status, res.Chat.SnoozedUntil)
	}
	if res.Chat.UnreadCount != 0 {
		t.Fatalf("expected unread count 0 after markRead=true, got %d", res.Chat.UnreadCount)
	}
	if contactRepo.contacts[contact1.ID].ClosedAt != nil {
		t.Fatalf("expected contact1 ClosedAt to be nil (reopened), got %v", contactRepo.contacts[contact1.ID].ClosedAt)
	}

	// List lazily reopens expired snooze on chat2 and syncs contact2.ReopenThread
	items, err := eng.List(ctx, wsID, domain.ChatFilter{}, 20, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	var foundChat2 *chat.ChatSummary
	for i := range items {
		if items[i].ID == chat2.ID {
			foundChat2 = &items[i]
			break
		}
	}
	if foundChat2 == nil {
		t.Fatalf("expected chat2 in list")
	}
	if foundChat2.Status != domain.ChatStatusOpen || foundChat2.SnoozedUntil != nil {
		t.Fatalf("expected chat2 in list to be reopened with nil SnoozedUntil, got status=%s", foundChat2.Status)
	}
	if contactRepo.contacts[contact2.ID].ClosedAt != nil {
		t.Fatalf("expected contact2 ClosedAt to be nil (reopened) on List, got %v", contactRepo.contacts[contact2.ID].ClosedAt)
	}

	// ListMessages lazily reopens expired snooze on chat3 and syncs contact3.ReopenThread
	chatRepo.chats[chat3.ID] = chat3
	reopenedChat3, _, err := eng.ListMessages(ctx, wsID, chat.ChatRef{ChatID: chat3.ID}, "", "", 20)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if reopenedChat3.Status != domain.ChatStatusOpen || reopenedChat3.SnoozedUntil != nil {
		t.Fatalf("expected chat3 on ListMessages to be reopened with nil SnoozedUntil, got status=%s snoozedUntil=%v", reopenedChat3.Status, reopenedChat3.SnoozedUntil)
	}
	if contactRepo.contacts[contact3.ID].ClosedAt != nil {
		t.Fatalf("expected contact3 ClosedAt to be nil (reopened) on ListMessages, got %v", contactRepo.contacts[contact3.ID].ClosedAt)
	}

	// Summarize lazily reopens expired snooze on chat4 and syncs contact4.ReopenThread
	chatRepo.chats[chat4.ID] = chat4
	if _, err := eng.Summarize(ctx, wsID, chat.ChatRef{ChatID: chat4.ID}, 50); err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if chatRepo.chats[chat4.ID].Status != domain.ChatStatusOpen || chatRepo.chats[chat4.ID].SnoozedUntil != nil {
		t.Fatalf("expected chat4 on Summarize to be reopened with nil SnoozedUntil, got status=%s snoozedUntil=%v", chatRepo.chats[chat4.ID].Status, chatRepo.chats[chat4.ID].SnoozedUntil)
	}
	if contactRepo.contacts[contact4.ID].ClosedAt != nil {
		t.Fatalf("expected contact4 ClosedAt to be nil (reopened) on Summarize, got %v", contactRepo.contacts[contact4.ID].ClosedAt)
	}
}

func TestEngine_TriageMutations_StatusTagsAssign(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()
	auditRepo := &fakeAuditRepo{}
	memberID := uuid.New()
	wsRepo := &fakeWsRepo{
		members: map[string]*repository.WorkspaceMember{
			"agent@pergo.io": {ID: memberID, WorkspaceID: wsID, Email: "agent@pergo.io", Role: "operator"},
		},
	}

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Tags:        []string{"initial"},
	}
	contactRepo.contacts[contact.ID] = contact

	c := &domain.Chat{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		ContactID:   contact.ID,
		Status:      domain.ChatStatusOpen,
		Tags:        []string{"initial"},
	}
	chatRepo.chats[c.ID] = c

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
		chat.WithWorkspaceRepo(wsRepo),
		chat.WithAuditRepo(auditRepo),
	)

	// 1. UpdateStatus -> closed syncs CloseThread
	updated, _, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{Status: "resolved"})
	if err != nil {
		t.Fatalf("UpdateStatus closed failed: %v", err)
	}
	if updated.Status != domain.ChatStatusClosed {
		t.Fatalf("expected closed status, got %s", updated.Status)
	}
	if contactRepo.contacts[contact.ID].ClosedAt == nil {
		t.Fatalf("expected contact ClosedAt to be non-nil")
	}

	// 2. UpdateStatus -> open syncs ReopenThread
	updated, _, err = eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{Status: "reopen"})
	if err != nil {
		t.Fatalf("UpdateStatus open failed: %v", err)
	}
	if updated.Status != domain.ChatStatusOpen {
		t.Fatalf("expected open status, got %s", updated.Status)
	}
	if contactRepo.contacts[contact.ID].ClosedAt != nil {
		t.Fatalf("expected contact ClosedAt to be nil (reopened), got %v", contactRepo.contacts[contact.ID].ClosedAt)
	}

	// 3. UpdateTags -> Add & Remove syncs chat and contact tags
	tags, err := eng.UpdateTags(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.TagMutation{
		Add:    []string{"vip", "urgent"},
		Remove: []string{"initial"},
	})
	if err != nil {
		t.Fatalf("UpdateTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", tags)
	}
	if len(contactRepo.contacts[contact.ID].Tags) != 2 {
		t.Fatalf("expected contact tags synced to 2 tags, got %v", contactRepo.contacts[contact.ID].Tags)
	}

	// 4. Assign -> non-member rejected, valid member succeeds
	_, _, err = eng.Assign(ctx, wsID, chat.ChatRef{ChatID: c.ID}, "stranger@example.com")
	if !errors.Is(err, chat.ErrInvalidMember) {
		t.Fatalf("expected ErrInvalidMember for unknown email, got %v", err)
	}

	assigned, member, err := eng.Assign(ctx, wsID, chat.ChatRef{ChatID: c.ID}, "agent@pergo.io")
	if err != nil {
		t.Fatalf("Assign valid member failed: %v", err)
	}
	if member == nil || assigned.AssignedUserID == nil || *assigned.AssignedUserID != memberID {
		t.Fatalf("expected AssignedUserID=%v, got %v", memberID, assigned.AssignedUserID)
	}
}

func TestEngine_SendMessage_PromoteDraft_SetAIEnabled_TakeoverAndBackpressure(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()
	auditRepo := &fakeAuditRepo{}
	pub := &fakePublisher{}
	debouncer := &fakeDebouncer{}
	ingestor := &fakeIngestor{}

	connID := uuid.New()
	connRepo := &fakeConnRepo{
		conns: map[uuid.UUID]*repository.Connection{
			connID: {ID: connID, WorkspaceID: wsID, Channel: "whatsapp_cloud", Status: "connected", SenderIdentity: "15550001111"},
		},
	}

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		BotActive:   true,
		Identities: []domain.ContactIdentity{
			{Channel: "whatsapp_cloud", SenderIdentity: "+5511888888888"},
		},
	}
	contactRepo.contacts[contact.ID] = contact

	future := time.Now().UTC().Add(2 * time.Hour)
	c := &domain.Chat{
		ID:                     uuid.New(),
		WorkspaceID:            wsID,
		ConnectionID:           &connID,
		ContactID:              contact.ID,
		Status:                 domain.ChatStatusOpen,
		AIDisabled:             false,
		ServiceWindowExpiresAt: &future,
	}
	chatRepo.chats[c.ID] = c

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
		chat.WithConnectionRepo(connRepo),
		chat.WithAuditRepo(auditRepo),
		chat.WithMessagePublisher(pub),
		chat.WithMessageIngestor(ingestor),
		chat.WithDebounceCanceller(debouncer),
	)

	// 1. SendMessage delegates to OutboundProcessor.Ingest, sets ai_disabled=true, bot_active=false, cancels debounce, publishes handoff events
	res, err := eng.SendMessage(ctx, wsID, chat.SendRequest{
		Ref:        chat.ChatRef{ChatID: c.ID},
		Body:       "Human agent stepping in",
		SenderType: string(domain.SenderTypeHumanAgent),
		SenderName: "Agent Smith",
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if !res.AIDisabled {
		t.Fatalf("expected AIDisabled=true on human send")
	}
	if len(ingestor.requests) != 1 {
		t.Fatalf("expected 1 call to ingestor.Ingest, got %d", len(ingestor.requests))
	}
	if !chatRepo.chats[c.ID].AIDisabled {
		t.Fatalf("expected chat.AIDisabled=true in repo")
	}
	if contactRepo.contacts[contact.ID].BotActive {
		t.Fatalf("expected contact.BotActive=false in repo")
	}
	if len(debouncer.cancelled) != 1 || debouncer.cancelled[0].contactID != contact.ID {
		t.Fatalf("expected debouncer.Cancel to be called for contact, got %v", debouncer.cancelled)
	}
	hasGlobalHandoff := false
	hasWsHandoff := false
	for _, p := range pub.published {
		if p.subject == "chat.handoff.human_takeover" {
			hasGlobalHandoff = true
		}
		if p.subject == "chat.handoff."+wsID.String() {
			hasWsHandoff = true
		}
	}
	if !hasGlobalHandoff || !hasWsHandoff {
		t.Fatalf("expected both handoff subjects published, got %+v", pub.published)
	}

	// 2. Queue-depth backpressure error from OutboundProcessor.Ingest propagates cleanly
	ingestor.err = outbound.ErrQueueFull
	_, err = eng.SendMessage(ctx, wsID, chat.SendRequest{
		Ref:  chat.ChatRef{ChatID: c.ID},
		Body: "Blocked by queue depth",
	})
	if !errors.Is(err, outbound.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	ingestor.err = nil

	// 3. SetAIEnabled re-enables AI and updates contact.BotActive = true, then disabling AI cancels debounce and publishes handoff
	updatedChat, err := eng.SetAIEnabled(ctx, wsID, chat.ChatRef{ChatID: c.ID}, true)
	if err != nil {
		t.Fatalf("SetAIEnabled(true) failed: %v", err)
	}
	if updatedChat.AIDisabled || !contactRepo.contacts[contact.ID].BotActive {
		t.Fatalf("expected AI enabled and BotActive=true")
	}

	prevCancelCount := len(debouncer.cancelled)
	updatedChat, err = eng.SetAIEnabled(ctx, wsID, chat.ChatRef{ChatID: c.ID}, false)
	if err != nil {
		t.Fatalf("SetAIEnabled(false) failed: %v", err)
	}
	if !updatedChat.AIDisabled || contactRepo.contacts[contact.ID].BotActive {
		t.Fatalf("expected AI disabled and BotActive=false")
	}
	if len(debouncer.cancelled) != prevCancelCount+1 {
		t.Fatalf("expected debouncer.Cancel on SetAIEnabled(false)")
	}

	// 4. PromoteDraft promotes a whisper note, dispatches via SendMessage, and marks original note metadata as promoted
	note, err := eng.CreateNote(ctx, wsID, chat.NoteRequest{
		Ref:        chat.ChatRef{ChatID: c.ID},
		Body:       "[AI Draft]: Here is your invoice",
		AuthorName: "Copilot",
	})
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}
	promoteRes, err := eng.PromoteDraft(ctx, wsID, chat.ChatRef{ChatID: c.ID}, note.UID, "Here is your updated invoice")
	if err != nil {
		t.Fatalf("PromoteDraft failed: %v", err)
	}
	if promoteRes.Message == nil || promoteRes.Message.Body != "Here is your updated invoice" {
		t.Fatalf("expected promoted message with edited body, got %+v", promoteRes.Message)
	}
	updatedNote, err := chatRepo.GetChatMessageByUID(ctx, wsID, note.UID)
	if err != nil || updatedNote == nil {
		t.Fatalf("failed to fetch original draft note after promotion: %v", err)
	}
	if updatedNote.Metadata["draft_promoted"] != true {
		t.Fatalf("expected original draft note metadata draft_promoted=true, got %+v", updatedNote.Metadata)
	}
	if updatedNote.Metadata["draft_status"] != "promoted" {
		t.Fatalf("expected original draft note metadata draft_status='promoted', got %+v", updatedNote.Metadata)
	}
	if updatedNote.Metadata["promoted_message_uid"] != promoteRes.TraceID {
		t.Fatalf("expected original draft note metadata promoted_message_uid=%q, got %+v", promoteRes.TraceID, updatedNote.Metadata["promoted_message_uid"])
	}

	// 5. Summarize produces structured ChatSynopsis
	synopsis, err := eng.Summarize(ctx, wsID, chat.ChatRef{ChatID: c.ID}, 50)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if synopsis.CurrentStatus == "" {
		raw, _ := json.Marshal(synopsis)
		t.Fatalf("expected non-empty synopsis, got %s", string(raw))
	}
}

func TestEngine_InboxConversationAndThreadMethods(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Inbox Contact",
		BotActive:   true,
	}
	contactRepo.contacts[contact.ID] = contact

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	// 1. EnsureChatForContact creates or resolves a chat for the contact
	c, err := eng.EnsureChatForContact(ctx, wsID, contact.ID, false)
	if err != nil || c == nil {
		t.Fatalf("EnsureChatForContact failed: %v", err)
	}
	chatRepo.chats[c.ID].UnreadCount = 4

	msgID := uuid.New()
	_ = chatRepo.AddChatMessage(ctx, &domain.ChatMessage{
		ID:          msgID,
		ChatID:      c.ID,
		WorkspaceID: wsID,
		UID:         "inbox-uid-1",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		Body:        "New message in inbox",
		Metadata:    map[string]any{"channel": "whatsapp"},
	})

	// 2. ListConversations returns single-query summary with unread=true
	convs, err := eng.ListConversations(ctx, wsID, "")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if len(convs) != 1 || !convs[0].Unread || convs[0].UnreadCount != 4 {
		t.Fatalf("expected 1 unread conversation with count 4, got %+v", convs)
	}

	// 3. ListThread with markRead=true returns thread and resets unread_count to 0
	thread, err := eng.ListThread(ctx, wsID, contact.ID, nil, true)
	if err != nil {
		t.Fatalf("ListThread failed: %v", err)
	}
	if len(thread) != 1 || thread[0].Body != "New message in inbox" {
		t.Fatalf("unexpected thread: %+v", thread)
	}
	if chatRepo.chats[c.ID].UnreadCount != 0 {
		t.Fatalf("expected unread_count reset to 0 after ListThread(markRead=true), got %d", chatRepo.chats[c.ID].UnreadCount)
	}

	// 4. ToggleContactBot toggles BotActive and syncs chat.AIDisabled
	updatedContact, err := eng.ToggleContactBot(ctx, wsID, contact.ID)
	if err != nil {
		t.Fatalf("ToggleContactBot failed: %v", err)
	}
	if updatedContact.BotActive {
		t.Fatalf("expected BotActive=false after toggle")
	}
	if !chatRepo.chats[c.ID].AIDisabled {
		t.Fatalf("expected chat.AIDisabled=true when BotActive toggled to false")
	}
}

func TestEngine_ResolveChat_WithConnectionID(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	connA := uuid.New()
	connB := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Multi-Conn Contact",
		Identities: []domain.ContactIdentity{
			{
				WorkspaceID:    wsID,
				ContactID:      uuid.Nil,
				Channel:        "whatsapp_cloud",
				SenderIdentity: "+5511999998888",
			},
		},
	}
	contact.Identities[0].ContactID = contact.ID
	contactRepo.contacts[contact.ID] = contact

	chatA := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ConnectionID: &connA,
		ContactID:    contact.ID,
		Status:       domain.ChatStatusOpen,
	}
	chatB := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ConnectionID: &connB,
		ContactID:    contact.ID,
		Status:       domain.ChatStatusOpen,
	}
	chatRepo.chats[chatA.ID] = chatA
	chatRepo.chats[chatB.ID] = chatB

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	// 1. Resolve by ContactID + ConnectionID should return chatB when connB is specified
	resolvedByContactB, err := eng.ResolveChat(ctx, wsID, chat.ChatRef{
		ContactID:    contact.ID,
		ConnectionID: &connB,
	})
	if err != nil {
		t.Fatalf("ResolveChat by ContactID+ConnectionID(B) failed: %v", err)
	}
	if resolvedByContactB.ID != chatB.ID {
		t.Fatalf("expected chatB (%s), got %s", chatB.ID, resolvedByContactB.ID)
	}

	resolvedByContactA, err := eng.ResolveChat(ctx, wsID, chat.ChatRef{
		ContactID:    contact.ID,
		ConnectionID: &connA,
	})
	if err != nil {
		t.Fatalf("ResolveChat by ContactID+ConnectionID(A) failed: %v", err)
	}
	if resolvedByContactA.ID != chatA.ID {
		t.Fatalf("expected chatA (%s), got %s", chatA.ID, resolvedByContactA.ID)
	}

	// 2. Resolve by Channel + Phone + ConnectionID should return chatB when connB is specified
	resolvedByPhoneB, err := eng.ResolveChat(ctx, wsID, chat.ChatRef{
		Channel:      "whatsapp_cloud",
		Phone:        "+5511999998888",
		ConnectionID: &connB,
	})
	if err != nil {
		t.Fatalf("ResolveChat by Channel+Phone+ConnectionID(B) failed: %v", err)
	}
	if resolvedByPhoneB.ID != chatB.ID {
		t.Fatalf("expected chatB (%s), got %s", chatB.ID, resolvedByPhoneB.ID)
	}

	resolvedByPhoneA, err := eng.ResolveChat(ctx, wsID, chat.ChatRef{
		Channel:      "whatsapp_cloud",
		Phone:        "+5511999998888",
		ConnectionID: &connA,
	})
	if err != nil {
		t.Fatalf("ResolveChat by Channel+Phone+ConnectionID(A) failed: %v", err)
	}
	if resolvedByPhoneA.ID != chatA.ID {
		t.Fatalf("expected chatA (%s), got %s", chatA.ID, resolvedByPhoneA.ID)
	}
}

func TestEngine_GetMessage(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	otherWS := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Message Details Contact",
	}
	contactRepo.contacts[contact.ID] = contact

	pastSnooze := time.Now().UTC().Add(-15 * time.Minute)
	c := &domain.Chat{
		ID:           uuid.New(),
		WorkspaceID:  wsID,
		ContactID:    contact.ID,
		Status:       domain.ChatStatusSnoozed,
		SnoozedUntil: &pastSnooze,
	}
	chatRepo.chats[c.ID] = c

	msg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      c.ID,
		WorkspaceID: wsID,
		UID:         "wamid.details.123",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		Body:        "Checking message details",
	}
	_ = chatRepo.AddChatMessage(ctx, msg)
	// Restore snoozed status after AddChatMessage
	c.Status = domain.ChatStatusSnoozed
	c.SnoozedUntil = &pastSnooze

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	// 1. Cross-workspace context must be rejected with ErrMessageNotFound / ErrChatNotFound
	scopedOtherCtx := tenant.WithWorkspaceID(ctx, otherWS)
	if _, _, _, err := eng.GetMessage(scopedOtherCtx, wsID, "wamid.details.123"); err == nil {
		t.Fatalf("expected error on cross-workspace context, got nil")
	}

	// 2. Non-existent message UID returns ErrMessageNotFound
	if _, _, _, err := eng.GetMessage(ctx, wsID, "wamid.missing.999"); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("expected ErrMessageNotFound for missing UID, got %v", err)
	}

	// 3. Valid lookup returns message, lazily reopened parent chat, and contact
	gotMsg, gotChat, gotContact, err := eng.GetMessage(ctx, wsID, "wamid.details.123")
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if gotMsg == nil || gotMsg.UID != "wamid.details.123" {
		t.Fatalf("unexpected message: %+v", gotMsg)
	}
	if gotChat == nil || gotChat.ID != c.ID {
		t.Fatalf("unexpected chat: %+v", gotChat)
	}
	if gotChat.Status != domain.ChatStatusOpen {
		t.Fatalf("expected expired snooze on parent chat to lazily reopen to 'open', got %q", gotChat.Status)
	}
	if gotContact == nil || gotContact.ID != contact.ID {
		t.Fatalf("unexpected contact: %+v", gotContact)
	}
}

func TestEngine_UpdateStatus_PendingAndSnoozedValidation(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Status Validation Contact",
	}
	contactRepo.contacts[contact.ID] = contact

	c := &domain.Chat{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		ContactID:   contact.ID,
		Status:      domain.ChatStatusOpen,
	}
	chatRepo.chats[c.ID] = c

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	// 1. Transition to "pending" succeeds
	updated, _, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{
		Status: string(domain.ChatStatusPending),
	})
	if err != nil {
		t.Fatalf("UpdateStatus to pending failed: %v", err)
	}
	if updated.Status != domain.ChatStatusPending {
		t.Fatalf("expected status %q, got %q", domain.ChatStatusPending, updated.Status)
	}

	// 2. Transition to "snoozed" with nil SnoozedUntil fails with ErrInvalidStatus
	if _, _, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{
		Status: string(domain.ChatStatusSnoozed),
	}); !errors.Is(err, chat.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus when SnoozedUntil is nil, got %v", err)
	}

	// 3. Transition to "snoozed" with past SnoozedUntil fails with ErrInvalidStatus
	past := time.Now().UTC().Add(-10 * time.Minute)
	if _, _, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{
		Status:       string(domain.ChatStatusSnoozed),
		SnoozedUntil: &past,
	}); !errors.Is(err, chat.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus when SnoozedUntil is in the past, got %v", err)
	}

	// 4. Transition to "snoozed" with future SnoozedUntil succeeds
	future := time.Now().UTC().Add(2 * time.Hour)
	snoozedChat, snoozedUntil, err := eng.UpdateStatus(ctx, wsID, chat.ChatRef{ChatID: c.ID}, chat.StatusUpdate{
		Status:       string(domain.ChatStatusSnoozed),
		SnoozedUntil: &future,
	})
	if err != nil {
		t.Fatalf("UpdateStatus to snoozed with future timestamp failed: %v", err)
	}
	if snoozedChat.Status != domain.ChatStatusSnoozed || snoozedUntil == nil || !snoozedUntil.Equal(future) {
		t.Fatalf("expected snoozed chat with future SnoozedUntil, got status=%q, snoozedUntil=%v", snoozedChat.Status, snoozedUntil)
	}
}

func TestEngine_MarkAsRead_SynchronizedOnGetAndListThread(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "MarkAsRead Contact",
	}
	contactRepo.contacts[contact.ID] = contact

	c := &domain.Chat{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		ContactID:   contact.ID,
		Status:      domain.ChatStatusOpen,
		UnreadCount: 3,
	}
	chatRepo.chats[c.ID] = c

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	// 1. Engine.Get(..., markRead=true) resets UnreadCount AND calls contactRepo.MarkAsRead
	if _, err := eng.Get(ctx, wsID, chat.ChatRef{ChatID: c.ID}, 50, true); err != nil {
		t.Fatalf("Engine.Get(markRead=true) failed: %v", err)
	}
	if chatRepo.chats[c.ID].UnreadCount != 0 {
		t.Fatalf("expected UnreadCount=0 after Get(markRead=true), got %d", chatRepo.chats[c.ID].UnreadCount)
	}
	if len(contactRepo.markedRead) != 1 || contactRepo.markedRead[0] != contact.ID {
		t.Fatalf("expected contactRepo.MarkAsRead called on Get(markRead=true), got %v", contactRepo.markedRead)
	}

	// 2. Engine.ListThread(..., markRead=true) resets UnreadCount AND calls contactRepo.MarkAsRead
	chatRepo.chats[c.ID].UnreadCount = 2
	if _, err := eng.ListThread(ctx, wsID, contact.ID, nil, true); err != nil {
		t.Fatalf("Engine.ListThread(markRead=true) failed: %v", err)
	}
	if chatRepo.chats[c.ID].UnreadCount != 0 {
		t.Fatalf("expected UnreadCount=0 after ListThread(markRead=true), got %d", chatRepo.chats[c.ID].UnreadCount)
	}
	if len(contactRepo.markedRead) != 2 || contactRepo.markedRead[1] != contact.ID {
		t.Fatalf("expected contactRepo.MarkAsRead called on ListThread(markRead=true), got %v", contactRepo.markedRead)
	}
}

func TestEngine_SendMessage_QueueDepthPrecedenceOverServiceWindow(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()
	ingestor := &fakeIngestor{}

	connID := uuid.New()
	connRepo := &fakeConnRepo{
		conns: map[uuid.UUID]*repository.Connection{
			connID: {ID: connID, WorkspaceID: wsID, Channel: "whatsapp_cloud", Status: "connected", SenderIdentity: "15550001111"},
		},
	}

	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Identities: []domain.ContactIdentity{
			{Channel: "whatsapp_cloud", SenderIdentity: "+5511777777777"},
		},
	}
	contactRepo.contacts[contact.ID] = contact

	past := time.Now().UTC().Add(-2 * time.Hour)
	c := &domain.Chat{
		ID:                     uuid.New(),
		WorkspaceID:            wsID,
		ConnectionID:           &connID,
		ContactID:              contact.ID,
		Status:                 domain.ChatStatusOpen,
		ServiceWindowExpiresAt: &past, // Expired WABA window
	}
	chatRepo.chats[c.ID] = c

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
		chat.WithConnectionRepo(connRepo),
		chat.WithMessageIngestor(ingestor),
	)

	// 1. When queue is full AND service window is expired, ErrQueueFull takes precedence over ErrServiceWindowExpired
	ingestor.err = outbound.ErrQueueFull
	_, err := eng.SendMessage(ctx, wsID, chat.SendRequest{
		Ref:                 chat.ChatRef{ChatID: c.ID},
		Body:                "Freeform message on expired chat when queue is full",
		StrictServiceWindow: true,
	})
	if !errors.Is(err, outbound.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull to take precedence over ErrServiceWindowExpired, got %v", err)
	}

	// 2. When queue has capacity, expired service window returns ErrServiceWindowExpired
	ingestor.err = &outbound.ValidationError{
		Response: &domain.ErrorResponse{
			Code:    "SESSION_WINDOW_EXPIRED",
			Message: "free-form message blocked: 24h customer service window is closed",
		},
	}
	_, err = eng.SendMessage(ctx, wsID, chat.SendRequest{
		Ref:                 chat.ChatRef{ChatID: c.ID},
		Body:                "Freeform message on expired chat",
		StrictServiceWindow: true,
	})
	if !errors.Is(err, chat.ErrServiceWindowExpired) {
		t.Fatalf("expected ErrServiceWindowExpired when window is closed, got %v", err)
	}
}

func TestEngine_List_BatchContactLoading(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	chatRepo := newFakeChatRepo()
	contactRepo := newFakeContactRepo()

	for i := 0; i < 5; i++ {
		ct := &domain.Contact{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			Name:        "Batch Contact",
			Identities: []domain.ContactIdentity{
				{Channel: "whatsapp", SenderIdentity: "+5511999990000"},
			},
		}
		contactRepo.contacts[ct.ID] = ct

		ch := &domain.Chat{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			ContactID:   ct.ID,
			Status:      domain.ChatStatusOpen,
		}
		chatRepo.chats[ch.ID] = ch
	}

	eng := chat.NewEngine(
		chatRepo,
		chat.WithContactRepo(contactRepo),
	)

	summaries, err := eng.List(ctx, wsID, domain.ChatFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("Engine.List failed: %v", err)
	}
	if len(summaries) != 5 {
		t.Fatalf("expected 5 chat summaries, got %d", len(summaries))
	}
	if contactRepo.getByIDsCalls != 1 {
		t.Fatalf("expected 1 batch GetByIDs call, got %d", contactRepo.getByIDsCalls)
	}
	if contactRepo.getByIDCalls != 0 {
		t.Fatalf("expected 0 N+1 GetByID calls when GetByIDs succeeds, got %d", contactRepo.getByIDCalls)
	}
}

