package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pablojhp.pergo/internal/repository"
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

		if s.webhookSubRepo == nil {
			return mcp.NewToolResultError("webhook subscription repository is not configured on this MCP server"), nil
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
	} else if targetURL == "" {
		// Both subscription_id and target_url are omitted: fallback to first active subscription for workspace.
		if s.webhookSubRepo == nil {
			return mcp.NewToolResultError("no active webhook subscriptions found for workspace"), nil
		}
		subs, err := s.webhookSubRepo.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to query webhook subscriptions: %v", err)), nil
		}
		var activeSub *repository.WebhookSubscription
		for _, s := range subs {
			if s.Active {
				activeSub = s
				break
			}
		}
		if activeSub == nil {
			return mcp.NewToolResultError("no active webhook subscriptions found for workspace"), nil
		}
		targetedSubID = &activeSub.ID
		destURL = activeSub.URL
		if len(activeSub.Secret) > 0 {
			secret = activeSub.Secret
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

	traceID := "mcp-sim-" + uuid.New().String()[:8]

	dispatchRes, err := executeWebhookDispatch(ctx, WebhookDispatchOptions{
		TargetURL: destURL,
		Payload:   bodyBytes,
		Secret:    secret,
		EventType: eventType,
		TraceID:   traceID,
		ExtraHeaders: map[string]string{
			"X-PerGo-Simulated": "true",
		},
		Client: s.safeClient,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res := WebhookSimulationTelemetry{
		SubscriptionID: targetedSubID,
		TargetURL:      destURL,
		EventType:      eventType,
		TraceID:        traceID,
		LatencyMS:      dispatchRes.LatencyMS,
		StatusCode:     dispatchRes.StatusCode,
		Success:        dispatchRes.Success,
		ResponseBody:   dispatchRes.ResponseBody,
		Error:          dispatchRes.Error,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal simulation result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}
