package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockSubscriptionStore struct {
	sub    *repository.WebhookSubscription
	getSub func(id uuid.UUID) (*repository.WebhookSubscription, error)
}

func (m *mockSubscriptionStore) Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error) {
	if m.getSub != nil {
		return m.getSub(id)
	}
	if m.sub != nil {
		return m.sub, nil
	}
	return nil, repository.ErrWebhookSubscriptionNotFound
}

type mockDLQStore struct {
	inserted []struct {
		workspaceID    uuid.UUID
		subscriptionID uuid.UUID
		traceID        string
		messageID      string
		eventType      string
		payload        []byte
		url            string
		attempts       int
		failureReason  *string
	}
}

func (m *mockDLQStore) InsertDLQ(
	ctx context.Context,
	workspaceID uuid.UUID,
	subscriptionID uuid.UUID,
	traceID, messageID, eventType string,
	payload []byte,
	url string,
	attempts int,
	failureReason *string,
) error {
	m.inserted = append(m.inserted, struct {
		workspaceID    uuid.UUID
		subscriptionID uuid.UUID
		traceID        string
		messageID      string
		eventType      string
		payload        []byte
		url            string
		attempts       int
		failureReason  *string
	}{workspaceID, subscriptionID, traceID, messageID, eventType, payload, url, attempts, failureReason})
	return nil
}

type mockWorkspaceStore struct {
	ws *repository.Workspace
}

func (m *mockWorkspaceStore) GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error) {
	return m.ws, nil
}

type mockHTTPClient struct {
	requests []*http.Request
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.requests = append(m.requests, req)
	if m.response != nil {
		return m.response, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte(""))),
	}, nil
}

