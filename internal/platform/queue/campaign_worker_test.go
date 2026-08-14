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
	"golang.org/x/time/rate"
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
	_ = messagesStream.Purge(ctx)

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}

	consumerName := "test-campaign-worker-consumer-" + uuid.New().String()
	campStream, err := js.Stream(ctx, "CAMPAIGNS")
	if err != nil {
		t.Fatalf("get campaigns stream failed: %v", err)
	}
	_ = campStream.Purge(ctx)

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
	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil, nil)
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

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil, nil)
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

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, nil, nil)
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

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, nil)
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
		if ev.EventType == "campaign.dispatch.sent" && ev.TraceID == expectedTrace && ev.WorkspaceID == ws.ID {
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["status"] == "sent" &&
					payload["recipient"] == "5511999991111" &&
					payload["recipient_id"] == "5511999991111" &&
					payload["workspace_id"] == ws.ID.String() &&
					payload["campaign_id"] == camp.ID.String() &&
					payload["trace_id"] == expectedTrace {
					foundSent = true
					break
				}
			}
		}
	}

	if !foundSent {
		t.Errorf("expected audit event with EventType 'campaign.dispatch.sent' for recipient 5511999991111, events: %+v", events)
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

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, nil)
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
		if ev.EventType == "campaign.dispatch.delivered" && ev.TraceID == traceID && ev.WorkspaceID == ws.ID {
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["status"] == "delivered" &&
					payload["recipient"] == "5511999992222" &&
					payload["recipient_id"] == "5511999992222" &&
					payload["workspace_id"] == ws.ID.String() &&
					payload["campaign_id"] == camp.ID.String() &&
					payload["trace_id"] == traceID {
					foundDelivered = true
					break
				}
			}
		}
	}

	if !foundDelivered {
		t.Errorf("expected audit event with EventType 'campaign.dispatch.delivered' for recipient 5511999992222, events: %+v", events)
	}
}

func TestCampaignWorker_AuditEmissions_Failed(t *testing.T) {
	wsID := uuid.New()
	campID := uuid.New()
	traceID := fmt.Sprintf("campaign_%s_%s", campID.String(), "5511999993333")

	mockAudit := &fakeAuditWriter{}
	worker := &CampaignWorker{auditWriter: mockAudit}

	err := worker.emitAuditLog(auditDispatchEvent{
		WorkspaceID: wsID,
		TraceID:     traceID,
		EventType:   "campaign.dispatch.failed",
		Status:      "failed",
		Recipient:   "5511999993333",
		CampaignID:  campID,
		Channel:     "whatsapp",
		ErrStr:      "publish error",
	})
	if err != nil {
		t.Fatalf("emitAuditLog returned error: %v", err)
	}

	events := mockAudit.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}

	ev := events[0]
	if ev.EventType != "campaign.dispatch.failed" || ev.TraceID != traceID || ev.WorkspaceID != wsID {
		t.Errorf("unexpected event metadata: %+v", ev)
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload["status"] != "failed" ||
		payload["recipient"] != "5511999993333" ||
		payload["recipient_id"] != "5511999993333" ||
		payload["workspace_id"] != wsID.String() ||
		payload["campaign_id"] != campID.String() ||
		payload["trace_id"] != traceID ||
		payload["error"] != "publish error" {
		t.Errorf("unexpected payload content: %+v", payload)
	}
}

