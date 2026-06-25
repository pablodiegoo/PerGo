package repository

import (
	"context"
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
	if filters.Start != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filters.Start)
		argIdx++
	}
	if filters.End != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filters.End)
		argIdx++
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// ListFiltered returns audit entries matching the given filters, with pagination.
// Returns entries, total count (before pagination), and error.
func (r *AuditRepository) ListFiltered(ctx context.Context, filters AuditFilters) ([]AuditEntry, int, error) {
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
