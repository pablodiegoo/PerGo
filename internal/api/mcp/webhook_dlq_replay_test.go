package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockPublisher struct {
	mu        sync.Mutex
	published []publishedMessage
	err       error
}

type publishedMessage struct {
	subject string
	data    []byte
	traceID string
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, publishedMessage{
		subject: subject,
		data:    data,
		traceID: traceID,
	})
	return nil
}

func (m *mockPublisher) getPublished() []publishedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]publishedMessage, len(m.published))
	copy(copied, m.published)
	return copied
}

func TestWebhookDLQReplay_SingleItemReEnqueue(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "DLQ Single Replay WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	subSecret := []byte("sub-secret-32-bytes-long-key-1234")
	sub, err := subRepo.Create(ctx, ws.ID, "https://example.com/webhooks", []string{"order.created"}, subSecret)
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	rawPayload := []byte(`{"order_id":"ord-999","amount":49.90}`)
	traceID := "trace-dlq-001"
	messageID := "msg-dlq-001"
	err = dlqRepo.InsertDLQ(ctx, ws.ID, sub.ID, traceID, messageID, "order.created", rawPayload, sub.URL, 2, nil)
	if err != nil {
		t.Fatalf("failed to insert DLQ item: %v", err)
	}

	items, err := dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("failed to retrieve inserted DLQ item: items=%v err=%v", items, err)
	}
	dlqItem := items[0]

	pub := &mockPublisher{}
	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
		WithPublisher(pub),
	)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Arguments = map[string]any{
		"workspace_id": ws.ID.String(),
		"dlq_id":       dlqItem.ID.String(),
		"action":       "re-enqueue",
	}

	res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %s", extractText(t, res))
	}

	var replayResult WebhookDLQReplayResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &replayResult); err != nil {
		t.Fatalf("failed to parse replay result JSON: %v", err)
	}

	if replayResult.TotalProcessed != 1 {
		t.Errorf("expected total_processed=1, got %d", replayResult.TotalProcessed)
	}
	if replayResult.SuccessCount != 1 {
		t.Errorf("expected success_count=1, got %d", replayResult.SuccessCount)
	}
	if replayResult.FailureCount != 0 {
		t.Errorf("expected failure_count=0, got %d", replayResult.FailureCount)
	}
	if len(replayResult.ReplayedItems) != 1 {
		t.Fatalf("expected 1 replayed item, got %d", len(replayResult.ReplayedItems))
	}

	replayed := replayResult.ReplayedItems[0]
	if replayed.DLQID != dlqItem.ID {
		t.Errorf("expected dlq_id=%s, got %s", dlqItem.ID, replayed.DLQID)
	}
	if !replayed.Success {
		t.Errorf("expected success=true, got false (error: %s)", replayed.Error)
	}
	if !replayed.DeletedFromDLQ {
		t.Errorf("expected deleted_from_dlq=true, got false")
	}
	expectedSubject := fmt.Sprintf("webhooks.deliveries.%s.%s", ws.ID, sub.ID)
	if replayed.Subject != expectedSubject {
		t.Errorf("expected subject %s, got %s", expectedSubject, replayed.Subject)
	}

	// Verify publisher was called with valid delivery task
	published := pub.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}
	if published[0].subject != expectedSubject {
		t.Errorf("expected published subject %s, got %s", expectedSubject, published[0].subject)
	}

	var task webhook.WebhookDeliveryTask
	if err := json.Unmarshal(published[0].data, &task); err != nil {
		t.Fatalf("failed to unmarshal delivery task: %v", err)
	}
	if task.ID == uuid.Nil {
		t.Errorf("delivery task ID should not be nil")
	}
	if task.WorkspaceID != ws.ID {
		t.Errorf("expected task workspace_id=%s, got %s", ws.ID, task.WorkspaceID)
	}
	if task.SubscriptionID != sub.ID {
		t.Errorf("expected task subscription_id=%s, got %s", sub.ID, task.SubscriptionID)
	}
	if task.Event != "order.created" {
		t.Errorf("expected task event='order.created', got %s", task.Event)
	}
	if task.TraceID != traceID {
		t.Errorf("expected task trace_id=%s, got %s", traceID, task.TraceID)
	}
	if task.MessageID != messageID {
		t.Errorf("expected task message_id=%s, got %s", messageID, task.MessageID)
	}
	if task.Mode != "outbound" {
		t.Errorf("expected task mode='outbound', got %s", task.Mode)
	}
	var expectedMap, actualMap map[string]any
	if err := json.Unmarshal(rawPayload, &expectedMap); err != nil {
		t.Fatalf("failed to unmarshal expected payload: %v", err)
	}
	if err := json.Unmarshal(task.Payload, &actualMap); err != nil {
		t.Fatalf("failed to unmarshal task payload: %v", err)
	}
	if fmt.Sprintf("%v", expectedMap) != fmt.Sprintf("%v", actualMap) {
		t.Errorf("expected task payload=%v, got %v", expectedMap, actualMap)
	}

	// Verify item was deleted from repository
	_, err = dlqRepo.GetDLQByID(ctx, dlqItem.ID)
	if err != repository.ErrWebhookDLQNotFound {
		t.Errorf("expected ErrWebhookDLQNotFound for deleted item, got %v", err)
	}
}

