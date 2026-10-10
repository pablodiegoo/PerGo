package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/presence"
)

func (h *InboxHandler) ensurePresence() {
	if h.Presence == nil {
		h.Presence = presence.NewTracker(15 * time.Second)
	}
}

// PresenceStream handles GET /admin/inbox/presence — SSE stream delivering real-time viewer and collision updates.
func (h *InboxHandler) PresenceStream(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	chatID := strings.TrimSpace(c.QueryParam("chat_id"))
	if chatID == "" {
		chatID = strings.TrimSpace(c.QueryParam("contact_id"))
	}
	if chatID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "chat_id or contact_id is required",
		})
	}

	h.ensurePresence()

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	flusher, _ := c.Response().(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	// Send initial presence state
	initial := h.Presence.GetActiveViewers(workspaceID, chatID)
	initData, _ := json.Marshal(initial)
	if _, err := fmt.Fprintf(c.Response(), "event: presence\ndata: %s\n\n", initData); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}

	ch, unsub := h.Presence.Subscribe(ctx, workspaceID, chatID)
	defer unsub()

	pingTicker := time.NewTicker(10 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pingTicker.C:
			if _, err := fmt.Fprintf(c.Response(), ": ping\n\n"); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		case viewers, ok := <-ch:
			if !ok {
				return nil
			}
			data, err := json.Marshal(viewers)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Response(), "event: presence\ndata: %s\n\n", data); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// PresenceHeartbeat handles POST /admin/inbox/presence — records active viewing or typing presence.
func (h *InboxHandler) PresenceHeartbeat(c *echo.Context) error {
	ctx := c.Request().Context()
	var workspaceID uuid.UUID
	if scope, sErr := domain.Require(ctx); sErr == nil && scope.WorkspaceID() != uuid.Nil {
		workspaceID = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		workspaceID = id
	}

	chatID := strings.TrimSpace(c.FormValue("chat_id"))
	if chatID == "" {
		chatID = strings.TrimSpace(c.QueryParam("chat_id"))
	}
	if chatID == "" {
		chatID = strings.TrimSpace(c.FormValue("contact_id"))
	}
	if chatID == "" {
		chatID = strings.TrimSpace(c.QueryParam("contact_id"))
	}
	if chatID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "chat_id or contact_id is required",
		})
	}

	status := strings.TrimSpace(c.FormValue("status"))
	if status == "" {
		status = strings.TrimSpace(c.QueryParam("status"))
	}
	if status != "typing" {
		status = "viewing"
	}

	// Extract or infer operator identity
	userID := strings.TrimSpace(c.FormValue("user_id"))
	email := strings.TrimSpace(c.FormValue("email"))
	name := strings.TrimSpace(c.FormValue("name"))

	if claimsVal := c.Get("claims"); claimsVal != nil {
		if claims, ok := claimsVal.(*SSOClaims); ok && claims != nil {
			if userID == "" {
				userID = claims.Sub
			}
			if email == "" {
				email = claims.Email
			}
			if name == "" {
				name = claims.Sub
			}
		}
	}

	if userID == "" {
		if email != "" {
			userID = email
		} else {
			userID = "operator"
		}
	}
	if name == "" {
		name = userID
	}
	if email == "" {
		email = userID + "@agora.io"
	}

	h.ensurePresence()
	active := h.Presence.Heartbeat(workspaceID, chatID, presence.Viewer{
		UserID: userID,
		Name:   name,
		Email:  email,
		Status: status,
	})

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"viewers": active,
	})
}
