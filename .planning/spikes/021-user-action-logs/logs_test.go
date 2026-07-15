package userlogs_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserActionLog maps to the proposed audit log record in Go.
type UserActionLog struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id"`
	ActorType   string         `json:"actor_type"`
	ActorID     string         `json:"actor_id"`
	ActorName   string         `json:"actor_name"`
	Action      string         `json:"action"`
	Source      string         `json:"source"`
	IPAddress   string         `json:"ip_address"`
	UserAgent   string         `json:"user_agent"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
}

func TestUserActionLogsSchemaAndInsertion(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL database not available: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL ping failed: %v", err)
	}

	// 1. Create a temporary test table
	t.Log("Creating temporary table user_action_logs_test...")
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS user_action_logs_test (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			actor_type VARCHAR(50) NOT NULL,
			actor_id VARCHAR(255) NOT NULL,
			actor_name VARCHAR(255) NOT NULL,
			action VARCHAR(100) NOT NULL,
			source VARCHAR(50) NOT NULL,
			ip_address VARCHAR(45),
			user_agent TEXT,
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS idx_user_action_logs_test_workspace_date 
		ON user_action_logs_test(workspace_id, created_at DESC);
	`)
	if err != nil {
		t.Fatalf("Failed to create temporary table: %v", err)
	}
	defer func() {
		t.Log("Dropping temporary table...")
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS user_action_logs_test;")
	}()

	// 2. Prepare test inputs
	workspaceID := uuid.New()
	apiKeyID := uuid.New().String()

	logsToInsert := []UserActionLog{
		{
			WorkspaceID: workspaceID,
			ActorType:   "user",
			ActorID:     "admin@pergo.dev",
			ActorName:   "Administrador Geral",
			Action:      "campaign.create",
			Source:      "dashboard",
			IPAddress:   "192.168.15.22",
			UserAgent:   "Chrome/126.0",
			Metadata: map[string]any{
				"campaign_name": "Father's Day Promo",
				"recipients":    150,
			},
		},
		{
			WorkspaceID: workspaceID,
			ActorType:   "api_key",
			ActorID:     apiKeyID,
			ActorName:   "CRM API (ab12cd34)",
			Action:      "message.send",
			Source:      "api",
			IPAddress:   "34.205.10.88",
			UserAgent:   "Go-http-client/1.1",
			Metadata: map[string]any{
				"recipient": "+5511999999999",
				"channel":   "whatsapp",
			},
		},
	}

	// 3. Insert records
	for _, log := range logsToInsert {
		metaBytes, err := json.Marshal(log.Metadata)
		if err != nil {
			t.Fatalf("Failed to marshal metadata: %v", err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO user_action_logs_test 
			(workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, log.WorkspaceID, log.ActorType, log.ActorID, log.ActorName, log.Action, log.Source, log.IPAddress, log.UserAgent, metaBytes)
		if err != nil {
			t.Fatalf("Failed to insert log entry: %v", err)
		}
	}

	// 4. Query and verify results
	rows, err := pool.Query(ctx, `
		SELECT id, workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata, created_at 
		FROM user_action_logs_test 
		WHERE workspace_id = $1 
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		t.Fatalf("Failed to query log entries: %v", err)
	}
	defer rows.Close()

	var queriedLogs []UserActionLog
	for rows.Next() {
		var l UserActionLog
		var metaBytes []byte
		err := rows.Scan(&l.ID, &l.WorkspaceID, &l.ActorType, &l.ActorID, &l.ActorName, &l.Action, &l.Source, &l.IPAddress, &l.UserAgent, &metaBytes, &l.CreatedAt)
		if err != nil {
			t.Fatalf("Failed to scan log row: %v", err)
		}

		err = json.Unmarshal(metaBytes, &l.Metadata)
		if err != nil {
			t.Fatalf("Failed to unmarshal JSONB metadata: %v", err)
		}

		queriedLogs = append(queriedLogs, l)
	}

	if len(queriedLogs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(queriedLogs))
	}

	// Verify details of the first queried log (should be the API key one due to ORDER BY created_at DESC)
	apiLog := queriedLogs[0]
	if apiLog.ActorType != "api_key" {
		t.Errorf("Expected actor_type 'api_key', got '%s'", apiLog.ActorType)
	}
	if apiLog.Source != "api" {
		t.Errorf("Expected source 'api', got '%s'", apiLog.Source)
	}
	if apiLog.Metadata["channel"] != "whatsapp" {
		t.Errorf("Expected metadata channel 'whatsapp', got '%v'", apiLog.Metadata["channel"])
	}

	t.Log("Successfully validated polymorphic schema, composite index, and query flow for user_action_logs!")
}
