package audit

import (
	"context"
	"expvar"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	droppedEventsCounter  = expvar.NewInt("audit_events_dropped")
	fallbackEventsCounter = expvar.NewInt("audit_events_fallback")
)

// Writer is the interface for writing audit events.
type Writer interface {
	Write(e Event) error
	Close() error
	EnsurePartitions(ctx context.Context) error
}

func NewWriter(pool *pgxpool.Pool, bufSize int, workers int) Writer {
	bw := &BatchWriter{
		ch:        make(chan Event, bufSize),
		pool:      pool,
		batchSize: 100,
		stopCh:    make(chan struct{}),
	}

	if pool != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = bw.EnsurePartitions(ctx)
		cancel()
	}

	bw.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go bw.worker()
	}

	go bw.partitionMaintenanceLoop()

	return bw
}

type BatchWriter struct {
	ch        chan Event
	pool      *pgxpool.Pool
	wg        sync.WaitGroup
	batchSize int
	stopCh    chan struct{}
}

func (w *BatchWriter) Write(e Event) error {
	select {
	case w.ch <- e:
		return nil
	default:
		droppedEventsCounter.Add(1)
		slog.Warn("audit channel full, dropping event",
			"event_type", e.EventType,
			"trace_id", e.TraceID,
		)
		return nil
	}
}

func (w *BatchWriter) Close() error {
	close(w.stopCh)
	close(w.ch)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		slog.Error("audit batch writer close timed out after 10s")
		return fmt.Errorf("audit close timeout")
	}
}

func (w *BatchWriter) EnsurePartitions(ctx context.Context) error {
	if w.pool == nil {
		return nil
	}

	now := time.Now().UTC()
	months := []time.Time{
		time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
		time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC),
	}

	for _, m := range months {
		start := m.Format("2006-01-02")
		nextMonth := m.AddDate(0, 1, 0)
		end := nextMonth.Format("2006-01-02")
		partName := fmt.Sprintf("audit_logs_y%04dm%02d", m.Year(), int(m.Month()))

		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF audit_logs FOR VALUES FROM ('%s') TO ('%s')",
			pgx.Identifier{partName}.Sanitize(),
			start,
			end,
		)
		_, err := w.pool.Exec(ctx, query)
		if err != nil {
			slog.Error("failed to create audit partition", "partition", partName, "error", err)
		}
	}

	// Detach partitions older than 90 days
	oldCutoff := now.AddDate(0, 0, -90)
	oldMonth := time.Date(oldCutoff.Year(), oldCutoff.Month(), 1, 0, 0, 0, 0, time.UTC)
	oldPartName := fmt.Sprintf("audit_logs_y%04dm%02d", oldMonth.Year(), int(oldMonth.Month()))
	detachQuery := fmt.Sprintf("ALTER TABLE audit_logs DETACH PARTITION %s", pgx.Identifier{oldPartName}.Sanitize())
	_, _ = w.pool.Exec(ctx, detachQuery)

	return nil
}

func (w *BatchWriter) partitionMaintenanceLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = w.EnsurePartitions(ctx)
			cancel()
		}
	}
}

func (w *BatchWriter) worker() {
	defer w.wg.Done()
	batch := make([]Event, 0, w.batchSize)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case e, ok := <-w.ch:
			if !ok {
				if len(batch) > 0 {
					w.flushWithRetry(batch)
				}
				return
			}
			batch = append(batch, e)
			if len(batch) >= w.batchSize {
				w.flushWithRetry(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushWithRetry(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *BatchWriter) flushWithRetry(events []Event) {
	if len(events) == 0 {
		return
	}

	maxRetries := 3
	backoff := 100 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := w.flush(events)
		if err == nil {
			return
		}

		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	// Last resort fallback: log events to slog and increment counter
	fallbackEventsCounter.Add(int64(len(events)))
	for _, e := range events {
		slog.Error("audit batch write failed after retries, logging to slog fallback",
			"workspace_id", e.WorkspaceID,
			"trace_id", e.TraceID,
			"event_type", e.EventType,
		)
	}
}

func (w *BatchWriter) flush(events []Event) error {
	if w.pool == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire pgx connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"audit_logs"},
		[]string{"id", "workspace_id", "trace_id", "event_type", "payload", "created_at"},
		pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
			e := events[i]
			return []any{uuid.New(), e.WorkspaceID, e.TraceID, e.EventType, e.Payload, e.CreatedAt}, nil
		}),
	)
	return err
}
