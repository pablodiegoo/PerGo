package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestCampaignWorker_Success(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize repos
	wsRepo := repository.NewWorkspaceRepository(pool)
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
	worker := NewCampaignWorker(ctx, consumer, campRepo, dispatchRepo, publisher)
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
	campRepo := repository.NewCampaignRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, _ := wsRepo.Create(ctx, "camp_worker_ws_cancel_"+uuid.New().String())
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
	camp, _ = campRepo.Create(ctx, camp)

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

	worker := NewCampaignWorker(ctx, consumer, campRepo, dispatchRepo, publisher)
	defer worker.Stop()

	// Wait to see if NATS message gets Acked without creating dispatches
	// Fetch from the campaigns stream consumer to verify it is empty (acked)
	time.Sleep(500 * time.Millisecond)

	traceID := fmt.Sprintf("campaign_%s_%s", camp.ID.String(), "5511999998888")
	_, err := dispatchRepo.GetByTraceID(ctx, traceID)
	if err == nil {
		t.Errorf("expected no dispatch log for cancelled campaign, but found one")
	}
}
