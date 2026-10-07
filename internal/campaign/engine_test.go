package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestEngine_StateTransitions_Valid(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "ws_trans_valid_"+uuid.New().String())
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	fakePub := NewFakePublisher()
	fakeAudit := newFakeAuditWriter()
	mockLister := newMockTagLister()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, fakeAudit, mockLister)

	// 1. draft -> sending via Start
	c1, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Draft to Sending",
		Status:      domain.CampaignStatusDraft,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	started, err := engine.Start(ctx, scope, c1.ID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != domain.CampaignStatusSending {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusSending, started.Status)
	}
	// Verify start message was published
	startMsgs := fakePub.MessagesBySubject("campaigns.start")
	if len(startMsgs) != 1 {
		t.Fatalf("expected 1 start message published, got %d", len(startMsgs))
	}

	// 2. sending -> paused via Pause
	paused, err := engine.Pause(ctx, scope, c1.ID)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused.Status != domain.CampaignStatusPaused {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusPaused, paused.Status)
	}

	// 3. paused -> sending via Resume (with pending recipients)
	_ = campRepo.AddRecipients(ctx, c1.ID, []domain.CampaignRecipientRecord{
		{Phone: "5511999990001", Status: domain.RecipientStatusPending},
	})
	resumed, err := engine.Resume(ctx, scope, c1.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != domain.CampaignStatusSending {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusSending, resumed.Status)
	}
	batchMsgs := fakePub.MessagesBySubject("campaigns.batches")
	if len(batchMsgs) != 1 {
		t.Fatalf("expected 1 batch message published on resume, got %d", len(batchMsgs))
	}

	// 4. sending -> cancelled via Cancel
	cancelled, err := engine.Cancel(ctx, scope, c1.ID)
	if err != nil {
		t.Fatalf("Cancel from sending: %v", err)
	}
	if cancelled.Status != domain.CampaignStatusCancelled {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusCancelled, cancelled.Status)
	}

	// 5. scheduled -> cancelled via Cancel
	c2, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Scheduled to Cancelled",
		Status:      domain.CampaignStatusScheduled,
	})
	if err != nil {
		t.Fatalf("create scheduled campaign: %v", err)
	}
	cancelledSched, err := engine.Cancel(ctx, scope, c2.ID)
	if err != nil {
		t.Fatalf("Cancel from scheduled: %v", err)
	}
	if cancelledSched.Status != domain.CampaignStatusCancelled {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusCancelled, cancelledSched.Status)
	}

	// 6. paused -> cancelled via Cancel
	c3, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Paused to Cancelled",
		Status:      domain.CampaignStatusPaused,
	})
	if err != nil {
		t.Fatalf("create paused campaign: %v", err)
	}
	cancelledPaused, err := engine.Cancel(ctx, scope, c3.ID)
	if err != nil {
		t.Fatalf("Cancel from paused: %v", err)
	}
	if cancelledPaused.Status != domain.CampaignStatusCancelled {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusCancelled, cancelledPaused.Status)
	}

	// 7. scheduled -> sending via Start
	c4, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Scheduled to Sending",
		Status:      domain.CampaignStatusScheduled,
	})
	if err != nil {
		t.Fatalf("create scheduled campaign: %v", err)
	}
	startedSched, err := engine.Start(ctx, scope, c4.ID)
	if err != nil {
		t.Fatalf("Start from scheduled: %v", err)
	}
	if startedSched.Status != domain.CampaignStatusSending {
		t.Errorf("expected status %s, got %s", domain.CampaignStatusSending, startedSched.Status)
	}
}

