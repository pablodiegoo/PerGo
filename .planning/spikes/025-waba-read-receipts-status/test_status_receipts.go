package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// wabaStatus represents the status update object from Meta.
type wabaStatus struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
}

// wabaWebhookPayload represents the webhook structure.
type wabaWebhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				MessagingProduct string       `json:"messaging_product"`
				Statuses         []wabaStatus `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func main() {
	dbURL := os.Getenv("PERGO_DATABASE_URL")
	if dbURL == "" {
		// Fallback to local docker port 5433
		dbURL = "postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Connecting to test database at %s...", dbURL)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Database connection successful!")

	// 1. Run migration to ensure provider_message_id column exists
	log.Println("Migrating table message_dispatches to include provider_message_id...")
	_, err = pool.Exec(ctx, `
		ALTER TABLE message_dispatches 
		ADD COLUMN IF NOT EXISTS provider_message_id VARCHAR(255) UNIQUE;
	`)
	if err != nil {
		log.Fatalf("Failed to alter message_dispatches table: %v", err)
	}
	log.Println("Migration completed successfully!")

	// 2. Create a mock workspace and connection
	wsID := uuid.New()
	_, err = pool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, $2)", wsID, "Spike Workspace")
	if err != nil {
		log.Fatalf("Failed to insert mock workspace: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM workspaces WHERE id = $1", wsID)

	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, channel, sender_identity, status, credentials)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, connID, wsID, "Test Connection", "whatsapp_cloud", "1234567", "connected", []byte("{}"))
	if err != nil {
		log.Fatalf("Failed to insert connection: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM connections WHERE id = $1", connID)

	// 3. Create a mock message dispatch
	traceID := "test_status_trace_" + uuid.New().String()[:8]
	dispatchID := uuid.New()
	log.Printf("Creating mock message dispatch record (trace_id: %s)...", traceID)
	_, err = pool.Exec(ctx, `
		INSERT INTO message_dispatches (id, workspace_id, trace_id, current_channel, status, fallback_index)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, dispatchID, wsID, traceID, "whatsapp_cloud", "queued", 0)
	if err != nil {
		log.Fatalf("Failed to insert message dispatch: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM message_dispatches WHERE id = $1", dispatchID)

	// Simulate outbound dispatch response which provides the wamid
	mockWABAID := "wamid.HBgLNTUxMTk5OTk5ODg4OBIVAgY0NkE0NkUzQzc5RjNFRDIyMkUAAg=="
	log.Printf("Simulating Meta response wamid: %s, updating dispatch provider_message_id...", mockWABAID)
	_, err = pool.Exec(ctx, `
		UPDATE message_dispatches 
		SET provider_message_id = $1, status = 'sent', updated_at = now()
		WHERE id = $2
	`, mockWABAID, dispatchID)
	if err != nil {
		log.Fatalf("Failed to update dispatch provider_message_id: %v", err)
	}

	// 4. Simulate receiving WABA status update webhook event
	mockWebhookPayload := `{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"id": "12345",
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"statuses": [
								{
									"id": "` + mockWABAID + `",
									"status": "delivered",
									"timestamp": "1675276634",
									"recipient_id": "5511999998888"
								}
							]
						}
					}
				]
			}
		]
	}`

	log.Println("Simulating webhook payload parsing...")
	var payload wabaWebhookPayload
	if err := json.Unmarshal([]byte(mockWebhookPayload), &payload); err != nil {
		log.Fatalf("Failed to parse mock webhook payload: %v", err)
	}

	// Extract status updates
	var statusUpdates []wabaStatus
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field == "messages" {
				statusUpdates = append(statusUpdates, change.Value.Statuses...)
			}
		}
	}

	if len(statusUpdates) == 0 {
		log.Fatalf("No status updates found in payload!")
	}
	log.Printf("Extracted %d status update event(s).", len(statusUpdates))

	// 5. Update database using the status update
	for _, su := range statusUpdates {
		log.Printf("Processing status update: message_id=%s status=%s recipient=%s", su.ID, su.Status, su.RecipientID)
		
		tag, err := pool.Exec(ctx, `
			UPDATE message_dispatches
			SET status = $1, updated_at = now()
			WHERE provider_message_id = $2
		`, su.Status, su.ID)
		if err != nil {
			log.Fatalf("Failed to update status from webhook event: %v", err)
		}

		if tag.RowsAffected() == 0 {
			log.Fatalf("No message dispatch record updated for provider_message_id: %s", su.ID)
		}
		log.Printf("Successfully updated status of dispatch record to '%s'!", su.Status)
	}

	// 6. Verify record is updated in the database
	var finalStatus string
	err = pool.QueryRow(ctx, "SELECT status FROM message_dispatches WHERE id = $1", dispatchID).Scan(&finalStatus)
	if err != nil {
		log.Fatalf("Failed to query final status: %v", err)
	}

	if finalStatus != "delivered" {
		log.Fatalf("Verification failed: expected status 'delivered', got '%s'", finalStatus)
	}

	fmt.Printf("SUCCESS: Spike validated successfully! Message dispatch status is '%s'.\n", finalStatus)
}
