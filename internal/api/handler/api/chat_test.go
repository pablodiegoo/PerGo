package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/api"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockChatRepo struct {
	mu        sync.RWMutex
	chats     map[uuid.UUID]*domain.Chat
	messages  map[uuid.UUID][]*domain.ChatMessage
	reactions map[string][]domain.Reaction
}

func newMockChatRepo() *mockChatRepo {
	return &mockChatRepo{
		chats:     make(map[uuid.UUID]*domain.Chat),
		messages:  make(map[uuid.UUID][]*domain.ChatMessage),
		reactions: make(map[string][]domain.Reaction),
	}
}

func (m *mockChatRepo) GetChat(ctx context.Context, workspaceID, chatID uuid.UUID) (*domain.Chat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return nil, repository.ErrChatNotFound
	}
	cp := *chat
	return &cp, nil
}

func (m *mockChatRepo) ListChats(ctx context.Context, workspaceID uuid.UUID, filter domain.ChatFilter, limit, offset int) ([]domain.Chat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []domain.Chat
	for _, c := range m.chats {
		if c.WorkspaceID != workspaceID {
			continue
		}
		if filter.Status != "" && string(c.Status) != filter.Status {
			continue
		}
		if filter.Tag != "" {
			has := false
			for _, t := range c.Tags {
				if t == filter.Tag {
					has = true
					break
				}
			}
			if !has {
				continue
			}
		}
		list = append(list, *c)
	}
	return list, nil
}

func (m *mockChatRepo) ListChatMessages(ctx context.Context, workspaceID, chatID uuid.UUID, beforeUID, afterUID string, limit int) ([]domain.ChatMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msgs := m.messages[chatID]
	var res []domain.ChatMessage
	for _, msg := range msgs {
		if msg.WorkspaceID == workspaceID {
			res = append(res, *msg)
		}
	}
	return res, nil
}

func (m *mockChatRepo) AddChatMessage(ctx context.Context, msg *domain.ChatMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	m.messages[msg.ChatID] = append(m.messages[msg.ChatID], msg)
	return nil
}

func (m *mockChatRepo) CreateInternalNote(ctx context.Context, workspaceID, chatID uuid.UUID, authorName, senderType, body string) (*domain.ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return nil, repository.ErrChatNotFound
	}
	note := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chatID,
		WorkspaceID: workspaceID,
		UID:         fmt.Sprintf("note-%s", uuid.New().String()),
		Direction:   domain.DirectionInternalNote,
		SenderType:  domain.SenderType(senderType),
		SenderName:  authorName,
		Body:        body,
		IsPrivate:   true,
		CreatedAt:   time.Now().UTC(),
	}
	m.messages[chatID] = append(m.messages[chatID], note)
	return note, nil
}

func (m *mockChatRepo) AssignChat(ctx context.Context, workspaceID, chatID uuid.UUID, memberID *uuid.UUID, email *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	chat.AssignedUserID = memberID
	chat.AssignedEmail = email
	return nil
}

func (m *mockChatRepo) SetChatTags(ctx context.Context, workspaceID, chatID uuid.UUID, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	chat.Tags = tags
	return nil
}

func (m *mockChatRepo) UpdateChatStatus(ctx context.Context, workspaceID, chatID uuid.UUID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	chat.Status = domain.ChatStatus(status)
	chat.SnoozedUntil = nil
	return nil
}

func (m *mockChatRepo) SnoozeChat(ctx context.Context, workspaceID, chatID uuid.UUID, snoozedUntil *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[chatID]
	if !ok || chat.WorkspaceID != workspaceID {
		return repository.ErrChatNotFound
	}
	chat.Status = domain.ChatStatusSnoozed
	chat.SnoozedUntil = snoozedUntil
	return nil
}

func (m *mockChatRepo) SetAIDisabled(ctx context.Context, workspaceID, chatID uuid.UUID, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if chat, ok := m.chats[chatID]; ok {
		chat.AIDisabled = disabled
	}
	return nil
}

