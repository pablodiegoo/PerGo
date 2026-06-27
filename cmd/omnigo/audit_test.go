package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/audit"
	"github.com/pablojhp.omnigo/internal/platform/obs"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
)

// TestTraceMiddlewareGeneratesID verifies that the trace middleware generates
// a UUID trace_id and stores it in the request context.
func TestTraceMiddlewareGeneratesID(t *testing.T) {
	e := echo.New()
	e.Use(middleware.TraceMiddleware())

	var capturedTraceID string
	e.GET("/test", func(c *echo.Context) error {
		id, ok := middleware.TraceIDFrom(c.Request().Context())
		if !ok {
			t.Error("expected trace_id in context")
		}
		capturedTraceID = id
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if capturedTraceID == "" {
		t.Fatal("expected non-empty trace_id")
	}

	// Verify it's a valid UUID
	if _, err := uuid.Parse(capturedTraceID); err != nil {
		t.Errorf("expected valid UUID trace_id, got %q: %v", capturedTraceID, err)
	}
}

// TestTraceMiddlewareExtractsHeader verifies that when an X-Trace-Id header
// is present, the middleware uses the provided value instead of generating one.
func TestTraceMiddlewareExtractsHeader(t *testing.T) {
	e := echo.New()
	e.Use(middleware.TraceMiddleware())

	expectedTraceID := "custom-trace-id-12345"
	var capturedTraceID string
	e.GET("/test", func(c *echo.Context) error {
		id, _ := middleware.TraceIDFrom(c.Request().Context())
		capturedTraceID = id
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Trace-Id", expectedTraceID)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if capturedTraceID != expectedTraceID {
		t.Errorf("expected trace_id %q, got %q", expectedTraceID, capturedTraceID)
	}
}

// TestTraceIDFromContext verifies that TraceIDFrom correctly retrieves a
// trace_id stored via WithContext.
func TestTraceIDFromContext(t *testing.T) {
	traceID := uuid.New().String()
	ctx := middleware.WithContext(context.Background(), traceID)

	got, ok := middleware.TraceIDFrom(ctx)
	if !ok {
		t.Fatal("expected trace_id in context")
	}
	if got != traceID {
		t.Errorf("expected %q, got %q", traceID, got)
	}

	// Empty context should return false
	_, ok = middleware.TraceIDFrom(context.Background())
	if ok {
		t.Error("expected no trace_id in empty context")
	}
}

// TestAuditEventWritten verifies that an event sent to the audit writer is
// eventually written to the PostgreSQL audit_logs table.
func TestAuditEventWritten(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	wsID := uuid.New()
	traceID := uuid.New().String()

	writer := audit.NewWriter(pool, 100, 1)

	event := audit.Event{
		WorkspaceID: wsID,
		TraceID:     traceID,
		EventType:   "test.event",
		Payload:     []byte(`{"key":"value"}`),
		CreatedAt:   time.Now(),
	}

	if err := writer.Write(event); err != nil {
		t.Fatalf("Write event: %v", err)
	}

	// Close the writer to force a flush/drain
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// Query audit_logs for the event
	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE trace_id = $1 AND event_type = $2",
		traceID, "test.event",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 audit log row, got %d", count)
	}
}

// TestBatchWriterFlushAt100 verifies that the batch writer flushes when the
// batch size reaches 100 events.
func TestBatchWriterFlushAt100(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	wsID := uuid.New()
	traceID := uuid.New().String()

	writer := audit.NewWriter(pool, 200, 1)
	defer writer.Close()

	// Send exactly 100 events
	for i := 0; i < 100; i++ {
		event := audit.Event{
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "test.batch",
			Payload:     []byte(`{}`),
			CreatedAt:   time.Now(),
		}
		if err := writer.Write(event); err != nil {
			t.Fatalf("Write event %d: %v", i, err)
		}
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE trace_id = $1 AND event_type = $2",
		traceID, "test.batch",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}

	if count != 100 {
		t.Errorf("expected 100 audit log rows, got %d", count)
	}
}

// TestBatchWriterDrainOnClose verifies that when the channel is closed, the
// batch writer flushes all remaining events.
func TestBatchWriterDrainOnClose(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	wsID := uuid.New()
	traceID := uuid.New().String()

	writer := audit.NewWriter(pool, 5000, 1)

	// Send 50 events
	for i := 0; i < 50; i++ {
		event := audit.Event{
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "test.drain",
			Payload:     []byte(`{}`),
			CreatedAt:   time.Now(),
		}
		if err := writer.Write(event); err != nil {
			t.Fatalf("Write event %d: %v", i, err)
		}
	}

	// Close writer — should drain remaining events
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE trace_id = $1 AND event_type = $2",
		traceID, "test.drain",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}

	if count != 50 {
		t.Errorf("expected 50 audit log rows, got %d", count)
	}
}

// TestStructuredLogWithTrace verifies that a logger with trace context includes
// the trace_id field in structured JSON output.
func TestStructuredLogWithTrace(t *testing.T) {
	traceID := uuid.New().String()
	var buf bytes.Buffer

	logger := obs.NewLoggerWithWriter(traceID, &buf)
	logger.Info("test message", "key", "value")

	// Parse JSON output
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log output: %v", err)
	}

	if tid, ok := entry["trace_id"]; !ok || tid != traceID {
		t.Errorf("expected trace_id %q in log output, got %v", traceID, tid)
	}

	msg, ok := entry["msg"]
	if !ok || msg != "test message" {
		t.Errorf("expected msg 'test message' in log output, got %v", msg)
	}
}

