package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/repository"
)

// ActionLogInserter defines the database persistence operation needed for auditing.
type ActionLogInserter interface {
	Insert(ctx context.Context, log *repository.UserActionLog) error
}

// AuditMiddleware returns an Echo middleware that automatically audits public API key actions.
func AuditMiddleware(repo ActionLogInserter) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			path := c.Request().URL.Path

			// Bypass check
			if path == "/" || path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/static") {
				return next(c)
			}

			// Read request body for JSON endpoints (e.g. POST /api/v1/messages)
			var bodyBytes []byte
			if c.Request().Body != nil && (c.Request().Method == http.MethodPost || c.Request().Method == http.MethodPut || c.Request().Method == http.MethodPatch) {
				if strings.Contains(c.Request().Header.Get("Content-Type"), "application/json") {
					var err error
					bodyBytes, err = io.ReadAll(c.Request().Body)
					if err == nil {
						c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
					}
				}
			}

			// Process request first to ensure the operation succeeded/completed
			err := next(c)

			// Get authenticated key from context
			val := c.Get("api_key")
			if apiKey, ok := val.(*repository.APIKey); ok {
				ip := c.RealIP()
				ua := c.Request().UserAgent()

				// Determine action name
				action := determineAction(c.Request().Method, path)

				// Determine metadata
				var metadataBytes []byte
				if len(bodyBytes) > 0 {
					var js map[string]any
					if json.Unmarshal(bodyBytes, &js) == nil {
						metadataBytes = bodyBytes
					}
				}
				if len(metadataBytes) == 0 {
					m := map[string]any{
						"method": c.Request().Method,
						"path":   path,
					}
					metadataBytes, _ = json.Marshal(m)
				}

				// Run insertion asynchronously
				logEntry := &repository.UserActionLog{
					WorkspaceID: apiKey.WorkspaceID,
					ActorType:   "api_key",
					ActorID:     apiKey.ID.String(),
					ActorName:   apiKey.Name,
					Action:      action,
					Source:      "api",
					IPAddress:   &ip,
					UserAgent:   &ua,
					Metadata:    metadataBytes,
				}

				go func(bgCtx context.Context, l *repository.UserActionLog) {
					if insertErr := repo.Insert(bgCtx, l); insertErr != nil {
						slog.Error("failed to asynchronously insert audit log", "error", insertErr, "action", l.Action)
					}
				}(context.Background(), logEntry)
			}

			return err
		}
	}
}

func determineAction(method, path string) string {
	if strings.HasSuffix(path, "/messages") && method == http.MethodPost {
		return "message.send"
	}
	if strings.HasSuffix(path, "/me") && method == http.MethodGet {
		return "me.view"
	}

	// generic fallback resource.action
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 { // e.g. api/v1/messages
		resource := parts[2]
		action := "view"
		switch method {
		case http.MethodPost:
			action = "create"
		case http.MethodPut, http.MethodPatch:
			action = "update"
		case http.MethodDelete:
			action = "delete"
		}
		return resource + "." + action
	}
	return "api.request"
}