func TestDefaultDispatcher_Dispatch(t *testing.T) {
	wsID := uuid.New()
	subID := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          subID,
		WorkspaceID: wsID,
		URL:         "https://api.myweb.com/events",
		Secret:      []byte("signing-secret-key-123"),
		Active:      true,
	}

	t.Run("Normal dispatch signs and sends HTTP request", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, httpClient, nil)

		event := map[string]string{
			"event":        "message.sent",
			"trace_id":     "trace-1",
			"message_id":   "msg-1",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.sent",
			TraceID:        "trace-1",
			MessageID:      "msg-1",
			Payload:        rawBytes,
			Mode:           "outbound",
		}

		err := d.Dispatch(context.Background(), task)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		if len(httpClient.requests) != 1 {
			t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
		}
		req := httpClient.requests[0]
		if req.URL.String() != sub.URL {
			t.Errorf("got URL %s, want %s", req.URL, sub.URL)
		}
		if req.Header.Get("X-Trace-ID") != "trace-1" {
			t.Errorf("got X-Trace-ID %q, want trace-1", req.Header.Get("X-Trace-ID"))
		}
		if req.Header.Get("X-PerGo-Signature") == "" {
			t.Errorf("expected signature header to be present")
		}
	})

	t.Run("Compliance checks redact PII on inbound events when workspace opted-out", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		workspaceStore := &mockWorkspaceStore{
			ws: &repository.Workspace{ID: wsID, PIIOptIn: false},
		}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, workspaceStore, httpClient, nil)

		inboundEvent := map[string]any{
			"event":        "message.received",
			"trace_id":     "trace-2",
			"message_id":   "msg-2",
			"workspace_id": wsID.String(),
			"from":         "+5511999998888",
			"body":         "Sensitive message content",
			"location": map[string]any{
				"latitude":  12.34,
				"longitude": 56.78,
			},
		}
		rawBytes, _ := json.Marshal(inboundEvent)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.received",
			TraceID:        "trace-2",
			MessageID:      "msg-2",
			Payload:        rawBytes,
			Mode:           "inbound",
		}

		err := d.Dispatch(context.Background(), task)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		req := httpClient.requests[0]
		bodyBytes, _ := io.ReadAll(req.Body)
		var sentEvent map[string]any
		json.Unmarshal(bodyBytes, &sentEvent)

		// Verification: 'from' should be hashed, 'location' should be nil
		fromVal := sentEvent["from"].(string)
		if fromVal == "+5511999998888" {
			t.Errorf("expected 'from' number to be hashed and redacted, but got original %q", fromVal)
		}
		if len(fromVal) != 64 {
			t.Errorf("expected SHA-256 hex string (64 characters) for 'from', got length %d", len(fromVal))
		}
		if sentEvent["location"] != nil {
			t.Errorf("expected location to be stripped/nil, got %v", sentEvent["location"])
		}
	})

	t.Run("Non-2xx HTTP responses wrap HTTPError", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		httpClient := &mockHTTPClient{
			response: &http.Response{
				StatusCode: 502,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(bytes.NewReader([]byte(""))),
			},
		}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, httpClient, nil)

		event := map[string]string{
			"event":        "message.sent",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.sent",
			Payload:        rawBytes,
			Mode:           "outbound",
		}

		err := d.Dispatch(context.Background(), task)
		var httpErr *webhook.HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("expected HTTPError, got %v", err)
		}
		if httpErr.StatusCode != 502 {
			t.Errorf("got status code %d, want 502", httpErr.StatusCode)
		}
	})

	t.Run("Group message delivery preserves group JID, sender_name, and metadata", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		workspaceStore := &mockWorkspaceStore{
			ws: &repository.Workspace{ID: wsID, PIIOptIn: true},
		}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, workspaceStore, httpClient, nil)

		groupJID := "120363024823904@g.us"
		participantJID := "5511999991234@s.whatsapp.net"
		pushName := "Alice GroupMember"

		inboundEvent := inbound.InboundEventPayload{
			Event:       "inbound_message",
			TraceID:     "trace-group-1",
			MessageID:   "wamid.group_msg_101",
			Channel:     "whatsapp",
			Timestamp:   "2026-08-20T12:00:00Z",
			WorkspaceID: wsID.String(),
			From:        groupJID,
			To:          "+5511888880000",
			Body:        "Group message payload test",
			SenderName:  pushName,
			Media: &inbound.EventMedia{
				MediaURL:  "https://example.com/media/group.jpg",
				MediaType: "image",
				Caption:   "Group photo",
			},
			Metadata: map[string]string{
				"is_group":         "true",
				"participant":      participantJID,
				"chat_jid":         groupJID,
				"sender_push_name": pushName,
			},
		}
		rawBytes, _ := json.Marshal(inboundEvent)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "inbound_message",
			TraceID:        "trace-group-1",
			MessageID:      "wamid.group_msg_101",
			Payload:        rawBytes,
			Mode:           "inbound",
		}

		err := d.Dispatch(context.Background(), task)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		if len(httpClient.requests) != 1 {
			t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
		}

		req := httpClient.requests[0]
		bodyBytes, _ := io.ReadAll(req.Body)
		var sentPayload inbound.InboundEventPayload
		if err := json.Unmarshal(bodyBytes, &sentPayload); err != nil {
			t.Fatalf("failed to unmarshal delivered payload: %v", err)
		}

		if sentPayload.From != groupJID {
			t.Errorf("expected From %q, got %q", groupJID, sentPayload.From)
		}
		if sentPayload.SenderName != pushName {
			t.Errorf("expected SenderName %q, got %q", pushName, sentPayload.SenderName)
		}
		if sentPayload.Metadata == nil {
			t.Fatal("expected Metadata map to be present in delivered payload")
		}
		if sentPayload.Metadata["is_group"] != "true" {
			t.Errorf("expected Metadata[is_group] == 'true', got %q", sentPayload.Metadata["is_group"])
		}
		if sentPayload.Metadata["participant"] != participantJID {
			t.Errorf("expected Metadata[participant] == %q, got %q", participantJID, sentPayload.Metadata["participant"])
		}
		if sentPayload.Metadata["chat_jid"] != groupJID {
			t.Errorf("expected Metadata[chat_jid] == %q, got %q", groupJID, sentPayload.Metadata["chat_jid"])
		}
		if sentPayload.Metadata["sender_push_name"] != pushName {
			t.Errorf("expected Metadata[sender_push_name] == %q, got %q", pushName, sentPayload.Metadata["sender_push_name"])
		}
		if sentPayload.Media == nil || sentPayload.Media.MediaURL != "https://example.com/media/group.jpg" {
			t.Errorf("expected Media with URL https://example.com/media/group.jpg, got %+v", sentPayload.Media)
		}
	})

	t.Run("Compliance redaction preserves sender_name and metadata while hashing from", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		workspaceStore := &mockWorkspaceStore{
			ws: &repository.Workspace{ID: wsID, PIIOptIn: false},
		}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, workspaceStore, httpClient, nil)

		groupJID := "120363024823904@g.us"
		participantJID := "5511999991234@s.whatsapp.net"
		pushName := "Alice GroupMember"

		inboundEvent := inbound.InboundEventPayload{
			Event:       "inbound_message",
			TraceID:     "trace-group-2",
			MessageID:   "wamid.group_msg_102",
			Channel:     "whatsapp",
			Timestamp:   "2026-08-20T12:00:00Z",
			WorkspaceID: wsID.String(),
			From:        groupJID,
			To:          "+5511888880000",
			Body:        "Group message redaction test",
			SenderName:  pushName,
			Location: &inbound.InboundLocation{
				Latitude: 1.23,
			},
			Contacts: []inbound.InboundContact{
				{Name: "Someone", Phone: "123"},
			},
			Metadata: map[string]string{
				"is_group":         "true",
				"participant":      participantJID,
				"chat_jid":         groupJID,
				"sender_push_name": pushName,
			},
		}
		rawBytes, _ := json.Marshal(inboundEvent)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "inbound_message",
			TraceID:        "trace-group-2",
			MessageID:      "wamid.group_msg_102",
			Payload:        rawBytes,
			Mode:           "inbound",
		}

		err := d.Dispatch(context.Background(), task)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		req := httpClient.requests[0]
		bodyBytes, _ := io.ReadAll(req.Body)
		var sentPayload inbound.InboundEventPayload
		if err := json.Unmarshal(bodyBytes, &sentPayload); err != nil {
			t.Fatalf("failed to unmarshal delivered payload: %v", err)
		}

		if sentPayload.From == groupJID {
			t.Errorf("expected From to be hashed, got raw %q", sentPayload.From)
		}
		if len(sentPayload.From) != 64 {
			t.Errorf("expected 64-char SHA256 hex for From, got %q", sentPayload.From)
		}
		if sentPayload.SenderName != pushName {
			t.Errorf("expected SenderName %q to be preserved, got %q", pushName, sentPayload.SenderName)
		}
		if sentPayload.Location != nil {
			t.Errorf("expected Location to be stripped, got %+v", sentPayload.Location)
		}
		if len(sentPayload.Contacts) != 0 {
			t.Errorf("expected Contacts to be stripped, got %+v", sentPayload.Contacts)
		}
		if sentPayload.Metadata == nil || sentPayload.Metadata["is_group"] != "true" {
			t.Errorf("expected Metadata to be preserved, got %+v", sentPayload.Metadata)
		}
		if sentPayload.Metadata["participant"] == participantJID {
			t.Errorf("expected participant in Metadata to be hashed, got raw %q", sentPayload.Metadata["participant"])
		}
		if len(sentPayload.Metadata["participant"]) != 64 {
			t.Errorf("expected 64-char SHA256 hex for participant in Metadata, got %q", sentPayload.Metadata["participant"])
		}
	})
}