// TestAuditLogSchema verifies that the audit_logs table has the expected
// columns with correct types.
func TestAuditLogSchema(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	// Query column information
	rows, err := pool.Query(context.Background(), `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'audit_logs' 
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = dataType
	}

	expected := map[string]string{
		"workspace_id": "uuid",
		"trace_id":     "text",
		"event_type":   "text",
		"payload":      "jsonb",
		"created_at":   "timestamp with time zone",
	}

	for col, wantType := range expected {
		gotType, ok := columns[col]
		if !ok {
			t.Errorf("missing column %q", col)
			continue
		}
		if gotType != wantType {
			t.Errorf("column %q: expected type %q, got %q", col, wantType, gotType)
		}
	}
}

// TestAuditNoDedup verifies that multiple events with the same trace_id are
// all written (no deduplication at the audit layer).
func TestAuditNoDedup(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	wsID := uuid.New()
	traceID := uuid.New().String()

	writer := audit.NewWriter(pool, 100, 1)

	// Send two events with same trace_id
	for i := 0; i < 2; i++ {
		event := audit.Event{
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "test.nodedup",
			Payload:     []byte(`{}`),
			CreatedAt:   time.Now(),
		}
		if err := writer.Write(event); err != nil {
			t.Fatalf("Write event %d: %v", i, err)
		}
	}

	// Close the writer to force a flush/drain
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE trace_id = $1 AND event_type = $2",
		traceID, "test.nodedup",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 audit log rows (no dedup), got %d", count)
	}
}

// TestWriterCloseDrains verifies that Writer.Close() blocks until all
// buffered events are flushed before returning.
func TestWriterCloseDrains(t *testing.T) {
	pool := mustAuditPool(t)
	defer pool.Close()

	wsID := uuid.New()
	traceID := uuid.New().String()

	writer := audit.NewWriter(pool, 5000, 1)

	// Send events
	for i := 0; i < 30; i++ {
		event := audit.Event{
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   "test.close",
			Payload:     []byte(`{}`),
			CreatedAt:   time.Now(),
		}
		if err := writer.Write(event); err != nil {
			t.Fatalf("Write event %d: %v", i, err)
		}
	}

	// Close should block until all events are flushed
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// After Close returns, all events should be in the database
	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE trace_id = $1 AND event_type = $2",
		traceID, "test.close",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}

	if count != 30 {
		t.Errorf("expected 30 audit log rows, got %d", count)
	}
}

// --- helpers ---

func mustAuditPool(t *testing.T) *pgxpool.Pool {
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
