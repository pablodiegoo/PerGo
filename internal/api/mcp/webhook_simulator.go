package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/webhook"
)

// WebhookSimulationTelemetry captures the structured execution metrics of a synthetic webhook dispatch.
type WebhookSimulationTelemetry struct {
	SubscriptionID *uuid.UUID `json:"subscription_id,omitempty"`
	TargetURL      string     `json:"target_url"`
	EventType      string     `json:"event_type"`
	TraceID        string     `json:"trace_id"`
	StatusCode     int        `json:"status_code"`
	LatencyMS      int64      `json:"latency_ms"`
	Success        bool       `json:"success"`
	ResponseBody   string     `json:"response_body,omitempty"`
	Error          string     `json:"error,omitempty"`
}

func (s *Server) handleSimulateWebhookEvent(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil || strings.TrimSpace(wsIDStr) == "" {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	ws, err := s.wsRepo.GetByID(ctx, workspaceID)
	if err != nil || ws == nil {
		return mcp.NewToolResultError("workspace not found"), nil
	}

	eventType, err := request.RequireString("event_type")
	if err != nil || strings.TrimSpace(eventType) == "" {
		return mcp.NewToolResultError("missing or invalid event_type argument"), nil
	}
	eventType = strings.TrimSpace(eventType)

	args := request.GetArguments()
	rawPayload, exists := args["payload"]
	if !exists || rawPayload == nil {
		return mcp.NewToolResultError("missing or invalid payload argument"), nil
	}

	var bodyBytes []byte
	switch v := rawPayload.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return mcp.NewToolResultError("payload cannot be empty"), nil
		}
		if json.Valid([]byte(trimmed)) {
			bodyBytes = []byte(trimmed)
		} else {
			b, err := json.Marshal(v)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to serialize payload: %v", err)), nil
			}
			bodyBytes = b
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to serialize payload: %v", err)), nil
		}
		bodyBytes = b
	}

	if len(bodyBytes) == 0 {
		return mcp.NewToolResultError("payload cannot be empty"), nil
	}

	subIDStr := strings.TrimSpace(request.GetString("subscription_id", ""))
	targetURL := strings.TrimSpace(request.GetString("target_url", ""))

	var destURL string
	var secret []byte
	var targetedSubID *uuid.UUID

	if subIDStr != "" {
		subID, err := uuid.Parse(subIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid subscription_id UUID: %v", err)), nil
		}

		sub, err := s.webhookSubRepo.Get(ctx, subID)
		if err != nil || sub == nil || sub.WorkspaceID != workspaceID {
			return mcp.NewToolResultError("webhook subscription not found for workspace"), nil
		}
		targetedSubID = &sub.ID
		destURL = sub.URL
		if len(sub.Secret) > 0 {
			secret = sub.Secret
		}
	}

	// Custom target_url override
	if targetURL != "" {
		destURL = targetURL
	}

	if destURL == "" {
		return mcp.NewToolResultError("either subscription_id or target_url must be provided"), nil
	}

	// Fallback to workspace secret if subscription secret not set
	if len(secret) == 0 && ws.WebhookSecret != nil && *ws.WebhookSecret != "" {
		secret = []byte(*ws.WebhookSecret)
	}

	u, err := url.ParseRequestURI(destURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return mcp.NewToolResultError(fmt.Sprintf("invalid destination URL %q: scheme must be http or https", destURL)), nil
	}

	var signature string
	if len(secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature = webhook.SignPayload(bodyBytes, secret, timestamp)
	}

	traceID := "mcp-sim-" + uuid.New().String()[:8]

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create dispatch request: %v", err)), nil
	}

	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-PerGo-Signature", signature)
	}
	req.Header.Set("X-PerGo-Simulated", "true")
	req.Header.Set("X-PerGo-Event", eventType)
	req.Header.Set("X-Trace-ID", traceID)

	client := s.safeClient
	if client == nil {
		client = netpolicy.NewSafeClient(netpolicy.WithTimeout(10 * time.Second))
	}

	start := time.Now()
	resp, reqErr := client.Do(req)
	latency := time.Since(start).Milliseconds()

	res := WebhookSimulationTelemetry{
		SubscriptionID: targetedSubID,
		TargetURL:      destURL,
		EventType:      eventType,
		TraceID:        traceID,
		LatencyMS:      latency,
	}

	if reqErr != nil {
		res.Success = false
		res.Error = fmt.Sprintf("dispatch error: %v", reqErr)
	} else {
		defer resp.Body.Close()
		res.StatusCode = resp.StatusCode
		res.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if len(respBody) > 0 {
			res.ResponseBody = string(respBody)
		}
		if !res.Success {
			res.Error = fmt.Sprintf("HTTP %s", resp.Status)
		}
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal simulation result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}
