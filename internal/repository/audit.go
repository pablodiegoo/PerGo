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

// ConversationSummary represents a contact-level conversation card.
type ConversationSummary struct {
	ContactID         uuid.UUID `json:"contact_id"`
	ContactName       string    `json:"contact_name"`
	LastMessageBody   string    `json:"last_message_body"`
	LastMessageTime   time.Time `json:"last_message_time"`
	TotalMessageCount int64     `json:"total_message_count"`
	Channel           string    `json:"channel"`            // channel of last message
	RecipientIdentity string    `json:"recipient_identity"` // recipient identity of last message
}

// ThreadMessage represents a single message in a chronological conversation thread.
type ThreadMessage struct {
	ID        uuid.UUID `json:"id"`
	TraceID   string    `json:"trace_id"`
	Direction string    `json:"direction"` // "inbound" or "outbound"
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// ListConversations lists unified conversations grouped by contact_id.
func (r *AuditRepository) ListConversations(ctx context.Context, workspaceID uuid.UUID, channelFilter string) ([]ConversationSummary, error) {
	query := `
		WITH MsgWithContact AS (
			SELECT 
				al.id,
				al.created_at,
				al.payload->>'body' AS body,
				al.payload->>'channel' AS channel,
				al.payload->>'to' AS recipient_identity,
				ci.contact_id,
				c.name AS contact_name
			FROM audit_logs al
			LEFT JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
				AND ci.channel = al.payload->>'channel' 
				AND ci.sender_identity = al.payload->>'from'
			LEFT JOIN contacts c ON c.id = ci.contact_id
			WHERE al.workspace_id = $1 
			  AND al.event_type = 'inbound_message'
		),
		RankedConversations AS (
			SELECT 
				contact_id,
				contact_name,
				channel,
				recipient_identity,
				body,
				created_at,
				ROW_NUMBER() OVER(PARTITION BY contact_id ORDER BY created_at DESC) as rn,
				COUNT(*) OVER(PARTITION BY contact_id) as total_count
			FROM MsgWithContact
		)
		SELECT 
			COALESCE(contact_id, '00000000-0000-0000-0000-000000000000'::uuid), 
			COALESCE(contact_name, ''), 
			channel, 
			COALESCE(recipient_identity, ''), 
			COALESCE(body, ''), 
			created_at, 
			total_count
		FROM RankedConversations
		WHERE rn = 1
		  AND ($2 = '' OR recipient_identity = $2)
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, channelFilter)
	if err != nil {
		return nil, fmt.Errorf("query conversations list: %w", err)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(&s.ContactID, &s.ContactName, &s.Channel, &s.RecipientIdentity, &s.LastMessageBody, &s.LastMessageTime, &s.TotalMessageCount); err != nil {
			return nil, fmt.Errorf("scan conversation summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}

	return summaries, nil
}

// ListThreadByContact performs a UNION between inbound and outbound messages matching ANY identity owned by the Contact.
func (r *AuditRepository) ListThreadByContact(ctx context.Context, workspaceID uuid.UUID, contactID uuid.UUID, afterID *uuid.UUID) ([]ThreadMessage, error) {
	query := `
		SELECT al.id, al.trace_id, 'inbound' AS direction, COALESCE(al.payload->>'body', '') AS body, al.created_at
		FROM audit_logs al
		JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
			AND ci.channel = al.payload->>'channel' 
			AND ci.sender_identity = al.payload->>'from'
		WHERE al.workspace_id = $1
		  AND ci.contact_id = $2
		  AND al.event_type = 'inbound_message'
		  AND ($3::uuid IS NULL OR al.id > $3::uuid)

		UNION ALL

		SELECT al.id, al.trace_id, 'outbound' AS direction, COALESCE(al.payload->'request'->>'body', '') AS body, al.created_at
		FROM audit_logs al
		JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
			AND ci.channel = al.payload->'request'->>'channel' 
			AND ci.sender_identity = al.payload->'request'->>'to'
		WHERE al.workspace_id = $1
		  AND ci.contact_id = $2
		  AND al.event_type = 'outbound_message'
		  AND ($3::uuid IS NULL OR al.id > $3::uuid)

		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, contactID, afterID)
	if err != nil {
		return nil, fmt.Errorf("query thread messages: %w", err)
	}
	defer rows.Close()

	var messages []ThreadMessage
	for rows.Next() {
		var m ThreadMessage
		if err := rows.Scan(&m.ID, &m.TraceID, &m.Direction, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan thread message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread messages: %w", err)
	}

	return messages, nil
}