func TestWebhookDLQReplay_BatchReEnqueueWithSubscriptionFilter(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "DLQ Batch Replay WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	subA, err := subRepo.Create(ctx, ws.ID, "https://example.com/subA", []string{"event.a"}, []byte("secret-a"))
	if err != nil {
		t.Fatalf("failed to create subA: %v", err)
	}
	subB, err := subRepo.Create(ctx, ws.ID, "https://example.com/subB", []string{"event.b"}, []byte("secret-b"))
	if err != nil {
		t.Fatalf("failed to create subB: %v", err)
	}

	// Insert 2 items for subA
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subA.ID, "trace-a1", "msg-a1", "event.a", []byte(`{"a":1}`), subA.URL, 1, nil)
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subA.ID, "trace-a2", "msg-a2", "event.a", []byte(`{"a":2}`), subA.URL, 1, nil)

	// Insert 1 item for subB
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subB.ID, "trace-b1", "msg-b1", "event.b", []byte(`{"b":1}`), subB.URL, 1, nil)

	pub := &mockPublisher{}
	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
		WithPublisher(pub),
	)

	// Replay only subA
	callReq := mcp.CallToolRequest{}
	callReq.Params.Arguments = map[string]any{
		"workspace_id":    ws.ID.String(),
		"subscription_id": subA.ID.String(),
		"action":          "re-enqueue",
		"limit":           10,
	}

	res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %s", extractText(t, res))
	}

	var replayResult WebhookDLQReplayResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &replayResult); err != nil {
		t.Fatalf("failed to parse replay result JSON: %v", err)
	}

	if replayResult.TotalProcessed != 2 {
		t.Errorf("expected total_processed=2 for subA filter, got %d", replayResult.TotalProcessed)
	}
	if replayResult.SuccessCount != 2 {
		t.Errorf("expected success_count=2, got %d", replayResult.SuccessCount)
	}
	if replayResult.FailureCount != 0 {
		t.Errorf("expected failure_count=0, got %d", replayResult.FailureCount)
	}

	// Verify only subA messages were published
	published := pub.getPublished()
	if len(published) != 2 {
		t.Fatalf("expected 2 published messages, got %d", len(published))
	}
	expectedSubjectA := fmt.Sprintf("webhooks.deliveries.%s.%s", ws.ID, subA.ID)
	for _, p := range published {
		if p.subject != expectedSubjectA {
			t.Errorf("expected subject %s, got %s", expectedSubjectA, p.subject)
		}
	}

	// Verify subB item remains in DLQ
	remaining, err := dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
	if err != nil {
		t.Fatalf("failed to list remaining DLQ items: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining DLQ item (subB), got %d", len(remaining))
	}
	if remaining[0].SubscriptionID != subB.ID {
		t.Errorf("expected remaining item for subB=%s, got %s", subB.ID, remaining[0].SubscriptionID)
	}

	// Replay remaining (subB) without filter
	callReqNoFilter := mcp.CallToolRequest{}
	callReqNoFilter.Params.Arguments = map[string]any{
		"workspace_id": ws.ID.String(),
		"action":       "re-enqueue",
	}

	resB, err := srv.handleReplayWebhookDLQ(ctx, callReqNoFilter)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if resB.IsError {
		t.Fatalf("handler returned tool error: %s", extractText(t, resB))
	}

	var replayResultB WebhookDLQReplayResult
	if err := json.Unmarshal([]byte(extractText(t, resB)), &replayResultB); err != nil {
		t.Fatalf("failed to parse replay result JSON: %v", err)
	}
	if replayResultB.TotalProcessed != 1 || replayResultB.SuccessCount != 1 {
		t.Errorf("expected 1 processed and 1 success for subB, got %+v", replayResultB)
	}

	remainingAfter, err := dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
	if err != nil {
		t.Fatalf("failed to list remaining DLQ items: %v", err)
	}
	if len(remainingAfter) != 0 {
		t.Errorf("expected 0 remaining DLQ items, got %d", len(remainingAfter))
	}
}

