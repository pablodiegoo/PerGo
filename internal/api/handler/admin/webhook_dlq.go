package admin

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
	"github.com/pablojhp.pergo/templates/pages"
)

type WebhookDLQHandler struct {
	Repo       *repository.WebhookDLQRepository
	Subs       *repository.WebhookSubscriptionRepository
	Workspaces *repository.WorkspaceRepository
	Publisher  *queue.JetStreamPublisher
}

func NewWebhookDLQHandler(
	repo *repository.WebhookDLQRepository,
	subs *repository.WebhookSubscriptionRepository,
	workspaces *repository.WorkspaceRepository,
	publisher *queue.JetStreamPublisher,
) *WebhookDLQHandler {
	return &WebhookDLQHandler{
		Repo:       repo,
		Subs:       subs,
		Workspaces: workspaces,
		Publisher:  publisher,
	}
}

func generateRandomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Page renders the webhooks config page for a workspace.
func (h *WebhookDLQHandler) Page(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Workspaces.GetByID(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	subs, err := h.Subs.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list subscriptions")
	}

	dlqItems, err := h.Repo.ListDLQ(c.Request().Context(), workspaceID, 100, 0)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list DLQ items")
	}

	page := pages.WorkspaceWebhooksPage(*ws, subs, dlqItems)
	return mw.Render(c, http.StatusOK, page)
}

// GetSubscriptionNewForm returns the new subscription modal form.
func (h *WebhookDLQHandler) GetSubscriptionNewForm(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	return mw.Render(c, http.StatusOK, pages.SubscriptionFormModal(workspaceID, nil))
}

// GetSubscriptionEditForm returns the edit subscription modal form.
func (h *WebhookDLQHandler) GetSubscriptionEditForm(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	sub, err := h.Subs.Get(c.Request().Context(), subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	return mw.Render(c, http.StatusOK, pages.SubscriptionFormModal(workspaceID, sub))
}

// CreateSubscription creates a new webhook subscription and reveals its signing secret.
func (h *WebhookDLQHandler) CreateSubscription(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	urlStr := strings.TrimSpace(c.FormValue("url"))
	if urlStr == "" {
		return c.String(http.StatusBadRequest, "URL is required")
	}

	// SSRF validation
	if err := netpolicy.ValidateURL(urlStr); err != nil {
		if errors.Is(err, netpolicy.ErrRestrictedIP) {
			return c.String(http.StatusUnprocessableEntity, "destination URL blocked by SSRF netpolicy (private/loopback IPs not allowed)")
		}
		return c.String(http.StatusBadRequest, fmt.Sprintf("invalid webhook URL: %v", err))
	}

	secretStr := strings.TrimSpace(c.FormValue("secret"))
	if secretStr == "" {
		randomSecret, err := generateRandomSecret()
		if err != nil {
			return c.String(http.StatusInternalServerError, "failed to generate signing secret")
		}
		secretStr = randomSecret
	}

	eventTypes := c.Request().Form["event_types"]
	if len(eventTypes) == 0 {
		eventTypes = []string{"*"}
	}

	sub, err := h.Subs.Create(c.Request().Context(), workspaceID, urlStr, eventTypes, []byte(secretStr))
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create subscription")
	}

	return mw.Render(c, http.StatusOK, pages.WebhookSecretRevealedModal(workspaceID, sub.URL, secretStr, false))
}

// UpdateSubscription updates an existing webhook subscription.
func (h *WebhookDLQHandler) UpdateSubscription(c *echo.Context) error {
	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	urlStr := strings.TrimSpace(c.FormValue("url"))
	if urlStr == "" {
		return c.String(http.StatusBadRequest, "URL is required")
	}

	// SSRF validation
	if err := netpolicy.ValidateURL(urlStr); err != nil {
		if errors.Is(err, netpolicy.ErrRestrictedIP) {
			return c.String(http.StatusUnprocessableEntity, "destination URL blocked by SSRF netpolicy (private/loopback IPs not allowed)")
		}
		return c.String(http.StatusBadRequest, fmt.Sprintf("invalid webhook URL: %v", err))
	}

	secret := c.FormValue("secret")
	active := c.FormValue("active") == "true"
	eventTypes := c.Request().Form["event_types"]
	if len(eventTypes) == 0 {
		eventTypes = []string{"*"}
	}

	var secretBytes []byte
	if secret != "********" && strings.TrimSpace(secret) != "" {
		secretBytes = []byte(secret)
	}

	err = h.Subs.Update(c.Request().Context(), subscriptionID, urlStr, eventTypes, active, secretBytes)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to update subscription")
	}

	c.Response().Header().Set("HX-Refresh", "true")
	return c.NoContent(http.StatusOK)
}

