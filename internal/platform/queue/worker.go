package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// Worker reads messages from a JetStream consumer and delegates processing
// to the DispatchOrchestrator. The worker owns the consumer lifecycle;
// the orchestrator owns the dispatch pipeline.
type Worker struct {
	consumer     jetstream.Consumer
	cancel       context.CancelFunc
	done         chan struct{}
	orchestrator *DispatchOrchestrator
}

// NewWorker starts a goroutine that reads messages from consumer and
// delegates to the orchestrator. Call Stop() to initiate shutdown.
func NewWorker(
	ctx context.Context,
	consumer jetstream.Consumer,
	orchestrator *DispatchOrchestrator,
) *Worker {
	ctx, cancel := context.WithCancel(ctx)
	w := &Worker{
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

	msgCtx, err := w.consumer.Messages()
	if err != nil {
		slog.Error("worker: failed to create messages context", "error", err)
		return
	}
	defer msgCtx.Stop()

	slog.Info("message worker started", "consumer", w.consumer.CachedInfo().Config.Name)

	for {
		msg, err := msgCtx.Next()
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("message worker stopped")
				return
			}
			slog.Error("worker: failed to get next message, recreating messages context", "error", err)
			msgCtx.Stop()

			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}

			newMsgCtx, err := w.consumer.Messages()
			if err != nil {
				slog.Error("worker: failed to recreate messages context", "error", err)
				continue
			}
			msgCtx = newMsgCtx
			continue
		}

		w.processMessage(ctx, msg)
	}
}

// processMessage deserializes the JSON payload, enriches the context,
// and delegates to the orchestrator's Process method.
func (w *Worker) processMessage(ctx context.Context, msg jetstream.Msg) {
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

	_ = w.orchestrator.Process(ctx, adaptMsg(msg), &qMsg, attempt)
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
	<-w.done
}