func TestWebhookDLQReplay_ImmediateDispatch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "DLQ Immediate Dispatch WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	var mu sync.Mutex
	var receivedHeaders http.Header
	var receivedBody []byte

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		receivedHeaders = r.Header.Clone()
		receivedBody = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	defer mockServer.Close()

	subSecret := []byte("sub-secret-immediate-dispatch-12")
	sub, err := subRepo.Create(ctx, ws.ID, mockServer.URL, []string{"payment.completed"}, subSecret)
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	payload := []byte(`{"payment_id":"pay-001","status":"success"}`)
	err = dlqRepo.InsertDLQ(ctx, ws.ID, sub.ID, "trace-imm-1", "msg-imm-1", "payment.completed", payload, sub.URL, 1, nil)
	if err != nil {
		t.Fatalf("failed to insert DLQ item: %v", err)
	}

	safeClient := netpolicy.NewSafeClient(
		netpolicy.WithAllowedIPs("127.0.0.1", "::1"),
		netpolicy.WithTimeout(5*time.Second),
	)

	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
		WithSafeClient(safeClient),
	)

	t.Run("DispatchImmediate_SuccessToSubscriptionURL", func(t *testing.T) {
		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"action":       "dispatch_immediate",
		}

		res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var replayResult WebhookDLQReplayResult
		if err := json.Unmarshal([]byte(extractText(t, res)), &replayResult); err != nil {
			t.Fatalf("failed to parse replay result JSON: %v", err)
		}

		if replayResult.TotalProcessed != 1 || replayResult.SuccessCount != 1 {
			t.Errorf("expected 1 processed and 1 success, got %+v", replayResult)
		}
		if replayResult.ReplayedItems[0].StatusCode != 200 {
			t.Errorf("expected status code 200, got %d", replayResult.ReplayedItems[0].StatusCode)
		}
		if !replayResult.ReplayedItems[0].Success {
			t.Errorf("expected success=true, got false")
		}
		if replayResult.ReplayedItems[0].ResponseBody != `{"received":true}` {
			t.Errorf("expected ResponseBody '{\"received\":true}', got %q", replayResult.ReplayedItems[0].ResponseBody)
		}

		mu.Lock()
		reqHeaders := receivedHeaders
		reqBody := receivedBody
		mu.Unlock()

		if reqHeaders.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", reqHeaders.Get("Content-Type"))
		}
		if reqHeaders.Get("X-PerGo-Replayed") != "true" {
			t.Errorf("expected X-PerGo-Replayed=true, got %s", reqHeaders.Get("X-PerGo-Replayed"))
		}
		if reqHeaders.Get("X-PerGo-Event") != "payment.completed" {
			t.Errorf("expected X-PerGo-Event=payment.completed, got %s", reqHeaders.Get("X-PerGo-Event"))
		}
		if reqHeaders.Get("X-Trace-ID") != "trace-imm-1" {
			t.Errorf("expected X-Trace-ID=trace-imm-1, got %s", reqHeaders.Get("X-Trace-ID"))
		}

		sigHeader := reqHeaders.Get("X-PerGo-Signature")
		if sigHeader == "" {
			t.Fatalf("missing X-PerGo-Signature header on received request")
		}
		if !webhook.VerifySignatureWithTolerance(reqBody, sigHeader, subSecret, 5*time.Minute) {
			t.Errorf("HMAC signature verification failed")
		}

		// Verify DLQ item was NOT deleted (dispatch_immediate leaves DLQ untouched)
		items, err := dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
		if err != nil || len(items) != 1 {
			t.Errorf("expected item to still remain in DLQ after dispatch_immediate, got %d items", len(items))
		}
	})

	t.Run("DispatchImmediate_WithTargetURLOverride", func(t *testing.T) {
		var overrideCalled bool
		overrideServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			overrideCalled = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		defer overrideServer.Close()

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"action":       "dispatch_immediate",
			"target_url":   overrideServer.URL,
		}

		res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		mu.Lock()
		called := overrideCalled
		mu.Unlock()

		if !called {
			t.Errorf("expected overrideServer to be called, but it was not")
		}
	})
}

