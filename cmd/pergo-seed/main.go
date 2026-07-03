// Command pergo-seed bootstraps a local PerGo instance with WABA connection
// data and sample conversations so the admin inbox can be exercised without
// a live provider webhook. Reads credentials from .env.seed.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABAConfig matches the credentials JSON expected by the WABA adapter.
type WABAConfig struct {
	PhoneNumberID string `json:"phone_number_id"`
	Token         string `json:"token"`
	WABAAccountID string `json:"waba_account_id"`
	VerifyToken   string `json:"verify_token"`
}

// inboundPayload matches the audit_logs event payload for inbound messages.
type inboundPayload struct {
	Event       string `json:"event"`
	TraceID     string `json:"trace_id"`
	MessageID   string `json:"message_id"`
	Channel     string `json:"channel"`
	Timestamp   string `json:"timestamp"`
	WorkspaceID string `json:"workspace_id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Body        string `json:"body"`
}

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if err := run(ctx, cfg); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed complete")
}

func run(ctx context.Context, cfg *config.Config) error {
	// Resolve workspace name from .env.seed (PERGO_WORKSPACE).
	workspaceName := envOrDefault("PERGO_WORKSPACE", "Agora")

	// Resolve WABA credentials from .env.seed.
	token := envOrDefault("ACCESS_TOKEN", "")
	phoneID := envOrDefault("PHONE_NUMBER_ID", "")
	wabaID := envOrDefault("WHATSAPP_BUSINESS_ACCOUNT_ID", "")
	if token == "" || phoneID == "" || wabaID == "" {
		return fmt.Errorf("missing WABA credentials in .env.seed: need ACCESS_TOKEN, PHONE_NUMBER_ID, WHATSAPP_BUSINESS_ACCOUNT_ID")
	}

	// Derive a display phone number used as the recipient identity.
	// For WABA the recipient identity is the DisplayPhoneNumber (a real E.164).
	// Seed a placeholder derived from the phone_number_id if none is known.
	displayPhone := envOrDefault("DISPLAY_PHONE_NUMBER", "")

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	kek := cfg.KEKBytes
	if len(kek) != 32 {
		kek = make([]byte, 32)
		copy(kek, []byte("dev-development-key-32-bytes-kek"))
	}
	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		return fmt.Errorf("init encryptor: %w", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, encryptor)
	sessRepo := repository.NewRecipientSessionRepository(pool)

	// 1. Workspace (create or reuse).
	ws, err := findOrCreateWorkspace(ctx, wsRepo, workspaceName)
	if err != nil {
		return fmt.Errorf("workspace setup: %w", err)
	}
	slog.Info("workspace ready", "id", ws.ID, "name", ws.Name)

	// 2. WABA connection (create if missing for this workspace).
	credJSON, _ := json.Marshal(WABAConfig{
		PhoneNumberID: phoneID,
		Token:         token,
		WABAAccountID: wabaID,
		VerifyToken:   "pergo-verify-token",
	})
	conn, err := ensureConnection(ctx, connRepo, ws.ID, displayPhone, credJSON)
	if err != nil {
		return fmt.Errorf("connection setup: %w", err)
	}
	slog.Info("connection ready", "id", conn.ID, "channel", conn.Channel, "sender", conn.SenderIdentity)

	// 3. Seed sample conversations (two contacts) so the inbox has content.
	if err := seedConversations(ctx, pool, sessRepo, ws.ID, conn); err != nil {
		return fmt.Errorf("seed conversations: %w", err)
	}

	fmt.Printf("\nSeeded workspace %q (%s)\n", ws.Name, ws.ID)
	fmt.Printf("Seeded WABA connection %q (%s) sender_identity=%s\n", conn.Name, conn.ID, conn.SenderIdentity)
	fmt.Printf("Inbox URL: http://localhost:%s/admin/inbox\n", cfg.ServerPort)
	fmt.Println("To trigger an inbound (toast test), POST a WABA webhook to:")
	fmt.Printf("  POST http://localhost:%s/webhooks/waba/%s\n", cfg.ServerPort, ws.ID)
	return nil
}

func findOrCreateWorkspace(ctx context.Context, wsRepo *repository.WorkspaceRepository, name string) (*repository.Workspace, error) {
	list, err := wsRepo.List(ctx, 100)
	if err == nil {
		for i := range list {
			if list[i].Name == name {
				return &list[i], nil
			}
		}
	}
	return wsRepo.Create(ctx, name)
}

func ensureConnection(ctx context.Context, repo *repository.ConnectionRepository, wsID uuid.UUID, displayPhone string, cred []byte) (*repository.Connection, error) {
	existing, err := repo.ListByWorkspace(ctx, wsID)
	if err == nil {
		for _, c := range existing {
			if c.Channel == "whatsapp_cloud" {
				return c, nil
			}
		}
	}
	sender := displayPhone
	if sender == "" {
		sender = "15551357931" // placeholder display number for seed
	}
	conn := &repository.Connection{
		WorkspaceID:    wsID,
		Name:           "WABA Seed",
		Channel:        "whatsapp_cloud",
		SenderIdentity: sender,
		Status:         "connected",
		IsDefault:      true,
		Credentials:    cred,
	}
	if err := repo.Create(ctx, conn); err != nil {
		return nil, err
	}
	return conn, nil
}

func seedConversations(ctx context.Context, pool *pgxpool.Pool, sessRepo *repository.RecipientSessionRepository, wsID uuid.UUID, conn *repository.Connection) error {
	now := time.Now().UTC()
	contacts := []struct {
		from string
		body string
	}{
		{"15551234567", "Oi, tudo bem? Vi o anúncio de vocês."},
		{"15551234567", "Quanto custa o plano mensal?"},
		{"15557654321", "Bom dia! Gostaria de tirar uma dúvida."},
	}
	recipientIdentity := conn.SenderIdentity

	for _, ct := range contacts {
		trace := uuid.New().String()
		payload, _ := json.Marshal(inboundPayload{
			Event:       "inbound_message",
			TraceID:     trace,
			MessageID:   "wamid." + randToken(8),
			Channel:     "whatsapp_cloud",
			Timestamp:   now.Format(time.RFC3339),
			WorkspaceID: wsID.String(),
			From:        ct.from,
			To:          recipientIdentity,
			Body:        ct.body,
		})
		// Insert directly via pool so we don't depend on the buffered writer.
		_, err := pool.Exec(ctx,
			`INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at) VALUES ($1, $2, 'inbound_message', $3, $4)`,
			wsID, trace, payload, now.Add(-time.Duration(rand.Intn(3600))*time.Second))
		if err != nil {
			return fmt.Errorf("insert audit log: %w", err)
		}
		// Upsert recipient session so the window checker and unread tracking work.
		_ = sessRepo.Upsert(ctx, wsID, ct.from, "whatsapp_cloud", recipientIdentity, now)
		slog.Info("seeded inbound", "from", ct.from, "body", ct.body)
	}
	return nil
}

func randToken(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
