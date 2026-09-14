package mcp

import (
	"context"
	"encoding/json"
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

type receivedWebhookRequest struct {
	Headers http.Header
	Body    []byte
}

func TestWebhookSimulator_SuccessfulDispatch(t *testing.T) {
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
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "Webhook Sim Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	wsSecret := "ws-secret-1234567890abcdef12345678"
	if err := wsRepo.SetWebhookSecret(ctx, ws.ID, wsSecret); err != nil {
		t.Fatalf("failed to set workspace webhook secret: %v", err)
	}

	var mu sync.Mutex
	var lastReq receivedWebhookRequest

	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		lastReq = receivedWebhookRequest{
			Headers: r.Header.Clone(),
			Body:    body,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true,"status":"accepted"}`))
	}))
	defer mockReceiver.Close()

	subSecret := []byte("sub-secret-abcdef1234567890abcdef")
	sub, err := webhookSubRepo.Create(ctx, ws.ID, mockReceiver.URL, []string{"message.received", "message.delivered"}, subSecret)
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	// Safe client with 127.0.0.1 allowlisted for local mock receiver
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
		webhookSubRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithSafeClient(safeClient),
	)

	t.Run("SubscriptionDispatch_ObjectPayload", func(t *testing.T) {
		payloadObj := map[string]any{
			"message_id": "msg-001",
			"sender":     "+5511999999999",
			"text":       "Hello Webhook Simulation",
		}

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": sub.ID.String(),
			"event_type":      "message.received",
			"payload":         payloadObj,
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var telem WebhookSimulationTelemetry
		if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
			t.Fatalf("failed to parse telemetry JSON: %v", err)
		}

		if !telem.Success {
			t.Errorf("expected success=true, got false (error: %s)", telem.Error)
		}
		if telem.StatusCode != 200 {
			t.Errorf("expected status_code=200, got %d", telem.StatusCode)
		}
		if telem.EventType != "message.received" {
			t.Errorf("expected event_type 'message.received', got %s", telem.EventType)
		}
		if telem.SubscriptionID == nil || *telem.SubscriptionID != sub.ID {
			t.Errorf("expected subscription_id=%s, got %v", sub.ID, telem.SubscriptionID)
		}
		if telem.TargetURL != mockReceiver.URL {
			t.Errorf("expected target_url=%s, got %s", mockReceiver.URL, telem.TargetURL)
		}
		if !strings.HasPrefix(telem.TraceID, "mcp-sim-") {
			t.Errorf("expected trace_id starting with 'mcp-sim-', got %s", telem.TraceID)
		}
		if !strings.Contains(telem.ResponseBody, "accepted") {
			t.Errorf("expected response_body to contain 'accepted', got %s", telem.ResponseBody)
		}
		if telem.LatencyMS < 0 {
			t.Errorf("latency_ms should be non-negative, got %d", telem.LatencyMS)
		}

		// Verify receiver saw valid headers and HMAC signature
		mu.Lock()
		reqReceived := lastReq
		mu.Unlock()

		if reqReceived.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", reqReceived.Headers.Get("Content-Type"))
		}
		if reqReceived.Headers.Get("X-PerGo-Simulated") != "true" {
			t.Errorf("expected X-PerGo-Simulated=true, got %s", reqReceived.Headers.Get("X-PerGo-Simulated"))
		}
		if reqReceived.Headers.Get("X-PerGo-Event") != "message.received" {
			t.Errorf("expected X-PerGo-Event=message.received, got %s", reqReceived.Headers.Get("X-PerGo-Event"))
		}
		if reqReceived.Headers.Get("X-Trace-ID") != telem.TraceID {
			t.Errorf("expected X-Trace-ID=%s, got %s", telem.TraceID, reqReceived.Headers.Get("X-Trace-ID"))
		}

		sigHeader := reqReceived.Headers.Get("X-PerGo-Signature")
		if sigHeader == "" {
			t.Fatalf("missing X-PerGo-Signature header on received request")
		}
		if !webhook.VerifySignatureWithTolerance(reqReceived.Body, sigHeader, subSecret, 5*time.Minute) {
			t.Errorf("HMAC-SHA256 signature verification failed for subscription secret")
		}
	})

	t.Run("SubscriptionDispatch_JSONStringPayload", func(t *testing.T) {
		jsonStrPayload := `{"event":"flow.submitted","flow_token":"tok-abc-123"}`

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": sub.ID.String(),
			"event_type":      "flow.submitted",
			"payload":         jsonStrPayload,
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var telem WebhookSimulationTelemetry
		if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
			t.Fatalf("failed to parse telemetry JSON: %v", err)
		}

		if !telem.Success || telem.StatusCode != 200 {
			t.Errorf("expected success=true, status_code=200, got %+v", telem)
		}

		mu.Lock()
		reqReceived := lastReq
		mu.Unlock()

		sigHeader := reqReceived.Headers.Get("X-PerGo-Signature")
		if !webhook.VerifySignatureWithTolerance(reqReceived.Body, sigHeader, subSecret, 5*time.Minute) {
			t.Errorf("HMAC-SHA256 signature verification failed for string payload")
		}
		if string(reqReceived.Body) != jsonStrPayload {
			t.Errorf("expected body=%s, got %s", jsonStrPayload, string(reqReceived.Body))
		}
	})

	t.Run("CustomTargetURL_OverrideSubscription", func(t *testing.T) {
		overrideServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			lastReq = receivedWebhookRequest{
				Headers: r.Header.Clone(),
				Body:    body,
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"override":true}`))
		}))
		defer overrideServer.Close()

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id":    ws.ID.String(),
			"subscription_id": sub.ID.String(),
			"target_url":      overrideServer.URL,
			"event_type":      "message.delivered",
			"payload":         map[string]any{"status": "delivered"},
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var telem WebhookSimulationTelemetry
		if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
			t.Fatalf("failed to parse telemetry JSON: %v", err)
		}

		if !telem.Success || telem.StatusCode != 200 {
			t.Errorf("expected success=true, status_code=200, got %+v", telem)
		}
		if telem.TargetURL != overrideServer.URL {
			t.Errorf("expected target_url=%s, got %s", overrideServer.URL, telem.TargetURL)
		}
		if telem.SubscriptionID == nil || *telem.SubscriptionID != sub.ID {
			t.Errorf("expected subscription_id=%s, got %v", sub.ID, telem.SubscriptionID)
		}

		// Secret should be inherited from subscription
		mu.Lock()
		reqReceived := lastReq
		mu.Unlock()

		sigHeader := reqReceived.Headers.Get("X-PerGo-Signature")
		if !webhook.VerifySignatureWithTolerance(reqReceived.Body, sigHeader, subSecret, 5*time.Minute) {
			t.Errorf("HMAC signature mismatch on override server")
		}
	})

	t.Run("CustomTargetURL_WorkspaceSecretFallback", func(t *testing.T) {
		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"target_url":   mockReceiver.URL,
			"event_type":   "workspace.event",
			"payload":      map[string]any{"data": 123},
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var telem WebhookSimulationTelemetry
		if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
			t.Fatalf("failed to parse telemetry JSON: %v", err)
		}

		if !telem.Success || telem.StatusCode != 200 {
			t.Errorf("expected success=true, status_code=200, got %+v", telem)
		}
		if telem.SubscriptionID != nil {
			t.Errorf("expected nil subscription_id, got %v", telem.SubscriptionID)
		}

		// Signature should be signed with workspace secret
		mu.Lock()
		reqReceived := lastReq
		mu.Unlock()

		sigHeader := reqReceived.Headers.Get("X-PerGo-Signature")
		if sigHeader == "" {
			t.Fatalf("missing X-PerGo-Signature header")
		}
		if !webhook.VerifySignatureWithTolerance(reqReceived.Body, sigHeader, []byte(wsSecret), 5*time.Minute) {
			t.Errorf("HMAC signature mismatch with workspace secret")
		}
	})

	t.Run("ActiveSubscriptionsFallback_AutoDispatchesFirstActive", func(t *testing.T) {
		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"event_type":   "message.delivered",
			"payload":      map[string]any{"status": "delivered"},
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned tool error: %s", extractText(t, res))
		}

		var telem WebhookSimulationTelemetry
		if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
			t.Fatalf("failed to parse telemetry JSON: %v", err)
		}

		if !telem.Success || telem.StatusCode != 200 {
			t.Errorf("expected success=true, status_code=200, got %+v", telem)
		}
		if telem.SubscriptionID == nil || *telem.SubscriptionID != sub.ID {
			t.Errorf("expected subscription_id %v, got %v", sub.ID, telem.SubscriptionID)
		}
		if telem.TargetURL != mockReceiver.URL {
			t.Errorf("expected target_url %q, got %q", mockReceiver.URL, telem.TargetURL)
		}
	})

	t.Run("ActiveSubscriptionsFallback_NoActiveSubscriptionsError", func(t *testing.T) {
		wsEmpty, err := wsRepo.Create(ctx, "Empty Subs Workspace")
		if err != nil {
			t.Fatalf("failed to create workspace: %v", err)
		}
		defer func() { _ = wsRepo.Delete(ctx, wsEmpty.ID) }()

		callReq := mcp.CallToolRequest{}
		callReq.Params.Arguments = map[string]any{
			"workspace_id": wsEmpty.ID.String(),
			"event_type":   "message.delivered",
			"payload":      map[string]any{"status": "delivered"},
		}

		res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error when no active subscriptions exist, got success")
		}
		errText := extractText(t, res)
		if !strings.Contains(errText, "no active webhook subscriptions found") {
			t.Errorf("expected 'no active webhook subscriptions found' error, got %q", errText)
		}
	})
}