func TestDefaultDispatcher_WriteToDLQ(t *testing.T) {
	wsID := uuid.New()
	subID := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          subID,
		WorkspaceID: wsID,
		URL:         "https://api.myweb.com/events",
		Secret:      []byte("signing-secret-key-123"),
		Active:      true,
	}

	subStore := &mockSubscriptionStore{sub: sub}
	dlqStore := &mockDLQStore{}
	d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, nil, nil)

	payload := []byte(`{"event":"test"}`)
	err := d.WriteToDLQ(context.Background(), wsID, subID, "trace-123", "msg-123", "message.sent", payload, 3, "something went wrong")
	if err != nil {
		t.Fatalf("WriteToDLQ failed: %v", err)
	}

	if len(dlqStore.inserted) != 1 {
		t.Fatalf("expected 1 DLQ insertion, got %d", len(dlqStore.inserted))
	}
	ins := dlqStore.inserted[0]
	if ins.workspaceID != wsID {
		t.Errorf("got workspace ID %s, want %s", ins.workspaceID, wsID)
	}
	if ins.subscriptionID != subID {
		t.Errorf("got subscription ID %s, want %s", ins.subscriptionID, subID)
	}
	if ins.url != sub.URL {
		t.Errorf("got url %s, want %s", ins.url, sub.URL)
	}
	if *ins.failureReason != "something went wrong" {
		t.Errorf("got failure reason %s", *ins.failureReason)
	}
}

type mockPublisher struct {
	published []struct {
		subject string
		payload []byte
		traceID string
	}
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.published = append(m.published, struct {
		subject string
		payload []byte
		traceID string
	}{subject, data, traceID})
	return nil
}

type mockRouteResolver struct {
	conn *repository.Connection
}

func (m *mockRouteResolver) GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
	if m.conn != nil {
		return m.conn, nil
	}
	return nil, errors.New("connection not found")
}

func (m *mockRouteResolver) GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error) {
	if m.conn != nil && m.conn.Slug == slug {
		return m.conn, nil
	}
	return nil, errors.New("connection not found by slug")
}

