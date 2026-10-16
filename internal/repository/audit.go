package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEntry represents a single audit log entry for review.
type AuditEntry struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	TraceID     string
	EventType   string
	Payload     []byte
	CreatedAt   time.Time
}

// AuditFilters holds filter parameters for audit log queries.
type AuditFilters struct {
	WorkspaceID *uuid.UUID
	TraceID     string
	EventType   string
	Channel     string
	Start       *time.Time
	End         *time.Time
	Page        int
	PageSize    int
}

// AuditRepository provides read operations for audit logs.
type AuditRepository struct {
	pool *pgxpool.Pool
}

// NewAuditRepository creates a new AuditRepository.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// buildWhereClause constructs the WHERE clause and args from the given filters.
// All filter values are parameterized to prevent SQL injection (threat T-02-09).
func buildWhereClause(filters AuditFilters) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1

	if filters.WorkspaceID != nil {
		conditions = append(conditions, fmt.Sprintf("workspace_id = $%d", argIdx))
		args = append(args, *filters.WorkspaceID)
		argIdx++
	}
	if filters.TraceID != "" {
		conditions = append(conditions, fmt.Sprintf("trace_id = $%d", argIdx))
		args = append(args, filters.TraceID)
		argIdx++
	}
	if filters.EventType != "" {
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", argIdx))
		args = append(args, filters.EventType)
		argIdx++
	}
	if filters.Channel != "" {
		conditions = append(conditions, fmt.Sprintf("payload->>'channel' = $%d", argIdx))
		args = append(args, filters.Channel)
		argIdx++
	}
	if filters.Start != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filters.Start)
		argIdx++
	}
	if filters.End != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filters.End)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// ListFiltered returns audit entries matching the given filters, with pagination.
// Returns entries, total count (before pagination), and error.
func (r *AuditRepository) ListFiltered(ctx context.Context, filters AuditFilters) ([]AuditEntry, int, error) {
	if filters.WorkspaceID != nil && *filters.WorkspaceID == uuid.Nil {
		return nil, 0, ErrInvalidWorkspaceID
	}
	whereClause, args := buildWhereClause(filters)

	// Count total matching rows (before pagination)
	countQuery := "SELECT COUNT(*) FROM audit_logs" + whereClause
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	// Default page size: 50 per CONTEXT.md
	pageSize := filters.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	page := filters.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// Query page of entries
	dataQuery := "SELECT id, workspace_id, trace_id, event_type, payload, created_at FROM audit_logs" + whereClause + " ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1) + " OFFSET $" + fmt.Sprintf("%d", len(args)+2)
	dataArgs := append(args, pageSize, offset)

	rows, err := r.pool.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.TraceID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return entries, total, nil
}

// ListAll returns all audit entries matching filters without pagination (for CSV export).
// Uses the same filter logic as ListFiltered but returns all matching rows.
func (r *AuditRepository) ListAll(ctx context.Context, filters AuditFilters) ([]AuditEntry, error) {
	if filters.WorkspaceID != nil && *filters.WorkspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	whereClause, args := buildWhereClause(filters)

	query := "SELECT id, workspace_id, trace_id, event_type, payload, created_at FROM audit_logs" + whereClause + " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit logs for export: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.TraceID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit logs: %w", err)
	}

	return entries, nil
}

// GetExpiredMessages finds unpurged inbound and outbound message audit entries older than cutoff.
func (r *AuditRepository) GetExpiredMessages(ctx context.Context, workspaceID uuid.UUID, cutoff time.Time, limit int) ([]AuditEntry, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, workspace_id, trace_id, event_type, payload, created_at
		FROM audit_logs
		WHERE workspace_id = $1
		  AND created_at < $2
		  AND event_type IN ('inbound_message', 'outbound_message')
		  AND COALESCE(payload->>'purged', 'false') != 'true'
		ORDER BY created_at ASC
		LIMIT $3`

	rows, err := r.pool.Query(ctx, query, workspaceID, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("query expired messages: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.TraceID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan expired message: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired messages: %w", err)
	}
	return entries, nil
}

// AnonymizeAuditLog updates an audit log entry's payload in place, targeted by primary key (id, created_at).
func (r *AuditRepository) AnonymizeAuditLog(ctx context.Context, id uuid.UUID, createdAt time.Time, sanitizedPayload []byte) error {
	query := `UPDATE audit_logs SET payload = $3 WHERE id = $1 AND created_at = $2`
	tag, err := r.pool.Exec(ctx, query, id, createdAt, sanitizedPayload)
	if err != nil {
		return fmt.Errorf("anonymize audit log: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("audit log not found for update: id=%s, created_at=%s", id, createdAt)
	}
	return nil
}

// InsertAuditLog inserts a new immutable audit log entry into PostgreSQL.
func (r *AuditRepository) InsertAuditLog(ctx context.Context, entry *AuditEntry) error {
	if entry == nil {
		return errors.New("audit entry is nil")
	}
	if entry.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.TraceID == "" {
		entry.TraceID = fmt.Sprintf("trace-%s", entry.ID.String())
	}
	if entry.EventType == "" {
		entry.EventType = "audit.event"
	}
	if entry.Payload == nil {
		entry.Payload = []byte("{}")
	}

	query := `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.pool.Exec(ctx, query, entry.ID, entry.WorkspaceID, entry.TraceID, entry.EventType, entry.Payload, entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// Insert is an alias for InsertAuditLog.
func (r *AuditRepository) Insert(ctx context.Context, entry *AuditEntry) error {
	return r.InsertAuditLog(ctx, entry)
}