func (m *mockChatRepo) TouchLastMessageAt(ctx context.Context, workspaceID, chatID uuid.UUID, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if chat, ok := m.chats[chatID]; ok {
		chat.LastMessageAt = t
	}
	return nil
}

func (m *mockChatRepo) GetChatMessageByUID(ctx context.Context, workspaceID uuid.UUID, uid string) (*domain.ChatMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, msgs := range m.messages {
		for _, msg := range msgs {
			if msg.UID == uid && msg.WorkspaceID == workspaceID {
				return msg, nil
			}
		}
	}
	return nil, errors.New("message not found")
}

func (m *mockChatRepo) GetReactions(ctx context.Context, workspaceID uuid.UUID, messageUID string) ([]domain.Reaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reactions[messageUID], nil
}

func (m *mockChatRepo) MutateReaction(ctx context.Context, workspaceID uuid.UUID, messageUID, emoji, sender, action string) (uuid.UUID, string, []domain.Reaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var foundMsg *domain.ChatMessage
	for _, msgs := range m.messages {
		for _, msg := range msgs {
			if msg.UID == messageUID && msg.WorkspaceID == workspaceID {
				foundMsg = msg
				break
			}
		}
	}
	if foundMsg == nil {
		return uuid.Nil, "", nil, repository.ErrChatMessageNotFound
	}

	if sender == "" {
		sender = "operator"
	}
	normAction := action
	if emoji == "" {
		normAction = "remove"
	}

	reactions := m.reactions[messageUID]
	var resolvedAction string
	switch normAction {
	case "", "toggle":
		hasReacted := false
		for _, r := range reactions {
			if r.Emoji == emoji && (r.Sender == sender || r.Sender == "operator") {
				hasReacted = true
				break
			}
		}
		if hasReacted {
			resolvedAction = "remove"
			var filtered []domain.Reaction
			for _, r := range reactions {
				if r.Emoji == emoji && (r.Sender == sender || r.Sender == "operator") {
					continue
				}
				filtered = append(filtered, r)
			}
			reactions = filtered
		} else {
			resolvedAction = "add"
			reactions = append(reactions, domain.Reaction{Emoji: emoji, Sender: sender, CreatedAt: time.Now().UTC()})
		}
	case "add":
		resolvedAction = "add"
		exists := false
		for _, r := range reactions {
			if r.Emoji == emoji && r.Sender == sender {
				exists = true
				break
			}
		}
		if !exists {
			reactions = append(reactions, domain.Reaction{Emoji: emoji, Sender: sender, CreatedAt: time.Now().UTC()})
		}
	case "remove":
		resolvedAction = "remove"
		var filtered []domain.Reaction
		for _, r := range reactions {
			if r.Emoji == emoji && (r.Sender == sender || sender == "operator" || r.Sender == "operator") {
				continue
			}
			filtered = append(filtered, r)
		}
		reactions = filtered
	default:
		return uuid.Nil, "", nil, fmt.Errorf("invalid reaction action %q", action)
	}
	if reactions == nil {
		reactions = []domain.Reaction{}
	}
	m.reactions[messageUID] = reactions
	return foundMsg.ChatID, resolvedAction, reactions, nil
}

func (m *mockChatRepo) AddReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, reaction domain.Reaction) ([]domain.Reaction, error) {
	_, _, rxs, err := m.MutateReaction(ctx, workspaceID, messageUID, reaction.Emoji, reaction.Sender, "add")
	return rxs, err
}

func (m *mockChatRepo) RemoveReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, emoji, sender string) ([]domain.Reaction, error) {
	_, _, rxs, err := m.MutateReaction(ctx, workspaceID, messageUID, emoji, sender, "remove")
	return rxs, err
}

type mockContactRepo struct {
	contacts map[uuid.UUID]*domain.Contact
}

func (m *mockContactRepo) GetByID(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Contact, error) {
	c, ok := m.contacts[contactID]
	if !ok {
		return nil, errors.New("contact not found")
	}
	return c, nil
}

func (m *mockContactRepo) SetTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error {
	if c, ok := m.contacts[contactID]; ok {
		c.Tags = tags
	}
	return nil
}