func TestWebhookDLQReplay_SSRFRejection(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "DLQ SSRF Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	sub, err := subRepo.Create(ctx, ws.ID, "https://example.com/hook", []string{"test"}, []byte("secret"))
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	err = dlqRepo.InsertDLQ(ctx, ws.ID, sub.ID, "trace-ssrf", "msg-ssrf", "test", []byte(`{"a":1}`), sub.URL, 1, nil)
	if err != nil {
		t.Fatalf("failed to insert DLQ: %v", err)
	}

	// Server without custom safeClient (default netpolicy rejects 127.0.0.1, 169.254.169.254, RFC1918)
	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
	)

	ssrfTargets := []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/hook",
		"http://192.168.1.1/hook",
	}

	for _, target := range ssrfTargets {
		t.Run(target, func(t *testing.T) {
			callReq := mcp.CallToolRequest{}
			callReq.Params.Arguments = map[string]any{
				"workspace_id": ws.ID.String(),
				"action":       "dispatch_immediate",
				"target_url":   target,
			}

			res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			if res.IsError {
				t.Fatalf("expected structured JSON telemetry rather than tool error: %s", extractText(t, res))
			}

			var replayResult WebhookDLQReplayResult
			if err := json.Unmarshal([]byte(extractText(t, res)), &replayResult); err != nil {
				t.Fatalf("failed to parse replay result JSON: %v", err)
			}

			if replayResult.SuccessCount != 0 {
				t.Errorf("expected success_count=0 for SSRF target %s, got %d", target, replayResult.SuccessCount)
			}
			if replayResult.FailureCount != 1 {
				t.Errorf("expected failure_count=1 for SSRF target %s, got %d", target, replayResult.FailureCount)
			}
			if len(replayResult.ReplayedItems) != 1 {
				t.Fatalf("expected 1 replayed item, got %d", len(replayResult.ReplayedItems))
			}
			if replayResult.ReplayedItems[0].Success {
				t.Errorf("expected item success=false for SSRF target %s", target)
			}
			if !strings.Contains(replayResult.ReplayedItems[0].Error, "restricted IP address blocked by netpolicy") {
				t.Errorf("expected netpolicy restricted IP error for %s, got %s", target, replayResult.ReplayedItems[0].Error)
			}
		})
	}
}