// DeleteSubscription deletes a webhook subscription.
func (h *WebhookDLQHandler) DeleteSubscription(c *echo.Context) error {
	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	err = h.Subs.Delete(c.Request().Context(), subscriptionID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete subscription")
	}

	return c.NoContent(http.StatusOK)
}

// GetRotateSecretForm renders the modal to rotate a webhook subscription secret.
func (h *WebhookDLQHandler) GetRotateSecretForm(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	sub, err := h.Subs.Get(c.Request().Context(), subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	return mw.Render(c, http.StatusOK, pages.RotateSecretModal(workspaceID, sub))
}

// RotateSubscriptionSecret generates or sets a new signing secret for a webhook subscription.
func (h *WebhookDLQHandler) RotateSubscriptionSecret(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	ctx := c.Request().Context()
	sub, err := h.Subs.Get(ctx, subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	newSecret := strings.TrimSpace(c.FormValue("secret"))
	if newSecret == "" {
		randomSecret, err := generateRandomSecret()
		if err != nil {
			return c.String(http.StatusInternalServerError, "failed to generate signing secret")
		}
		newSecret = randomSecret
	}

	err = h.Subs.Update(ctx, subscriptionID, sub.URL, sub.EventTypes, sub.Active, []byte(newSecret))
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to rotate signing secret")
	}

	return mw.Render(c, http.StatusOK, pages.WebhookSecretRevealedModal(workspaceID, sub.URL, newSecret, true))
}

// PingSubscription dispatches a simulated ping payload to verify endpoint connectivity.
func (h *WebhookDLQHandler) PingSubscription(c *echo.Context) error {
	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	ctx := c.Request().Context()
	sub, err := h.Subs.Get(ctx, subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	pingPayload, err := json.Marshal(map[string]any{
		"event":           "ping",
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
		"workspace_id":    sub.WorkspaceID.String(),
		"subscription_id": sub.ID.String(),
		"message":         "PerGo Webhook Ping Test",
	})
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.PingStatusBadge(0, 0, "failed to serialize ping payload"))
	}

	var signature string
	if len(sub.Secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature = webhook.SignPayload(pingPayload, sub.Secret, timestamp)
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(pingPayload))
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.PingStatusBadge(0, 0, fmt.Sprintf("invalid request: %v", err)))
	}

	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-PerGo-Signature", signature)
	}
	req.Header.Set("X-Trace-ID", "ping-"+uuid.New().String()[:8])
	req.Header.Set("X-PerGo-Simulated", "true")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return mw.Render(c, http.StatusOK, pages.PingStatusBadge(0, latency, fmt.Sprintf("connection error: %v", err)))
	}
	defer resp.Body.Close()

	var errMsg string
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}
	}

	return mw.Render(c, http.StatusOK, pages.PingStatusBadge(resp.StatusCode, latency, errMsg))
}

