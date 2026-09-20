// Command pergo-seed bootstraps a local PerGo instance with rich, deterministic
// mock fixtures and zero real PII so that all platform views (Omnichannel Inbox,
// Broadcaster, Connections, Contacts, Templates, Scalar Docs) can be explored
// and captured at 1440x900 resolution without requiring live third-party provider accounts.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/obs"
)

// WABAConfig matches the credentials JSON expected by the WABA adapter.
type WABAConfig struct {
	PhoneNumberID string `json:"phone_number_id"`
	Token         string `json:"token"`
	WABAAccountID string `json:"waba_account_id"`
	VerifyToken   string `json:"verify_token"`
}

func main() {
	slog.SetDefault(slog.New(obs.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, nil))))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()

	if err := run(ctx, cfg); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed complete")
}

func run(ctx context.Context, cfg *config.Config) error {
	workspaceName := envOrDefault("PERGO_WORKSPACE", "PerGo Demo")

	var workspaceID *uuid.UUID
	if wsIDStr := envOrDefault("PERGO_DEV_WORKSPACE_ID", ""); wsIDStr != "" {
		if id, err := uuid.Parse(wsIDStr); err == nil && id != uuid.Nil {
			workspaceID = &id
		}
	}

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

	opts := SeedOptions{
		WorkspaceName: workspaceName,
		WorkspaceID:   workspaceID,
		Deterministic: true,
	}

	res, err := Seed(ctx, pool, encryptor, opts)
	if err != nil {
		return fmt.Errorf("seed harness: %w", err)
	}

	fmt.Println("\n=======================================================")
	fmt.Printf("✓ Seeded Workspace:  %q (%s)\n", res.Workspace.Name, res.Workspace.ID)
	fmt.Printf("✓ Channels/Connections: %d (WABA Cloud, WhatsApp Web, Telegram)\n", len(res.Connections))
	fmt.Printf("✓ API Keys Active:   %d (CRM & Omnichannel Ingest Key)\n", res.APIKeyCount)
	fmt.Printf("✓ Tags Configured:   %d (VIP, Lead Qualificado, Cliente Ativo, etc.)\n", len(res.Tags))
	fmt.Printf("✓ Contacts Seeded:   %d (with multi-channel identities & custom attributes)\n", len(res.Contacts))
	fmt.Printf("✓ Templates Seeded:  %d (approved Meta utility & marketing templates)\n", len(res.Templates))
	fmt.Printf("✓ Campaigns Seeded:  %d (with delivery metrics & recipient records)\n", len(res.Campaigns))
	fmt.Printf("✓ Conversations:     %d threads (%d total messages seeded)\n", res.ConversationCount, res.MessageCount)
	fmt.Println("✓ PII Safety:        100% synthetic mock fixtures, zero real PII")
	fmt.Println("=======================================================")
	fmt.Printf("\nKey Views for 1440x900 Screenshot Capture (see docs/SCREENSHOTS.md):\n")
	fmt.Printf("  • Live Omnichannel Inbox:  http://localhost:%s/admin/inbox\n", cfg.ServerPort)
	fmt.Printf("  • Campaign Broadcaster:    http://localhost:%s/admin/campaigns\n", cfg.ServerPort)
	fmt.Printf("  • Channel Connections:     http://localhost:%s/admin/connections\n", cfg.ServerPort)
	fmt.Printf("  • Contact Management:      http://localhost:%s/admin/contacts\n", cfg.ServerPort)
	fmt.Printf("  • WABA Template Manager:   http://localhost:%s/admin/templates\n", cfg.ServerPort)
	fmt.Printf("  • Scalar Interactive Docs: http://localhost:%s/docs\n", cfg.ServerPort)
	fmt.Printf("  • Operator Dashboard:      http://localhost:%s/admin\n\n", cfg.ServerPort)

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
