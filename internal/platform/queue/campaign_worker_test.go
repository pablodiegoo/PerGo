package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
)

// fakeAuditWriter implements audit.Writer for tests across the queue package.
type fakeAuditWriter struct {
	mu     sync.Mutex
	events []audit.Event
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

// mockEngine implements CampaignEngine for worker adapter smoke tests.
type mockEngine struct {
	startTaskInvoked bool
	lastStartTask    domain.CampaignStartTask
	startTaskErr     error

	batchTaskInvoked bool
	lastBatchTask    domain.CampaignBatchTask
	batchTaskErr     error
}

func (m *mockEngine) ProcessStartTask(ctx context.Context, task domain.CampaignStartTask) error {
	m.startTaskInvoked = true
	m.lastStartTask = task
	return m.startTaskErr
}

func (m *mockEngine) ProcessBatchTask(ctx context.Context, task domain.CampaignBatchTask) error {
	m.batchTaskInvoked = true
	m.lastBatchTask = task
	return m.batchTaskErr
}

// mockJetStreamMsg implements jetstream.Msg in-memory without external broker dependencies.
type mockJetStreamMsg struct {
	data       []byte
	subject    string
	acked      bool
	nacked     bool
	nakDelay   time.Duration
	termed     bool
	termReason string
}

func (m *mockJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockJetStreamMsg) Data() []byte                              { return m.data }
func (m *mockJetStreamMsg) Headers() nats.Header                     { return nil }
func (m *mockJetStreamMsg) Subject() string                           { return m.subject }
func (m *mockJetStreamMsg) Reply() string                             { return "" }
func (m *mockJetStreamMsg) Ack() error {
	m.acked = true
	return nil
}
func (m *mockJetStreamMsg) DoubleAck(context.Context) error {
	m.acked = true
	return nil
}
func (m *mockJetStreamMsg) Nak() error {
	m.nacked = true
	return nil
}
func (m *mockJetStreamMsg) NakWithDelay(d time.Duration) error {
	m.nacked = true
	m.nakDelay = d
	return nil
}
func (m *mockJetStreamMsg) InProgress() error { return nil }
func (m *mockJetStreamMsg) Term() error {
	m.termed = true
	return nil
}
func (m *mockJetStreamMsg) TermWithReason(reason string) error {
	m.termed = true
	m.termReason = reason
	return nil
}

// mockConsumer implements a minimal jetstream.Consumer for worker lifecycle testing.
type mockConsumer struct {
	jetstream.Consumer
	handlerMu sync.Mutex
	handler   jetstream.MessageHandler
	consumeCh chan struct{}
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{
		consumeCh: make(chan struct{}),
	}
}

func (m *mockConsumer) Consume(handler jetstream.MessageHandler, opts ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	m.handlerMu.Lock()
	m.handler = handler
	m.handlerMu.Unlock()
	close(m.consumeCh)
	return newMockConsumeContext(), nil
}

func (m *mockConsumer) getHandler() jetstream.MessageHandler {
	m.handlerMu.Lock()
	defer m.handlerMu.Unlock()
	return m.handler
}

func (m *mockConsumer) CachedInfo() *jetstream.ConsumerInfo {
	return &jetstream.ConsumerInfo{
		Config: jetstream.ConsumerConfig{Name: "test-campaign-worker"},
	}
}

type mockConsumeContext struct {
	closed chan struct{}
}

func newMockConsumeContext() *mockConsumeContext {
	ch := make(chan struct{})
	close(ch)
	return &mockConsumeContext{closed: ch}
}

func (m *mockConsumeContext) Stop()  {}
func (m *mockConsumeContext) Drain() {}
func (m *mockConsumeContext) Closed() <-chan struct{} {
	return m.closed
}

func TestCampaignWorker_StartTask_Success(t *testing.T) {
	engine := &mockEngine{}
	worker := NewCampaignWorker(context.Background(), nil, engine)

	campaignID := uuid.New()
	workspaceID := uuid.New()
	task := domain.CampaignStartTask{
		CampaignID:  campaignID,
		WorkspaceID: workspaceID,
	}

	payload, err := json.Marshal(task)
	require.NoError(t, err)

	msg := &mockJetStreamMsg{
		data:    payload,
		subject: "campaigns.start",
	}

	worker.HandleStartCampaign(context.Background(), msg)

	assert.True(t, engine.startTaskInvoked, "expected ProcessStartTask to be invoked")
	assert.Equal(t, campaignID, engine.lastStartTask.CampaignID)
	assert.Equal(t, workspaceID, engine.lastStartTask.WorkspaceID)
	assert.True(t, msg.acked, "expected message to be acked")
	assert.False(t, msg.nacked, "expected message not to be nacked")
}

func TestCampaignWorker_BatchTask_Success(t *testing.T) {
	engine := &mockEngine{}
	worker := NewCampaignWorker(context.Background(), nil, engine)

	campaignID := uuid.New()
	workspaceID := uuid.New()
	rateLimit := 120
	task := domain.CampaignBatchTask{
		CampaignID:   campaignID,
		WorkspaceID:  workspaceID,
		BatchIndex:   2,
		TotalBatches: 5,
		Recipients: []domain.CampaignRecipient{
			{To: "+5511999990001", Variables: map[string]string{"name": "Alice"}},
			{To: "+5511999990002", Variables: map[string]string{"name": "Bob"}},
		},
		DelaySeconds:     3,
		RateLimitPerMin:  &rateLimit,
		FallbackChannels: []string{"telegram"},
	}

	payload, err := json.Marshal(task)
	require.NoError(t, err)

	msg := &mockJetStreamMsg{
		data:    payload,
		subject: "campaigns.batches",
	}

	worker.HandleBatchCampaign(context.Background(), msg)

	assert.True(t, engine.batchTaskInvoked, "expected ProcessBatchTask to be invoked")
	assert.Equal(t, campaignID, engine.lastBatchTask.CampaignID)
	assert.Equal(t, workspaceID, engine.lastBatchTask.WorkspaceID)
	assert.Equal(t, 2, engine.lastBatchTask.BatchIndex)
	assert.Equal(t, 5, engine.lastBatchTask.TotalBatches)
	assert.Len(t, engine.lastBatchTask.Recipients, 2)
	assert.Equal(t, 3, engine.lastBatchTask.DelaySeconds)
	assert.Equal(t, &rateLimit, engine.lastBatchTask.RateLimitPerMin)
	assert.Equal(t, []string{"telegram"}, engine.lastBatchTask.FallbackChannels)
	assert.True(t, msg.acked, "expected message to be acked")
	assert.False(t, msg.nacked, "expected message not to be nacked")
}

func TestCampaignWorker_StartTask_MalformedPayload(t *testing.T) {
	engine := &mockEngine{}
	worker := NewCampaignWorker(context.Background(), nil, engine)

	msg := &mockJetStreamMsg{
		data:    []byte(`{"invalid_json": `),
		subject: "campaigns.start",
	}

	worker.HandleStartCampaign(context.Background(), msg)

	assert.False(t, engine.startTaskInvoked, "ProcessStartTask should not be invoked for malformed payload")
	assert.True(t, msg.acked, "malformed poison pill message should be acknowledged to prevent consumer stall")
	assert.False(t, msg.nacked)
}

func TestCampaignWorker_BatchTask_MalformedPayload(t *testing.T) {
	engine := &mockEngine{}
	worker := NewCampaignWorker(context.Background(), nil, engine)

	msg := &mockJetStreamMsg{
		data:    []byte(`not a json at all`),
		subject: "campaigns.batches",
	}

	worker.HandleBatchCampaign(context.Background(), msg)

	assert.False(t, engine.batchTaskInvoked, "ProcessBatchTask should not be invoked for malformed payload")
	assert.True(t, msg.acked, "malformed poison pill message should be acknowledged to prevent consumer stall")
	assert.False(t, msg.nacked)
}

func TestCampaignWorker_EngineError_Acknowledged(t *testing.T) {
	t.Run("start task error acknowledged", func(t *testing.T) {
		engine := &mockEngine{
			startTaskErr: errors.New("simulated database failure"),
		}
		worker := NewCampaignWorker(context.Background(), nil, engine)

		task := domain.CampaignStartTask{
			CampaignID:  uuid.New(),
			WorkspaceID: uuid.New(),
		}
		payload, err := json.Marshal(task)
		require.NoError(t, err)

		msg := &mockJetStreamMsg{
			data:    payload,
			subject: "campaigns.start",
		}

		worker.HandleStartCampaign(context.Background(), msg)

		assert.True(t, engine.startTaskInvoked)
		assert.True(t, msg.acked, "message must be acked even when engine returns error to avoid infinite loop on terminal errors")
	})

	t.Run("batch task error acknowledged", func(t *testing.T) {
		engine := &mockEngine{
			batchTaskErr: errors.New("simulated dispatch failure"),
		}
		worker := NewCampaignWorker(context.Background(), nil, engine)

		task := domain.CampaignBatchTask{
			CampaignID:  uuid.New(),
			WorkspaceID: uuid.New(),
			BatchIndex:  1,
		}
		payload, err := json.Marshal(task)
		require.NoError(t, err)

		msg := &mockJetStreamMsg{
			data:    payload,
			subject: "campaigns.batches",
		}

		worker.HandleBatchCampaign(context.Background(), msg)

		assert.True(t, engine.batchTaskInvoked)
		assert.True(t, msg.acked, "message must be acked even when engine returns error")
	})
}

func TestCampaignWorker_ConsumerRoutingAndLifecycle(t *testing.T) {
	engine := &mockEngine{}
	cons := newMockConsumer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := NewCampaignWorker(ctx, cons, engine)

	select {
	case <-cons.consumeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to call consumer.Consume")
	}

	handler := cons.getHandler()
	require.NotNil(t, handler, "worker should have registered Consume handler")

	// 1. Deliver start task via consumer callback
	startPayload, err := json.Marshal(domain.CampaignStartTask{
		CampaignID:  uuid.New(),
		WorkspaceID: uuid.New(),
	})
	require.NoError(t, err)

	startMsg := &mockJetStreamMsg{
		data:    startPayload,
		subject: "campaigns.start",
	}
	handler(startMsg)

	assert.True(t, engine.startTaskInvoked)
	assert.True(t, startMsg.acked)

	// 2. Deliver batch task via consumer callback
	batchPayload, err := json.Marshal(domain.CampaignBatchTask{
		CampaignID:  uuid.New(),
		WorkspaceID: uuid.New(),
		BatchIndex:  1,
	})
	require.NoError(t, err)

	batchMsg := &mockJetStreamMsg{
		data:    batchPayload,
		subject: "campaigns.batches",
	}
	handler(batchMsg)

	assert.True(t, engine.batchTaskInvoked)
	assert.True(t, batchMsg.acked)

	// 3. Graceful shutdown
	worker.Stop()
}
