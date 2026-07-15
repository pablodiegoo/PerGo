package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockActionLogInserter struct {
	ch chan *repository.UserActionLog
}

func (m *mockActionLogInserter) Insert(ctx context.Context, log *repository.UserActionLog) error {
	m.ch <- log
	return nil
}

func TestAuditMiddleware(t *testing.T) {
	e := echo.New()

	h := func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	inserter := &mockActionLogInserter{
		ch: make(chan *repository.UserActionLog, 10),
	}

	e.POST("/api/v1/messages", h, AuditMiddleware(inserter))
	e.GET("/healthz", h, AuditMiddleware(inserter))

	t.Run("bypass route does not audit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}

		select {
		case log := <-inserter.ch:
			t.Errorf("unexpected log recorded: %+v", log)
		case <-time.After(50 * time.Millisecond):
			// Success, no log recorded
		}
	})

	t.Run("unauthenticated API request does not audit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(`{"to":"+5511999999999"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		select {
		case log := <-inserter.ch:
			t.Errorf("unexpected log recorded for unauthenticated request: %+v", log)
		case <-time.After(50 * time.Millisecond):
			// Success
		}
	})

	t.Run("authenticated API request records audit log asynchronously", func(t *testing.T) {
		body := `{"to":"+5511999999999","channel":"whatsapp","body":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "TestAgent")
		rec := httptest.NewRecorder()

		// Simulate AuthMiddleware injection
		wsID := uuid.New()
		apiKey := &repository.APIKey{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			Name:        "Test Key",
		}

		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/messages")
		c.Set("api_key", apiKey)

		// Wrap context with workspace ID
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		c.SetRequest(req.WithContext(ctx))

		// Execute middleware directly
		mw := AuditMiddleware(inserter)
		handlerCalled := false
		err := mw(func(ec *echo.Context) error {
			handlerCalled = true
			return ec.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})(c)

		if err != nil {
			t.Fatalf("middleware execution failed: %v", err)
		}
		if !handlerCalled {
			t.Fatal("handler was not called")
		}

		select {
		case log := <-inserter.ch:
			if log.WorkspaceID != wsID {
				t.Errorf("expected workspace ID %s, got %s", wsID, log.WorkspaceID)
			}
			if log.ActorType != "api_key" {
				t.Errorf("expected actor type 'api_key', got '%s'", log.ActorType)
			}
			if log.ActorID != apiKey.ID.String() {
				t.Errorf("expected actor ID %s, got '%s'", apiKey.ID, log.ActorID)
			}
			if log.ActorName != "Test Key" {
				t.Errorf("expected actor name 'Test Key', got '%s'", log.ActorName)
			}
			if log.Action != "message.send" {
				t.Errorf("expected action 'message.send', got '%s'", log.Action)
			}
			if log.Source != "api" {
				t.Errorf("expected source 'api', got '%s'", log.Source)
			}
			if log.UserAgent == nil || *log.UserAgent != "TestAgent" {
				t.Errorf("expected user agent 'TestAgent', got '%v'", log.UserAgent)
			}
			// Verify metadata
			var meta map[string]any
			if err := json.Unmarshal(log.Metadata, &meta); err != nil {
				t.Fatalf("failed to unmarshal metadata JSON: %v", err)
			}
			if meta["to"] != "+5511999999999" || meta["channel"] != "whatsapp" {
				t.Errorf("unexpected metadata payload: %+v", meta)
			}

		case <-time.After(200 * time.Millisecond):
			t.Fatal("timeout waiting for asynchronous audit log insertion")
		}
	})
}
