package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/campaign"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockDueTriggerer struct {
	triggered []uuid.UUID
	err       error
	calls     int
}

func (m *mockDueTriggerer) TriggerDue(ctx context.Context) ([]uuid.UUID, error) {
	m.calls++
	return m.triggered, m.err
}

func TestCampaignScheduler_Unit(t *testing.T) {
	t.Run("nil triggerer returns 0", func(t *testing.T) {
		s := NewCampaignScheduler(nil)
		count, err := s.CheckDueCampaigns(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0, got %d", count)
		}
	})

	t.Run("success delegates and returns count", func(t *testing.T) {
		id1, id2 := uuid.New(), uuid.New()
		mock := &mockDueTriggerer{triggered: []uuid.UUID{id1, id2}}
		s := NewCampaignScheduler(mock)
		count, err := s.CheckDueCampaigns(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2, got %d", count)
		}
		if mock.calls != 1 {
			t.Fatalf("expected 1 call, got %d", mock.calls)
		}
	})

	t.Run("error returned directly", func(t *testing.T) {
		wantErr := errors.New("database down")
		mock := &mockDueTriggerer{err: wantErr}
		s := NewCampaignScheduler(mock)
		count, err := s.CheckDueCampaigns(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected error %v, got %v", wantErr, err)
		}
		if count != 0 {
			t.Fatalf("expected 0, got %d", count)
		}
	})
}

func TestCampaignScheduler_CheckDueCampaigns_Success(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

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

	engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, publisher, fakeAudit, tagRepo)
	scheduler := NewCampaignScheduler(engine)

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
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

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

	engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, publisher, fakeAudit, tagRepo)
	scheduler := NewCampaignScheduler(engine)

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
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

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

	engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, publisher, fakeAudit, tagRepo)
	scheduler := NewCampaignScheduler(engine)
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
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

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
	engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, &failingPublisher{}, fakeAudit, tagRepo)
	scheduler := NewCampaignScheduler(engine)

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

// TestCampaignScheduler_Concurrency_NoDuplicateClaims verifies that multiple concurrent
// scheduler instances invoking CheckDueCampaigns simultaneously claim each due campaign
// exactly once without collisions or duplicate executions (verifying SKIP LOCKED behavior).
func TestCampaignScheduler_Concurrency_NoDuplicateClaims(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

	ws, err := wsRepo.Create(ctx, "sched_conc_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Ensure Streams & Consumer
	_, err = EnsureCampaignStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureCampaignStream failed: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}

	consumerName := "test-sched-conc-" + uuid.New().String()
	campStream, err := js.Stream(ctx, "CAMPAIGNS")
	if err != nil {
		t.Fatalf("get campaigns stream failed: %v", err)
	}
	_ = campStream.Purge(ctx)

	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	// Create 20 due scheduled campaigns
	const numCampaigns = 20
	past := time.Now().UTC().Add(-10 * time.Minute)
	channel := "whatsapp"

	createdCampaignIDs := make(map[uuid.UUID]bool, numCampaigns)
	for i := 0; i < numCampaigns; i++ {
		c, err := campRepo.Create(ctx, &domain.Campaign{
			WorkspaceID: ws.ID,
			Name:        fmt.Sprintf("Concurrent Due Campaign %d", i),
			Status:      domain.CampaignStatusScheduled,
			ScheduledAt: &past,
			Channel:     &channel,
		})
		if err != nil {
			t.Fatalf("failed to create campaign %d: %v", i, err)
		}
		createdCampaignIDs[c.ID] = true
	}

	publisher := NewJetStreamPublisher(nc)
	fakeAudit := &fakeAuditWriter{}

	// Create multiple scheduler instances sharing the same DB and publisher
	// to simulate a multi-instance / multi-worker PerGo deployment.
	const numWorkers = 8
	schedulers := make([]*CampaignScheduler, numWorkers)
	for i := 0; i < numWorkers; i++ {
		engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, publisher, fakeAudit, tagRepo)
		schedulers[i] = NewCampaignScheduler(engine)
	}

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	workerErrors := make(chan error, numWorkers)

	var mu sync.Mutex
	totalReported := 0

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			<-startBarrier // Synchronize all workers to trigger simultaneously

			count, err := schedulers[workerIdx].CheckDueCampaigns(ctx)
			if err != nil {
				workerErrors <- fmt.Errorf("worker %d error: %w", workerIdx, err)
				return
			}

			mu.Lock()
			totalReported += count
			mu.Unlock()
		}(i)
	}

	// Release all workers simultaneously
	close(startBarrier)
	wg.Wait()
	close(workerErrors)

	for err := range workerErrors {
		t.Fatalf("concurrent scheduler execution failed: %v", err)
	}

	// 1. Total claimed across all workers must equal exactly numCampaigns
	if totalReported != numCampaigns {
		t.Fatalf("expected total reported claims %d, got %d", numCampaigns, totalReported)
	}

	// 2. Verify all campaigns in DB transitioned to 'sending'
	for campID := range createdCampaignIDs {
		camp, err := campRepo.GetByID(ctx, campID)
		if err != nil {
			t.Fatalf("failed to get campaign %s: %v", campID, err)
		}
		if camp.Status != domain.CampaignStatusSending {
			t.Errorf("campaign %s has status %s, expected 'sending'", campID, camp.Status)
		}
	}

	// 3. Verify NATS received exactly numCampaigns messages, each unique
	msgCtx, err := consumer.Messages()
	if err != nil {
		t.Fatalf("failed to create consumer message context: %v", err)
	}
	defer msgCtx.Stop()

	receivedCampaignIDs := make(map[uuid.UUID]int)
	for i := 0; i < numCampaigns; i++ {
		msg, err := msgCtx.Next()
		if err != nil {
			t.Fatalf("failed to receive message %d from NATS: %v", i, err)
		}
		_ = msg.Ack()

		var task domain.CampaignStartTask
		if err := json.Unmarshal(msg.Data(), &task); err != nil {
			t.Fatalf("failed to unmarshal task: %v", err)
		}
		receivedCampaignIDs[task.CampaignID]++
	}

	if len(receivedCampaignIDs) != numCampaigns {
		t.Errorf("expected %d unique campaigns in NATS, got %d", numCampaigns, len(receivedCampaignIDs))
	}
	for id, count := range receivedCampaignIDs {
		if count != 1 {
			t.Errorf("campaign %s received %d times in NATS (expected 1)", id, count)
		}
		if !createdCampaignIDs[id] {
			t.Errorf("unexpected campaign %s received in NATS", id)
		}
	}

	// 4. Verify exactly numCampaigns audit events were emitted with zero duplicates
	auditEvents := fakeAudit.Events()
	var schedEvents []audit.Event
	for _, e := range auditEvents {
		if e.EventType == "campaign.dispatch.scheduled_triggered" && e.WorkspaceID == ws.ID {
			schedEvents = append(schedEvents, e)
		}
	}
	if len(schedEvents) != numCampaigns {
		t.Fatalf("expected %d audit events, got %d", numCampaigns, len(schedEvents))
	}

	auditCampaignIDs := make(map[string]int)
	for _, e := range schedEvents {
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err == nil {
			campIDStr, _ := payload["campaign_id"].(string)
			auditCampaignIDs[campIDStr]++
		}
	}
	if len(auditCampaignIDs) != numCampaigns {
		t.Errorf("expected %d unique campaign IDs in audit events, got %d", numCampaigns, len(auditCampaignIDs))
	}

	// 5. Subsequent check from any scheduler should find 0 due campaigns
	nextCount, err := schedulers[0].CheckDueCampaigns(ctx)
	if err != nil {
		t.Fatalf("subsequent CheckDueCampaigns failed: %v", err)
	}
	if nextCount != 0 {
		t.Errorf("expected 0 due campaigns on second pass, got %d", nextCount)
	}
}

