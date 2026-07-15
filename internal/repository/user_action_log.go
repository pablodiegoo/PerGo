package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserActionLog represents an audit log entry for user or API action.
type UserActionLog struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	ActorType   string
	ActorID     string
	ActorName   string
	Action      string
	Source      string
	IPAddress   *string
	UserAgent   *string
	Metadata    []byte
	CreatedAt   time.Time
}

// UserActionLogRepository provides methods to persist and retrieve action logs.
type UserActionLogRepository struct {
	pool *pgxpool.Pool
}

// NewUserActionLogRepository creates a new UserActionLogRepository.
func NewUserActionLogRepository(pool *pgxpool.Pool) *UserActionLogRepository {
	return &UserActionLogRepository{pool: pool}
}

// Insert writes a new UserActionLog into the database.
func (r *UserActionLogRepository) Insert(ctx context.Context, log *UserActionLog) error {
	var id uuid.UUID
	var createdAt time.Time

	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_action_logs 
		(workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at
	`, log.WorkspaceID, log.ActorType, log.ActorID, log.ActorName, log.Action, log.Source, log.IPAddress, log.UserAgent, log.Metadata).Scan(&id, &createdAt)
	if err != nil {
		return fmt.Errorf("insert user action log: %w", err)
	}

	log.ID = id
	log.CreatedAt = createdAt
	return nil
}

// ListByWorkspace returns a paginated list of logs and the total count.
func (r *UserActionLogRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]UserActionLog, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Get total count
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM user_action_logs WHERE workspace_id = $1
	`, workspaceID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count user action logs: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata, created_at
		FROM user_action_logs
		WHERE workspace_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, workspaceID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list user action logs: %w", err)
	}
	defer rows.Close()

	var logs []UserActionLog
	for rows.Next() {
		var log UserActionLog
		err := rows.Scan(
			&log.ID,
			&log.WorkspaceID,
			&log.ActorType,
			&log.ActorID,
			&log.ActorName,
			&log.Action,
			&log.Source,
			&log.IPAddress,
			&log.UserAgent,
			&log.Metadata,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan user action log: %w", err)
		}
		logs = append(logs, log)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows user action logs: %w", err)
	}

	return logs, total, nil
}
