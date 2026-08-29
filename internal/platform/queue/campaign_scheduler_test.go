package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestCampaignScheduler_CheckDueCampaigns_Success(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "sched_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Ensure Streams
	_, err = EnsureCampaignStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureCampaignStream failed: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}

	consumerName := "test-sched-consumer-" + uuid.New().String()
	campStream, err := js.Stream(ctx, "CAMPAIGNS")
	if err != nil {
		t.Fatalf("get campaigns stream failed: %v", err)
	}
	_ = campStream.Purge(ctx)

	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	past := time.Now().UTC().Add(-5 * time.Minute)
	channel := "whatsapp"
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Due Scheduled Campaign",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
		Channel:     &channel,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	fakeAudit := &fakeAuditWriter{}

	scheduler := NewCampaignScheduler(campRepo, publisher, fakeAudit)

	// Trigger due campaigns
	count, err := scheduler.CheckDueCampaigns(ctx)
	if err != nil {
		t.Fatalf("CheckDueCampaigns failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 campaign triggered, got %d", count)
	}

	// 1. Verify DB status transitioned to sending
	updatedCamp, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("failed to get campaign: %v", err)
	}
	if updatedCamp.Status != domain.CampaignStatusSending {
		t.Errorf("expected status 'sending', got %s", updatedCamp.Status)
	}

	// 2. Verify NATS received CampaignStartTask on campaigns.start
	msgCtx, err := consumer.Messages()
	if err != nil {
		t.Fatalf("failed to create messages context: %v", err)
	}
	defer msgCtx.Stop()

	msg, err := msgCtx.Next()
	if err != nil {
		t.Fatalf("failed to receive published start task message from NATS: %v", err)
	}
	_ = msg.Ack()

	if msg.Subject() != "campaigns.start" {
		t.Errorf("expected subject 'campaigns.start', got %s", msg.Subject())
	}

	var startTask domain.CampaignStartTask
	if err := json.Unmarshal(msg.Data(), &startTask); err != nil {
		t.Fatalf("failed to unmarshal CampaignStartTask: %v", err)
	}
	if startTask.CampaignID != camp.ID {
		t.Errorf("expected CampaignID %s, got %s", camp.ID, startTask.CampaignID)
	}
	if startTask.WorkspaceID != ws.ID {
		t.Errorf("expected WorkspaceID %s, got %s", ws.ID, startTask.WorkspaceID)
	}

	// 3. Verify audit log emitted campaign.dispatch.scheduled_triggered
	events := fakeAudit.Events()
	var foundAudit bool
	for _, e := range events {
		if e.EventType == "campaign.dispatch.scheduled_triggered" && e.WorkspaceID == ws.ID {
			foundAudit = true
			var payload map[string]any
			if err := json.Unmarshal(e.Payload, &payload); err == nil {
				if payload["campaign_id"] != camp.ID.String() {
					t.Errorf("expected audit campaign_id %s, got %v", camp.ID, payload["campaign_id"])
				}
				if payload["status"] != "sending" {
					t.Errorf("expected audit status 'sending', got %v", payload["status"])
				}
			}
			break
		}
	}
	if !foundAudit {
		t.Errorf("expected audit event 'campaign.dispatch.scheduled_triggered' not found in %v", events)
	}
}

func TestCampaignScheduler_CheckDueCampaigns_FutureIgnored(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "sched_future_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	future := time.Now().UTC().Add(2 * time.Hour)
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Future Scheduled Campaign",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &future,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	fakeAudit := &fakeAuditWriter{}

	scheduler := NewCampaignScheduler(campRepo, publisher, fakeAudit)

	count, err := scheduler.CheckDueCampaigns(ctx)
	if err != nil {
		t.Fatalf("CheckDueCampaigns failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 campaigns triggered, got %d", count)
	}

	// Verify status remains scheduled
	fetched, _ := campRepo.GetByID(ctx, camp.ID)
	if fetched.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected status 'scheduled', got %s", fetched.Status)
	}

	// Verify no audit events
	if len(fakeAudit.Events()) != 0 {
		t.Errorf("expected 0 audit events, got %d", len(fakeAudit.Events()))
	}
}

func TestCampaignScheduler_Run_Lifecycle(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "sched_run_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	past := time.Now().UTC().Add(-1 * time.Minute)
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Auto Ticker Campaign",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	fakeAudit := &fakeAuditWriter{}

	scheduler := NewCampaignScheduler(campRepo, publisher, fakeAudit)
	scheduler.SetInterval(50 * time.Millisecond)

	runCtx, runCancel := context.WithCancel(ctx)
	doneChan := make(chan struct{})
	go func() {
		scheduler.Run(runCtx)
		close(doneChan)
	}()

	// Wait for ticker to fire
	time.Sleep(150 * time.Millisecond)
	runCancel()
	<-doneChan

	// Verify campaign transitioned to sending
	fetched, err := campRepo.GetByID(context.Background(), camp.ID)
	if err != nil {
		t.Fatalf("failed to get campaign: %v", err)
	}
	if fetched.Status != domain.CampaignStatusSending {
		t.Errorf("expected campaign status 'sending', got %s", fetched.Status)
	}
}

type failingPublisher struct{}

func (f *failingPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	return errors.New("nats connection lost")
}

func TestCampaignScheduler_CheckDueCampaigns_RollbackOnPublishFailure(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)

	ws, err := wsRepo.Create(ctx, "sched_fail_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	past := time.Now().UTC().Add(-2 * time.Minute)
	camp, err := campRepo.Create(ctx, &domain.Campaign{
		WorkspaceID: ws.ID,
		Name:        "Rollback Due Campaign",
		Status:      domain.CampaignStatusScheduled,
		ScheduledAt: &past,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	fakeAudit := &fakeAuditWriter{}
	scheduler := NewCampaignScheduler(campRepo, &failingPublisher{}, fakeAudit)

	triggered, err := scheduler.CheckDueCampaigns(ctx)
	if err != nil {
		t.Fatalf("CheckDueCampaigns returned unexpected error: %v", err)
	}
	if triggered != 0 {
		t.Errorf("expected 0 triggered campaigns due to publish failure, got %d", triggered)
	}

	// Verify campaign was rolled back to scheduled in database
	fetched, err := campRepo.GetByID(ctx, camp.ID)
	if err != nil {
		t.Fatalf("failed to get campaign: %v", err)
	}
	if fetched.Status != domain.CampaignStatusScheduled {
		t.Errorf("expected campaign status 'scheduled' after rollback, got %s", fetched.Status)
	}
}