func TestCampaignWorker_StartTask_DynamicResolution(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Initialize repos
	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	// Create workspace
	ws, err := wsRepo.Create(ctx, "camp_start_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Create tag
	tag, err := tagRepo.CreateTag(ctx, ws.ID, "VIP", "#3b82f6")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	// Create contacts with whatsapp identities
	contact1, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511999998888", "Alice", "", "5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact1: %v", err)
	}
	contact2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5521988887777", "Bob", "", "5521988887777")
	if err != nil {
		t.Fatalf("failed to create contact2: %v", err)
	}

	// Tag both contacts
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact1.ID, tag.ID)
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact2.ID, tag.ID)

	// Ensure Streams
	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-start-consumer-" + uuid.New().String()
	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	// Create campaign with tag_ids and no pre-resolved recipients
	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Dynamic Tag Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients:   []domain.CampaignRecipient{}, // empty — tags will resolve
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	// Publish a CampaignStartTask (not a batch task)
	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	err = publisher.Publish(ctx, "campaigns.start", startBytes, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to publish start task: %v", err)
	}

	// Start worker
	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	// Wait for campaign to complete (start resolves → publishes batches → batches dispatch → completed)
	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		status := "nil"
		if finalCamp != nil {
			status = string(finalCamp.Status)
		}
		t.Fatalf("campaign expected completed, got: %s", status)
	}

	// Verify recipients were persisted
	if finalCamp.TotalRecipients != 2 {
		t.Errorf("expected 2 total recipients, got %d", finalCamp.TotalRecipients)
	}

	// Verify recipient records exist
	pendingStatus := domain.RecipientStatusPending
	_ = pendingStatus
	allRecs, err := campRepo.ListRecipients(ctx, camp.ID, nil, 100)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Errorf("expected 2 recipient records, got %d", len(allRecs))
	}
}

func TestCampaignWorker_StartTask_EmptyResolution(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_empty_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Create a tag with no contacts
	tag, err := tagRepo.CreateTag(ctx, ws.ID, "EmptyTag", "#ff0000")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	_, _ = EnsureCampaignStream(ctx, nc)
	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-empty-start-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Empty Tag Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients:   []domain.CampaignRecipient{},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	_ = publisher.Publish(ctx, "campaigns.start", startBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	// Wait for campaign to complete (should complete with zero recipients)
	var finalCamp *domain.Campaign
	for i := 0; i < 20; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		status := "nil"
		if finalCamp != nil {
			status = string(finalCamp.Status)
		}
		t.Fatalf("campaign with empty tags expected completed, got: %s", status)
	}

	// Verify audit event was emitted for completed_empty
	events := mockAudit.Events()
	foundEmpty := false
	for _, ev := range events {
		if ev.EventType == "campaign.dispatch.completed_empty" && ev.WorkspaceID == ws.ID {
			foundEmpty = true
			break
		}
	}
	if !foundEmpty {
		t.Errorf("expected campaign.dispatch.completed_empty audit event, got events: %+v", events)
	}
}

func TestCampaignWorker_StartTask_TagPlusCSVMerge(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_merge_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Create tag and contact
	tag, err := tagRepo.CreateTag(ctx, ws.ID, "TagMerge", "#00ff00")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511999998888", "Alice", "", "5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact.ID, tag.ID)

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-merge-start-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Merge Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients: []domain.CampaignRecipient{
			{To: "5511999998888", Variables: map[string]string{"name": "Alice CSV"}},   // duplicate with tag contact
			{To: "5531977776666", Variables: map[string]string{"name": "Charlie CSV"}}, // unique from CSV
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	_ = publisher.Publish(ctx, "campaigns.start", startBytes, uuid.New().String())

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		status := "nil"
		if finalCamp != nil {
			status = string(finalCamp.Status)
		}
		t.Fatalf("campaign expected completed, got: %s", status)
	}

	// Should have 2 recipients: Alice (from tag, deduped from CSV) + Charlie (from CSV only)
	if finalCamp.TotalRecipients != 2 {
		t.Errorf("expected 2 total recipients (tag + CSV, deduped), got %d", finalCamp.TotalRecipients)
	}

	allRecs, err := campRepo.ListRecipients(ctx, camp.ID, nil, 100)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Errorf("expected 2 recipient records, got %d", len(allRecs))
	}

	// Verify the phones are the ones we expect
	phones := make(map[string]bool)
	for _, rec := range allRecs {
		phones[rec.Phone] = true
	}
	if !phones["5511999998888"] {
		t.Errorf("expected Alice's phone 5511999998888 in recipients")
	}
	if !phones["5531977776666"] {
		t.Errorf("expected Charlie's phone 5531977776666 in recipients")
	}
}

