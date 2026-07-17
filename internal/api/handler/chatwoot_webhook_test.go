package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

func TestChatwootWebhookAuth(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)

	// Create workspace
	ws, err := wsRepo.Create(ctx, "chatwoot_webhook_auth_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Create API Key
	_, rawKey, err := apiKeyRepo.Create(ctx, ws.ID, "Test Key")
	if err != nil {
		t.Fatalf("failed to create API Key: %v", err)
	}

	h := handler.NewChatwootWebhookHandler()

	t.Run("ValidTokenQueryParam_Returns200", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/chatwoot?token=%s", rawKey), nil)
		rec := httptest.NewRecorder()

		// Serve HTTP
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("InvalidTokenQueryParam_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/chatwoot?token=invalidkey12345", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("MissingTokenQueryParam_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/chatwoot", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
