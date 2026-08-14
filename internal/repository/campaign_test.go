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

func TestCampaignRepository_ClaimDueScheduledCampaigns(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "campaign_test_ws_claim_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	now := time.Now().UTC()
	past5Min := now.Add(-5 * time.Minute)
	past1Min := now.Add(-1 * time.Minute)
	future1Hr := now.Add(1 * time.Hour)

	// Due scheduled campaign 1
	c1, err := repo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due Camp 1",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past5Min,
	})
	if err != nil {
		t.Fatalf("failed to create c1: %v", err)
	}

	// Due scheduled campaign 2
	c2, err := repo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due Camp 2",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past1Min,
	})
	if err != nil {
		t.Fatalf("failed to create c2: %v", err)
	}

	// Future scheduled campaign (not due)
	c3, err := repo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Future Camp",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &future1Hr,
	})
	if err != nil {
		t.Fatalf("failed to create c3: %v", err)
	}

	// Draft campaign with past scheduled_at (not status=scheduled)
	c4, err := repo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Draft Past Camp",
		Status:      domain.CampaignStatusDraft,
		ScheduledAt: &past5Min,
	})
	if err != nil {
		t.Fatalf("failed to create c4: %v", err)
	}

	// Claim due campaigns
	claimed, err := repo.ClaimDueScheduledCampaigns(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueScheduledCampaigns failed: %v", err)
	}

	// Should contain c1 and c2
	claimedIDs := make(map[uuid.UUID]bool)
	for _, c := range claimed {
		claimedIDs[c.ID] = true
	}

	if !claimedIDs[c1.ID] || !claimedIDs[c2.ID] {
		t.Errorf("expected c1 and c2 to be claimed, got IDs: %v", claimedIDs)
	}
	if claimedIDs[c3.ID] {
		t.Errorf("future campaign c3 was claimed unexpectedly")
	}
	if claimedIDs[c4.ID] {
		t.Errorf("draft campaign c4 was claimed unexpectedly")
	}

	// Verify statuses in DB
	c1Fetched, _ := repo.GetByID(ctx, c1.ID)
	if c1Fetched.Status != domain.CampaignStatusSending {
		t.Errorf("expected c1 status 'sending', got %s", c1Fetched.Status)
	}
	c2Fetched, _ := repo.GetByID(ctx, c2.ID)
	if c2Fetched.Status != domain.CampaignStatusSending {
		t.Errorf("expected c2 status 'sending', got %s", c2Fetched.Status)
	}
	c3Fetched, _ := repo.GetByID(ctx, c3.ID)
	if c3Fetched.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected c3 status 'scheduled', got %s", c3Fetched.Status)
	}

	// Subsequent claim call should return 0 due campaigns for these
	claimedSecond, err := repo.ClaimDueScheduledCampaigns(ctx, now, 10)
	if err != nil {
		t.Fatalf("second ClaimDueScheduledCampaigns failed: %v", err)
	}
	for _, c := range claimedSecond {
		if c.ID == c1.ID || c.ID == c2.ID || c.ID == c3.ID || c.ID == c4.ID {
			t.Errorf("campaign %v claimed twice", c.ID)
		}
	}
}

func TestCampaignRepository_ClaimDueScheduledCampaigns_Concurrency(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "campaign_test_ws_claim_concurrent_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	now := time.Now().UTC()
	past := now.Add(-10 * time.Minute)

	numCampaigns := 10
	campaignIDs := make(map[uuid.UUID]bool)
	for i := 0; i < numCampaigns; i++ {
		c, err := repo.Create(ctx, &domain.Campaign{
			WorkspaceID: ws.ID,
			Name:        "Concurrent Camp",
			Status:      domain.CampaignStatusScheduled,
			ScheduledAt: &past,
		})
		if err != nil {
			t.Fatalf("failed to create campaign: %v", err)
		}
		campaignIDs[c.ID] = true
	}

	// 5 concurrent workers attempting to claim
	numWorkers := 5
	resultsChan := make(chan []domain.Campaign, numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			claimed, _ := repo.ClaimDueScheduledCampaigns(ctx, now, 10)
			resultsChan <- claimed
		}()
	}

	claimedMap := make(map[uuid.UUID]int)
	for i := 0; i < numWorkers; i++ {
		claimedList := <-resultsChan
		for _, c := range claimedList {
			if campaignIDs[c.ID] {
				claimedMap[c.ID]++
			}
		}
	}

	if len(claimedMap) != numCampaigns {
		t.Errorf("expected %d unique campaigns claimed, got %d", numCampaigns, len(claimedMap))
	}
	for id, count := range claimedMap {
		if count > 1 {
			t.Errorf("campaign %s was claimed %d times (race condition!)", id, count)
		}
	}
}