// GetSubscriptionTestForm returns the simulation modal form.
func (h *WebhookDLQHandler) GetSubscriptionTestForm(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	sub, err := h.Subs.Get(c.Request().Context(), subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	return mw.Render(c, http.StatusOK, pages.TestWebhookModal(workspaceID, sub))
}

// TestSubscription runs a synchronous simulated webhook POST request.
func (h *WebhookDLQHandler) TestSubscription(c *echo.Context) error {
	subIDStr, err := echo.PathParam[string](c, "subscription_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}
	subscriptionID, err := uuid.Parse(subIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid subscription ID")
	}

	ctx := c.Request().Context()
	sub, err := h.Subs.Get(ctx, subscriptionID)
	if err != nil {
		return c.String(http.StatusNotFound, "subscription not found")
	}

	payloadStr := c.FormValue("payload")
	if payloadStr == "" {
		return c.String(http.StatusBadRequest, "payload is required")
	}

	var signature string
	if len(sub.Secret) > 0 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		signature = webhook.SignPayload([]byte(payloadStr), sub.Secret, timestamp)
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader([]byte(payloadStr)))
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("failed to construct request: %v", err))
	}

	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-PerGo-Signature", signature)
	}
	req.Header.Set("X-Trace-ID", "simulated-"+uuid.New().String()[:8])
	req.Header.Set("X-PerGo-Simulated", "true")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	latency := time.Since(start).Milliseconds()

	if err != nil {
		return mw.Render(c, http.StatusOK, pages.TestResultFragment(
			http.StatusGatewayTimeout,
			latency,
			signature,
			fmt.Sprintf("HTTP Request Failed: %v", err),
			nil,
		))
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if len(bodyStr) > 1000 {
		bodyStr = bodyStr[:1000] + "... (truncated)"
	}

	return mw.Render(c, http.StatusOK, pages.TestResultFragment(
		resp.StatusCode,
		latency,
		signature,
		bodyStr,
		resp.Header,
	))
}

// GlobalPage renders the global webhooks and DLQ page for the sidebar.
func (h *WebhookDLQHandler) GlobalPage(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaces, err := h.Workspaces.List(ctx, 100)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list workspaces")
	}

	dlqItems, err := h.Repo.ListAllDLQ(ctx, 100, 0)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to fetch dead-lettered logs")
	}

	page := pages.WebhooksPage(workspaces, dlqItems)
	return mw.Render(c, http.StatusOK, page)
}

// GetBadgeCount returns the badge count fragment for the sidebar.
func (h *WebhookDLQHandler) GetBadgeCount(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaces, err := h.Workspaces.List(ctx, 100)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list workspaces")
	}

	total := 0
	for _, ws := range workspaces {
		count, err := h.Repo.GetDLQBadgeCount(ctx, ws.ID)
		if err == nil {
			total += count
		}
	}

	return mw.Render(c, http.StatusOK, pages.DLQBadgeFragment(total))
}

// GetDetails renders the details modal for a DLQ item.
func (h *WebhookDLQHandler) GetDetails(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	item, err := h.Repo.GetDLQByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "DLQ log not found")
	}

	return mw.Render(c, http.StatusOK, pages.DLQDetailModal(item))
}

// DeleteDLQ deletes a specific DLQ item.
func (h *WebhookDLQHandler) DeleteDLQ(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	err = h.Repo.DeleteDLQ(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete DLQ log")
	}

	return c.NoContent(http.StatusOK)
}

// RetryDLQ triggers manual retry by re-enqueueing the delivery task to NATS.
func (h *WebhookDLQHandler) RetryDLQ(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	ctx := c.Request().Context()
	item, err := h.Repo.GetDLQByID(ctx, id)
	if err != nil {
		return c.String(http.StatusNotFound, "DLQ log not found")
	}

	// Publish directly back to NATS WEBHOOK_DELIVERIES workqueue subject:
	// webhooks.deliveries.<workspace_id>.<subscription_id>
	task := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: item.SubscriptionID,
		WorkspaceID:    item.WorkspaceID,
		Event:          item.EventType,
		TraceID:        item.TraceID,
		MessageID:      item.MessageID,
		Payload:        item.Payload,
		Mode:           "outbound", // Retried DLQs are outbound deliveries
	}

	payload, err := json.Marshal(task)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to marshal retry payload")
	}

	subject := fmt.Sprintf("webhooks.deliveries.%s.%s", item.WorkspaceID, item.SubscriptionID)
	err = h.Publisher.Publish(ctx, subject, payload, task.ID.String())
	if err != nil {
		slog.Error("admin: failed to publish retry event to NATS", "error", err, "dlq_id", id, "subject", subject)
		return c.String(http.StatusInternalServerError, "failed to retry webhook delivery: please check your connection and try again.")
	}

	// Delete from DLQ table
	err = h.Repo.DeleteDLQ(ctx, id)
	if err != nil {
		slog.Error("admin: failed to delete retried DLQ log from DB", "error", err, "dlq_id", id)
	}

	return c.HTML(http.StatusOK, `<td colspan="8" class="success-icon" style="color: var(--color-success); text-align: center;">✓ Re-enqueued for delivery</td>`)
}