func (m *mockRouteResolver) GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
	if m.conn != nil {
		return m.conn, nil
	}
	return nil, errors.New("default connection not found")
}

func TestDefaultDispatcher_VerbsIntegration(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		return
	}
	defer pool.Close()

	ctx := context.Background()

	// Clean up tables
	_, _ = pool.Exec(ctx, "DELETE FROM user_action_logs")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	contactRepo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)
	logsRepo := repository.NewUserActionLogRepository(pool)

	// Create workspace with PIIOptIn = false to verify PII compliance redaction does not mutate task.Payload
	ws, err := wsRepo.Create(ctx, "verbs_integration_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Explicitly set PIIOptIn to false and save
	_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = false WHERE id = $1", ws.ID)
	if err != nil {
		t.Fatalf("failed to disable pii_opt_in: %v", err)
	}

	// Create a webhook subscription
	subID := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          subID,
		WorkspaceID: ws.ID,
		URL:         "https://api.myweb.com/events",
		Secret:      []byte("signing-secret-key-123"),
		Active:      true,
	}
	subStore := &mockSubscriptionStore{sub: sub}
	dlqStore := &mockDLQStore{}

	// Resolve the contact first so we have the ID to query later
	c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "+5511999998888", "", "", "")
	if err != nil {
		t.Fatalf("ResolveContact failed: %v", err)
	}

	// Setup mocks
	connID := uuid.New()
	mPublisher := &mockPublisher{}
	mResolver := &mockRouteResolver{
		conn: &repository.Connection{
			ID:             connID,
			WorkspaceID:    ws.ID,
			Channel:        "telegram",
			SenderIdentity: "bot-1",
		},
	}

	// Instantiate the VerbsEngine
	engine := webhook.NewVerbsEngine(mPublisher, contactRepo, logsRepo, mResolver)

	// Mock HTTP client to return a 2xx response containing a JSON array of verbs
	verbsJSON := `[
		{"action": "reply", "params": {"body": "automated reply body"}},
		{"action": "tag", "params": {"tags": ["vip", "verified"]}},
		{"action": "close", "params": {}}
	]`
	httpClient := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader([]byte(verbsJSON))),
		},
	}

	// Setup DefaultDispatcher with VerbsEngine
	d := webhook.NewDefaultDispatcher(subStore, dlqStore, wsRepo, httpClient, engine)

	// Setup payload containing the raw/unredacted PII details
	evt := inbound.InboundEventPayload{
		Event:       "message.received",
		TraceID:     "trace-verbs-1",
		MessageID:   "msg-verbs-1",
		Channel:     "telegram",
		WorkspaceID: ws.ID.String(),
		From:        "+5511999998888",
		To:          "bot-1",
		Body:        "Hello there",
	}
	evtBytes, _ := json.Marshal(evt)

	task := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: subID,
		WorkspaceID:    ws.ID,
		Event:          "message.received",
		TraceID:        "trace-verbs-1",
		MessageID:      "msg-verbs-1",
		Payload:        evtBytes,
		Mode:           "inbound",
	}

	// Dispatch the webhook task
	err = d.Dispatch(ctx, task)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	// Verify HTTP request payload was redacted (pii_opt_in = false)
	if len(httpClient.requests) != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
	}
	reqBodyBytes, _ := io.ReadAll(httpClient.requests[0].Body)
	var httpPayload struct {
		From string `json:"from"`
	}
	_ = json.Unmarshal(reqBodyBytes, &httpPayload)
	// Hashed "+5511999998888" should not equal raw "+5511999998888"
	if httpPayload.From == "+5511999998888" {
		t.Errorf("expected PII redaction on HTTP request payload, but it was unredacted: %s", httpPayload.From)
	}

	// Poll/wait for asynchronous verbs execution to complete (background goroutine)
	deadline := time.Now().Add(500 * time.Millisecond)
	var contactTags []string
	var contactClosedAt *time.Time
	var logsCount int
	var actionLogs []repository.UserActionLog

	for time.Now().Before(deadline) {
		// Check tags and closed_at
		var tempTags []string
		var tempClosedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT tags, closed_at FROM contacts WHERE id = $1", c.ID).Scan(&tempTags, &tempClosedAt)
		if err == nil {
			contactTags = tempTags
			contactClosedAt = tempClosedAt
		}

		// Check logs
		rows, err := pool.Query(ctx, "SELECT id, workspace_id, actor_type, actor_id, actor_name, action, source, metadata, created_at FROM user_action_logs WHERE workspace_id = $1 AND action = 'webhook.verbs'", ws.ID)
		if err == nil {
			var tempLogs []repository.UserActionLog
			for rows.Next() {
				var l repository.UserActionLog
				if err := rows.Scan(&l.ID, &l.WorkspaceID, &l.ActorType, &l.ActorID, &l.ActorName, &l.Action, &l.Source, &l.Metadata, &l.CreatedAt); err == nil {
					tempLogs = append(tempLogs, l)
				}
			}
			rows.Close()
			actionLogs = tempLogs
			logsCount = len(actionLogs)
		}

		// Break early if everything succeeded
		if len(contactTags) >= 2 && contactClosedAt != nil && logsCount >= 1 && len(mPublisher.published) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 1. Verify contact updates (tag and close verbs succeeded using unredacted "from")
	if len(contactTags) != 2 {
		t.Errorf("expected 2 contact tags (vip, verified), got: %v", contactTags)
	}
	if contactClosedAt == nil {
		t.Errorf("expected contact to be closed (closed_at non-nil)")
	}

	// 2. Verify NATS publishing (reply verb published to messages.outbound)
	if len(mPublisher.published) != 1 {
		t.Errorf("expected 1 published NATS message, got %d", len(mPublisher.published))
	} else {
		pub := mPublisher.published[0]
		if pub.subject != "messages.outbound" {
			t.Errorf("expected subject 'messages.outbound', got %s", pub.subject)
		}
		var qMsg domain.QueueMessage
		if err := json.Unmarshal(pub.payload, &qMsg); err != nil {
			t.Fatalf("failed to unmarshal published NATS queue message: %v", err)
		}
		if qMsg.Body != "automated reply body" {
			t.Errorf("expected NATS message body 'automated reply body', got %q", qMsg.Body)
		}
		if qMsg.To != "+5511999998888" {
			t.Errorf("expected NATS message to go to '+5511999998888', got %q", qMsg.To)
		}
	}

	// 3. Verify user action logs persisted
	if logsCount != 1 {
		t.Errorf("expected 1 user action log entry under 'webhook.verbs', got %d", logsCount)
	} else {
		log := actionLogs[0]
		if log.ActorID != "verbs_engine" {
			t.Errorf("expected ActorID 'verbs_engine', got %s", log.ActorID)
		}
		if log.Action != "webhook.verbs" {
			t.Errorf("expected Action 'webhook.verbs', got %s", log.Action)
		}
		var meta struct {
			Status string `json:"status"`
			Verbs  []struct {
				Action string `json:"action"`
				Status string `json:"status"`
			} `json:"verbs"`
		}
		if err := json.Unmarshal(log.Metadata, &meta); err != nil {
			t.Fatalf("failed to unmarshal log metadata: %v", err)
		}
		if meta.Status != "success" {
			t.Errorf("expected log status 'success', got %s", meta.Status)
		}
		if len(meta.Verbs) != 3 {
			t.Errorf("expected 3 verbs recorded in metadata, got %d", len(meta.Verbs))
		}
	}
}