func (m *mockContactRepo) CloseThread(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	return nil
}

func (m *mockContactRepo) ReopenThread(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	return nil
}

func (m *mockContactRepo) UpdateBotState(ctx context.Context, workspaceID, contactID uuid.UUID, botActive bool, pausedAt *time.Time) error {
	return nil
}

type mockChatConnectionRepo struct {
	connections map[uuid.UUID]*repository.Connection
}

func (m *mockChatConnectionRepo) GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error) {
	conn, ok := m.connections[id]
	if !ok {
		return nil, errors.New("connection not found")
	}
	return conn, nil
}

func (m *mockChatConnectionRepo) GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
	for _, conn := range m.connections {
		if conn.WorkspaceID == workspaceID && conn.Channel == channel {
			return conn, nil
		}
	}
	return nil, errors.New("no default connection")
}

func (m *mockChatConnectionRepo) GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
	for _, conn := range m.connections {
		if conn.WorkspaceID == workspaceID && conn.SenderIdentity == senderIdentity {
			return conn, nil
		}
	}
	return nil, errors.New("connection not found")
}

func (m *mockChatConnectionRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error) {
	var list []*repository.Connection
	for _, c := range m.connections {
		if c.WorkspaceID == workspaceID {
			list = append(list, c)
		}
	}
	return list, nil
}

type mockPublisher struct {
	subjects  []string
	published [][]byte
}

func (p *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.subjects = append(p.subjects, subject)
	p.published = append(p.published, data)
	return nil
}

type mockChatAuditRepo struct {
	entries []*repository.AuditEntry
}

func (m *mockChatAuditRepo) InsertAuditLog(ctx context.Context, entry *repository.AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

type mockChatWebhookDispatcher struct {
	reactions []struct {
		WorkspaceID uuid.UUID
		MessageUID  string
		Payload     []byte
		TraceID     string
	}
}

func (m *mockChatWebhookDispatcher) DispatchReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, payload []byte, traceID string) error {
	m.reactions = append(m.reactions, struct {
		WorkspaceID uuid.UUID
		MessageUID  string
		Payload     []byte
		TraceID     string
	}{workspaceID, messageUID, payload, traceID})
	return nil
}

