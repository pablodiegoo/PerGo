// Package main is the composition root for the OmniGo server.
// It wires dependencies, starts the HTTP server, and handles graceful shutdown.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.omnigo/internal/api/handler"
	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/audit"
	echosrv "github.com/pablojhp.omnigo/internal/platform/echo"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// --- Config from env vars ---
	dsn := envOrDefault("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/omnigo?sslmode=disable")
	natsURL := envOrDefault("NATS_URL", "nats://localhost:4222")
	serverPort := envOrDefault("SERVER_PORT", "8080")

	// --- PostgreSQL ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		slog.Error("failed to create pgxpool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		slog.Error("failed to create stdlib sql.DB", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied successfully")

	// --- NATS ---
	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("failed to connect to NATS", "error", err, "url", natsURL)
		os.Exit(1)
	}
	defer nc.Close()
	slog.Info("connected to NATS", "url", natsURL)

	// --- Audit writer ---
	auditWriter := audit.NewWriter(pool, 5000, 2)
	defer func() {
		slog.Info("flushing audit buffer")
		if err := auditWriter.Close(); err != nil {
			slog.Error("audit writer close failed", "error", err)
		}
		slog.Info("audit buffer flushed")
	}()

	// --- Echo HTTP server ---
	e := echosrv.New()

	// Trace middleware runs before auth so trace_id is available for logging
	e.Use(middleware.TraceMiddleware())

	healthHandler := &handler.HealthHandler{
		Pool: pool,
		NATS: &natsConn{nc: nc},
	}
	healthHandler.RegisterRoutes(e)

	// Start HTTP server
	srv := &http.Server{
		Addr:    net.JoinHostPort("", serverPort),
		Handler: e,
	}

	go func() {
		slog.Info("starting server", "port", serverPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("received shutdown signal", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown error", "error", err)
	}

	cancel() // signal runServer to exit if used
	slog.Info("server stopped")
}

// --- helpers ---

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// natsConn wraps *nats.Conn to satisfy the handler.NATSConn interface.
type natsConn struct {
	nc *nats.Conn
}

func (c *natsConn) Ping() error {
	if !c.nc.IsConnected() {
		return fmt.Errorf("nats not connected")
	}
	return nil
}

// runServer starts the HTTP server and blocks until ctx is cancelled.
func runServer(ctx context.Context, pool *pgxpool.Pool, db *sql.DB) error {
	_ = pool
	_ = db
	<-ctx.Done()
	return nil
}
