package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestUserLogsHandler(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		dsnFallback := "postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable"
		pool, err = pgxpool.New(ctx, dsnFallback)
		if err != nil {
			t.Skip("PostgreSQL not available for testing")
		}
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	repo := repository.NewUserActionLogRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "user_logs_handler_test")
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Seed a test log
	metaBytes, _ := json.Marshal(map[string]any{"test": "metadata"})
	log := &repository.UserActionLog{
		WorkspaceID: ws.ID,
		ActorType:   "user",
		ActorID:     "admin",
		ActorName:   "Admin User",
		Action:      "test.action",
		Source:      "dashboard",
		Metadata:    metaBytes,
	}

	err = repo.Insert(ctx, log)
	if err != nil {
		t.Fatalf("failed to insert log: %v", err)
	}

	h := admin.NewUserLogsHandler(repo)
	e := echo.New()

	t.Run("List Logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/user-logs", nil)
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set context values to avoid layout crash
		reqCtx := context.WithValue(req.Context(), "active_path", "/admin/user-logs")
		reqCtx = context.WithValue(reqCtx, "active_workspace", ws)
		reqCtx = context.WithValue(reqCtx, "workspaces_list", []repository.Workspace{*ws})
		c.SetRequest(req.WithContext(reqCtx))

		if err := h.List(c); err != nil {
			t.Fatalf("List returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Logs de Ações") {
			t.Errorf("expected response to contain 'Logs de Ações', got: %s", body)
		}
		if !strings.Contains(body, "Admin User") {
			t.Errorf("expected response to contain actor name 'Admin User', got: %s", body)
		}
	})

	t.Run("Get Log Metadata Modal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/user-logs/"+log.ID.String()+"/metadata", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/user-logs/:id/metadata")
		c.SetPathValues(echo.PathValues{
			{Name: "id", Value: log.ID.String()},
		})

		if err := h.GetMetadata(c); err != nil {
			t.Fatalf("GetMetadata returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Metadados da Ação") {
			t.Errorf("expected response to contain modal title, got: %s", body)
		}
		if !strings.Contains(body, "metadata") {
			t.Errorf("expected response to contain metadata keyword, got: %s", body)
		}
	})
}