func TestCampaignWorker_StartTask_SkippedContacts(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_skipped_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	tag, err := tagRepo.CreateTag(ctx, ws.ID, "MixedTag", "#a855f7")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	// Contact 1: Valid WhatsApp contact
	contact1, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511999998888", "Alice Valid", "", "5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact1: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact1.ID, tag.ID)

	// Contact 2: Telegram only contact (no WhatsApp identity)
	contact2, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "telegram_user_bob", "Bob TelegramOnly", "", "telegram_user_bob")
	if err != nil {
		t.Fatalf("failed to create contact2: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact2.ID, tag.ID)

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-skipped-consumer-" + uuid.New().String()
	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Skipped Contacts Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients:   []domain.CampaignRecipient{},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	err = publisher.Publish(ctx, "campaigns.start", startBytes, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to publish start task: %v", err)
	}

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		status := "nil"
		if finalCamp != nil {
			status = string(finalCamp.Status)
		}
		t.Fatalf("campaign expected completed, got: %s", status)
	}

	// Verify TotalRecipients in DB has 2 records
	if finalCamp.TotalRecipients != 2 {
		t.Errorf("expected total_recipients = 2, got %d", finalCamp.TotalRecipients)
	}

	// Verify records in DB: 1 pending/sent, 1 skipped
	allRecs, err := campRepo.ListRecipients(ctx, camp.ID, nil, 100)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Fatalf("expected 2 recipient records, got %d", len(allRecs))
	}

	var foundPendingOrSent, foundSkipped bool
	for _, r := range allRecs {
		if r.Phone == "5511999998888" && (r.Status == domain.RecipientStatusPending || r.Status == domain.RecipientStatusSent) {
			foundPendingOrSent = true
		}
		if r.Phone == "telegram_user_bob" && r.Status == domain.RecipientStatusSkipped {
			foundSkipped = true
		}
	}
	if !foundPendingOrSent {
		t.Errorf("expected Alice with phone 5511999998888 to be pending or sent")
	}
	if !foundSkipped {
		t.Errorf("expected Bob with identity telegram_user_bob to have status skipped, got recs: %+v", allRecs)
	}

	// Verify individual audit log was emitted for the skipped contact
	events := mockAudit.Events()
	var skippedEventFound bool
	expectedBobTrace := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "telegram_user_bob")
	for _, ev := range events {
		if ev.EventType == "campaign.dispatch.skipped" && ev.TraceID == expectedBobTrace && ev.WorkspaceID == ws.ID {
			skippedEventFound = true
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["status"] != "skipped" {
					t.Errorf("expected audit payload status 'skipped', got %v", payload["status"])
				}
				if payload["recipient"] != "telegram_user_bob" {
					t.Errorf("expected audit payload recipient 'telegram_user_bob', got %v", payload["recipient"])
				}
				if payload["recipient_id"] != "telegram_user_bob" {
					t.Errorf("expected audit payload recipient_id 'telegram_user_bob', got %v", payload["recipient_id"])
				}
				if payload["workspace_id"] != ws.ID.String() {
					t.Errorf("expected audit payload workspace_id '%s', got %v", ws.ID.String(), payload["workspace_id"])
				}
				if payload["campaign_id"] != camp.ID.String() {
					t.Errorf("expected audit payload campaign_id '%s', got %v", camp.ID.String(), payload["campaign_id"])
				}
				if payload["trace_id"] != expectedBobTrace {
					t.Errorf("expected audit payload trace_id '%s', got %v", expectedBobTrace, payload["trace_id"])
				}
			}
		}
	}
	if !skippedEventFound {
		t.Errorf("expected campaign.dispatch.skipped audit event for Bob, got events: %+v", events)
	}

	// Verify sent count is 1 (only Alice was dispatched, Bob was omitted from batch payload)
	if finalCamp.SentRecipients != 1 {
		t.Errorf("expected 1 sent recipient, got %d", finalCamp.SentRecipients)
	}
}

