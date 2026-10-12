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
	createFunc                func(ctx context.Context, name string) (*repository.Workspace, error)
	createWithIDFunc          func(ctx context.Context, id uuid.UUID, name string) (*repository.Workspace, error)
	generateSecretFunc        func(ctx context.Context, id uuid.UUID) (string, error)
	setFlowURLFunc            func(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error
	setMediaRetentionDaysFunc func(ctx context.Context, id uuid.UUID, days int) error
	listFunc                  func(ctx context.Context, limit int) ([]repository.Workspace, error)
	addMemberFunc             func(ctx context.Context, member *repository.WorkspaceMember) error
	removeMemberByEmailFunc   func(ctx context.Context, workspaceID uuid.UUID, email string) error
}

func (m *mockWorkspaceRepo) SetMediaRetentionDays(ctx context.Context, id uuid.UUID, days int) error {
	if m.setMediaRetentionDaysFunc != nil {
		return m.setMediaRetentionDaysFunc(ctx, id, days)
	}
	return nil
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

func (m *mockWorkspaceRepo) CreateWithID(ctx context.Context, id uuid.UUID, name string) (*repository.Workspace, error) {
	if m.createWithIDFunc != nil {
		return m.createWithIDFunc(ctx, id, name)
	}
	return &repository.Workspace{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (m *mockWorkspaceRepo) AddMember(ctx context.Context, member *repository.WorkspaceMember) error {
	if m.addMemberFunc != nil {
		return m.addMemberFunc(ctx, member)
	}
	if member.ID == uuid.Nil {
		member.ID = uuid.New()
	}
	member.CreatedAt = time.Now().UTC()
	member.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *mockWorkspaceRepo) RemoveMemberByEmail(ctx context.Context, workspaceID uuid.UUID, email string) error {
	if m.removeMemberByEmailFunc != nil {
		return m.removeMemberByEmailFunc(ctx, workspaceID, email)
	}
	return nil
}


func (m *mockWorkspaceRepo) GenerateWebhookSecret(ctx context.Context, id uuid.UUID) (string, error) {
	if m.generateSecretFunc != nil {
		return m.generateSecretFunc(ctx, id)
	}
	return "mock_whsec_placeholder_secret", nil
}

func (m *mockWorkspaceRepo) SetFlowWebhookURL(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error {
	if m.setFlowURLFunc != nil {
		return m.setFlowURLFunc(ctx, id, flowWebhookURL)
	}
	return nil
}

func (m *mockWorkspaceRepo) List(ctx context.Context, limit int) ([]repository.Workspace, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, limit)
	}
	return []repository.Workspace{
		{
			ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Name:      "Workspace 1",
			PIIOptIn:  false,
			CreatedAt: time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Name:      "Workspace 2",
			PIIOptIn:  true,
			CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		},
	}, nil
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
	if res.WebhookSecret == nil || *res.WebhookSecret != "mock_whsec_placeholder_secret" {
		t.Errorf("expected webhook_secret 'mock_whsec_placeholder_secret', got %v", res.WebhookSecret)
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

func TestWorkspaceAPIHandler_Create_WithFlowWebhookURL(t *testing.T) {
	e := echo.New()
	var setCalled bool
	var setURL string
	wsRepo := &mockWorkspaceRepo{
		setFlowURLFunc: func(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error {
			setCalled = true
			if flowWebhookURL != nil {
				setURL = *flowWebhookURL
			}
			return nil
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	flowURL := "https://crm.partner.io/flows/webhook"
	body := `{"name": "Flow Tenant", "flow_webhook_url": "` + flowURL + `"}`
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

	if !setCalled {
		t.Errorf("expected SetFlowWebhookURL to be called")
	}
	if setURL != flowURL {
		t.Errorf("expected URL %q, got %q", flowURL, setURL)
	}
	if res.FlowWebhookURL == nil || *res.FlowWebhookURL != flowURL {
		t.Errorf("expected response FlowWebhookURL %q, got %v", flowURL, res.FlowWebhookURL)
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

	t.Run("GET list authorized via Bearer master key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET list authorized via X-Master-Key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
		req.Header.Set("X-Master-Key", masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET list unauthorized with missing master key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	wsID := uuid.New()
	t.Run("POST /api/v1/master/workspaces/:id/members authorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+wsID.String()+"/members", bytes.NewReader([]byte(`{"name":"Alice","email":"alice@example.com","role":"admin"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/master/workspaces/:id/members/:email authorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/master/workspaces/"+wsID.String()+"/members/alice@example.com", nil)
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/workspaces/:id/members authorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+wsID.String()+"/members", bytes.NewReader([]byte(`{"name":"Bob","email":"bob@example.com","role":"operator"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Master-Key", masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/workspaces/:id/members/:email authorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+wsID.String()+"/members/bob@example.com", nil)
		req.Header.Set("X-Master-Key", masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/master/workspaces/:id/members unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+wsID.String()+"/members", bytes.NewReader([]byte(`{"name":"Alice","email":"alice@example.com"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("DELETE /api/v1/master/workspaces/:id/members/:email unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/master/workspaces/"+wsID.String()+"/members/alice@example.com", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}

func TestWorkspaceAPIHandler_List_Success(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var res apipkg.ListWorkspacesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(res.Workspaces))
	}

	if res.Workspaces[0].Name != "Workspace 1" {
		t.Errorf("expected first workspace name 'Workspace 1', got %q", res.Workspaces[0].Name)
	}
	if res.Workspaces[0].ID != uuid.MustParse("11111111-1111-1111-1111-111111111111") {
		t.Errorf("expected first workspace ID '11111111-1111-1111-1111-111111111111', got %s", res.Workspaces[0].ID)
	}
	if res.Workspaces[1].Name != "Workspace 2" {
		t.Errorf("expected second workspace name 'Workspace 2', got %q", res.Workspaces[1].Name)
	}
}

func TestWorkspaceAPIHandler_List_Empty(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{
		listFunc: func(ctx context.Context, limit int) ([]repository.Workspace, error) {
			return []repository.Workspace{}, nil
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var res apipkg.ListWorkspacesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Workspaces == nil || len(res.Workspaces) != 0 {
		t.Fatalf("expected empty non-nil slice, got %v", res.Workspaces)
	}
}

func TestWorkspaceAPIHandler_List_LimitParam(t *testing.T) {
	e := echo.New()
	var capturedLimit int
	wsRepo := &mockWorkspaceRepo{
		listFunc: func(ctx context.Context, limit int) ([]repository.Workspace, error) {
			capturedLimit = limit
			return []repository.Workspace{}, nil
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	t.Run("custom limit parsed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces?limit=10", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.List(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedLimit != 10 {
			t.Errorf("expected limit 10, got %d", capturedLimit)
		}
	})

	t.Run("default limit when omitted or invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces?limit=-5", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.List(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedLimit != 50 {
			t.Errorf("expected default limit 50, got %d", capturedLimit)
		}
	})

	t.Run("limit capped at maximum 500", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces?limit=9999", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.List(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedLimit != 500 {
			t.Errorf("expected limit capped at 500, got %d", capturedLimit)
		}
	})
}

func TestWorkspaceAPIHandler_List_Error(t *testing.T) {
	e := echo.New()
	wsRepo := &mockWorkspaceRepo{
		listFunc: func(ctx context.Context, limit int) ([]repository.Workspace, error) {
			return nil, errors.New("db error")
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", rec.Code)
	}

	var errRes map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errRes); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if errRes["code"] != "internal_error" {
		t.Errorf("expected code 'internal_error', got %q", errRes["code"])
	}
}

func TestWorkspaceAPIHandler_UpdateRetention(t *testing.T) {
	e := echo.New()
	targetID := uuid.New()
	var capturedDays int

	wsRepo := &mockWorkspaceRepo{
		setMediaRetentionDaysFunc: func(ctx context.Context, id uuid.UUID, days int) error {
			if id != targetID {
				return errors.New("unexpected id")
			}
			capturedDays = days
			return nil
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	t.Run("successful update", func(t *testing.T) {
		body := `{"media_retention_days": 60}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+targetID.String()+"/retention", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:id/retention")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

		if err := h.UpdateRetention(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		if capturedDays != 60 {
			t.Errorf("expected capturedDays 60, got %d", capturedDays)
		}
	})

	t.Run("negative days returns bad request", func(t *testing.T) {
		body := `{"media_retention_days": -5}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+targetID.String()+"/retention", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:id/retention")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

		if err := h.UpdateRetention(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("invalid uuid returns bad request", func(t *testing.T) {
		body := `{"media_retention_days": 30}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/invalid-uuid/retention", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/workspaces/:id/retention")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: "invalid-uuid"}})

		if err := h.UpdateRetention(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestWorkspaceAPIHandler_UpsertMember(t *testing.T) {
	e := echo.New()
	targetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	t.Run("successful upsert", func(t *testing.T) {
		var capturedMember *repository.WorkspaceMember
		wsRepo := &mockWorkspaceRepo{
			addMemberFunc: func(ctx context.Context, member *repository.WorkspaceMember) error {
				capturedMember = member
				member.ID = uuid.New()
				return nil
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		body := `{"name":"Alice Smith","email":"alice@example.com","role":"admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+targetID.String()+"/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

		if err := h.UpsertMember(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		if capturedMember == nil {
			t.Fatal("expected member to be captured")
		}
		if capturedMember.Email != "alice@example.com" || capturedMember.Role != "admin" {
			t.Errorf("unexpected captured member: %+v", capturedMember)
		}

		var res struct {
			Status string                     `json:"status"`
			Member repository.WorkspaceMember `json:"member"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.Status != "ok" || res.Member.Email != "alice@example.com" {
			t.Errorf("unexpected response: %+v", res)
		}
	})

	t.Run("missing email returns bad request", func(t *testing.T) {
		h := apipkg.NewWorkspaceAPIHandler(&mockWorkspaceRepo{}, &mockAPIKeyRepo{})
		body := `{"name":"No Email","role":"operator"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+targetID.String()+"/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

		_ = h.UpsertMember(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("invalid workspace id returns bad request", func(t *testing.T) {
		h := apipkg.NewWorkspaceAPIHandler(&mockWorkspaceRepo{}, &mockAPIKeyRepo{})
		body := `{"name":"Alice","email":"alice@example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/invalid-uuid/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: "invalid-uuid"}})

		_ = h.UpsertMember(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("repo error returns internal error", func(t *testing.T) {
		wsRepo := &mockWorkspaceRepo{
			addMemberFunc: func(ctx context.Context, member *repository.WorkspaceMember) error {
				return errors.New("db error")
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		body := `{"email":"alice@example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+targetID.String()+"/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

		_ = h.UpsertMember(c)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("invalid role returns bad request", func(t *testing.T) {
		h := apipkg.NewWorkspaceAPIHandler(&mockWorkspaceRepo{}, &mockAPIKeyRepo{})
		invalidRoles := []string{"billing_admin", "superadmin", "guest", "random"}

		for _, badRole := range invalidRoles {
			body := `{"name":"Invalid Role User","email":"bad@example.com","role":"` + badRole + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+targetID.String()+"/members", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/v1/master/workspaces/:id/members")
			c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

			_ = h.UpsertMember(c)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("for role %q: expected 400 Bad Request, got %d", badRole, rec.Code)
			}
		}
	})

	t.Run("valid roles accepted and normalized", func(t *testing.T) {
		validRoles := []struct {
			inputRole    string
			expectedRole string
		}{
			{"owner", "owner"},
			{"ADMIN", "admin"},
			{"Operator", "operator"},
			{"agent", "agent"},
			{"", "operator"}, // default empty role
		}

		for _, tc := range validRoles {
			var capturedMember *repository.WorkspaceMember
			wsRepo := &mockWorkspaceRepo{
				addMemberFunc: func(ctx context.Context, member *repository.WorkspaceMember) error {
					capturedMember = member
					member.ID = uuid.New()
					return nil
				},
			}
			h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

			body := `{"name":"User","email":"valid@example.com","role":"` + tc.inputRole + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/master/workspaces/"+targetID.String()+"/members", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/v1/master/workspaces/:id/members")
			c.SetPathValues(echo.PathValues{{Name: "id", Value: targetID.String()}})

			if err := h.UpsertMember(c); err != nil {
				t.Fatalf("unexpected error for role %q: %v", tc.inputRole, err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("for role %q: expected 200 OK, got %d", tc.inputRole, rec.Code)
			}
			if capturedMember.Role != tc.expectedRole {
				t.Errorf("for input %q: expected normalized role %q, got %q", tc.inputRole, tc.expectedRole, capturedMember.Role)
			}
		}
	})
}

func TestWorkspaceAPIHandler_RemoveMember(t *testing.T) {
	e := echo.New()
	targetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	t.Run("successful removal", func(t *testing.T) {
		var removedEmail string
		wsRepo := &mockWorkspaceRepo{
			removeMemberByEmailFunc: func(ctx context.Context, workspaceID uuid.UUID, email string) error {
				removedEmail = email
				return nil
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/master/workspaces/"+targetID.String()+"/members/alice@example.com", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members/:email")
		c.SetPathValues(echo.PathValues{
			{Name: "id", Value: targetID.String()},
			{Name: "email", Value: "alice@example.com"},
		})

		if err := h.RemoveMember(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		if removedEmail != "alice@example.com" {
			t.Errorf("expected removedEmail 'alice@example.com', got %q", removedEmail)
		}
	})

	t.Run("invalid workspace id returns bad request", func(t *testing.T) {
		h := apipkg.NewWorkspaceAPIHandler(&mockWorkspaceRepo{}, &mockAPIKeyRepo{})
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/master/workspaces/invalid/members/alice@example.com", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members/:email")
		c.SetPathValues(echo.PathValues{
			{Name: "id", Value: "invalid"},
			{Name: "email", Value: "alice@example.com"},
		})

		_ = h.RemoveMember(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("repo error returns internal error", func(t *testing.T) {
		wsRepo := &mockWorkspaceRepo{
			removeMemberByEmailFunc: func(ctx context.Context, workspaceID uuid.UUID, email string) error {
				return errors.New("db failure")
			},
		}
		h := apipkg.NewWorkspaceAPIHandler(wsRepo, &mockAPIKeyRepo{})

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/master/workspaces/"+targetID.String()+"/members/alice@example.com", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/master/workspaces/:id/members/:email")
		c.SetPathValues(echo.PathValues{
			{Name: "id", Value: targetID.String()},
			{Name: "email", Value: "alice@example.com"},
		})

		_ = h.RemoveMember(c)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})
}

func TestWorkspaceAPIHandler_Create_WithMembers(t *testing.T) {
	e := echo.New()
	var addedMembers []*repository.WorkspaceMember

	wsRepo := &mockWorkspaceRepo{
		addMemberFunc: func(ctx context.Context, member *repository.WorkspaceMember) error {
			addedMembers = append(addedMembers, member)
			return nil
		},
	}
	apiKeyRepo := &mockAPIKeyRepo{}
	h := apipkg.NewWorkspaceAPIHandler(wsRepo, apiKeyRepo)

	body := `{
		"name": "Team Workspace",
		"members": [
			{"name": "Owner User", "email": "owner@example.com", "role": "owner"},
			{"name": "Agent User", "email": "agent@example.com", "role": "operator"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Create(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(addedMembers) != 2 {
		t.Fatalf("expected 2 members added, got %d", len(addedMembers))
	}
	if addedMembers[0].Email != "owner@example.com" || addedMembers[0].Role != "owner" {
		t.Errorf("unexpected member 0: %+v", addedMembers[0])
	}
	if addedMembers[1].Email != "agent@example.com" || addedMembers[1].Role != "operator" {
		t.Errorf("unexpected member 1: %+v", addedMembers[1])
	}
}

