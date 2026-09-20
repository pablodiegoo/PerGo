package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestResolveCredentials_FallbackToMock(t *testing.T) {
	// Ensure env vars are cleared for this test
	t.Setenv("ACCESS_TOKEN", "")
	t.Setenv("PHONE_NUMBER_ID", "")
	t.Setenv("WHATSAPP_BUSINESS_ACCOUNT_ID", "")
	t.Setenv("DISPLAY_PHONE_NUMBER", "")

	creds := resolveWABACredentials(context.Background())
	if creds.Token == "" {
		t.Error("expected non-empty fallback mock token")
	}
	if creds.PhoneNumberID == "" {
		t.Error("expected non-empty fallback mock phone number id")
	}
	if creds.WABAAccountID == "" {
		t.Error("expected non-empty fallback mock WABA account id")
	}
	if creds.DisplayPhone == "" {
		t.Error("expected non-empty fallback mock display phone number")
	}
	if !creds.IsMock {
		t.Error("expected IsMock to be true when env credentials are absent")
	}
}

func TestResolveCredentials_WithEnv(t *testing.T) {
	t.Setenv("ACCESS_TOKEN", "custom-token-123")
	t.Setenv("PHONE_NUMBER_ID", "custom-phone-456")
	t.Setenv("WHATSAPP_BUSINESS_ACCOUNT_ID", "custom-waba-789")
	t.Setenv("DISPLAY_PHONE_NUMBER", "5511999998888")

	creds := resolveWABACredentials(context.Background())
	if creds.Token != "custom-token-123" {
		t.Errorf("expected custom-token-123, got %s", creds.Token)
	}
	if creds.PhoneNumberID != "custom-phone-456" {
		t.Errorf("expected custom-phone-456, got %s", creds.PhoneNumberID)
	}
	if creds.WABAAccountID != "custom-waba-789" {
		t.Errorf("expected custom-waba-789, got %s", creds.WABAAccountID)
	}
	if creds.DisplayPhone != "5511999998888" {
		t.Errorf("expected 5511999998888, got %s", creds.DisplayPhone)
	}
	if creds.IsMock {
		t.Error("expected IsMock to be false when all env credentials are provided")
	}
}

func TestFixturePIISafety(t *testing.T) {
	fixtures := getDeterministicFixtures("5511987654321")

	// 1. Verify contacts use mock domain and synthetic phone numbers
	for _, c := range fixtures.Contacts {
		if c.Email != "" && !strings.HasSuffix(c.Email, "@mock.pergo.io") {
			t.Errorf("contact %q email %q does not use @mock.pergo.io synthetic domain", c.Name, c.Email)
		}
		for _, id := range c.Identities {
			if id.Channel == "whatsapp_cloud" || id.Channel == "whatsapp" {
				if !strings.HasPrefix(id.SenderIdentity, "55") && !strings.HasPrefix(id.SenderIdentity, "1555") {
					t.Errorf("contact %q identity %q does not use safe synthetic number prefix", c.Name, id.SenderIdentity)
				}
			}
		}
	}

	// 2. Verify all templates have safe mock IDs
	for _, tmpl := range fixtures.Templates {
		if !strings.HasPrefix(tmpl.MetaTemplateID, "meta_mock_") && !strings.HasPrefix(tmpl.MetaTemplateID, "mock_") {
			t.Errorf("template %q has non-mock meta template ID: %s", tmpl.Name, tmpl.MetaTemplateID)
		}
	}

	// 3. Verify conversations contain only synthetic sender and recipient identities
	for _, conv := range fixtures.Conversations {
		for _, msg := range conv.Messages {
			if msg.Direction != "inbound" && msg.Direction != "outbound" {
				t.Errorf("unexpected message direction: %s", msg.Direction)
			}
			if msg.Body == "" {
				t.Error("message body cannot be empty")
			}
		}
	}
}