func TestEngine_StateTransitions_Invalid_TableDriven(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "ws_trans_invalid_"+uuid.New().String())
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	tests := []struct {
		name          string
		initialStatus domain.CampaignStatus
		action        string // "start", "pause", "resume", "cancel"
		expectedTo    domain.CampaignStatus
	}{
		// Illegal actions from draft
		{name: "draft cannot pause", initialStatus: domain.CampaignStatusDraft, action: "pause", expectedTo: domain.CampaignStatusPaused},
		{name: "draft cannot resume", initialStatus: domain.CampaignStatusDraft, action: "resume", expectedTo: domain.CampaignStatusSending},
		{name: "draft cannot cancel", initialStatus: domain.CampaignStatusDraft, action: "cancel", expectedTo: domain.CampaignStatusCancelled},

		// Illegal actions from scheduled (start is allowed; pause and resume are illegal)
		{name: "scheduled cannot pause", initialStatus: domain.CampaignStatusScheduled, action: "pause", expectedTo: domain.CampaignStatusPaused},
		{name: "scheduled cannot resume", initialStatus: domain.CampaignStatusScheduled, action: "resume", expectedTo: domain.CampaignStatusSending},

		// Illegal actions from sending
		{name: "sending cannot start again", initialStatus: domain.CampaignStatusSending, action: "start", expectedTo: domain.CampaignStatusSending},
		{name: "sending cannot resume", initialStatus: domain.CampaignStatusSending, action: "resume", expectedTo: domain.CampaignStatusSending},

		// Illegal actions from paused
		{name: "paused cannot start", initialStatus: domain.CampaignStatusPaused, action: "start", expectedTo: domain.CampaignStatusSending},
		{name: "paused cannot pause again", initialStatus: domain.CampaignStatusPaused, action: "pause", expectedTo: domain.CampaignStatusPaused},

		// Terminal state: completed (strictly immutable)
		{name: "completed cannot start", initialStatus: domain.CampaignStatusCompleted, action: "start", expectedTo: domain.CampaignStatusSending},
		{name: "completed cannot pause", initialStatus: domain.CampaignStatusCompleted, action: "pause", expectedTo: domain.CampaignStatusPaused},
		{name: "completed cannot resume", initialStatus: domain.CampaignStatusCompleted, action: "resume", expectedTo: domain.CampaignStatusSending},
		{name: "completed cannot cancel", initialStatus: domain.CampaignStatusCompleted, action: "cancel", expectedTo: domain.CampaignStatusCancelled},

		// Terminal state: cancelled (strictly immutable)
		{name: "cancelled cannot start", initialStatus: domain.CampaignStatusCancelled, action: "start", expectedTo: domain.CampaignStatusSending},
		{name: "cancelled cannot pause", initialStatus: domain.CampaignStatusCancelled, action: "pause", expectedTo: domain.CampaignStatusPaused},
		{name: "cancelled cannot resume", initialStatus: domain.CampaignStatusCancelled, action: "resume", expectedTo: domain.CampaignStatusSending},
		{name: "cancelled cannot cancel again", initialStatus: domain.CampaignStatusCancelled, action: "cancel", expectedTo: domain.CampaignStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			camp, err := campRepo.Create(ctx, &domain.Campaign{
				WorkspaceID: ws.ID,
				Name:        "Test Camp " + tt.name,
				Status:      tt.initialStatus,
			})
			if err != nil {
				t.Fatalf("create campaign: %v", err)
			}

			var actErr error
			switch tt.action {
			case "start":
				_, actErr = engine.Start(ctx, scope, camp.ID)
			case "pause":
				_, actErr = engine.Pause(ctx, scope, camp.ID)
			case "resume":
				_, actErr = engine.Resume(ctx, scope, camp.ID)
			case "cancel":
				_, actErr = engine.Cancel(ctx, scope, camp.ID)
			default:
				t.Fatalf("unknown action: %s", tt.action)
			}

			if actErr == nil {
				t.Fatalf("expected error for illegal transition from %s via %s, got nil", tt.initialStatus, tt.action)
			}

			var transErr domain.ErrInvalidCampaignTransition
			if !errors.As(actErr, &transErr) {
				t.Fatalf("expected domain.ErrInvalidCampaignTransition, got %T: %v", actErr, actErr)
			}

			if transErr.From != tt.initialStatus {
				t.Errorf("expected transErr.From %s, got %s", tt.initialStatus, transErr.From)
			}
			if transErr.To != tt.expectedTo {
				t.Errorf("expected transErr.To %s, got %s", tt.expectedTo, transErr.To)
			}
			if transErr.Code() != "INVALID_CAMPAIGN_TRANSITION" {
				t.Errorf("expected code INVALID_CAMPAIGN_TRANSITION, got %s", transErr.Code())
			}

			// Verify status in DB was unchanged
			after, err := campRepo.GetByID(ctx, camp.ID)
			if err != nil {
				t.Fatalf("get campaign after: %v", err)
			}
			if after.Status != tt.initialStatus {
				t.Errorf("campaign status was modified in DB! want %s, got %s", tt.initialStatus, after.Status)
			}
		})
	}
}

func TestEngine_Delete_Rules(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "ws_delete_"+uuid.New().String())
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	engine := NewBroadcasterEngine(campRepo, nil, nil, NewFakePublisher(), nil, nil)

	// Can delete draft
	cDraft, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Draft", Status: domain.CampaignStatusDraft})
	if err := engine.Delete(ctx, scope, cDraft.ID); err != nil {
		t.Errorf("expected draft to be deletable, got %v", err)
	}

	// Cannot delete cancelled
	cCancelled, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Cancelled", Status: domain.CampaignStatusCancelled})
	if err := engine.Delete(ctx, scope, cCancelled.ID); err == nil {
		t.Errorf("expected cancelled campaign deletion to be rejected")
	}

	// Cannot delete completed
	cCompleted, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Completed", Status: domain.CampaignStatusCompleted})
	if err := engine.Delete(ctx, scope, cCompleted.ID); err == nil {
		t.Errorf("expected completed campaign deletion to be rejected")
	}

	// Cannot delete sending
	cSending, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Sending", Status: domain.CampaignStatusSending})
	if err := engine.Delete(ctx, scope, cSending.ID); err == nil {
		t.Errorf("expected sending campaign deletion to be rejected")
	}

	// Cannot delete scheduled
	cScheduled, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Scheduled", Status: domain.CampaignStatusScheduled})
	if err := engine.Delete(ctx, scope, cScheduled.ID); err == nil {
		t.Errorf("expected scheduled campaign deletion to be rejected")
	}

	// Cannot delete paused
	cPaused, _ := campRepo.Create(ctx, &domain.Campaign{WorkspaceID: ws.ID, Name: "Paused", Status: domain.CampaignStatusPaused})
	if err := engine.Delete(ctx, scope, cPaused.ID); err == nil {
		t.Errorf("expected paused campaign deletion to be rejected")
	}
}

