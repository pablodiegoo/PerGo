package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

type logRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type memoryLogHandler struct {
	mu      sync.Mutex
	records []logRecord
}

func (h *memoryLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *memoryLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.records = append(h.records, logRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h *memoryLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *memoryLogHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *memoryLogHandler) findRecord(msg string) (logRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == msg {
			return r, true
		}
	}
	return logRecord{}, false
}

func TestEnforceWorkspaceScope_TableDriven(t *testing.T) {
	ws1 := uuid.New()
	ws2 := uuid.New()

	tests := []struct {
		name                 string
		routePattern         string
		requestPath          string
		headers              map[string]string
		queryParams          map[string]string
		setupContext         func(ctx context.Context) context.Context
		expectedStatus       int
		expectedBodyContains string
		expectedErrorCode    string
		expectedWorkspaceID  uuid.UUID
		expectWarningLog     bool
	}{
		{
			name:         "matching :workspace_id returns 200",
			routePattern: "/api/v1/workspaces/:workspace_id/campaigns",
			requestPath:  "/api/v1/workspaces/" + ws1.String() + "/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewWorkspaceScope(ws1, domain.CapabilityWorkspaceScoped))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws1,
		},
		{
			name:         "mismatched :workspace_id returns 403 with WORKSPACE_ID_MISMATCH and warning log",
			routePattern: "/api/v1/workspaces/:workspace_id/campaigns",
			requestPath:  "/api/v1/workspaces/" + ws2.String() + "/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewWorkspaceScope(ws1, domain.CapabilityWorkspaceScoped))
			},
			expectedStatus:       http.StatusForbidden,
			expectedErrorCode:    "WORKSPACE_ID_MISMATCH",
			expectedBodyContains: "authenticated workspace does not match URL parameter",
			expectWarningLog:     true,
		},
		{
			name:         "flat route with workspace-scoped API key returns 200",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewWorkspaceScope(ws1, domain.CapabilityWorkspaceScoped))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws1,
		},
		{
			name:         "operator scoped with :workspace_id sets target and returns 200",
			routePattern: "/api/v1/workspaces/:workspace_id/campaigns",
			requestPath:  "/api/v1/workspaces/" + ws2.String() + "/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(uuid.Nil))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws2,
		},
		{
			name:         "operator scoped flat route with X-Workspace-ID returns 200",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns",
			headers: map[string]string{
				"X-Workspace-ID": ws2.String(),
			},
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(uuid.Nil))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws2,
		},
		{
			name:         "operator scoped flat route with query param workspace_id returns 200",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns?workspace_id=" + ws2.String(),
			queryParams: map[string]string{
				"workspace_id": ws2.String(),
			},
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(uuid.Nil))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws2,
		},
		{
			name:         "operator scoped flat route without target returns 400 WORKSPACE_REQUIRED",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(uuid.Nil))
			},
			expectedStatus:       http.StatusBadRequest,
			expectedErrorCode:    "WORKSPACE_REQUIRED",
			expectedBodyContains: "workspace target must be specified via header or URL for operator scope",
		},
		{
			name:         "operator scoped flat route with pre-resolved active workspace returns 200",
			routePattern: "/admin/campaigns",
			requestPath:  "/admin/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(ws1))
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws1,
		},
		{
			name:         "fallback when context only has tenant.WorkspaceIDFrom builds CapabilityWorkspaceScoped",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return tenant.WithWorkspaceID(ctx, ws1)
			},
			expectedStatus:      http.StatusOK,
			expectedWorkspaceID: ws1,
		},
		{
			name:         "unauthenticated missing scope returns 401",
			routePattern: "/api/v1/campaigns",
			requestPath:  "/api/v1/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return ctx
			},
			expectedStatus:    http.StatusUnauthorized,
			expectedErrorCode: "UNAUTHORIZED",
		},
		{
			name:         "operator with invalid :workspace_id param returns 400",
			routePattern: "/api/v1/workspaces/:workspace_id/campaigns",
			requestPath:  "/api/v1/workspaces/invalid-uuid/campaigns",
			setupContext: func(ctx context.Context) context.Context {
				return domain.ContextWithScope(ctx, domain.NewOperatorScope(uuid.Nil))
			},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: "INVALID_WORKSPACE_ID",
		},
		{
			name:         "public excluded endpoint /healthz passes through without scope",
			routePattern: "/healthz",
			requestPath:  "/healthz",
			setupContext: func(ctx context.Context) context.Context {
				return ctx
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:         "public excluded endpoint /docs passes through without scope",
			routePattern: "/docs",
			requestPath:  "/docs",
			setupContext: func(ctx context.Context) context.Context {
				return ctx
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:         "public excluded endpoint /llms.txt passes through without scope",
			routePattern: "/llms.txt",
			requestPath:  "/llms.txt",
			setupContext: func(ctx context.Context) context.Context {
				return ctx
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			memHandler := &memoryLogHandler{}
			origLogger := slog.Default()
			slog.SetDefault(slog.New(memHandler))
			defer slog.SetDefault(origLogger)

			e := echo.New()

			// Context bridge verification variables
			var capturedScope domain.WorkspaceScope
			var capturedTenantID uuid.UUID
			var handlerInvoked bool

			handler := func(c *echo.Context) error {
				handlerInvoked = true
				var ok bool
				capturedScope, ok = domain.FromContext(c.Request().Context())
				if !ok && tc.routePattern != "/healthz" && tc.routePattern != "/docs" && tc.routePattern != "/llms.txt" {
					t.Error("expected domain.WorkspaceScope in handler context")
				}
				capturedTenantID, _ = tenant.WorkspaceIDFrom(c.Request().Context())

				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			}

			// Mount EnforceWorkspaceScope middleware on route
			e.GET(tc.routePattern, handler, EnforceWorkspaceScope())

			req := httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.setupContext != nil {
				req = req.WithContext(tc.setupContext(req.Context()))
			}

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected HTTP status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.expectedErrorCode != "" {
				var errResp map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to decode error json: %v", err)
				}
				if errResp["code"] != tc.expectedErrorCode {
					t.Errorf("expected error code %q, got %q", tc.expectedErrorCode, errResp["code"])
				}
				if tc.expectedBodyContains != "" && errResp["message"] != tc.expectedBodyContains {
					t.Errorf("expected message %q, got %q", tc.expectedBodyContains, errResp["message"])
				}
			}

			if tc.expectWarningLog {
				rec, found := memHandler.findRecord("security.workspace_mismatch_detected")
				if !found {
					t.Error("expected warning log 'security.workspace_mismatch_detected' to be emitted")
				} else {
					if rec.Level != slog.LevelWarn {
						t.Errorf("expected log level WARN, got %v", rec.Level)
					}
					if rec.Attrs["authenticated_workspace_id"] != ws1 {
						t.Errorf("expected authenticated_workspace_id %v, got %v", ws1, rec.Attrs["authenticated_workspace_id"])
					}
					if rec.Attrs["attempted_workspace_id"] != ws2.String() {
						t.Errorf("expected attempted_workspace_id %v, got %v", ws2.String(), rec.Attrs["attempted_workspace_id"])
					}
				}
			}

			// Verify context bridge when status is 200 and not excluded endpoint
			if tc.expectedStatus == http.StatusOK && tc.expectedWorkspaceID != uuid.Nil {
				if !handlerInvoked {
					t.Fatal("expected handler to be invoked on 200 OK")
				}
				// Context bridge: tenant.WorkspaceIDFrom matches scope.WorkspaceID()
				if capturedTenantID != tc.expectedWorkspaceID {
					t.Errorf("tenant.WorkspaceIDFrom mismatch: expected %s, got %s", tc.expectedWorkspaceID, capturedTenantID)
				}
				if capturedScope.WorkspaceID() != tc.expectedWorkspaceID {
					t.Errorf("scope.WorkspaceID() mismatch: expected %s, got %s", tc.expectedWorkspaceID, capturedScope.WorkspaceID())
				}
				if capturedTenantID != capturedScope.WorkspaceID() {
					t.Errorf("context bridge mismatch: tenant %s != scope %s", capturedTenantID, capturedScope.WorkspaceID())
				}
			}
		})
	}
}

func TestEnforceWorkspaceScope_RequireHelper(t *testing.T) {
	wsID := uuid.New()
	e := echo.New()

	var requiredScope domain.WorkspaceScope
	var requireErr error

	e.GET("/api/v1/test", func(c *echo.Context) error {
		requiredScope, requireErr = domain.Require(c.Request().Context())
		return c.NoContent(http.StatusOK)
	}, EnforceWorkspaceScope())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	ctx := domain.ContextWithScope(req.Context(), domain.NewWorkspaceScope(wsID, domain.CapabilityWorkspaceScoped))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if requireErr != nil {
		t.Fatalf("unexpected Require error: %v", requireErr)
	}
	if requiredScope.WorkspaceID() != wsID {
		t.Fatalf("expected scope workspace %s, got %s", wsID, requiredScope.WorkspaceID())
	}
}
