package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

// WebhookDLQReplayItemResult captures execution details for a single dead-letter replay attempt.
type WebhookDLQReplayItemResult struct {
	DLQID          uuid.UUID `json:"dlq_id"`
	SubscriptionID uuid.UUID `json:"subscription_id"`
	EventType      string    `json:"event_type"`
	TraceID        string    `json:"trace_id"`
	MessageID      string    `json:"message_id,omitempty"`
	TargetURL      string    `json:"target_url,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	StatusCode     int       `json:"status_code,omitempty"`
	LatencyMS      int64     `json:"latency_ms,omitempty"`
	Success        bool      `json:"success"`
	DeletedFromDLQ bool      `json:"deleted_from_dlq,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// WebhookDLQReplayResult captures the aggregate execution telemetry of a DLQ replay operation.
type WebhookDLQReplayResult struct {
	WorkspaceID    uuid.UUID                    `json:"workspace_id"`
	Action         string                       `json:"action"`
	TotalProcessed int                          `json:"total_processed"`
	SuccessCount   int                          `json:"success_count"`
	FailureCount   int                          `json:"failure_count"`
	ReplayedItems  []WebhookDLQReplayItemResult `json:"replayed_items"`
}

func (s *Server) handleReplayWebhookDLQ(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.webhookDLQRepo == nil {
		return mcp.NewToolResultError("webhook DLQ repository is not configured on this MCP server"), nil
	}

	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil || strings.TrimSpace(wsIDStr) == "" {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	var ws *repository.Workspace
	if s.wsRepo != nil {
		ws, err = s.wsRepo.GetByID(ctx, workspaceID)
		if err != nil || ws == nil {
			return mcp.NewToolResultError("workspace not found"), nil
		}
	}

	action := strings.ToLower(strings.TrimSpace(request.GetString("action", "re-enqueue")))
	if action == "" {
		action = "re-enqueue"
	}
	if action != "re-enqueue" && action != "dispatch_immediate" {
		return mcp.NewToolResultError("invalid action: must be 're-enqueue' or 'dispatch_immediate'"), nil
	}

	if action == "re-enqueue" && s.publisher == nil {
		return mcp.NewToolResultError("JetStream publisher is not configured on this MCP server for re-enqueueing"), nil
	}

	limit := int(request.GetInt("limit", 10))
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	targetURL := strings.TrimSpace(request.GetString("target_url", ""))
	if targetURL != "" {
		u, err := url.ParseRequestURI(targetURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return mcp.NewToolResultError(fmt.Sprintf("invalid target_url %q: scheme must be http or https", targetURL)), nil
		}
	}

	var filterSubID uuid.UUID
	subIDStr := strings.TrimSpace(request.GetString("subscription_id", ""))
	if subIDStr != "" {
		subID, err := uuid.Parse(subIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid subscription_id UUID: %v", err)), nil
		}
		if s.webhookSubRepo != nil {
			sub, err := s.webhookSubRepo.Get(ctx, subID)
			if err != nil || sub == nil || sub.WorkspaceID != workspaceID {
				return mcp.NewToolResultError("webhook subscription not found for workspace"), nil
			}
		}
		filterSubID = subID
	}

	var items []*repository.WebhookDLQ
	dlqIDStr := strings.TrimSpace(request.GetString("dlq_id", ""))
	if dlqIDStr != "" {
		dlqID, err := uuid.Parse(dlqIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid dlq_id UUID: %v", err)), nil
		}
		item, err := s.webhookDLQRepo.GetDLQByID(ctx, dlqID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("DLQ item not found: %v", err)), nil
		}
		if item.WorkspaceID != workspaceID {
			return mcp.NewToolResultError("DLQ item does not belong to the specified workspace"), nil
		}
		if filterSubID != uuid.Nil && item.SubscriptionID != filterSubID {
			items = []*repository.WebhookDLQ{}
		} else {
			items = []*repository.WebhookDLQ{item}
		}
	} else {
		rawItems, err := s.webhookDLQRepo.ListDLQ(ctx, workspaceID, limit, 0)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list DLQ items: %v", err)), nil
		}
		if filterSubID != uuid.Nil {
			for _, it := range rawItems {
				if it.SubscriptionID == filterSubID {
					items = append(items, it)
				}
			}
		} else {
			items = rawItems
		}
	}

	res := WebhookDLQReplayResult{
		WorkspaceID:    workspaceID,
		Action:         action,
		TotalProcessed: len(items),
		ReplayedItems:  make([]WebhookDLQReplayItemResult, 0, len(items)),
	}

	for _, item := range items {
		itemResult := WebhookDLQReplayItemResult{
			DLQID:          item.ID,
			SubscriptionID: item.SubscriptionID,
			EventType:      item.EventType,
			TraceID:        item.TraceID,
			MessageID:      item.MessageID,
		}

		if action == "re-enqueue" {
			subject := fmt.Sprintf("webhooks.deliveries.%s.%s", item.WorkspaceID, item.SubscriptionID)
			itemResult.Subject = subject

			task := webhook.WebhookDeliveryTask{
				ID:             uuid.New(),
				SubscriptionID: item.SubscriptionID,
				WorkspaceID:    item.WorkspaceID,
				Event:          item.EventType,
				TraceID:        item.TraceID,
				MessageID:      item.MessageID,
				Payload:        item.Payload,
				Mode:           "outbound",
			}

			taskPayload, err := json.Marshal(task)
			if err != nil {
				itemResult.Success = false
				itemResult.Error = fmt.Sprintf("failed to marshal delivery task: %v", err)
				res.FailureCount++
				res.ReplayedItems = append(res.ReplayedItems, itemResult)
				continue
			}

			start := time.Now()
			pubErr := s.publisher.Publish(ctx, subject, taskPayload, task.ID.String())
			itemResult.LatencyMS = time.Since(start).Milliseconds()

			if pubErr != nil {
				itemResult.Success = false
				itemResult.Error = fmt.Sprintf("failed to publish to JetStream: %v", pubErr)
				res.FailureCount++
			} else {
				itemResult.Success = true
				res.SuccessCount++
				if delErr := s.webhookDLQRepo.DeleteDLQ(ctx, item.ID); delErr != nil {
					itemResult.DeletedFromDLQ = false
				} else {
					itemResult.DeletedFromDLQ = true
				}
			}
			res.ReplayedItems = append(res.ReplayedItems, itemResult)
		} else { // dispatch_immediate
			destURL := targetURL
			if destURL == "" {
				destURL = item.WebhookURL
			}
			itemResult.TargetURL = destURL

			if destURL == "" {
				itemResult.Success = false
				itemResult.Error = "no destination webhook URL configured for item"
				res.FailureCount++
				res.ReplayedItems = append(res.ReplayedItems, itemResult)
				continue
			}

			u, err := url.ParseRequestURI(destURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				itemResult.Success = false
				itemResult.Error = fmt.Sprintf("invalid destination URL %q: scheme must be http or https", destURL)
				res.FailureCount++
				res.ReplayedItems = append(res.ReplayedItems, itemResult)
				continue
			}

			var secret []byte
			if s.webhookSubRepo != nil && item.SubscriptionID != uuid.Nil {
				if sub, err := s.webhookSubRepo.Get(ctx, item.SubscriptionID); err == nil && sub != nil && len(sub.Secret) > 0 {
					secret = sub.Secret
				}
			}
			if len(secret) == 0 && ws != nil && ws.WebhookSecret != nil && *ws.WebhookSecret != "" {
				secret = []byte(*ws.WebhookSecret)
			}

			var signature string
			if len(secret) > 0 {
				timestamp := fmt.Sprintf("%d", time.Now().Unix())
				signature = webhook.SignPayload(item.Payload, secret, timestamp)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, destURL, bytes.NewReader(item.Payload))
			if err != nil {
				itemResult.Success = false
				itemResult.Error = fmt.Sprintf("failed to create HTTP request: %v", err)
				res.FailureCount++
				res.ReplayedItems = append(res.ReplayedItems, itemResult)
				continue
			}

			req.Header.Set("Content-Type", "application/json")
			if signature != "" {
				req.Header.Set("X-PerGo-Signature", signature)
			}
			req.Header.Set("X-PerGo-Replayed", "true")
			req.Header.Set("X-PerGo-Event", item.EventType)
			req.Header.Set("X-Trace-ID", item.TraceID)
			if item.MessageID != "" {
				req.Header.Set("X-Message-ID", item.MessageID)
			}

			client := s.safeClient
			if client == nil {
				client = netpolicy.NewSafeClient(netpolicy.WithTimeout(10 * time.Second))
			}

			start := time.Now()
			resp, reqErr := client.Do(req)
			itemResult.LatencyMS = time.Since(start).Milliseconds()

			if reqErr != nil {
				itemResult.Success = false
				itemResult.Error = fmt.Sprintf("dispatch error: %v", reqErr)
				res.FailureCount++
			} else {
				itemResult.StatusCode = resp.StatusCode
				itemResult.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
				_ = resp.Body.Close()
				if itemResult.Success {
					res.SuccessCount++
				} else {
					itemResult.Error = fmt.Sprintf("HTTP %s", resp.Status)
					res.FailureCount++
				}
			}
			res.ReplayedItems = append(res.ReplayedItems, itemResult)
		}
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal replay result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}