func TestEngine_WorkspaceIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws1, _ := wsRepo.Create(ctx, "ws1_"+uuid.New().String())
	ws2, _ := wsRepo.Create(ctx, "ws2_"+uuid.New().String())
	defer func() {
		_ = wsRepo.Delete(ctx, ws1.ID)
		_ = wsRepo.Delete(ctx, ws2.ID)
	}()

	scope1 := domain.NewWorkspaceScope(ws1.ID, domain.CapabilityWorkspaceScoped)
	scope2 := domain.NewWorkspaceScope(ws2.ID, domain.CapabilityWorkspaceScoped)
	operatorScope := domain.NewOperatorScope(uuid.Nil)

	engine := NewBroadcasterEngine(campRepo, nil, nil, NewFakePublisher(), nil, nil)

	camp1, _ := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws1.ID,
		Name:        "WS1 Camp",
		Status:      domain.CampaignStatusDraft,
	})

	// scope2 attempting to Start camp1 -> rejected with ErrCampaignNotFound
	_, err := engine.Start(ctx, scope2, camp1.ID)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("expected ErrCampaignNotFound for cross-tenant access, got %v", err)
	}

	// scope2 attempting to Delete camp1 -> rejected with ErrCampaignNotFound
	err = engine.Delete(ctx, scope2, camp1.ID)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("expected ErrCampaignNotFound for cross-tenant delete, got %v", err)
	}

	// scope1 can start camp1
	started, err := engine.Start(ctx, scope1, camp1.ID)
	if err != nil {
		t.Errorf("expected scope1 to succeed, got %v", err)
	}
	if started.Status != domain.CampaignStatusSending {
		t.Errorf("expected status sending, got %s", started.Status)
	}

	// Reset status to draft for operator scope test
	_ = campRepo.UpdateStatus(ctx, camp1.ID, domain.CampaignStatusDraft)

	// Operator scope can start camp1
	startedOp, err := engine.Start(ctx, operatorScope, camp1.ID)
	if err != nil {
		t.Errorf("expected operator scope to succeed, got %v", err)
	}
	if startedOp.Status != domain.CampaignStatusSending {
		t.Errorf("expected status sending, got %s", startedOp.Status)
	}

	// Missing scope on Create
	emptyScope := domain.WorkspaceScope{}
	_, err = engine.Create(ctx, emptyScope, CreateCampaignParams{
		Name: "Test",
	})
	if !errors.Is(err, domain.ErrMissingScope) {
		t.Errorf("expected ErrMissingScope, got %v", err)
	}
}

func TestEngine_Create_ValidationAndConnection(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)

	ws, _ := wsRepo.Create(ctx, "ws_create_val_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	engine := NewBroadcasterEngine(campRepo, connRepo, nil, NewFakePublisher(), nil, nil)

	// Create an active connection in ws
	activeConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Active",
		Slug:           "wa-active-" + uuid.New().String()[:8],
		Channel:        "whatsapp",
		SenderIdentity: "+5511999990000",
		Status:         "active",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, activeConn); err != nil {
		t.Fatalf("create active connection: %v", err)
	}

	// Create a disconnected connection in ws
	discConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Disc",
		Slug:           "wa-disc-" + uuid.New().String()[:8],
		Channel:        "whatsapp",
		SenderIdentity: "+5511999990001",
		Status:         "disconnected",
	}
	if err := connRepo.Create(ctx, discConn); err != nil {
		t.Fatalf("create disc connection: %v", err)
	}

	var err error

	// 1. Missing name
	_, err = engine.Create(ctx, scope, CreateCampaignParams{
		ConnectionSlug: activeConn.Slug,
		Recipients:     []domain.CampaignRecipient{{To: "5511999990002"}},
	})
	if err == nil {
		t.Error("expected error for empty campaign name")
	}

	// 2. Missing recipients & tags
	_, err = engine.Create(ctx, scope, CreateCampaignParams{
		Name:           "Empty Audience",
		ConnectionSlug: activeConn.Slug,
	})
	if err == nil {
		t.Error("expected error for empty recipients & tags")
	}

	// 3. Inactive connection
	_, err = engine.Create(ctx, scope, CreateCampaignParams{
		Name:           "Inactive Conn Camp",
		ConnectionSlug: discConn.Slug,
		Recipients:     []domain.CampaignRecipient{{To: "5511999990002"}},
	})
	if err == nil {
		t.Error("expected error for inactive connection")
	}

	// 4. Invalid rate limit <= 0
	badRL := 0
	_, err = engine.Create(ctx, scope, CreateCampaignParams{
		Name:            "Bad RL",
		ConnectionSlug:  activeConn.Slug,
		RateLimitPerMin: &badRL,
		Recipients:      []domain.CampaignRecipient{{To: "5511999990002"}},
	})
	if err == nil {
		t.Error("expected error for rate_limit_per_min <= 0")
	}

	// 5. Valid draft creation
	campDraft, err := engine.Create(ctx, scope, CreateCampaignParams{
		Name:           "Valid Draft",
		ConnectionSlug: activeConn.Slug,
		Recipients:     []domain.CampaignRecipient{{To: "5511999990002"}},
	})
	if err != nil {
		t.Fatalf("create valid draft: %v", err)
	}
	if campDraft.Status != domain.CampaignStatusDraft {
		t.Errorf("expected draft status, got %s", campDraft.Status)
	}
	if campDraft.BatchSize != 100 {
		t.Errorf("expected default batch size 100, got %d", campDraft.BatchSize)
	}
	if campDraft.DelaySeconds != 5 {
		t.Errorf("expected default delay seconds 5, got %d", campDraft.DelaySeconds)
	}

	// 6. Valid scheduled creation
	future := time.Now().Add(24 * time.Hour).UTC()
	campSched, err := engine.Create(ctx, scope, CreateCampaignParams{
		Name:           "Valid Scheduled",
		ConnectionSlug: activeConn.Slug,
		ScheduledAt:    &future,
		Recipients:     []domain.CampaignRecipient{{To: "5511999990002"}},
	})
	if err != nil {
		t.Fatalf("create valid scheduled: %v", err)
	}
	if campSched.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected scheduled status, got %s", campSched.Status)
	}
}

