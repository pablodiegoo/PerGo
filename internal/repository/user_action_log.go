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
func (r *UserActionLogRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, limit, offset int, actorType, source string) ([]UserActionLog, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	baseQuery := `FROM user_action_logs WHERE workspace_id = $1`
	args := []any{workspaceID}
	placeholderIndex := 2

	if actorType != "" {
		baseQuery += fmt.Sprintf(" AND actor_type = $%d", placeholderIndex)
		args = append(args, actorType)
		placeholderIndex++
	}
	if source != "" {
		baseQuery += fmt.Sprintf(" AND source = $%d", placeholderIndex)
		args = append(args, source)
		placeholderIndex++
	}

	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) ` + baseQuery
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count user action logs: %w", err)
	}

	// For select query, append limit/offset args
	selectQuery := `SELECT id, workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata, created_at ` + baseQuery + ` ORDER BY created_at DESC LIMIT $` + fmt.Sprintf("%d OFFSET $%d", placeholderIndex, placeholderIndex+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, selectQuery, args...)
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

// GetByID retrieves a single UserActionLog by its ID.
func (r *UserActionLogRepository) GetByID(ctx context.Context, id uuid.UUID) (*UserActionLog, error) {
	var log UserActionLog
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata, created_at
		FROM user_action_logs
		WHERE id = $1
	`, id).Scan(
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
		return nil, fmt.Errorf("get user action log by id: %w", err)
	}
	return &log, nil
}
