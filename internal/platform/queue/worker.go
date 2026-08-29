package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// Worker reads messages from a JetStream consumer and delegates processing
// to the DispatchOrchestrator. The worker owns the consumer lifecycle;
// the orchestrator owns the dispatch pipeline.
type Worker struct {
	consumer     jetstream.Consumer
	cancel       context.CancelFunc
	done         chan struct{}
	orchestrator *DispatchOrchestrator
	inFlight     atomic.Int64
	drainTimeout time.Duration
	claimsRepo   *repository.DeliveryClaimRepository
	workerID     string
}

// NewWorker starts a goroutine that reads messages from consumer and
// delegates to the orchestrator. Call Stop() to initiate shutdown.

func (w *Worker) SetClaimsRepo(repo *repository.DeliveryClaimRepository, workerID string) {
	w.claimsRepo = repo
	w.workerID = workerID
}

func (w *Worker) SetDrainTimeout(d time.Duration) {
	w.drainTimeout = d
}

func (w *Worker) InFlightCount() int64 {
	return w.inFlight.Load()
}

func NewWorker(
	ctx context.Context,
	consumer jetstream.Consumer,
	orchestrator *DispatchOrchestrator,
) *Worker {
	ctx, cancel := context.WithCancel(ctx)
	w := &Worker{
		drainTimeout: 30 * time.Second,
		consumer:     consumer,
		cancel:       cancel,
		done:         make(chan struct{}),
		orchestrator: orchestrator,
	}

	go w.run(ctx)
	return w
}

// run is the main consumer loop. It reads messages, deserializes them,
// and delegates to the orchestrator.
func (w *Worker) run(ctx context.Context) {
	defer close(w.done)

	consumeCtx, err := w.consumer.Consume(func(msg jetstream.Msg) {
		w.processMessage(ctx, msg)
	})
	if err != nil {
		slog.Error("worker: failed to start consume", "error", err)
		return
	}
	defer consumeCtx.Stop()

	slog.Info("message worker started", "consumer", w.consumer.CachedInfo().Config.Name)

	<-ctx.Done()
	slog.Info("message worker stopped")
}

// processMessage deserializes the JSON payload, enriches the context,
// and delegates to the orchestrator's Process method.
func (w *Worker) processMessage(ctx context.Context, msg jetstream.Msg) {
	w.inFlight.Add(1)
	defer w.inFlight.Add(-1)
	var qMsg domain.QueueMessage
	if err := json.Unmarshal(msg.Data(), &qMsg); err != nil {
		slog.Error("worker: failed to unmarshal payload", "error", err)
		_ = msg.Ack()
		return
	}

	traceID := qMsg.TraceID
	if traceID == "" {
		if headers := msg.Headers(); headers != nil {
			traceID = headers.Get("Nats-Msg-Id")
		}
	}

	workspaceID := qMsg.WorkspaceID
	ctx = tenant.WithWorkspaceID(ctx, workspaceID)

	attempt := retryAttempt(adaptMsg(msg))

	if w.orchestrator == nil {
		slog.Warn("worker: no orchestrator configured, acking message", "trace_id", traceID)
		_ = msg.Ack()
		return
	}

	if w.claimsRepo != nil && workspaceID != uuid.Nil {
		claim := &repository.DeliveryClaim{
			WorkspaceID:      workspaceID,
			TraceID:          traceID,
			MessageSubject:   msg.Subject(),
			WorkerInstanceID: w.workerID,
		}
		inserted, claimErr := w.claimsRepo.CreateClaim(ctx, claim)
		if claimErr == nil && !inserted {
			slog.Warn("worker: message delivery already claimed by another worker, skipping", "trace_id", traceID)
			_ = msg.Ack()
			return
		}
		defer func() {
			_ = w.claimsRepo.ReleaseClaim(ctx, workspaceID, traceID)
		}()
	}

	err := w.orchestrator.Process(ctx, adaptMsg(msg), &qMsg, attempt)
	if err != nil && channel.IsUncertain(err) {
		slog.Warn("worker: dispatch returned uncertain error, skipping auto-retry to prevent double dispatch", "trace_id", traceID, "error", err)
	}
}

// adaptMsg wraps a jetstream.Msg as a DispatchMessage.
func adaptMsg(msg jetstream.Msg) DispatchMessage {
	return &jetStreamMsg{msg: msg}
}

// jetStreamMsg adapts jetstream.Msg to the DispatchMessage port.
type jetStreamMsg struct {
	msg jetstream.Msg
}

func (m *jetStreamMsg) Data() []byte { return m.msg.Data() }

func (m *jetStreamMsg) Headers() map[string]string {
	h := m.msg.Headers()
	if h == nil {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func (m *jetStreamMsg) Ack() error { return m.msg.Ack() }

func (m *jetStreamMsg) NakWithDelay(delay time.Duration) error { return m.msg.NakWithDelay(delay) }

// Stop cancels the consumer context and waits for the goroutine to finish.
func (w *Worker) Stop() {
	w.cancel()

	// Graceful drain in-flight messages up to drainTimeout
	timeout := w.drainTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for w.inFlight.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	<-w.done
}
