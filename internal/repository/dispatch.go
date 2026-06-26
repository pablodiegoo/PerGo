package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDispatchNotFound is returned when a message dispatch record cannot be found.
var ErrDispatchNotFound = errors.New("dispatch not found")

// MessageDispatch represents a row in the message_dispatches table.
type MessageDispatch struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	TraceID        string
	CurrentChannel string
	Status         string
	FallbackIndex  int
	ErrorMessage   *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MessageDispatchRepository manages message dispatch state in the database.
type MessageDispatchRepository struct {
	pool *pgxpool.Pool
}

// NewMessageDispatchRepository creates a new MessageDispatchRepository.
func NewMessageDispatchRepository(pool *pgxpool.Pool) *MessageDispatchRepository {
	return &MessageDispatchRepository{pool: pool}
}

// GetOrCreateDispatch retrieves an existing dispatch by trace_id or inserts a new one if it doesn't exist.
func (r *MessageDispatchRepository) GetOrCreateDispatch(ctx context.Context, workspaceID uuid.UUID, traceID string, initialChannel string) (*MessageDispatch, error) {
	var d MessageDispatch
	err := r.pool.QueryRow(ctx,
		`INSERT INTO message_dispatches (workspace_id, trace_id, current_channel, status, fallback_index)
		 VALUES ($1, $2, $3, 'queued', 0)
		 ON CONFLICT (trace_id) DO UPDATE 
		 SET trace_id = EXCLUDED.trace_id -- dummy update to return existing row
		 RETURNING id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, created_at, updated_at`,
		workspaceID, traceID, initialChannel,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateDispatchStatus updates the state of an existing message dispatch.
func (r *MessageDispatchRepository) UpdateDispatchStatus(ctx context.Context, id uuid.UUID, status string, currentChannel string, fallbackIndex int, errMsg *string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE message_dispatches
		 SET status = $1, current_channel = $2, fallback_index = $3, error_message = $4, updated_at = now()
		 WHERE id = $5`,
		status, currentChannel, fallbackIndex, errMsg, id,
	)
	return err
}

// GetByTraceID retrieves a message dispatch record by trace_id.
func (r *MessageDispatchRepository) GetByTraceID(ctx context.Context, traceID string) (*MessageDispatch, error) {
	var d MessageDispatch
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, created_at, updated_at
		 FROM message_dispatches WHERE trace_id = $1`,
		traceID,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispatchNotFound
		}
		return nil, err
	}
	return &d, nil
}
