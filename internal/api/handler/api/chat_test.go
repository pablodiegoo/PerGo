package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/api"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type noOpCrypto struct{}

func (noOpCrypto) Encrypt(plaintext []byte) ([]byte, string, int, error) {
	return plaintext, "noop", 1, nil
}

func (noOpCrypto) Decrypt(ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
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

func setupChatAPITestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		bgCtx := context.Background()
		pgContainer, err := tcpostgres.Run(bgCtx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo_test"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("failed to start postgres container: %v", err)
		}
		t.Cleanup(func() {
			_ = pgContainer.Terminate(bgCtx)
		})
		connStr, err := pgContainer.ConnectionString(bgCtx, "sslmode=disable")
		if err != nil {
			t.Fatalf("failed to get connection string: %v", err)
		}
		dsn = connStr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to wrap pool: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return pool
}

func TestChatAPIHandler(t *testing.T) {
	pool := setupChatAPITestPool(t)
	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "chat_api_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()
	wsID := ws.ID

	if err := wsRepo.AddMember(ctx, &repository.WorkspaceMember{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Email:       "agent@example.com",
		Name:        "Agent",
		Role:        "member",
	}); err != nil {
		t.Fatalf("failed to add workspace member: %v", err)
	}

	connRepo := repository.NewConnectionRepository(pool, noOpCrypto{})
	connID := uuid.New()
	if err := connRepo.Create(ctx, &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           "Main WhatsApp",
		Channel:        "whatsapp",
		SenderIdentity: "5511888888888",
		Status:         "connected",
	}); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	contactRepo := repository.NewContactRepository(pool)
	contact, err := contactRepo.ResolveContact(ctx, wsID, "whatsapp", "5511999999999", "Alice Doe", "", "5511999999999")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}
	contactID := contact.ID
	if err := contactRepo.SetTags(ctx, wsID, contactID, []string{"lead"}); err != nil {
		t.Fatalf("failed to set contact tags: %v", err)
	}

	chatRepo := repository.NewChatRepository(pool)
	chatRow, err := chatRepo.FindOrCreateChat(ctx, wsID, &connID, contactID)
	if err != nil {
		t.Fatalf("failed to ensure chat: %v", err)
	}
	chatID := chatRow.ID
	if err := chatRepo.SetChatTags(ctx, wsID, chatID, []string{"lead"}); err != nil {
		t.Fatalf("failed to set chat tags: %v", err)
	}

	// Initial inbound message
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
	if err := chatRepo.AddChatMessage(ctx, initialMsg); err != nil {
		t.Fatalf("failed to seed initial inbound message: %v", err)
	}

	publisher := &mockPublisher{}
	auditRepo := &mockChatAuditRepo{}
	webhookDispatcher := &mockChatWebhookDispatcher{}

	rec := activity.New(pool, publisher, nil, nil)
	ingestor := outbound.NewProcessor(
		nil,
		nil,
		connRepo,
		publisher,
	).SetActivity(rec)

	handler := api.NewChatAPIHandler(
		chatRepo,
		api.WithChatContactRepo(contactRepo),
		api.WithChatConnectionRepo(connRepo),
		api.WithChatWorkspaceRepo(wsRepo),
		api.WithChatAuditRepo(auditRepo),
		api.WithChatMessagePublisher(publisher),
		api.WithChatMessageIngestor(ingestor),
		api.WithChatActivity(rec),
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

	t.Run("POST /api/v1/chats/:id/messages defaults sender_type to ai_agent without pausing AI", func(t *testing.T) {
		payload := `{"body": "Hello Alice, how can I help you today?"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID.String()+"/messages", bytes.NewBufferString(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		httpRec := httptest.NewRecorder()
		e.ServeHTTP(httpRec, req)

		if httpRec.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", httpRec.Code, httpRec.Body.String())
		}
		if len(publisher.published) == 0 {
			t.Fatalf("expected message to be published to JetStream")
		}

		state, _, err := rec.Snapshot(ctx, wsID, activity.ChatID(chatID))
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}
		if state.AI.Mode != domain.AIModeActive {
			t.Fatalf("expected AI mode engaged after default ai_agent outbound, got %q", state.AI.Mode)
		}
	})

	t.Run("PATCH /api/v1/chats/:id/ai toggles AI off and back to engaged", func(t *testing.T) {
		// Disable AI
		reqOff := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/ai", bytes.NewBufferString(`{"disabled": true}`))
		reqOff.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recOff := httptest.NewRecorder()
		e.ServeHTTP(recOff, reqOff)

		if recOff.Code != http.StatusOK {
			t.Fatalf("expected status 200 on disable AI, got %d: %s", recOff.Code, recOff.Body.String())
		}
		state, _, err := rec.Snapshot(ctx, wsID, activity.ChatID(chatID))
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}
		if state.AI.Mode != domain.AIModeOff {
			t.Fatalf("expected AI mode off, got %q", state.AI.Mode)
		}

		// Re-enable AI
		reqOn := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/ai", bytes.NewBufferString(`{"disabled": false}`))
		reqOn.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recOn := httptest.NewRecorder()
		e.ServeHTTP(recOn, reqOn)

		if recOn.Code != http.StatusOK {
			t.Fatalf("expected status 200 on enable AI, got %d: %s", recOn.Code, recOn.Body.String())
		}
		state, _, err = rec.Snapshot(ctx, wsID, activity.ChatID(chatID))
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}
		if state.AI.Mode != domain.AIModeActive {
			t.Fatalf("expected AI mode engaged, got %q", state.AI.Mode)
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
		updated, err := chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updated.AssignedEmail == nil || *updated.AssignedEmail != email {
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
		updated, err = chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updated.AssignedEmail != nil {
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

		updated, err := chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		tags := updated.Tags
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
		updated, err := chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updated.Status != domain.ChatStatusSnoozed {
			t.Fatalf("expected status snoozed, got %s", updated.Status)
		}
		if updated.SnoozedUntil == nil {
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
		updated, err = chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updated.Status != domain.ChatStatusClosed {
			t.Fatalf("expected status closed, got %s", updated.Status)
		}

		// Pending
		pendingPayload := `{"status": "pending"}`
		reqPending := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/status", bytes.NewBufferString(pendingPayload))
		reqPending.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recPending := httptest.NewRecorder()
		e.ServeHTTP(recPending, reqPending)
		if recPending.Code != http.StatusOK {
			t.Fatalf("expected status 200 on pending, got %d: %s", recPending.Code, recPending.Body.String())
		}
		updated, err = chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updated.Status != domain.ChatStatusPending {
			t.Fatalf("expected status pending, got %s", updated.Status)
		}

		// Snoozed without snoozed_until -> 400 Bad Request
		reqNoUntil := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/status", bytes.NewBufferString(`{"status": "snoozed"}`))
		reqNoUntil.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recNoUntil := httptest.NewRecorder()
		e.ServeHTTP(recNoUntil, reqNoUntil)
		if recNoUntil.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 when snoozed_until is missing, got %d: %s", recNoUntil.Code, recNoUntil.Body.String())
		}

		// Snoozed with past snoozed_until -> 400 Bad Request
		pastUntil := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		reqPastUntil := httptest.NewRequest(http.MethodPatch, "/api/v1/chats/"+chatID.String()+"/status", bytes.NewBufferString(fmt.Sprintf(`{"status": "snoozed", "snoozed_until": "%s"}`, pastUntil)))
		reqPastUntil.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recPastUntil := httptest.NewRecorder()
		e.ServeHTTP(recPastUntil, reqPastUntil)
		if recPastUntil.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 when snoozed_until is in the past, got %d: %s", recPastUntil.Code, recPastUntil.Body.String())
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

		msgs, err := chatRepo.ListChatMessages(ctx, wsID, chatID, "", "", 50)
		if err != nil {
			t.Fatalf("ListChatMessages failed: %v", err)
		}
		lastMsg := msgs[len(msgs)-1]
		if lastMsg.Body != "[Template: order_update (pt_BR)]" {
			t.Fatalf("expected display body '[Template: order_update (pt_BR)]', got %q", lastMsg.Body)
		}
	})

	t.Run("GET and POST /api/v1/chats/:id/summarize", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, "/api/v1/chats/"+chatID.String()+"/summarize", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200 for %s summarize, got %d: %s", method, rec.Code, rec.Body.String())
			}

			var synopsis domain.ChatSynopsis
			if err := json.Unmarshal(rec.Body.Bytes(), &synopsis); err != nil {
				t.Fatalf("failed to unmarshal synopsis: %v", err)
			}
			if synopsis.CurrentStatus == "" {
				t.Fatalf("expected non-empty current_status in synopsis: %+v", synopsis)
			}
		}
	})

	t.Run("Derived snooze reopen on GET /api/v1/chats/:id", func(t *testing.T) {
		past := time.Now().UTC().Add(-5 * time.Minute)
		if err := chatRepo.SnoozeChat(ctx, wsID, chatID, &past); err != nil {
			t.Fatalf("SnoozeChat failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/chats/"+chatID.String(), nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		updatedChat, err := chatRepo.GetChat(ctx, wsID, chatID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if updatedChat.Status != domain.ChatStatusOpen || updatedChat.SnoozedUntil != nil {
			t.Fatalf("expected chat to report open status, got status=%s", updatedChat.Status)
		}
	})
}