func TestDispatcher_WorkspaceWebhookSecretFallback(t *testing.T) {
	wsID := uuid.New()
	subID := uuid.New()
	secretStr := "workspace_secret_key_12345"

	subStore := &mockSubscriptionStore{
		sub: &repository.WebhookSubscription{
			ID:          subID,
			WorkspaceID: wsID,
			URL:         "http://example.com/webhook",
			Active:      true,
			Secret:      nil, // Empty subscription secret
		},
	}

	wsStore := &mockWorkspaceStore{
		ws: &repository.Workspace{
			ID:            wsID,
			Name:          "Secret Test WS",
			WebhookSecret: &secretStr,
		},
	}

	client := &mockHTTPClient{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		},
	}

	dispatcher := webhook.NewDefaultDispatcher(subStore, nil, wsStore, client, nil)
	task := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: subID,
		WorkspaceID:    wsID,
		Event:          "message.created",
		Payload:        []byte(`{"text":"hello"}`),
	}

	err := dispatcher.Dispatch(context.Background(), task)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	if len(client.requests) == 0 {
		t.Fatalf("expected 1 http request made")
	}

	capturedSignature := client.requests[0].Header.Get("X-PerGo-Signature")
	if capturedSignature == "" {
		t.Errorf("expected X-PerGo-Signature header to be set")
	}
	if !strings.Contains(capturedSignature, "v1=") {
		t.Errorf("expected signature to contain v1=, got: %s", capturedSignature)
	}
}