func TestWebhookSimulator_SSRFRejection(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "SSRF Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Server configured with DEFAULT SafeClient (no allowlist override), enforcing strict anti-SSRF protections
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
	)

	ssrfTargets := []struct {
		name      string
		targetURL string
	}{
		{
			name:      "loopback_127.0.0.1",
			targetURL: "http://127.0.0.1:8080/webhook",
		},
		{
			name:      "cloud_metadata_169.254.169.254",
			targetURL: "http://169.254.169.254/latest/meta-data",
		},
		{
			name:      "private_10.0.0.1",
			targetURL: "http://10.0.0.1/webhook",
		},
		{
			name:      "private_192.168.1.1",
			targetURL: "http://192.168.1.1/webhook",
		},
		{
			name:      "cgnat_100.64.0.1",
			targetURL: "http://100.64.0.1/webhook",
		},
	}

	for _, tc := range ssrfTargets {
		t.Run(tc.name, func(t *testing.T) {
			callReq := mcp.CallToolRequest{}
			callReq.Params.Arguments = map[string]any{
				"workspace_id": ws.ID.String(),
				"target_url":   tc.targetURL,
				"event_type":   "ssrf.test",
				"payload":      map[string]any{"test": true},
			}

			res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			// Safe client rejection should return telemetry JSON with success=false, not a tool crash
			if res.IsError {
				t.Fatalf("expected tool result text with telemetry, got error: %s", extractText(t, res))
			}

			var telem WebhookSimulationTelemetry
			if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
				t.Fatalf("failed to parse telemetry JSON: %v", err)
			}

			if telem.Success {
				t.Errorf("expected success=false for SSRF target %s, got true", tc.targetURL)
			}
			if telem.StatusCode != 0 {
				t.Errorf("expected status_code=0 for blocked SSRF dispatch, got %d", telem.StatusCode)
			}
			if !strings.Contains(telem.Error, "restricted IP address blocked by netpolicy") {
				t.Errorf("expected error to contain 'restricted IP address blocked by netpolicy', got: %s", telem.Error)
			}
		})
	}
}

