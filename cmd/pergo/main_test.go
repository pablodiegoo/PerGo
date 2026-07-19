package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/integration/chatwoot"
	"github.com/pablojhp.pergo/internal/integration/typebot"
	echosrv "github.com/pablojhp.pergo/internal/platform/echo"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// TestServerBootHealthz verifies the server starts on a random port and
// responds to the liveness probe.
func TestServerBootHealthz(t *testing.T) {
	e := echosrv.New()
	h := &handler.HealthHandler{}
	h.RegisterRoutes(e)

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestServerBootReadyz verifies the readiness probe returns 200 when both
// pgx and NATS are reachable.
func TestServerBootReadyz(t *testing.T) {
	pool, err := postgres.NewPool(context.Background(), testDSN())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	defer pool.Close()

	// Verify PostgreSQL is actually reachable
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	e := echosrv.New()
	h := &handler.HealthHandler{
		Pool: pool,
		NATS: &noopNATS{},
	}
	h.RegisterRoutes(e)

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestServerBootReadyzDown verifies readiness returns 503 when NATS is
// unreachable.
func TestServerBootReadyzDown(t *testing.T) {
	pool, err := postgres.NewPool(context.Background(), testDSN())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	defer pool.Close()

	// Verify PostgreSQL is actually reachable
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	e := echosrv.New()
	h := &handler.HealthHandler{
		Pool: pool,
		NATS: &failingNATS{},
	}
	h.RegisterRoutes(e)

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

// TestGracefulShutdown verifies the server exits within 5 seconds when the
// context is cancelled.
func TestGracefulShutdown(t *testing.T) {
	pool, err := postgres.NewPool(context.Background(), testDSN())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	defer pool.Close()

	// Verify PostgreSQL is actually reachable
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("NewSQLDB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Start server in background; cancel context to trigger shutdown
	done := make(chan error, 1)
	go func() {
		done <- runServer(ctx, pool, db)
	}()

	// Give server time to start, then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Server exited cleanly
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down within 5 seconds")
	}
}

// --- helpers ---

func TestCompositionRoot_AllIntegrationsWired(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, testDSN())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	integrationRepo := repository.NewIntegrationRepository(pool, enc)
	chatwootMappingRepo := repository.NewChatwootMappingRepository(pool)
	typebotSessionRepo := repository.NewTypebotSessionRepository(pool)

	chatwootSyncer := chatwoot.NewChatwootSyncer(integrationRepo, chatwootMappingRepo, nil)
	typebotForwarder := typebot.NewForwarder(typebotSessionRepo, integrationRepo, nil)

	if chatwootSyncer == nil {
		t.Fatal("chatwootSyncer must not be nil — regression of Phase 21 wiring")
	}
	if typebotForwarder == nil {
		t.Fatal("typebotForwarder must not be nil — regression of TYPE-04 wiring")
	}
}

func testDSN() string {
	if dsn := os.Getenv("PERGO_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://postgres:postgres@localhost:5432/pergo_test?sslmode=disable"
}

// noopNATS satisfies the handler.NATSConn interface and always succeeds.
type noopNATS struct{}

func (n *noopNATS) Ping() error { return nil }

// failingNATS satisfies the handler.NATSConn interface and always fails.
type failingNATS struct{}

func (f *failingNATS) Ping() error { return fmt.Errorf("nats unavailable") }
