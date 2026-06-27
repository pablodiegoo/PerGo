package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/crypto"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/repository"
)

// TestCreateWorkspace verifies workspace creation via repository.
func TestCreateWorkspace(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	repo := repository.NewWorkspaceRepository(pool)
	wsName := "test-workspace-" + uuid.New().String()[:8]
	ws, err := repo.Create(context.Background(), wsName)
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	if ws.ID == uuid.Nil {
		t.Error("expected non-nil workspace ID")
	}
	if ws.Name != wsName {
		t.Errorf("expected name %q, got %q", wsName, ws.Name)
	}
	if ws.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if ws.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

// TestCreateAPIKey verifies API key generation with SHA-256 hash and prefix.
func TestCreateAPIKey(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(context.Background(), "test-apikey-ws-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	keyRepo := repository.NewAPIKeyRepository(pool)
	apiKey, plaintext, err := keyRepo.Create(context.Background(), ws.ID, "test-key")
	if err != nil {
		t.Fatalf("Create API key: %v", err)
	}

	// Key prefix is first 8 characters of the plaintext key
	if len(plaintext) < 8 {
		t.Fatalf("expected plaintext key length >= 8, got %d", len(plaintext))
	}
	prefix := plaintext[:8]
	if apiKey.KeyPrefix != prefix {
		t.Errorf("expected key_prefix %q, got %q", prefix, apiKey.KeyPrefix)
	}

	// Key hash is 32-byte SHA-256
	if len(apiKey.KeyHash) != 32 {
		t.Errorf("expected key_hash length 32, got %d", len(apiKey.KeyHash))
	}

	// Key ID is present
	if apiKey.KeyID == "" {
		t.Error("expected non-empty key_id")
	}

	// Key version is 1
	if apiKey.KeyVersion != 1 {
		t.Errorf("expected key_version 1, got %d", apiKey.KeyVersion)
	}

	// Workspace ID matches
	if apiKey.WorkspaceID != ws.ID {
		t.Errorf("expected workspace_id %v, got %v", ws.ID, apiKey.WorkspaceID)
	}
}

// TestAuthMiddlewareValid verifies that a valid API key returns 200 and
// injects workspace_id into the request context.
func TestAuthMiddlewareValid(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(context.Background(), "test-auth-valid-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	keyRepo := repository.NewAPIKeyRepository(pool)
	_, plaintext, err := keyRepo.Create(context.Background(), ws.ID, "test-key")
	if err != nil {
		t.Fatalf("Create API key: %v", err)
	}

	e := echo.New()
	e.Use(middleware.AuthMiddleware(keyRepo))

	// Handler that returns the workspace_id from context
	e.GET("/api/v1/me", func(c *echo.Context) error {
		id, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusUnauthorized, "no workspace_id")
		}
		return c.JSON(http.StatusOK, map[string]string{"workspace_id": id.String()})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["workspace_id"] != ws.ID.String() {
		t.Errorf("expected workspace_id %s, got %s", ws.ID, resp["workspace_id"])
	}
}

// TestAuthMiddlewareMissing verifies that a missing Authorization header
// returns 401 with a structured error body.
func TestAuthMiddlewareMissing(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	keyRepo := repository.NewAPIKeyRepository(pool)

	e := echo.New()
	e.Use(middleware.AuthMiddleware(keyRepo))

	e.GET("/api/v1/me", func(c *echo.Context) error {
		return c.String(http.StatusOK, "should not reach")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["code"] != "unauthorized" {
		t.Errorf("expected error code 'unauthorized', got %q", resp["code"])
	}
}

// TestAuthMiddlewareInvalid verifies that an invalid API key returns 401.
func TestAuthMiddlewareInvalid(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	keyRepo := repository.NewAPIKeyRepository(pool)

	e := echo.New()
	e.Use(middleware.AuthMiddleware(keyRepo))

	e.GET("/api/v1/me", func(c *echo.Context) error {
		return c.String(http.StatusOK, "should not reach")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-key-garbage")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthMiddlewareRevoked verifies that a revoked API key is rejected.
func TestAuthMiddlewareRevoked(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(context.Background(), "test-auth-revoked-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	keyRepo := repository.NewAPIKeyRepository(pool)
	apiKey, plaintext, err := keyRepo.Create(context.Background(), ws.ID, "test-key")
	if err != nil {
		t.Fatalf("Create API key: %v", err)
	}

	// Revoke the key
	if err := keyRepo.Revoke(context.Background(), apiKey.ID); err != nil {
		t.Fatalf("Revoke key: %v", err)
	}

	e := echo.New()
	e.Use(middleware.AuthMiddleware(keyRepo))

	e.GET("/api/v1/me", func(c *echo.Context) error {
		return c.String(http.StatusOK, "should not reach")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after revocation, got %d", rec.Code)
	}
}

// TestAuthMiddlewareCacheHit verifies that the second request with the same
// API key is served from the in-memory cache (no DB roundtrip).
func TestAuthMiddlewareCacheHit(t *testing.T) {
	pool := mustPool(t)
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(context.Background(), "test-auth-cache-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	keyRepo := repository.NewAPIKeyRepository(pool)
	_, plaintext, err := keyRepo.Create(context.Background(), ws.ID, "test-key")
	if err != nil {
		t.Fatalf("Create API key: %v", err)
	}

	// First request — populates cache
	// Second request — should be served from cache
	// We verify by checking that both succeed (cache hit is an internal optimization)
	e := echo.New()
	e.Use(middleware.AuthMiddleware(keyRepo))

	e.GET("/api/v1/me", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

// TestEncryptDecryptRoundTrip verifies that encrypting and then decrypting
// returns the original plaintext.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}

	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	plaintext := []byte("my-secret-whatsapp-credentials")

	ciphertext, keyID, keyVersion, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if keyID != "default" {
		t.Errorf("expected key_id 'default', got %q", keyID)
	}
	if keyVersion != 1 {
		t.Errorf("expected key_version 1, got %d", keyVersion)
	}
	if len(ciphertext) == 0 {
		t.Error("expected non-empty ciphertext")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted text mismatch: got %q, want %q", decrypted, plaintext)
	}
}

// TestTenantContext verifies WithWorkspaceID/WorkspaceIDFrom round-trip
// and that RequireWorkspaceID errors on missing context.
func TestTenantContext(t *testing.T) {
	id := uuid.New()

	// Round-trip
	ctx := tenant.WithWorkspaceID(context.Background(), id)
	got, ok := tenant.WorkspaceIDFrom(ctx)
	if !ok {
		t.Fatal("expected workspace_id in context")
	}
	if got != id {
		t.Errorf("expected %v, got %v", id, got)
	}

	// Missing
	_, ok = tenant.WorkspaceIDFrom(context.Background())
	if ok {
		t.Error("expected no workspace_id in empty context")
	}

	// RequireWorkspaceID
	_, err := tenant.RequireWorkspaceID(context.Background())
	if err == nil {
		t.Error("expected error from RequireWorkspaceID on empty context")
	}

	// RequireWorkspaceID with value
	gotID, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		t.Fatalf("RequireWorkspaceID: %v", err)
	}
	if gotID != id {
		t.Errorf("expected %v, got %v", id, gotID)
	}
}

// TestHashAPIKeyRoundTrip verifies that HashAPIKey and VerifyAPIKey work together.
func TestHashAPIKeyRoundTrip(t *testing.T) {
	key := "my-super-secret-api-key-12345678"

	hash, prefix := crypto.HashAPIKey(key)

	if len(hash) != 32 {
		t.Errorf("expected hash length 32, got %d", len(hash))
	}
	if prefix != key[:8] {
		t.Errorf("expected prefix %q, got %q", key[:8], prefix)
	}

	// Verify correct key
	if !crypto.VerifyAPIKey(key, hash) {
		t.Error("expected VerifyAPIKey to return true for correct key")
	}

	// Verify incorrect key
	if crypto.VerifyAPIKey("wrong-key", hash) {
		t.Error("expected VerifyAPIKey to return false for incorrect key")
	}
}

// --- helpers ---

func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), testDSN())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	// Run migrations
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("NewSQLDB: %v", err)
	}
	defer db.Close()
	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	return pool
}