func TestChatAPIHandler(t *testing.T) {
	wsID := uuid.New()
	contactID := uuid.New()
	connID := uuid.New()
	chatID := uuid.New()

	chatRepo := newMockChatRepo()
	contactRepo := &mockContactRepo{
		contacts: map[uuid.UUID]*domain.Contact{
			contactID: {
				ID:          contactID,
				WorkspaceID: wsID,
				Name:        "Alice Doe",
				Identities: []domain.ContactIdentity{
					{Channel: "whatsapp", SenderIdentity: "5511999999999"},
				},
				Tags: []string{"lead"},
			},
		},
	}
	connRepo := &mockChatConnectionRepo{
		connections: map[uuid.UUID]*repository.Connection{
			connID: {
				ID:             connID,
				WorkspaceID:    wsID,
				Name:           "Main WhatsApp",
				Channel:        "whatsapp",
				SenderIdentity: "5511888888888",
				Status:         "connected",
			},
		},
	}
	publisher := &mockPublisher{}
	auditRepo := &mockChatAuditRepo{}
	webhookDispatcher := &mockChatWebhookDispatcher{}

	chat := &domain.Chat{
		ID:           chatID,
		WorkspaceID:  wsID,
		ConnectionID: &connID,
		ContactID:    contactID,
		Status:       domain.ChatStatusOpen,
		Tags:         []string{"lead"},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chatRepo.chats[chatID] = chat

	// Initial message
	initialMsg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chatID,
		WorkspaceID: wsID,
		UID:         "msg-001",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  "Alice",
		Body:        "Hello there!",
		CreatedAt:   time.Now().UTC(),
	}
	chatRepo.messages[chatID] = []*domain.ChatMessage{initialMsg}

	handler := api.NewChatAPIHandler(
		chatRepo,
		api.WithChatContactRepo(contactRepo),
		api.WithChatConnectionRepo(connRepo),
		api.WithChatAuditRepo(auditRepo),
		api.WithChatMessagePublisher(publisher),
	)
	handler.SetWebhookDispatcher(webhookDispatcher)

	e := echo.New()
	authMiddleware := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.SetRequest(c.Request().WithContext(tenant.WithWorkspaceID(c.Request().Context(), wsID)))
			return next(c)
		}
	}
	handler.RegisterRoutes(e, authMiddleware)

	t.Run("GET /api/v1/chats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/chats", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Chats []api.ChatSummaryDTO `json:"chats"`
			Count int                  `json:"count"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp.Count != 1 {
			t.Fatalf("expected 1 chat, got %d", resp.Count)
		}
		if resp.Chats[0].ContactName != "Alice Doe" {
			t.Fatalf("expected contact name Alice Doe, got %s", resp.Chats[0].ContactName)
		}
	})

	t.Run("GET /api/v1/chats/:id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/chats/"+chatID.String(), nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp["chat"] == nil || resp["contact"] == nil {
			t.Fatalf("expected chat and contact in response: %v", resp)
		}
	})

	t.Run("GET /api/v1/chats/:id/messages", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/chats/"+chatID.String()+"/messages", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Messages []domain.ChatMessage `json:"messages"`
			Count    int                  `json:"count"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp.Count != 1 {
			t.Fatalf("expected 1 message, got %d", resp.Count)
		}
		if resp.Messages[0].Body != "Hello there!" {
			t.Fatalf("expected 'Hello there!', got %s", resp.Messages[0].Body)
		}
	})

	t.Run("POST /api/v1/chats/:id/messages", func(t *testing.T) {
		payload := `{"body": "Hello Alice, how can I help you today?"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/messages", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}
		if len(publisher.published) == 0 {
			t.Fatalf("expected message to be published to JetStream")
		}
	})

	t.Run("POST /api/v1/chats/:id/notes", func(t *testing.T) {
		payload := `{"body": "Customer is interested in Enterprise plan", "author_name": "Pablo"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/notes", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Note domain.ChatMessage `json:"note"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal note response: %v", err)
		}
		if !resp.Note.IsPrivate {
			t.Fatalf("expected note to be private")
		}
		if resp.Note.Body != "Customer is interested in Enterprise plan" {
			t.Fatalf("unexpected note body: %s", resp.Note.Body)
		}
	})

	t.Run("POST /api/v1/chats/:id/react", func(t *testing.T) {
		prevPubCount := len(publisher.published)
		prevAuditCount := len(auditRepo.entries)

		payload := `{"message_uid": "msg-001", "emoji": "👍", "action": "add"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/react", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Status    string            `json:"status"`
			Action    string            `json:"action"`
			Reactions []domain.Reaction `json:"reactions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal reaction response: %v", err)
		}
		if resp.Action != "add" || len(resp.Reactions) != 1 || resp.Reactions[0].Emoji != "👍" {
			t.Fatalf("expected add reaction 👍, got action=%q reactions=%v", resp.Action, resp.Reactions)
		}

		// Verify NATS reaction_updated event published
		if len(publisher.published) != prevPubCount+1 {
			t.Fatalf("expected NATS event published on REST react, got %d new events", len(publisher.published)-prevPubCount)
		}
		if publisher.subjects[len(publisher.subjects)-1] != "messages.events.reaction_updated" {
			t.Errorf("expected subject messages.events.reaction_updated, got %s", publisher.subjects[len(publisher.subjects)-1])
		}

		// Verify webhookDispatcher.DispatchReaction called
		if len(webhookDispatcher.reactions) != 1 || webhookDispatcher.reactions[0].MessageUID != "msg-001" {
			t.Fatalf("expected 1 webhook reaction dispatch for msg-001, got %+v", webhookDispatcher.reactions)
		}

		// Verify reaction_updated audit entry written
		if len(auditRepo.entries) != prevAuditCount+1 || auditRepo.entries[len(auditRepo.entries)-1].EventType != "reaction_updated" {
			t.Fatalf("expected reaction_updated audit entry on REST react, got %+v", auditRepo.entries)
		}

		// Toggle existing reaction off
		togglePayload := `{"message_uid": "msg-001", "emoji": "👍", "action": "toggle"}`
		reqToggle := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/react", bytes.NewBufferString(togglePayload))
		reqToggle.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recToggle := httptest.NewRecorder()
		e.ServeHTTP(recToggle, reqToggle)

		if recToggle.Code != http.StatusOK {
			t.Fatalf("expected status 200 on toggle, got %d: %s", recToggle.Code, recToggle.Body.String())
		}
		if err := json.Unmarshal(recToggle.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal toggle response: %v", err)
		}
		if resp.Action != "remove" || len(resp.Reactions) != 0 {
			t.Fatalf("expected toggle to remove reaction 👍, got action=%q reactions=%v", resp.Action, resp.Reactions)
		}
	})

	t.Run("PATCH /api/v1/chats/:id/assign", func(t *testing.T) {
		email := "agent@example.com"
		payload := fmt.Sprintf(`{"email": "%s"}`, email)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/assign", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if chatRepo.chats[chatID].AssignedEmail == nil || *chatRepo.chats[chatID].AssignedEmail != email {
			t.Fatalf("expected chat assigned to %s", email)
		}

		// Unassign
		unassignPayload := `{"email": null}`
		req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/assign", bytes.NewBufferString(unassignPayload))
		req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec2 := httptest.NewRecorder()
		e.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected status 200 on unassign, got %d: %s", rec2.Code, rec2.Body.String())
		}
		if chatRepo.chats[chatID].AssignedEmail != nil {
			t.Fatalf("expected chat unassigned")
		}
	})

	t.Run("PATCH /api/v1/chats/:id/labels", func(t *testing.T) {
		payload := `{"add": ["vip", "support"], "remove": ["lead"]}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/labels", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		tags := chatRepo.chats[chatID].Tags
		hasVIP, hasSupport, hasLead := false, false, false
		for _, tag := range tags {
			if tag == "vip" {
				hasVIP = true
			}
			if tag == "support" {
				hasSupport = true
			}
			if tag == "lead" {
				hasLead = true
			}
		}
		if !hasVIP || !hasSupport || hasLead {
			t.Fatalf("unexpected tags: %v", tags)
		}
	})

	t.Run("PATCH /api/v1/chats/:id/status", func(t *testing.T) {
		// Snooze for 2 hours
		snoozedUntil := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
		payload := fmt.Sprintf(`{"status": "snoozed", "snoozed_until": "%s"}`, snoozedUntil)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/status", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if chatRepo.chats[chatID].Status != domain.ChatStatusSnoozed {
			t.Fatalf("expected status snoozed, got %s", chatRepo.chats[chatID].Status)
		}
		if chatRepo.chats[chatID].SnoozedUntil == nil {
			t.Fatalf("expected snoozed_until to be set")
		}

		// Close
		closePayload := `{"status": "closed"}`
		req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/status", bytes.NewBufferString(closePayload))
		req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec2 := httptest.NewRecorder()
		e.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected status 200 on close, got %d: %s", rec2.Code, rec2.Body.String())
		}
		if chatRepo.chats[chatID].Status != domain.ChatStatusClosed {
			t.Fatalf("expected status closed, got %s", chatRepo.chats[chatID].Status)
		}
	})

	t.Run("Security: route workspace_id parameter mismatch returns 403 Forbidden", func(t *testing.T) {
		otherWSID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/chats", otherWSID.String()), nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 Forbidden on workspace mismatch, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/chats/:id/messages with template_name populates display body", func(t *testing.T) {
		payload := `{"template_name": "order_update", "language": "pt_BR"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/messages", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created for template message, got %d: %s", rec.Code, rec.Body.String())
		}

		msgs := chatRepo.messages[chatID]
		lastMsg := msgs[len(msgs)-1]
		if lastMsg.Body != "📋 [Template: order_update]" {
			t.Fatalf("expected display body '📋 [Template: order_update]', got %q", lastMsg.Body)
		}
	})
}