func TestEngine_ProcessStartTask_DynamicTagAndCSVMerge(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_start_task_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	fakeAudit := newFakeAuditWriter()
	mockLister := newMockTagLister()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, fakeAudit, mockLister)

	tagID := uuid.New()
	channel := "whatsapp"

	// Contact 1: Valid whatsapp identity
	c1ID := uuid.New()
	// Contact 2: Channel mismatch (e.g. telegram identity only) -> should be skipped
	c2ID := uuid.New()

	_, err := pool.Exec(ctx, `INSERT INTO contacts (id, workspace_id, name) VALUES ($1, $2, $3), ($4, $2, $5)`,
		c1ID, ws.ID, "Tag User 1", c2ID, "Tag User 2 No WA")
	if err != nil {
		t.Fatalf("insert test contacts: %v", err)
	}

	mockLister.SetContacts(tagID, []domain.Contact{
		{
			ID:   c1ID,
			Name: "Tag User 1",
			Identities: []domain.ContactIdentity{
				{Channel: "whatsapp", SenderIdentity: "5511999991111"},
			},
			Attributes: map[string]string{"plan": "premium"},
		},
		{
			ID:   c2ID,
			Name: "Tag User 2 No WA",
			Identities: []domain.ContactIdentity{
				{Channel: "telegram", SenderIdentity: "12345678"},
			},
			Attributes: map[string]string{"plan": "basic"},
		},
	})

	// CSV recipients:
	// - "5511999991111": duplicate of Contact 1 -> CSV variables supplement contact attributes
	// - "5511999992222": unique CSV recipient
	csvRecipients := []domain.CampaignRecipient{
		{To: "5511999991111", Variables: map[string]string{"discount": "15%"}},
		{To: "5511999992222", Variables: map[string]string{"name": "CSV User 2", "discount": "20%"}},
	}

	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Dynamic Merge Camp",
		Status:       domain.CampaignStatusDraft,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tagID},
		Recipients:   csvRecipients,
		BatchSize:    2,
		DelaySeconds: 1,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}

	if err := engine.ProcessStartTask(ctx, startTask); err != nil {
		t.Fatalf("ProcessStartTask: %v", err)
	}

	// Verify recipients persisted in campaign_recipients
	persisted, err := campRepo.ListRecipients(ctx, camp.ID, nil, 10)
	if err != nil {
		t.Fatalf("list persisted recipients: %v", err)
	}
	if len(persisted) != 3 {
		t.Fatalf("expected 3 persisted records (2 valid + 1 skipped), got %d", len(persisted))
	}

	// Verify Contact 1 has merged variables
	var foundMerged bool
	for _, r := range persisted {
		if r.Phone == "5511999991111" {
			foundMerged = true
			if r.Status != domain.RecipientStatusPending {
				t.Errorf("expected pending status, got %s", r.Status)
			}
			if r.Variables["plan"] != "premium" || r.Variables["discount"] != "15%" {
				t.Errorf("expected merged variables plan=premium, discount=15%%, got %+v", r.Variables)
			}
		}
	}
	if !foundMerged {
		t.Error("merged recipient 5511999991111 not found in persisted records")
	}

	// Verify skipped contact emitted audit event
	skippedAudits := fakeAudit.EventsByType("campaign.dispatch.skipped")
	if len(skippedAudits) != 1 {
		t.Errorf("expected 1 campaign.dispatch.skipped audit event, got %d", len(skippedAudits))
	}

	// Verify batches published: 2 valid recipients with batchSize=2 -> 1 batch
	batchMsgs := fakePub.MessagesBySubject("campaigns.batches")
	if len(batchMsgs) != 1 {
		t.Fatalf("expected 1 batch task published, got %d", len(batchMsgs))
	}

	var publishedBatch domain.CampaignBatchTask
	if err := json.Unmarshal(batchMsgs[0].Data, &publishedBatch); err != nil {
		t.Fatalf("unmarshal batch task: %v", err)
	}
	if publishedBatch.BatchIndex != 1 || publishedBatch.TotalBatches != 1 {
		t.Errorf("expected BatchIndex=1, TotalBatches=1, got %d/%d", publishedBatch.BatchIndex, publishedBatch.TotalBatches)
	}
	if len(publishedBatch.Recipients) != 2 {
		t.Errorf("expected 2 recipients in batch, got %d", len(publishedBatch.Recipients))
	}

	// Verify campaign status updated to sending
	updated, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	if updated.Status != domain.CampaignStatusSending {
		t.Errorf("expected status sending, got %s", updated.Status)
	}
}

