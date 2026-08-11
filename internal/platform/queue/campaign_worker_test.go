package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

type fakeAuditWriter struct {
	events []audit.Event
	mu     sync.Mutex
}

func (f *fakeAuditWriter) Write(e audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeAuditWriter) Close() error { return nil }

func (f *fakeAuditWriter) EnsurePartitions(ctx context.Context) error { return nil }

func (f *fakeAuditWriter) Events() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]audit.Event, len(f.events))
	copy(cp, f.events)
	return cp
}

func TestCampaignWorker_Success(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize repos
	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	// Create workspace
	ws, err := wsRepo.Create(ctx, "camp_worker_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Ensure Streams
	_, err = EnsureCampaignStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureCampaignStream failed: %v", err)
	}

	messagesStream, err := EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureStream failed: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}

	consumerName := "test-campaign-worker-consumer-" + uuid.New().String()
	campStream, err := js.Stream(ctx, "CAMPAIGNS")
	if err != nil {
		t.Fatalf("get campaigns stream failed: %v", err)
	}

	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	// Create campaign
	tmplName := "Ola {{nome}}!"
	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Success Camp",
		Status:       domain.CampaignStatusSending,
		BatchSize:    1,
		DelaySeconds: 1,
		TemplateName: &tmplName,
		Channel:      &channel,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999998888", Variables: map[string]string{"nome": "Maria"}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Create messages outbound consumer to verify worker sends messages
	outboundConsumer, err := EnsureConsumer(ctx, messagesStream, "test-outbound-verifier-"+uuid.New().String())
	if err != nil {
		t.Fatalf("EnsureConsumer for MESSAGES failed: %v", err)
	}

	// Publish batch task
	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients:   camp.Recipients,
		DelaySeconds: 0, // no delay for test speed
	}
	taskBytes, _ := json.Marshal(task)
	err = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to publish batch task: %v", err)
	}

	// Start Worker
	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil)
	defer worker.Stop()

	// Wait for completion in database
	var finalCamp *domain.Campaign
	for i := 0; i < 20; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("campaign expected to be completed, got: %s", finalCamp.Status)
	}

	// Verify dispatch record was created
	traceID := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511999998888")
	disp, err := dispatchRepo.GetByTraceID(ctx, traceID)
	if err != nil {
		t.Fatalf("failed to fetch dispatch log: %v", err)
	}
	if disp.CampaignID == nil || *disp.CampaignID != camp.ID {
		t.Errorf("dispatch CampaignID mismatch")
	}

	// Verify NATS outbound queue message was received
	msgs, err := outboundConsumer.Messages()
	if err != nil {
		t.Fatalf("failed to get messages context: %v", err)
	}
	defer msgs.Stop()

	msg, err := msgs.Next()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	var qMsg domain.QueueMessage
	_ = json.Unmarshal(msg.Data(), &qMsg)

	if qMsg.To != "5511999998888" {
		t.Errorf("expected QueueMessage.To 5511999998888, got %s", qMsg.To)
	}
	if qMsg.Body != "Ola Maria!" {
		t.Errorf("expected QueueMessage.Body 'Ola Maria!', got %s", qMsg.Body)
	}
	_ = msg.Ack()
}

func TestCampaignWorker_Cancelled(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_worker_ws_cancel_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, _ = EnsureCampaignStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-cancel-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	// Create campaign
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Cancel Camp",
		Status:       domain.CampaignStatusCancelled, // cancelled!
		BatchSize:    1,
		DelaySeconds: 1,
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999998888", Variables: map[string]string{"nome": "Maria"}},
		},
		DelaySeconds: 0,
	}
	taskBytes, _ := json.Marshal(task)
	_ = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil)
	defer worker.Stop()

	// Wait to see if NATS message gets Acked without creating dispatches
	time.Sleep(500 * time.Millisecond)

	traceID := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511999998888")
	_, err = dispatchRepo.GetByTraceID(ctx, traceID)
	if err == nil {
		t.Errorf("expected no dispatch log for cancelled campaign, but found one")
	}
}