// TestCampaignEngine_TriggerDue_Concurrency_DisjointUUIDs verifies that when multiple goroutines
// invoke TriggerDue directly on BroadcasterEngine concurrently, the sets of returned UUIDs across
// all goroutines are completely pairwise disjoint (no double-claims) and together cover all due campaigns.
func TestCampaignEngine_TriggerDue_Concurrency_DisjointUUIDs(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

	ws, err := wsRepo.Create(ctx, "engine_conc_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Ensure Streams
	_, err = EnsureCampaignStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureCampaignStream failed: %v", err)
	}

	const numCampaigns = 24
	past := time.Now().UTC().Add(-10 * time.Minute)
	channel := "whatsapp"

	expectedIDs := make(map[uuid.UUID]bool, numCampaigns)
	for i := 0; i < numCampaigns; i++ {
		c, err := campRepo.Create(ctx, &domain.Campaign{
			WorkspaceID: ws.ID,
			Name:        fmt.Sprintf("Disjoint Due Campaign %d", i),
			Status:      domain.CampaignStatusScheduled,
			ScheduledAt: &past,
			Channel:     &channel,
		})
		if err != nil {
			t.Fatalf("failed to create campaign %d: %v", i, err)
		}
		expectedIDs[c.ID] = true
	}

	publisher := NewJetStreamPublisher(nc)
	fakeAudit := &fakeAuditWriter{}
	engine := campaign.NewBroadcasterEngine(campRepo, connRepo, dispatchRepo, publisher, fakeAudit, tagRepo)

	const numWorkers = 6
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	type workerResult struct {
		workerIdx int
		ids       []uuid.UUID
		err       error
	}
	results := make([]workerResult, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			<-startBarrier // Fire together

			ids, err := engine.TriggerDue(ctx)
			results[workerIdx] = workerResult{
				workerIdx: workerIdx,
				ids:       ids,
				err:       err,
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	seenIDs := make(map[uuid.UUID]int)
	totalClaimed := 0

	for _, res := range results {
		if res.err != nil {
			t.Fatalf("worker %d returned error: %v", res.workerIdx, res.err)
		}
		for _, id := range res.ids {
			seenIDs[id]++
			totalClaimed++
			if !expectedIDs[id] {
				t.Errorf("worker %d claimed unexpected campaign ID: %s", res.workerIdx, id)
			}
		}
	}

	// Verify all campaigns were claimed
	if totalClaimed != numCampaigns {
		t.Fatalf("expected total claimed %d, got %d", numCampaigns, totalClaimed)
	}

	// Verify zero duplicates (pairwise disjoint)
	for id, count := range seenIDs {
		if count > 1 {
			t.Errorf("campaign %s was claimed %d times across workers (expected 1)", id, count)
		}
	}

	if len(seenIDs) != numCampaigns {
		t.Errorf("expected %d unique campaigns claimed, got %d", numCampaigns, len(seenIDs))
	}
}