func TestEngine_ProcessStartTask_EmptyAudience(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_empty_aud_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	fakeAudit := newFakeAuditWriter()
	mockLister := newMockTagLister()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, fakeAudit, mockLister)

	tagID := uuid.New()
	mockLister.SetContacts(tagID, nil) // 0 contacts

	channel := "whatsapp"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Empty Audience Camp",
		Status:      domain.CampaignStatusDraft,
		Channel:     &channel,
		TagIDs:      []uuid.UUID{tagID},
		BatchSize:   50,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}

	if err := engine.ProcessStartTask(ctx, startTask); err != nil {
		t.Fatalf("ProcessStartTask: %v", err)
	}

	// Campaign status must transition directly to completed
	updated, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	if updated.Status != domain.CampaignStatusCompleted {
		t.Errorf("expected status completed for empty audience, got %s", updated.Status)
	}

	// Emits campaign.dispatch.completed_empty audit log
	emptyAudits := fakeAudit.EventsByType("campaign.dispatch.completed_empty")
	if len(emptyAudits) != 1 {
		t.Fatalf("expected 1 completed_empty audit event, got %d", len(emptyAudits))
	}

	// 0 batches published
	if len(fakePub.Messages()) != 0 {
		t.Errorf("expected 0 published messages for empty audience, got %d", len(fakePub.Messages()))
	}
}

