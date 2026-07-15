package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/platform/crypto"
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

	// Ping to check if connection actually works
	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	// Run migrations to ensure schema is up to date
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

func TestWABATemplateHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	tmplRepo := repository.NewWABATemplateRepository(pool)

	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "handler_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	wabaConfig := map[string]string{
		"phone_number_id": "12345",
		"token":           "meta-token-abc",
		"waba_account_id": "99999",
	}
	configBytes, _ := json.Marshal(wabaConfig)
	err = connRepo.Create(ctx, &repository.Connection{
		WorkspaceID:    ws.ID,
		Name:           "WABA Cloud Test",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "12345",
		Credentials:    configBytes,
		Status:         "active",
	})
	if err != nil {
		t.Fatalf("failed to create WABA connection: %v", err)
	}

	t.Run("Create Template Success", func(t *testing.T) {
		metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST request, got %s", r.Method)
			}
			if !strings.Contains(r.URL.Path, "/99999/message_templates") {
				t.Errorf("unexpected path %s", r.URL.Path)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"meta-tmpl-id-555","status":"APPROVED","category":"UTILITY"}`))
		}))
		defer metaServer.Close()

		h := admin.NewWABATemplateHandler(tmplRepo, connRepo)
		h.BaseURL = metaServer.URL

		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/templates", ws.ID), strings.NewReader(`{"name":"welcome_template","language":"en_US","category":"UTILITY","components":[]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/templates")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		err := h.Create(c)
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}

		if rec.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d. Body: %s", http.StatusCreated, rec.Code, rec.Body.String())
		}

		var created repository.WABATemplate
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("failed to unmarshal created template: %v", err)
		}

		if created.MetaTemplateID != "meta-tmpl-id-555" {
			t.Errorf("expected meta template id meta-tmpl-id-555, got %s", created.MetaTemplateID)
		}
	})

	t.Run("Sync Template Success", func(t *testing.T) {
		// Insert local template first
		tmpl := &repository.WABATemplate{
			WorkspaceID:    ws.ID,
			MetaTemplateID: "meta-tmpl-id-sync",
			Name:           "sync_template",
			Language:       "en_US",
			Status:         "PENDING",
			Category:       "UTILITY",
		}
		local, err := tmplRepo.Create(ctx, tmpl)
		if err != nil {
			t.Fatalf("failed to insert local template: %v", err)
		}

		metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", r.Method)
			}
			if !strings.Contains(r.URL.Path, "/meta-tmpl-id-sync") {
				t.Errorf("unexpected path %s", r.URL.Path)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"meta-tmpl-id-sync","status":"APPROVED"}`))
		}))
		defer metaServer.Close()

		h := admin.NewWABATemplateHandler(tmplRepo, connRepo)
		h.BaseURL = metaServer.URL

		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/templates/%s/sync", ws.ID, local.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/templates/:template_id/sync")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
			{Name: "template_id", Value: local.ID.String()},
		})

		err = h.Sync(c)
		if err != nil {
			t.Fatalf("Sync returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
		}

		var synced repository.WABATemplate
		if err := json.Unmarshal(rec.Body.Bytes(), &synced); err != nil {
			t.Fatalf("failed to unmarshal synced template: %v", err)
		}

		if synced.Status != "APPROVED" {
			t.Errorf("expected status APPROVED, got %s", synced.Status)
		}
	})
}