func TestWebhookDLQReplay_UnconfiguredDependencies(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "Unconfigured Deps WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	t.Run("MissingDLQRepository", func(t *testing.T) {
		srv := NewServer(
			wsRepo,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			[]byte("test-sso-secret"),
			"http://localhost:8080",
			// No WithWebhookDLQRepo
		)

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"action":       "re-enqueue",
		}

		res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if !res.IsError {
			t.Errorf("expected error when DLQ repo is not configured")
		}
		if !strings.Contains(extractText(t, res), "webhook DLQ repository is not configured") {
			t.Errorf("expected actionable error message about DLQ repository, got: %s", extractText(t, res))
		}
	})

	t.Run("MissingPublisher_ReEnqueueAction", func(t *testing.T) {
		srv := NewServer(
			wsRepo,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			[]byte("test-sso-secret"),
			"http://localhost:8080",
			WithWebhookDLQRepo(dlqRepo),
			// No Publisher
		)

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"action":       "re-enqueue",
		}

		res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if !res.IsError {
			t.Errorf("expected error when publisher is not configured for re-enqueue")
		}
		if !strings.Contains(extractText(t, res), "JetStream publisher is not configured") {
			t.Errorf("expected actionable error message about JetStream publisher, got: %s", extractText(t, res))
		}
	})

	t.Run("MissingPublisher_DispatchImmediateActionAllowed", func(t *testing.T) {
		srv := NewServer(
			wsRepo,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			[]byte("test-sso-secret"),
			"http://localhost:8080",
			WithWebhookDLQRepo(dlqRepo),
			// No Publisher
		)

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"action":       "dispatch_immediate",
		}

		res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		// When no items exist, dispatch_immediate should succeed with total_processed=0, not fail on missing publisher
		if res.IsError {
			t.Errorf("dispatch_immediate should not require publisher, got error: %s", extractText(t, res))
		}
	})
}

func TestWebhookDLQReplay_ValidationAndIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws1, err := wsRepo.Create(ctx, "Validation WS 1")
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws1.ID) }()

	ws2, err := wsRepo.Create(ctx, "Validation WS 2")
	if err != nil {
		t.Fatalf("failed to create ws2: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws2.ID) }()

	_, err = subRepo.Create(ctx, ws1.ID, "https://example.com/ws1", []string{"test"}, []byte("secret1"))
	if err != nil {
		t.Fatalf("failed to create sub1: %v", err)
	}
	sub2, err := subRepo.Create(ctx, ws2.ID, "https://example.com/ws2", []string{"test"}, []byte("secret2"))
	if err != nil {
		t.Fatalf("failed to create sub2: %v", err)
	}

	err = dlqRepo.InsertDLQ(ctx, ws2.ID, sub2.ID, "trace-ws2", "msg-ws2", "test", []byte(`{}`), sub2.URL, 1, nil)
	if err != nil {
		t.Fatalf("failed to insert DLQ in ws2: %v", err)
	}
	itemsWS2, err := dlqRepo.ListDLQ(ctx, ws2.ID, 10, 0)
	if err != nil || len(itemsWS2) != 1 {
		t.Fatalf("failed to retrieve ws2 item")
	}
	dlqWS2Item := itemsWS2[0]

	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
		WithPublisher(&mockPublisher{}),
	)

	testCases := []struct {
		name        string
		args        map[string]any
		expectedErr string
	}{
		{
			name:        "missing_workspace_id",
			args:        map[string]any{},
			expectedErr: "missing or invalid workspace_id argument",
		},
		{
			name:        "invalid_workspace_uuid",
			args:        map[string]any{"workspace_id": "not-a-uuid"},
			expectedErr: "invalid workspace_id UUID",
		},
		{
			name:        "non_existent_workspace",
			args:        map[string]any{"workspace_id": uuid.New().String()},
			expectedErr: "workspace not found",
		},
		{
			name:        "invalid_action",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "action": "purge_everything"},
			expectedErr: "invalid action: must be 're-enqueue' or 'dispatch_immediate'",
		},
		{
			name:        "invalid_dlq_uuid",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "dlq_id": "bad-uuid"},
			expectedErr: "invalid dlq_id UUID",
		},
		{
			name:        "dlq_item_not_found",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "dlq_id": uuid.New().String()},
			expectedErr: "DLQ item not found",
		},
		{
			name:        "cross_tenant_isolation_dlq_item_mismatch",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "dlq_id": dlqWS2Item.ID.String()},
			expectedErr: "DLQ item does not belong to the specified workspace",
		},
		{
			name:        "invalid_subscription_uuid",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "subscription_id": "bad-sub-uuid"},
			expectedErr: "invalid subscription_id UUID",
		},
		{
			name:        "cross_tenant_isolation_subscription_mismatch",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "subscription_id": sub2.ID.String()},
			expectedErr: "webhook subscription not found for workspace",
		},
		{
			name:        "invalid_target_url_scheme",
			args:        map[string]any{"workspace_id": ws1.ID.String(), "target_url": "ftp://example.com/hook"},
			expectedErr: "scheme must be http or https",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callReq := mcp.CallToolRequest{}
			callReq.Params.Arguments = tc.args

			res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected tool error for %s, but got success: %s", tc.name, extractText(t, res))
			}
			if !strings.Contains(extractText(t, res), tc.expectedErr) {
				t.Errorf("expected error to contain %q, got %q", tc.expectedErr, extractText(t, res))
			}
		})
	}
}

