package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	apipkg "github.com/pablojhp.pergo/internal/api/handler/api"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockWorkspaceRepo struct {
	createFunc         func(ctx context.Context, name string) (*repository.Workspace, error)
	generateSecretFunc func(ctx context.Context, id uuid.UUID) (string, error)
}

func (m *mockWorkspaceRepo) Create(ctx context.Context, name string) (*repository.Workspace, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, name)
	}
	return &repository.Workspace{
		ID:        uuid.New(),
		Name:      name,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (m *mockWorkspaceRepo) GenerateWebhookSecret(ctx context.Context, id uuid.UUID) (string, error) {
	if m.generateSecretFunc != nil {
		return m.generateSecretFunc(ctx, id)
	}
	return "whsec_mocked1234567890abcdef1234567890abcdef", nil
}

type mockAPIKeyRepo struct {
	createFunc func(ctx context.Context, workspaceID uuid.UUID, name string) (*repository.APIKey, string, error)
}

func (m *mockAPIKeyRepo) Create(ctx context.Context, workspaceID uuid.UUID, name string) (*repository.APIKey, string, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, workspaceID, name)
	}
	return &repository.APIKey{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        name,
		CreatedAt:   time.Now().UTC(),
	}, "pgo_live_mocked1234567890abcdef", nil
}

func TestWorkspaceAPIHandler_Create_Success(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	body := `{"name": "Client Acme Inc."}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Create(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var res apipkg.CreateWorkspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.ID == uuid.Nil {
		t.Errorf("expected non-nil ID")
	}
	if res.Name != "Client Acme Inc." {
		t.Errorf("expected name 'Client Acme Inc.', got %q", res.Name)
	}
	if res.APIKey == nil || *res.APIKey != "pgo_live_mocked1234567890abcdef" {
		t.Errorf("expected api_key 'pgo_live_mocked1234567890abcdef', got %v", res.APIKey)
	}
	if res.WebhookSecret == nil || *res.WebhookSecret != "whsec_mocked1234567890abcdef1234567890abcdef" {
		t.Errorf("expected webhook_secret 'whsec_mocked1234567890abcdef1234567890abcdef', got %v", res.WebhookSecret)
	}
}

func TestWorkspaceAPIHandler_Create_CustomFlags(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	// Explicit false for api_key and webhook_secret
	body := `{"name": "No Keys Inc", "generate_api_key": false, "generate_webhook_secret": false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Create(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}

	var res apipkg.CreateWorkspaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.APIKey != nil {
		t.Errorf("expected nil api_key, got %v", *res.APIKey)
	}
	if res.WebhookSecret != nil {
		t.Errorf("expected nil webhook_secret, got %v", *res.WebhookSecret)
	}
}

func TestWorkspaceAPIHandler_Create_Validation(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "empty name",
			body:       `{"name": "   "}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "missing name field",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "malformed json",
			body:       `{"name": "bad"`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			_ = h.Create(c)
			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var errRes map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &errRes); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}
			if errRes["code"] != tt.wantCode {
				t.Errorf("expected code %q, got %q", tt.wantCode, errRes["code"])
			}
		})
	}
}

func TestWorkspaceAPIHandler_Create_Errors(t *testing.T) {
	e := echo.New()

	t.Run("wsRepo error", func(t *testing.T) {
		wsRepo := &mockWorkspaceRepo{
			createFunc: func(ctx context.Context, name string) (*repository.Workspace, error) {
				return nil, errors.New("db error")
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		_ = h.Create(c)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("apiKeyRepo error", func(t *testing.T) {
		apiKeyRepo := &mockAPIKeyRepo{
			createFunc: func(ctx context.Context, workspaceID uuid.UUID, name string) (*repository.APIKey, string, error) {
				return nil, "", errors.New("key gen error")
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(&mockWorkspaceRepo{}, apiKeyRepo)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		_ = h.Create(c)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("webhook secret gen error", func(t *testing.T) {
		wsRepo := &mockWorkspaceRepo{
			generateSecretFunc: func(ctx context.Context, id uuid.UUID) (string, error) {
				return "", errors.New("secret gen error")
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		_ = h.Create(c)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})
}

func TestWorkspaceAPIHandler_MasterAuth_Integration(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	masterKey := "super-secret-master-key-12345"
	h.RegisterRoutes(e, middleware.MasterAuthMiddleware(masterKey, ""))

	t.Run("authorized via Bearer master key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("authorized via X-Master-Key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Master-Key", masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthorized with invalid master key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer wrong-key")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("unauthorized with missing master key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader([]byte(`{"name": "Acme"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}