func TestCampaignWorker_StartTask_TagOverridesCSV(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_override_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	tag, err := tagRepo.CreateTag(ctx, ws.ID, "CanonicalTag", "#10b981")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	// Tag contact with canonical name, custom attributes, and database ID
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511999998888", "Canonical Alice From Tag", "", "5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}
	_ = contactRepo.UpdateAttributes(ctx, ws.ID, contact.ID, map[string]string{
		"tier": "Gold",
		"city": "São Paulo",
	})
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact.ID, tag.ID)

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-override-consumer-" + uuid.New().String()
	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Override Test Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients: []domain.CampaignRecipient{
			{
				To: "5511999998888",
				Variables: map[string]string{
					"name":     "Alice Override From CSV",
					"city":     "Campinas",
					"discount": "20%",
				},
			},
			{To: "5511988887777", Variables: map[string]string{"name": "Unique Bob From CSV"}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	err = publisher.Publish(ctx, "campaigns.start", startBytes, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to publish start task: %v", err)
	}

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("campaign expected completed, got: %v", finalCamp)
	}

	// Verify recipients in DB
	allRecs, err := campRepo.ListRecipients(ctx, camp.ID, nil, 100)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Fatalf("expected 2 total recipients, got %d", len(allRecs))
	}

	for _, rec := range allRecs {
		if rec.Phone == "5511999998888" {
			// Tag contact identity wins (ContactID set)
			if rec.ContactID == nil || *rec.ContactID != contact.ID {
				t.Errorf("expected tag contact ID %s, got %v", contact.ID, rec.ContactID)
			}
			// CSV variable overrides name and city
			if rec.Variables["name"] != "Alice Override From CSV" {
				t.Errorf("expected CSV overridden name 'Alice Override From CSV', got %q", rec.Variables["name"])
			}
			if rec.Variables["city"] != "Campinas" {
				t.Errorf("expected CSV overridden city 'Campinas', got %q", rec.Variables["city"])
			}
			// CSV variable supplements discount
			if rec.Variables["discount"] != "20%" {
				t.Errorf("expected CSV variable discount '20%%', got %q", rec.Variables["discount"])
			}
			// Contact attribute tier is preserved
			if rec.Variables["tier"] != "Gold" {
				t.Errorf("expected preserved contact attribute tier 'Gold', got %q", rec.Variables["tier"])
			}
		}
		if rec.Phone == "5511988887777" {
			if rec.ContactID != nil {
				t.Errorf("expected nil ContactID for static CSV recipient, got %v", rec.ContactID)
			}
			if rec.Variables["name"] != "Unique Bob From CSV" {
				t.Errorf("expected name 'Unique Bob From CSV', got %q", rec.Variables["name"])
			}
		}
	}
}

func TestCampaignWorker_StartTask_Idempotency(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_idempotent_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	tag, err := tagRepo.CreateTag(ctx, ws.ID, "IdempotentTag", "#f59e0b")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	contact1, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5511999998888", "Alice", "", "5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact1: %v", err)
	}
	contact2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "5521988887777", "Bob", "", "5521988887777")
	if err != nil {
		t.Fatalf("failed to create contact2: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact1.ID, tag.ID)
	_ = tagRepo.AddTagToContact(ctx, ws.ID, contact2.ID, tag.ID)

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-idempotent-consumer-" + uuid.New().String()
	consumer, err := EnsureCampaignConsumer(ctx, campStream, consumerName)
	if err != nil {
		t.Fatalf("EnsureCampaignConsumer failed: %v", err)
	}

	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Idempotent Dynamic Camp",
		Status:       domain.CampaignStatusDraft,
		BatchSize:    10,
		DelaySeconds: 1,
		Channel:      &channel,
		TagIDs:       []uuid.UUID{tag.ID},
		Recipients:   []domain.CampaignRecipient{},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// 1. Verify AddRecipients repository idempotency directly: calling it twice with identical records succeeds
	c1ID := contact1.ID
	c2ID := contact2.ID
	recs := []domain.CampaignRecipientRecord{
		{ContactID: &c1ID, Phone: "5511999998888", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Alice"}},
		{ContactID: &c2ID, Phone: "5521988887777", Status: domain.RecipientStatusPending, Variables: map[string]string{"name": "Bob"}},
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recs); err != nil {
		t.Fatalf("first AddRecipients failed: %v", err)
	}
	if err := campRepo.AddRecipients(ctx, camp.ID, recs); err != nil {
		t.Fatalf("second AddRecipients failed on duplicate insert: %v", err)
	}

	mockAudit := &fakeAuditWriter{}
	publisher := NewJetStreamPublisher(nc)

	// 2. Publish start task #1
	startTask := domain.CampaignStartTask{
		CampaignID:  camp.ID,
		WorkspaceID: ws.ID,
	}
	startBytes, _ := json.Marshal(startTask)
	startTraceID := fmt.Sprintf("campaign_%s_start", camp.ID)
	err = publisher.Publish(ctx, "campaigns.start", startBytes, startTraceID)
	if err != nil {
		t.Fatalf("failed to publish start task #1: %v", err)
	}

	// Publish start task #2 (simulating duplicate delivery / retry)
	err = publisher.Publish(ctx, "campaigns.start", startBytes, startTraceID)
	if err != nil {
		t.Fatalf("failed to publish start task #2: %v", err)
	}

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, tagRepo)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("campaign expected completed, got: %v", finalCamp)
	}

	// Total recipients must remain exactly 2 without duplicate DB rows
	if finalCamp.TotalRecipients != 2 {
		t.Errorf("expected total_recipients = 2, got %d", finalCamp.TotalRecipients)
	}

	allRecs, err := campRepo.ListRecipients(ctx, camp.ID, nil, 100)
	if err != nil {
		t.Fatalf("failed to list recipients: %v", err)
	}
	if len(allRecs) != 2 {
		t.Errorf("expected exactly 2 recipient rows, got %d", len(allRecs))
	}
}

