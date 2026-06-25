package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecentEntry represents a recent audit log entry for the dashboard.
type RecentEntry struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	TraceID     string
	EventType   string
	Payload     []byte
	CreatedAt   time.Time
}

// Querier provides read-only access to audit log entries.
type Querier struct {
	pool *pgxpool.Pool
}

// NewQuerier creates a new audit log querier.
func NewQuerier(pool *pgxpool.Pool) *Querier {
	return &Querier{pool: pool}
}

// ListRecent returns the most recent audit log entries, limited to the given count.
func (q *Querier) ListRecent(ctx context.Context, limit int) ([]RecentEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := q.pool.Query(ctx,
		`SELECT id, workspace_id, trace_id, event_type, payload, created_at
		 FROM audit_logs
		 ORDER BY created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []RecentEntry
	for rows.Next() {
		var e RecentEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.TraceID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