func TestCampaignWorker_PauseAndResume(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_worker_ws_pause_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-pause-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	// Create paused campaign
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Pause Camp",
		Status:       domain.CampaignStatusPaused, // Starts paused!
		BatchSize:    1,
		DelaySeconds: 1,
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients: []domain.CampaignRecipient{
			{To: "5511977778888", Variables: map[string]string{}},
		},
		DelaySeconds: 0,
	}
	taskBytes, _ := json.Marshal(task)
	_ = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil)
	defer worker.Stop()

	// Wait 500ms while paused — no dispatch should be created
	time.Sleep(500 * time.Millisecond)

	traceID := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511977778888")
	_, err = dispatchRepo.GetByTraceID(ctx, traceID)
	if err == nil {
		t.Fatalf("expected no dispatch log while paused, but found one")
	}

	// Resume campaign by updating status to sending
	_ = campRepo.UpdateStatus(ctx, camp.ID, domain.CampaignStatusSending)

	// Wait for completion
	var finalCamp *domain.Campaign
	for i := 0; i < 20; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("campaign expected to be completed after resume, got: %s", finalCamp.Status)
	}
}

func TestCampaignWorker_AuditEmissions_Sent(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_audit_sent_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-audit-sent-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Audit Sent Camp",
		Status:       domain.CampaignStatusSending,
		BatchSize:    1,
		DelaySeconds: 1,
		Channel:      &channel,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999991111", Variables: map[string]string{}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients:   camp.Recipients,
		DelaySeconds: 0,
	}
	taskBytes, _ := json.Marshal(task)
	_ = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 20; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	events := mockAudit.Events()
	if len(events) == 0 {
		t.Fatalf("expected audit events to be emitted, got 0")
	}

	foundSent := false
	expectedTrace := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511999991111")
	for _, ev := range events {
		if ev.EventType == "campaign_dispatch" && ev.TraceID == expectedTrace && ev.WorkspaceID == ws.ID {
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["status"] == "sent" && payload["recipient"] == "5511999991111" {
					foundSent = true
					break
				}
			}
		}
	}

	if !foundSent {
		t.Errorf("expected audit event with status 'sent' for recipient 5511999991111, events: %+v", events)
	}
}

func TestCampaignWorker_AuditEmissions_Delivered(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_audit_deliv_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-audit-deliv-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Audit Delivered Camp",
		Status:       domain.CampaignStatusSending,
		BatchSize:    1,
		DelaySeconds: 1,
		Channel:      &channel,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999992222", Variables: map[string]string{}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Pre-create dispatch record with delivered status
	traceID := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511999992222")
	disp, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, channel, &camp.ID, nil, nil)
	if err != nil {
		t.Fatalf("failed to create dispatch record: %v", err)
	}
	err = dispatchRepo.UpdateDispatchStatus(ctx, disp.ID, "delivered", channel, 0, nil)
	if err != nil {
		t.Fatalf("failed to update dispatch status to delivered: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:   camp.ID,
		WorkspaceID:  ws.ID,
		BatchIndex:   1,
		TotalBatches: 1,
		Recipients:   camp.Recipients,
		DelaySeconds: 0,
	}
	taskBytes, _ := json.Marshal(task)
	_ = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 20; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	events := mockAudit.Events()
	foundDelivered := false
	for _, ev := range events {
		if ev.EventType == "campaign_dispatch" && ev.TraceID == traceID && ev.WorkspaceID == ws.ID {
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["status"] == "delivered" && payload["recipient"] == "5511999992222" {
					foundDelivered = true
					break
				}
			}
		}
	}

	if !foundDelivered {
		t.Errorf("expected audit event with status 'delivered' for recipient 5511999992222, events: %+v", events)
	}
}

func TestCampaignWorker_AuditEmissions_Failed(t *testing.T) {
	wsID := uuid.New()
	campID := uuid.New()
	traceID := fmt.Sprintf("campaign_%s_%s", campID.String(), "5511999993333")

	mockAudit := &fakeAuditWriter{}
	worker := &CampaignWorker{auditWriter: mockAudit}

	err := worker.EmitAuditLog(wsID, traceID, "campaign_dispatch", "failed", "5511999993333", campID, "whatsapp", "publish error")
	if err != nil {
		t.Fatalf("EmitAuditLog returned error: %v", err)
	}

	events := mockAudit.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}

	ev := events[0]
	if ev.EventType != "campaign_dispatch" || ev.TraceID != traceID || ev.WorkspaceID != wsID {
		t.Errorf("unexpected event metadata: %+v", ev)
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload["status"] != "failed" || payload["recipient"] != "5511999993333" || payload["error"] != "publish error" {
		t.Errorf("unexpected payload content: %+v", payload)
	}
}