func TestCampaignWorker_PostgresAuditIntegration(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_pg_audit_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE workspace_id = $1", ws.ID)
		_ = wsRepo.Delete(context.Background(), ws.ID)
	}()

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-pg-audit-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	channel := "whatsapp"
	recipientPhone := "5511998877665"
	camp := &domain.Campaign{
		WorkspaceID:  ws.ID,
		Name:         "Postgres Audit Integration Campaign",
		Status:       domain.CampaignStatusSending,
		BatchSize:    1,
		DelaySeconds: 1,
		Channel:      &channel,
		Recipients: []domain.CampaignRecipient{
			{To: recipientPhone, Variables: map[string]string{"name": "AuditTester"}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Real batch writer to Postgres audit_logs
	auditWriter := audit.NewWriter(pool, 100, 1)

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

	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, auditWriter, nil)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 50; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("expected campaign to complete, got status: %v", finalCamp)
	}

	// Close audit writer to flush batch to Postgres
	if err := auditWriter.Close(); err != nil {
		t.Fatalf("failed to close auditWriter: %v", err)
	}

	// Query PostgreSQL audit_logs table directly to verify persistence
	expectedTrace := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), recipientPhone)
	var count int
	var payloadBytes []byte
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(payload::text), '{}')::bytea
		 FROM audit_logs 
		 WHERE workspace_id = $1 AND trace_id = $2 AND event_type = $3`,
		ws.ID, expectedTrace, "campaign.dispatch.sent",
	).Scan(&count, &payloadBytes)
	if err != nil {
		t.Fatalf("failed to query audit_logs: %v", err)
	}

	if count < 1 {
		t.Fatalf("expected at least 1 audit_logs row for trace_id %s, got %d", expectedTrace, count)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("failed to unmarshal audit_logs payload: %v", err)
	}

	if payload["status"] != "sent" {
		t.Errorf("expected payload status 'sent', got %v", payload["status"])
	}
	if payload["recipient"] != recipientPhone {
		t.Errorf("expected payload recipient '%s', got %v", recipientPhone, payload["recipient"])
	}
	if payload["recipient_id"] != recipientPhone {
		t.Errorf("expected payload recipient_id '%s', got %v", recipientPhone, payload["recipient_id"])
	}
	if payload["workspace_id"] != ws.ID.String() {
		t.Errorf("expected payload workspace_id '%s', got %v", ws.ID.String(), payload["workspace_id"])
	}
	if payload["campaign_id"] != camp.ID.String() {
		t.Errorf("expected payload campaign_id '%s', got %v", camp.ID.String(), payload["campaign_id"])
	}
	if payload["trace_id"] != expectedTrace {
		t.Errorf("expected payload trace_id '%s', got %v", expectedTrace, payload["trace_id"])
	}
}

func TestCampaignWorker_PrecisionRateLimiting(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "camp_ratelimit_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, _ = EnsureCampaignStream(ctx, nc)
	_, _ = EnsureStream(ctx, nc)

	js, _ := jetstream.New(nc)
	campStream, _ := js.Stream(ctx, "CAMPAIGNS")
	consumerName := "test-ratelimit-consumer-" + uuid.New().String()
	consumer, _ := EnsureCampaignConsumer(ctx, campStream, consumerName)

	// 600 msgs/min = 10 msgs/sec = 100ms per message
	rateLimit := 600
	channel := "whatsapp"
	camp := &domain.Campaign{
		WorkspaceID:     ws.ID,
		Name:            "Precision Rate Limit Camp",
		Status:          domain.CampaignStatusSending,
		BatchSize:       10,
		DelaySeconds:    0,
		RateLimitPerMin: &rateLimit,
		Channel:         &channel,
		Recipients: []domain.CampaignRecipient{
			{To: "5511999991111", Variables: map[string]string{}},
			{To: "5511999992222", Variables: map[string]string{}},
			{To: "5511999993333", Variables: map[string]string{}},
		},
	}
	camp, err = campRepo.Create(ctx, camp)
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	task := CampaignBatchTask{
		CampaignID:      camp.ID,
		WorkspaceID:     ws.ID,
		BatchIndex:      1,
		TotalBatches:    1,
		Recipients:      camp.Recipients,
		DelaySeconds:    0,
		RateLimitPerMin: &rateLimit,
	}
	taskBytes, _ := json.Marshal(task)

	start := time.Now()
	_ = publisher.Publish(ctx, "campaigns.batches", taskBytes, uuid.New().String())

	mockAudit := &fakeAuditWriter{}
	worker := NewCampaignWorker(ctx, consumer, campRepo, connRepo, dispatchRepo, publisher, mockAudit, nil)
	defer worker.Stop()

	// Wait for campaign completion
	var finalCamp *domain.Campaign
	for i := 0; i < 30; i++ {
		finalCamp, _ = campRepo.GetByID(ctx, camp.ID)
		if finalCamp != nil && finalCamp.Status == domain.CampaignStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := time.Since(start)

	if finalCamp == nil || finalCamp.Status != domain.CampaignStatusCompleted {
		t.Fatalf("expected campaign to be completed, got status: %v", finalCamp)
	}

	// 3 recipients paced at 100ms interval:
	// First is instant (burst 1), 2nd waits 100ms, 3rd waits 100ms -> at least ~200ms
	if elapsed < 150*time.Millisecond {
		t.Errorf("expected pacing to take at least 150ms for 3 recipients at 600 msgs/min, took %v", elapsed)
	}
}

func TestCreateRateLimiter(t *testing.T) {
	rate60 := 60
	rate120 := 120
	rate0 := 0
	rateNegative := -5

	tests := []struct {
		name            string
		rateLimitPerMin *int
		delaySeconds    int
		expectedLimit   rate.Limit
	}{
		{
			name:            "precision rate 60 msgs/min -> 1 msg/sec",
			rateLimitPerMin: &rate60,
			delaySeconds:    10,
			expectedLimit:   rate.Every(1 * time.Second),
		},
		{
			name:            "precision rate 120 msgs/min -> 2 msgs/sec",
			rateLimitPerMin: &rate120,
			delaySeconds:    5,
			expectedLimit:   rate.Every(500 * time.Millisecond),
		},
		{
			name:            "nil rate limit with delay_seconds 5 -> 0.2 msgs/sec",
			rateLimitPerMin: nil,
			delaySeconds:    5,
			expectedLimit:   rate.Every(5 * time.Second),
		},
		{
			name:            "nil rate limit with delay_seconds <= 0 -> default 1s",
			rateLimitPerMin: nil,
			delaySeconds:    0,
			expectedLimit:   rate.Every(1 * time.Second),
		},
		{
			name:            "zero rate limit falls back to delay_seconds",
			rateLimitPerMin: &rate0,
			delaySeconds:    3,
			expectedLimit:   rate.Every(3 * time.Second),
		},
		{
			name:            "negative rate limit falls back to default 1s",
			rateLimitPerMin: &rateNegative,
			delaySeconds:    0,
			expectedLimit:   rate.Every(1 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lim := createRateLimiter(tt.rateLimitPerMin, tt.delaySeconds)
			if lim == nil {
				t.Fatalf("expected non-nil limiter")
			}
			if lim.Limit() != tt.expectedLimit {
				t.Errorf("expected limit %v, got %v", tt.expectedLimit, lim.Limit())
			}
			if lim.Burst() != 1 {
				t.Errorf("expected burst 1, got %d", lim.Burst())
			}
		})
	}
}