func TestEngine_ProcessStartTask_AllSkippedAudience(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_all_skipped_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	fakeAudit := newFakeAuditWriter()
	mockLister := newMockTagLister()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, fakeAudit, mockLister)

	tagID := uuid.New()
	cID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO contacts (id, workspace_id, name) VALUES ($1, $2, $3)`,
		cID, ws.ID, "Alice Telegram Only")
	if err != nil {
		t.Fatalf("insert test contact: %v", err)
	}

	// Set a contact with a different channel ("telegram") so that channel filter ("whatsapp") skips it
	mockLister.SetContacts(tagID, []domain.Contact{
		{
			ID:   cID,
			Name: "Alice Telegram Only",
			Identities: []domain.ContactIdentity{
				{Channel: "telegram", SenderIdentity: "12345678"},
			},
			Attributes: map[string]string{"name": "Alice"},
		},
	})

	channel := "whatsapp"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "All Skipped Campaign",
		Status:      domain.CampaignStatusDraft,
		Channel:     &channel,
		TagIDs:      []uuid.UUID{tagID},
		BatchSize:   50,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}

	if err := engine.ProcessStartTask(ctx, startTask); err != nil {
		t.Fatalf("ProcessStartTask: %v", err)
	}

	// Status must transition to completed
	updated, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	if updated.Status != domain.CampaignStatusCompleted {
		t.Errorf("expected status completed when all contacts skipped, got %s", updated.Status)
	}

	// Must emit campaign.dispatch.skipped for the skipped contact
	skippedAudits := fakeAudit.EventsByType("campaign.dispatch.skipped")
	if len(skippedAudits) != 1 {
		t.Fatalf("expected 1 skipped audit event, got %d", len(skippedAudits))
	}

	// Must emit campaign.dispatch.completed_empty audit log
	emptyAudits := fakeAudit.EventsByType("campaign.dispatch.completed_empty")
	if len(emptyAudits) != 1 {
		t.Fatalf("expected 1 completed_empty audit event, got %d", len(emptyAudits))
	}

	// 0 batches published
	if len(fakePub.Messages()) != 0 {
		t.Errorf("expected 0 published messages for all-skipped audience, got %d", len(fakePub.Messages()))
	}
}

func TestEngine_ProcessBatchTask_FailSafePause(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_failsafe_pause_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	channel := "whatsapp"
	body := "Hello {{name}}"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Pause Test Camp",
		Status:       domain.CampaignStatusSending,
		Channel:      &channel,
		MessageBody:  &body,
		DelaySeconds: 0,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	// Insert 3 recipients
	recipients := []domain.CampaignRecipientRecord{
		{Phone: "5511999990001", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Recip 1"}},
		{Phone: "5511999990002", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Recip 2"}},
		{Phone: "5511999990003", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Recip 3"}},
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recipients); err != nil {
		t.Fatalf("add recipients: %v", err)
	}

	// Pause the campaign before or during batch processing
	if _, err := engine.Pause(ctx, scope, camp.ID); err != nil {
		t.Fatalf("pause campaign: %v", err)
	}

	task := domain.CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999990001", Variables: map[string]string{"name": "Recip 1"}},
			{To: "5511999990002", Variables: map[string]string{"name": "Recip 2"}},
			{To: "5511999990003", Variables: map[string]string{"name": "Recip 3"}},
		},
		DelaySeconds: 0,
	}

	// ProcessBatchTask should exit cleanly immediately without looping or sending
	start := time.Now()
	if err := engine.ProcessBatchTask(ctx, task); err != nil {
		t.Fatalf("ProcessBatchTask: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("ProcessBatchTask took %v, expected near-instant exit without busy-waiting", elapsed)
	}

	// 0 messages published to messages.outbound
	outboundMsgs := fakePub.MessagesBySubject("messages.outbound")
	if len(outboundMsgs) != 0 {
		t.Errorf("expected 0 outbound messages while paused, got %d", len(outboundMsgs))
	}

	// All 3 recipients remain pending in DB
	pending, err := campRepo.ListPendingRecipients(ctx, camp.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("expected 3 pending recipients, got %d", len(pending))
	}

	// Now resume campaign
	resumed, err := engine.Resume(ctx, scope, camp.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != domain.CampaignStatusSending {
		t.Errorf("expected status sending, got %s", resumed.Status)
	}

	// Batch message was published for the 3 pending recipients
	batches := fakePub.MessagesBySubject("campaigns.batches")
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch published on resume, got %d", len(batches))
	}
}

func TestEngine_ProcessBatchTask_AtomicCompletion(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_atomic_comp_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	channel := "whatsapp"
	body := "Hi {{name}}"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Out of Order Batches Camp",
		Status:       domain.CampaignStatusSending,
		Channel:      &channel,
		MessageBody:  &body,
		DelaySeconds: 0,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	// 4 recipients total across 2 batches
	recipients := []domain.CampaignRecipientRecord{
		{Phone: "5511999990001", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Batch1 R1"}},
		{Phone: "5511999990002", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Batch1 R2"}},
		{Phone: "5511999990003", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Batch2 R1"}},
		{Phone: "5511999990004", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Batch2 R2"}},
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recipients); err != nil {
		t.Fatalf("add recipients: %v", err)
	}

	batch2Task := domain.CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   2, // Batch 2 is TotalBatches!
		TotalBatches: 2,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999990003", Variables: map[string]string{"name": "Batch2 R1"}},
			{To: "5511999990004", Variables: map[string]string{"name": "Batch2 R2"}},
		},
		DelaySeconds: 0,
	}

	// Process Batch 2 FIRST (simulating out-of-order execution)
	if err := engine.ProcessBatchTask(ctx, batch2Task); err != nil {
		t.Fatalf("process batch 2: %v", err)
	}

	// In the old bug, finishing BatchIndex == TotalBatches marked campaign completed!
	// Here, Batch 1 recipients are still pending, so campaign MUST STILL BE SENDING!
	check1, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign after batch 2: %v", err)
	}
	if check1.Status != domain.CampaignStatusSending {
		t.Fatalf("BUG: campaign marked %s after batch 2 finished while batch 1 is still pending!", check1.Status)
	}

	// Process Batch 1
	batch1Task := domain.CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 2,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999990001", Variables: map[string]string{"name": "Batch1 R1"}},
			{To: "5511999990002", Variables: map[string]string{"name": "Batch1 R2"}},
		},
		DelaySeconds: 0,
	}

	if err := engine.ProcessBatchTask(ctx, batch1Task); err != nil {
		t.Fatalf("process batch 1: %v", err)
	}

	// Now that ALL recipients have been processed (0 pending/processing), status must be COMPLETED!
	check2, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign after batch 1: %v", err)
	}
	if check2.Status != domain.CampaignStatusCompleted {
		t.Fatalf("expected campaign to be completed after all batches finished, got %s", check2.Status)
	}
}

func TestEngine_ProcessBatchTask_DuplicateDispatch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_dup_disp_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, dispatchRepo, fakePub, nil, nil)

	channel := "whatsapp"
	body := "Hello {{name}}"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Duplicate Dispatch Camp",
		Status:       domain.CampaignStatusSending,
		Channel:      &channel,
		MessageBody:  &body,
		DelaySeconds: 0,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	phone1 := "5511999990001"
	phone2 := "5511999990002"

	// 2 recipients, initially pending
	recipients := []domain.CampaignRecipientRecord{
		{Phone: phone1, Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Alice"}},
		{Phone: phone2, Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Bob"}},
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recipients); err != nil {
		t.Fatalf("add recipients: %v", err)
	}

	// Pre-create an existing dispatch for phone1 with status "delivered"
	traceID1 := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), phone1)
	disp1, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID1, channel, &camp.ID, nil, nil)
	if err != nil {
		t.Fatalf("create pre-existing dispatch: %v", err)
	}
	if err := dispatchRepo.UpdateDispatchStatus(ctx, disp1.ID, "delivered", channel, 0, nil); err != nil {
		t.Fatalf("update pre-existing dispatch status: %v", err)
	}

	batchTask := domain.CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients: []domain.CampaignRecipient{
			{To: phone1, Variables: map[string]string{"name": "Alice"}},
			{To: phone2, Variables: map[string]string{"name": "Bob"}},
		},
		DelaySeconds: 0,
	}

	if err := engine.ProcessBatchTask(ctx, batchTask); err != nil {
		t.Fatalf("ProcessBatchTask: %v", err)
	}

	// Phone 1 must have been marked as sent in DB even though dispatch already existed
	persisted, err := campRepo.ListRecipients(ctx, camp.ID, nil, 10)
	if err != nil {
		t.Fatalf("list persisted recipients: %v", err)
	}
	for _, r := range persisted {
		if r.Phone == phone1 && r.Status != domain.RecipientStatusSent {
			t.Errorf("expected duplicate dispatch recipient %s to be marked 'sent', got %s", phone1, r.Status)
		}
		if r.Phone == phone2 && r.Status != domain.RecipientStatusSent {
			t.Errorf("expected fresh recipient %s to be marked 'sent', got %s", phone2, r.Status)
		}
	}

	// Since all recipients are sent, campaign must be marked completed
	updatedCamp, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	if updatedCamp.Status != domain.CampaignStatusCompleted {
		t.Errorf("expected campaign status completed, got %s", updatedCamp.Status)
	}

	// Only 1 message was published to messages.outbound (for phone2, since phone1 was duplicate)
	outboundMsgs := fakePub.MessagesBySubject("messages.outbound")
	if len(outboundMsgs) != 1 {
		t.Errorf("expected 1 outbound message published, got %d", len(outboundMsgs))
	}
}

func TestEngine_CalculateJitteredDelay_Bounds(t *testing.T) {
	// 1. delaySeconds <= 0 should return 0
	if d := CalculateJitteredDelay(0, nil); d != 0 {
		t.Errorf("expected 0 for delaySeconds=0, got %v", d)
	}
	if d := CalculateJitteredDelay(-5, nil); d != 0 {
		t.Errorf("expected 0 for delaySeconds=-5, got %v", d)
	}

	// 2. delaySeconds = 5 with uniform jitter in [-0.5s, +0.5s]
	delaySec := 5
	minAllowed := 4500 * time.Millisecond
	maxAllowed := 5500 * time.Millisecond

	for i := 0; i < 5000; i++ {
		d := CalculateJitteredDelay(delaySec, nil)
		if d < minAllowed || d > maxAllowed {
			t.Fatalf("iteration %d: delay %v out of bounds [%v, %v]", i, d, minAllowed, maxAllowed)
		}
	}

	// 3. deterministic RNG checks
	rng := rand.New(rand.NewPCG(42, 42))
	for i := 0; i < 100; i++ {
		d := CalculateJitteredDelay(delaySec, rng)
		if d < minAllowed || d > maxAllowed {
			t.Fatalf("deterministic iteration %d: delay %v out of bounds [%v, %v]", i, d, minAllowed, maxAllowed)
		}
	}
}

func TestEngine_TriggerDue_ClaimAndRollback(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_trigger_due_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	fakeAudit := newFakeAuditWriter()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, fakeAudit, nil)

	past := time.Now().Add(-1 * time.Hour).UTC()
	future := time.Now().Add(1 * time.Hour).UTC()

	// Due campaign 1
	cDue1, _ := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due 1",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
	})

	// Due campaign 2
	cDue2, _ := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due 2 (Publisher Error)",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
	})

	// Future campaign (not due)
	cFuture, _ := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Future",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &future,
	})

	// 1. Success path for cDue1
	triggered, err := engine.TriggerDue(ctx)
	if err != nil {
		t.Fatalf("TriggerDue: %v", err)
	}

	// Both cDue1 and cDue2 were claimed and published
	if len(triggered) != 2 {
		t.Errorf("expected 2 campaigns triggered, got %d", len(triggered))
	}
	triggeredMap := make(map[uuid.UUID]bool)
	for _, id := range triggered {
		triggeredMap[id] = true
	}
	if !triggeredMap[cDue1.ID] || !triggeredMap[cDue2.ID] {
		t.Errorf("expected cDue1 and cDue2 in triggered, got %+v", triggered)
	}

	// Future campaign remains scheduled
	checkFuture, _ := campRepo.GetByID(ctx, cFuture.ID)
	if checkFuture.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected future campaign to remain scheduled, got %s", checkFuture.Status)
	}

	// Audit logs emitted for triggered campaigns
	audits := fakeAudit.EventsByType("campaign.dispatch.scheduled_triggered")
	if len(audits) != 2 {
		t.Errorf("expected 2 scheduled_triggered audit events, got %d", len(audits))
	}

	// 2. Rollback path: create due campaign and make publisher return error
	fakePub.Reset()
	fakePub.SetError(errors.New("nats connection failed"))

	cDueFail, _ := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due Rollback Test",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
	})

	triggeredFail, err := engine.TriggerDue(ctx)
	if err != nil {
		t.Fatalf("TriggerDue: %v", err)
	}
	if len(triggeredFail) != 0 {
		t.Errorf("expected 0 triggered on publisher error, got %d", len(triggeredFail))
	}

	// Verify status was rolled back to scheduled!
	checkRollback, err := campRepo.GetByID(ctx, cDueFail.ID)
	if err != nil {
		t.Fatalf("get campaign after rollback: %v", err)
	}
	if checkRollback.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected status rolled back to scheduled, got %s", checkRollback.Status)
	}
}

func TestEngine_Start_RollbackOnPublishFailure(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_start_rollback_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Start Rollback Draft",
		Status:      domain.CampaignStatusDraft,
	})
	if err != nil {
		t.Fatalf("create draft campaign: %v", err)
	}

	// Configure publisher failure
	fakePub.SetError(errors.New("nats publisher error"))

	_, err = engine.Start(ctx, scope, camp.ID)
	if err == nil {
		t.Fatalf("expected error from Start when publisher fails")
	}

	// Verify status rolled back to draft
	campAfter, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("get campaign after failed start: %v", err)
	}
	if campAfter.Status != domain.CampaignStatusDraft {
		t.Errorf("expected status rolled back to draft, got %s", campAfter.Status)
	}
}

func TestEngine_Resume_UniqueTraceIDPerCycle(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_resume_trace_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	scope := domain.NewWorkspaceScope(ws.ID, domain.CapabilityWorkspaceScoped)
	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Resume Trace ID Test",
		Status:      domain.CampaignStatusPaused,
		BatchSize:   1,
	})
	if err != nil {
		t.Fatalf("create paused campaign: %v", err)
	}

	_ = campRepo.AddRecipients(ctx, camp.ID, []domain.CampaignRecipientRecord{
		{Phone: "5511999990001", Status: domain.RecipientStatusPending},
	})

	resumed, err := engine.Resume(ctx, scope, camp.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != domain.CampaignStatusSending {
		t.Errorf("expected sending status, got %s", resumed.Status)
	}

	batchMsgs := fakePub.MessagesBySubject("campaigns.batches")
	if len(batchMsgs) != 1 {
		t.Fatalf("expected 1 batch message published on resume, got %d", len(batchMsgs))
	}

	// Verify traceID contains "resumed_" and does NOT match the initial "campaign_<id>_batch_1" format
	expectedPrefix := fmt.Sprintf("campaign_%s_resumed_", camp.ID.String())
	if !strings.HasPrefix(batchMsgs[0].TraceID, expectedPrefix) {
		t.Errorf("expected traceID to start with %q to avoid JetStream dedup collision, got %q", expectedPrefix, batchMsgs[0].TraceID)
	}
}

func TestEngine_ProcessBatchTask_MidBatchPause(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, _ := wsRepo.Create(ctx, "ws_midbatch_pause_"+uuid.New().String())
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	fakePub := NewFakePublisher()
	engine := NewBroadcasterEngine(campRepo, nil, nil, fakePub, nil, nil)

	channel := "whatsapp"
	body := "Hello {{name}}"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Mid Batch Pause Test",
		Status:       domain.CampaignStatusSending,
		Channel:      &channel,
		MessageBody:  &body,
		DelaySeconds: 0,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	// Insert 2 recipients
	recipients := []domain.CampaignRecipientRecord{
		{Phone: "5511999990001", Status: domain.RecipientStatusPending},
		{Phone: "5511999990002", Status: domain.RecipientStatusPending},
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recipients); err != nil {
		t.Fatalf("add recipients: %v", err)
	}

	// Simulate pausing campaign before second recipient
	// We can update the status directly in DB right now
	_ = campRepo.UpdateStatus(ctx, camp.ID, domain.CampaignStatusPaused)

	task := domain.CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999990001"},
			{To: "5511999990002"},
		},
		DelaySeconds: 0,
	}

	if err := engine.ProcessBatchTask(ctx, task); err != nil {
		t.Fatalf("ProcessBatchTask: %v", err)
	}

	// Since camp was paused, 0 messages should be published and batch exits cleanly
	outboundMsgs := fakePub.MessagesBySubject("messages.outbound")
	if len(outboundMsgs) != 0 {
		t.Errorf("expected 0 messages published when campaign is paused, got %d", len(outboundMsgs))
	}
}

