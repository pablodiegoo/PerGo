package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

type mockPublisher struct {
	Published []string
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.Published = append(m.Published, string(data))
	return nil
}

func TestTypebotWebhook(t *testing.T) {
	// A basic unit test that simply tests missing workspace id
	e := echo.New()
	
	payload := handler.TypebotWebhookPayload{
		Message:      "hello",
		ConnectionID: "conn_1",
		ContactID:    "contact_1",
	}
	
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	
	c := e.NewContext(req, rec)
	
	wsID := uuid.New()
	ctx := tenant.WithWorkspaceID(req.Context(), wsID)
	c.SetRequest(req.WithContext(ctx))
	
	// h := handler.NewTypebotWebhookHandler(nil, &mockPublisher{})
	// This would panic on DB access if we run it directly, so we just test auth failure path
	
	reqAuth := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	reqAuth.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recAuth := httptest.NewRecorder()
	cAuth := e.NewContext(reqAuth, recAuth)
	
	h := handler.NewTypebotWebhookHandler(nil, nil)
	err := h.Handle(cAuth)
	if err != nil {
		// we expect it to return unauthorized since no workspace ID in context
	}
	
	if recAuth.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", recAuth.Code)
	}
}