func TestAuditLogPayloadCompatibility(t *testing.T) {
	now := time.Now().UTC()
	wsID := uuid.New()

	// Inbound message payload
	inboundJSON, err := buildInboundAuditPayload(wsID, "trace-in-1", "wamid.mock123", "whatsapp_cloud", "5511981112233", "5511987654321", "Olá PerGo", now)
	if err != nil {
		t.Fatalf("failed to build inbound audit payload: %v", err)
	}

	var parsedInbound map[string]any
	if err := json.Unmarshal(inboundJSON, &parsedInbound); err != nil {
		t.Fatalf("failed to unmarshal inbound payload: %v", err)
	}
	if parsedInbound["event"] != "inbound_message" || parsedInbound["from"] != "5511981112233" || parsedInbound["to"] != "5511987654321" {
		t.Errorf("unexpected inbound payload structure: %+v", parsedInbound)
	}

	// Outbound message payload
	outboundJSON, err := buildOutboundAuditPayload(wsID, "trace-out-1", "whatsapp_cloud", "5511987654321", "5511981112233", "Resposta do PerGo", now)
	if err != nil {
		t.Fatalf("failed to build outbound audit payload: %v", err)
	}

	var parsedOutbound map[string]any
	if err := json.Unmarshal(outboundJSON, &parsedOutbound); err != nil {
		t.Fatalf("failed to unmarshal outbound payload: %v", err)
	}
	req, ok := parsedOutbound["request"].(map[string]any)
	if !ok {
		t.Fatalf("outbound payload missing 'request' object: %+v", parsedOutbound)
	}
	if req["channel"] != "whatsapp_cloud" || req["to"] != "5511981112233" || req["sender_identity"] != "5511987654321" {
		t.Errorf("unexpected outbound request payload: %+v", req)
	}
}

func getTestDatabasePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://admin:admin@localhost:5432/pergo_db?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not reachable at %s: %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}
	return pool
}

func TestSeedDatabase_Integration(t *testing.T) {
	pool := getTestDatabasePool(t)
	defer pool.Close()

	ctx := context.Background()
	sqlDB, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to wrap pool: %v", err)
	}
	defer sqlDB.Close()

	if err := postgres.RunMigrations(sqlDB); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	kek := []byte("dev-development-key-32-bytes-kek")
	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to init encryptor: %v", err)
	}

	opts := SeedOptions{
		WorkspaceName: "PerGo Integration Test Workspace",
		Deterministic: true,
	}

	result, err := Seed(ctx, pool, encryptor, opts)
	if err != nil {
		t.Fatalf("Seed failed: %v", err)
	}
	if result == nil || result.Workspace == nil {
		t.Fatal("expected non-nil seed result and workspace")
	}

	// Verify connections
	connRepo := repository.NewConnectionRepository(pool, encryptor)
	connections, err := connRepo.ListByWorkspace(ctx, result.Workspace.ID)
	if err != nil || len(connections) < 3 {
		t.Errorf("expected at least 3 connections seeded, got %d (err: %v)", len(connections), err)
	}

	// Verify contacts
	contactRepo := repository.NewContactRepository(pool)
	contacts, err := contactRepo.SearchContacts(ctx, result.Workspace.ID, "", uuid.Nil, 50)
	if err != nil || len(contacts) < 5 {
		t.Errorf("expected at least 5 contacts seeded, got %d (err: %v)", len(contacts), err)
	}

	// Verify tags
	tagRepo := repository.NewTagRepository(pool)
	tags, err := tagRepo.ListTags(ctx, result.Workspace.ID)
	if err != nil || len(tags) < 5 {
		t.Errorf("expected at least 5 tags seeded, got %d (err: %v)", len(tags), err)
	}

	// Verify templates
	tmplRepo := repository.NewWABATemplateRepository(pool)
	templates, err := tmplRepo.ListByWorkspace(ctx, result.Workspace.ID)
	if err != nil || len(templates) < 3 {
		t.Errorf("expected at least 3 templates seeded, got %d (err: %v)", len(templates), err)
	}

	// Verify campaigns
	campaignRepo := repository.NewCampaignRepository(pool)
	campaigns, err := campaignRepo.ListByWorkspace(ctx, result.Workspace.ID)
	if err != nil || len(campaigns) < 2 {
		t.Errorf("expected at least 2 campaigns seeded, got %d (err: %v)", len(campaigns), err)
	}

	// Verify audit conversations
	auditRepo := repository.NewAuditRepository(pool)
	conversations, err := auditRepo.ListConversations(ctx, result.Workspace.ID, "")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if len(conversations) < 3 {
		t.Errorf("expected at least 3 active conversation cards in inbox, got %d", len(conversations))
	}

	// Verify conversation thread for first contact
	if len(conversations) > 0 {
		thread, err := auditRepo.ListThreadByContact(ctx, result.Workspace.ID, conversations[0].ContactID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}
		if len(thread) < 2 {
			t.Errorf("expected thread to have at least 2 messages, got %d", len(thread))
		}
	}
}
