package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
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

	return pool
}

type mockOutboundProcessor struct {
	IngestFunc func(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error)
}

func (m *mockOutboundProcessor) Ingest(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error) {
	if m.IngestFunc != nil {
		return m.IngestFunc(ctx, workspaceID, traceID, req)
	}
	return &domain.QueueMessage{
		WorkspaceID: workspaceID,
		To:          req.To,
		Channel:     req.Channel,
		Body:        req.Body,
		QueuedAt:    time.Now(),
	}, nil
}

func TestMCPServerTools(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Initialize repositories
	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	contactRepo := repository.NewContactRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "MCP Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	mockIngestor := &mockOutboundProcessor{}
	srv := NewServer(wsRepo, connRepo, contactRepo, auditRepo, mockIngestor)

	t.Run("ListWorkspaces", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		res, err := srv.handleListWorkspaces(ctx, req)
		if err != nil {
			t.Fatalf("handleListWorkspaces returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleListWorkspaces returned error result: %+v", res.Content)
		}

		// Verify workspace lists contains our test workspace ID
		var workspaces []repository.Workspace
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := mcp.AsTextContent(res.Content[0]); ok {
				text = tc.Text
			}
		}
		if text == "" {
			t.Fatalf("expected text content in response")
		}

		if err := json.Unmarshal([]byte(text), &workspaces); err != nil {
			t.Fatalf("failed to unmarshal workspaces: %v", err)
		}

		found := false
		for _, w := range workspaces {
			if w.ID == ws.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected workspace %s to be listed", ws.ID)
		}
	})

	t.Run("ListConnections", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleListConnections(ctx, req)
		if err != nil {
			t.Fatalf("handleListConnections returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleListConnections returned error result: %+v", res.Content)
		}
	})

	t.Run("SearchContacts", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"query":        "NonExistentContact",
		}

		res, err := srv.handleSearchContacts(ctx, req)
		if err != nil {
			t.Fatalf("handleSearchContacts returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleSearchContacts returned error result: %+v", res.Content)
		}
	})

	t.Run("SendMessage", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"to":           "+1234567890",
			"channel":      "telegram",
			"body":         "Hello from MCP test",
		}

		res, err := srv.handleSendMessage(ctx, req)
		if err != nil {
			t.Fatalf("handleSendMessage returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleSendMessage returned error result: %+v", res.Content)
		}

		var sendRes map[string]any
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := mcp.AsTextContent(res.Content[0]); ok {
				text = tc.Text
			}
		}
		if text == "" {
			t.Fatalf("expected text content in response")
		}

		if err := json.Unmarshal([]byte(text), &sendRes); err != nil {
			t.Fatalf("failed to unmarshal send message response: %v", err)
		}

		if sendRes["status"] != "queued" {
			t.Errorf("expected status 'queued', got %v", sendRes["status"])
		}
	})

	t.Run("GetAuditLogs", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleGetAuditLogs(ctx, req)
		if err != nil {
			t.Fatalf("handleGetAuditLogs returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleGetAuditLogs returned error result: %+v", res.Content)
		}
	})
}
