package audit

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer is the interface for writing audit events.
type Writer interface {
	// Write sends an event to the buffered channel for batch writing.
	// If the channel is full, the event is dropped and a warning is logged.
	Write(e Event) error

	// Close shuts down the writer, draining all remaining events before returning.
	Close() error
}

// NewWriter creates a new buffered batch writer that sends events to PostgreSQL
// via pgx.CopyFrom. Events are buffered in a bounded channel and flushed by
// background worker goroutines.
//
// Parameters:
//   - pool: the pgxpool.Pool for database connections
//   - bufSize: capacity of the internal event channel
//   - workers: number of background goroutines processing events
func NewWriter(pool *pgxpool.Pool, bufSize int, workers int) Writer {
	bw := &BatchWriter{
		ch:        make(chan Event, bufSize),
		pool:      pool,
		batchSize: 100,
	}
	bw.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go bw.worker()
	}
	return bw
}

// BatchWriter is the internal implementation of Writer that collects events
// into batches and flushes them via pgx.CopyFrom.
type BatchWriter struct {
	ch        chan Event
	pool      *pgxpool.Pool
	wg        sync.WaitGroup
	batchSize int
}

// Write sends an event to the batch writer's channel. If the channel is full,
// the event is dropped and a warning is logged.
func (w *BatchWriter) Write(e Event) error {
	select {
	case w.ch <- e:
		return nil
	default:
		slog.Warn("audit channel full, dropping event",
			"event_type", e.EventType,
			"trace_id", e.TraceID,
		)
		return nil
	}
}

// Close shuts down the writer by closing the channel and waiting for all
// workers to drain remaining events.
func (w *BatchWriter) Close() error {
	close(w.ch)
	w.wg.Wait()
	return nil
}

// worker collects events into a batch slice and flushes them when the batch
// is full or when the channel is closed.
func (w *BatchWriter) worker() {
	defer w.wg.Done()
	batch := make([]Event, 0, w.batchSize)
	for e := range w.ch {
		batch = append(batch, e)
		if len(batch) >= w.batchSize {
			w.flush(batch)
			batch = batch[:0]
		}
	}
	// Flush any remaining events after channel close
	if len(batch) > 0 {
		w.flush(batch)
	}
}

// flush writes a batch of events to PostgreSQL using pgx.CopyFrom.
func (w *BatchWriter) flush(events []Event) {
	if len(events) == 0 {
		return
	}

	conn, err := w.pool.Acquire(context.Background())
	if err != nil {
		slog.Error("failed to acquire pgx connection for audit flush",
			"error", err,
			"events_dropped", len(events),
		)
		return
	}
	defer conn.Release()

	_, err = conn.Conn().CopyFrom(
		context.Background(),
		pgx.Identifier{"audit_logs"},
		[]string{"workspace_id", "trace_id", "event_type", "payload", "created_at"},
		pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
			e := events[i]
			return []any{e.WorkspaceID, e.TraceID, e.EventType, e.Payload, e.CreatedAt}, nil
		}),
	)
	if err != nil {
		slog.Error("failed to flush audit batch",
			"error", err,
			"events_dropped", len(events),
		)
	}
}