func TestWebhookSimulator_ReceiverError(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "Receiver Error Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal_failure"}`))
	}))
	defer errServer.Close()

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
		nil,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
		WithSafeClient(safeClient),
	)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Arguments = map[string]any{
		"workspace_id": ws.ID.String(),
		"target_url":   errServer.URL,
		"event_type":   "error.test",
		"payload":      map[string]any{"data": "err"},
	}

	res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(t, res))
	}

	var telem WebhookSimulationTelemetry
	if err := json.Unmarshal([]byte(extractText(t, res)), &telem); err != nil {
		t.Fatalf("failed to parse telemetry JSON: %v", err)
	}

	if telem.Success {
		t.Errorf("expected success=false for 500 response, got true")
	}
	if telem.StatusCode != 500 {
		t.Errorf("expected status_code=500, got %d", telem.StatusCode)
	}
	if !strings.Contains(telem.ResponseBody, "internal_failure") {
		t.Errorf("expected response_body to contain 'internal_failure', got %s", telem.ResponseBody)
	}
	if !strings.Contains(telem.Error, "500") {
		t.Errorf("expected error to mention 500, got %s", telem.Error)
	}
}

func TestWebhookSimulator_ValidationErrors(t *testing.T) {
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
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

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

	subOther, err := webhookSubRepo.Create(ctx, ws2.ID, "https://example.com/webhook", []string{"test"}, []byte("secret"))
	if err != nil {
		t.Fatalf("failed to create sub in ws2: %v", err)
	}

	srv := NewServer(
		wsRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		webhookSubRepo,
		nil,
		nil,
		[]byte("test-sso-secret"),
		"http://localhost:8080",
	)

	testCases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing_workspace_id",
			args: map[string]any{
				"event_type": "test",
				"payload":    map[string]any{"a": 1},
				"target_url": "https://example.com/hook",
			},
		},
		{
			name: "invalid_workspace_uuid",
			args: map[string]any{
				"workspace_id": "not-a-uuid",
				"event_type":   "test",
				"payload":      map[string]any{"a": 1},
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "non_existent_workspace",
			args: map[string]any{
				"workspace_id": uuid.New().String(),
				"event_type":   "test",
				"payload":      map[string]any{"a": 1},
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "missing_event_type",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"payload":      map[string]any{"a": 1},
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "empty_event_type",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "   ",
				"payload":      map[string]any{"a": 1},
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "missing_payload",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "test",
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "empty_payload_string",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "test",
				"payload":      "   ",
				"target_url":   "https://example.com/hook",
			},
		},
		{
			name: "missing_both_sub_and_url",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "test",
				"payload":      map[string]any{"a": 1},
			},
		},
		{
			name: "invalid_subscription_uuid",
			args: map[string]any{
				"workspace_id":    ws1.ID.String(),
				"subscription_id": "bad-uuid",
				"event_type":      "test",
				"payload":         map[string]any{"a": 1},
			},
		},
		{
			name: "subscription_not_found",
			args: map[string]any{
				"workspace_id":    ws1.ID.String(),
				"subscription_id": uuid.New().String(),
				"event_type":      "test",
				"payload":         map[string]any{"a": 1},
			},
		},
		{
			name: "subscription_workspace_mismatch",
			args: map[string]any{
				"workspace_id":    ws1.ID.String(),
				"subscription_id": subOther.ID.String(),
				"event_type":      "test",
				"payload":         map[string]any{"a": 1},
			},
		},
		{
			name: "invalid_target_url_scheme",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "test",
				"payload":      map[string]any{"a": 1},
				"target_url":   "ftp://example.com/hook",
			},
		},
		{
			name: "invalid_target_url_format",
			args: map[string]any{
				"workspace_id": ws1.ID.String(),
				"event_type":   "test",
				"payload":      map[string]any{"a": 1},
				"target_url":   "://not-a-valid-url",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callReq := mcp.CallToolRequest{}
			callReq.Params.Arguments = tc.args

			res, err := srv.handleSimulateWebhookEvent(ctx, callReq)
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			if !res.IsError {
				t.Errorf("expected tool error for %s, but got success: %s", tc.name, extractText(t, res))
			}
		})
	}
}
