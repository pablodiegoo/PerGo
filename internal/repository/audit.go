package repository

import (
	"context"
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

// ListFiltered returns audit entries matching the given filters, with pagination.
// Returns entries, total count (before pagination), and error.
func (r *AuditRepository) ListFiltered(ctx context.Context, filters AuditFilters) ([]AuditEntry, int, error) {
	// Stub — to be implemented in GREEN phase
	return nil, 0, nil
}

// ListAll returns all audit entries matching filters without pagination (for CSV export).
func (r *AuditRepository) ListAll(ctx context.Context, filters AuditFilters) ([]AuditEntry, error) {
	// Stub — to be implemented in GREEN phase
	return nil, nil
}
