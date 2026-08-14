package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestCampaignRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "campaign_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	tmplName := "hello_world"
	channel := "whatsapp"

	// 1. Create
	c := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Promo Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    50,
		DelaySeconds: 2,
		TemplateName: &tmplName,
		Channel:      &channel,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999998888", Variables: map[string]string{"nome": "João"}},
			{To: "5511999997777", Variables: map[string]string{"nome": "Maria"}},
		},
		SkippedRows: []domain.SkippedRow{
			{LineNumber: 3, RawInput: "invalid_phone,foo", Reason: "invalid phone format"},
		},
	}

	created, err := repo.Create(ctx, c)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Errorf("expected generated UUID, got Nil")
	}
	if created.Name != c.Name {
		t.Errorf("expected Name %s, got %s", c.Name, created.Name)
	}
	if len(created.Recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(created.Recipients))
	}
	if len(created.SkippedRows) != 1 {
		t.Errorf("expected 1 skipped row, got %d", len(created.SkippedRows))
	}

	// 2. GetByID
	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get campaign: %v", err)
	}
	if fetched.Name != c.Name {
		t.Errorf("expected Name %s, got %s", c.Name, fetched.Name)
	}

	// 3. UpdateStatus
	err = repo.UpdateStatus(ctx, created.ID, domain.CampaignStatusSending)
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	fetched, _ = repo.GetByID(ctx, created.ID)
	if fetched.Status != domain.CampaignStatusSending {
		t.Errorf("expected status 'sending', got %s", fetched.Status)
	}

	// 4. UpdateRecipients
	newRecipients := []domain.CampaignRecipient{
		{To: "5511999996666", Variables: map[string]string{"nome": "José"}},
	}
	err = repo.UpdateRecipients(ctx, created.ID, newRecipients, nil)
	if err != nil {
		t.Fatalf("failed to update recipients: %v", err)
	}

	fetched, _ = repo.GetByID(ctx, created.ID)
	if len(fetched.Recipients) != 1 || fetched.Recipients[0].To != "5511999996666" {
		t.Errorf("expected 1 recipient 5511999996666, got %v", fetched.Recipients)
	}

	// 5. ListByWorkspace
	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list campaigns: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 campaign, got %d", len(list))
	}

	// 6. Delete
	err = repo.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to delete campaign: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if err == nil {
		t.Errorf("expected error fetching deleted campaign, got nil")
	}
}

func TestCampaignRepositoryScheduledAt(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "campaign_test_ws_sched_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	schedTime := time.Now().Add(1 * time.Hour)
	c := &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Scheduled Camp",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &schedTime,
	}

	created, err := repo.Create(ctx, c)
	if err != nil {
		t.Fatalf("failed to create scheduled campaign: %v", err)
	}

	fetched, _ := repo.GetByID(ctx, created.ID)
	if fetched.ScheduledAt == nil || !fetched.ScheduledAt.Truncate(time.Second).Equal(schedTime.Truncate(time.Second)) {
		t.Errorf("expected ScheduledAt %v, got %v", schedTime, fetched.ScheduledAt)
	}
}

func TestCampaignRepository_AddRecipients_Idempotent(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "campaign_test_ws_recips_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	c, err := repo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Idempotent Recipients Camp",
		Status:      domain.CampaignStatusDraft,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	records := []domain.CampaignRecipientRecord{
		{Phone: "5511999991111", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "User 1"}},
		{Phone: "5511999992222", Status: domain.RecipientStatusSkipped, Variables: map[string]string{"name": "User 2"}},
	}

	// First insert
	if err := repo.AddRecipients(ctx, c.ID, records); err != nil {
		t.Fatalf("first AddRecipients failed: %v", err)
	}

	// Re-insert same recipients (simulate retry / crash resumption)
	if err := repo.AddRecipients(ctx, c.ID, records); err != nil {
		t.Fatalf("second AddRecipients should be idempotent but failed: %v", err)
	}

	// Check total recipients
	fetched, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("failed to get campaign: %v", err)
	}
	if fetched.TotalRecipients != 2 {
		t.Errorf("expected total_recipients 2, got %d", fetched.TotalRecipients)
	}

	// List all
	allRecs, err := repo.ListRecipients(ctx, c.ID, nil, 10)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Errorf("expected 2 recipients in table, got %d", len(allRecs))
	}
}

func TestCampaignRepository_RateLimitPerMin(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "campaign_test_ws_ratelimit_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	rateLimit := 60
	c := &domain.Campaign{
		WorkspaceID:     ws.ID,
		Name:            "Rate Limited Camp",
		Status:          domain.CampaignStatusDraft,
		BatchSize:       100,
		DelaySeconds:    5,
		RateLimitPerMin: &rateLimit,
	}

	created, err := repo.Create(ctx, c)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	if created.RateLimitPerMin == nil || *created.RateLimitPerMin != 60 {
		t.Fatalf("expected created RateLimitPerMin 60, got %v", created.RateLimitPerMin)
	}

	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to fetch campaign by ID: %v", err)
	}
	if fetched.RateLimitPerMin == nil || *fetched.RateLimitPerMin != 60 {
		t.Fatalf("expected fetched RateLimitPerMin 60, got %v", fetched.RateLimitPerMin)
	}

	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list campaigns: %v", err)
	}
	if len(list) != 1 || list[0].RateLimitPerMin == nil || *list[0].RateLimitPerMin != 60 {
		t.Fatalf("expected listed campaign RateLimitPerMin 60, got %+v", list)
	}
}


