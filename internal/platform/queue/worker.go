package queue

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
)

// Worker reads messages from a JetStream consumer and logs them.
// It is a stub that validates the consumer pipeline end-to-end.
// Phase 4 replaces the log-and-ack body with a real Dispatcher.
type Worker struct {
	consumer jetstream.Consumer
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewWorker starts a goroutine that reads messages from consumer and logs them.
// The goroutine stops when ctx is cancelled. Call Stop() to initiate shutdown.
func NewWorker(ctx context.Context, consumer jetstream.Consumer) *Worker {
	ctx, cancel := context.WithCancel(ctx)
	w := &Worker{
		consumer: consumer,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	go w.run(ctx)
	return w
}

// run is the main consumer loop. It reads messages from the consumer, logs
// each one, and acknowledges it.
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
			// ErrMsgIteratorClosed means context was cancelled or Stop was called
			if ctx.Err() != nil {
				slog.Info("message worker stopped")
				return
			}
			slog.Error("worker: failed to get next message", "error", err)
			continue
		}

		// Extract trace_id from Nats-Msg-Id header
		traceID := ""
		if headers := msg.Headers(); headers != nil {
			traceID = headers.Get("Nats-Msg-Id")
		}

		// Stub: just log the message (Phase 4 replaces with real dispatch)
		data := msg.Data()
		preview := string(data)
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		slog.Info("message dispatched",
			"trace_id", traceID,
			"subject", msg.Subject(),
			"payload_preview", preview,
		)

		if err := msg.Ack(); err != nil {
			slog.Error("worker: failed to ack message", "error", err, "trace_id", traceID)
		}
	}
}

// Stop cancels the consumer context and waits for the goroutine to finish.
func (w *Worker) Stop() {
	w.cancel()
	<-w.done
}