func TestWebhookDLQReplay_SubscriptionFilterPaginationAvoidsStarvation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "DLQ Starvation Test WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	subA, err := subRepo.Create(ctx, ws.ID, "https://example.com/subA", []string{"event.a"}, []byte("secret-a"))
	if err != nil {
		t.Fatalf("failed to create subA: %v", err)
	}
	subB, err := subRepo.Create(ctx, ws.ID, "https://example.com/subB", []string{"event.b"}, []byte("secret-b"))
	if err != nil {
		t.Fatalf("failed to create subB: %v", err)
	}

	// Insert DLQ items such that subB items appear first in DESC created_at order:
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subA.ID, "trace-a1", "msg-a1", "event.a", []byte(`{"a":1}`), subA.URL, 1, nil)
	time.Sleep(10 * time.Millisecond)
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subB.ID, "trace-b1", "msg-b1", "event.b", []byte(`{"b":1}`), subB.URL, 1, nil)
	time.Sleep(10 * time.Millisecond)
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subA.ID, "trace-a2", "msg-a2", "event.a", []byte(`{"a":2}`), subA.URL, 1, nil)
	time.Sleep(10 * time.Millisecond)
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subB.ID, "trace-b2", "msg-b2", "event.b", []byte(`{"b":2}`), subB.URL, 1, nil)
	time.Sleep(10 * time.Millisecond)
	_ = dlqRepo.InsertDLQ(ctx, ws.ID, subB.ID, "trace-b3", "msg-b3", "event.b", []byte(`{"b":3}`), subB.URL, 1, nil)

	pub := &mockPublisher{}
	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		subRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithWebhookDLQRepo(dlqRepo),
		WithPublisher(pub),
	)

	// Filter by subA with limit=2. Top 2 items in the workspace DLQ are subB (b3, b2).
	// Without pagination, subA would be starved. With pagination, it pages until limit (2) subA items are gathered.
	callReq := mcp.CallToolRequest{}
	callReq.Params.Arguments = map[string]any{
		"workspace_id":    ws.ID.String(),
		"subscription_id": subA.ID.String(),
		"action":          "re-enqueue",
		"limit":           2,
	}

	res, err := srv.handleReplayWebhookDLQ(ctx, callReq)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %s", extractText(t, res))
	}

	var replayResult WebhookDLQReplayResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &replayResult); err != nil {
		t.Fatalf("failed to parse replay result JSON: %v", err)
	}

	if replayResult.TotalProcessed != 2 || replayResult.SuccessCount != 2 {
		t.Fatalf("expected 2 processed and 2 successes for subA, got %+v", replayResult)
	}

	for _, item := range replayResult.ReplayedItems {
		if item.SubscriptionID != subA.ID {
			t.Errorf("expected item for subA %s, got %s", subA.ID, item.SubscriptionID)
		}
	}
}
