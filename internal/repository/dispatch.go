package repository

import (
	"context"
	"encoding/json"
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
	ID                uuid.UUID
	WorkspaceID       uuid.UUID
	TraceID           string
	CurrentChannel    string
	Status            string
	FallbackIndex     int
	ErrorMessage      *string
	CampaignID        *uuid.UUID
	TemplateName      *string
	VariablesJSON     map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ProviderMessageID *string
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
func (r *MessageDispatchRepository) GetOrCreateDispatch(
	ctx context.Context,
	workspaceID uuid.UUID,
	traceID string,
	initialChannel string,
	campaignID *uuid.UUID,
	templateName *string,
	variablesJSON map[string]string,
) (*MessageDispatch, error) {
	var varsJSON []byte
	var err error
	if variablesJSON != nil {
		varsJSON, err = json.Marshal(variablesJSON)
		if err != nil {
			return nil, err
		}
	}

	var d MessageDispatch
	var varsRaw []byte
	err = r.pool.QueryRow(ctx,
		`INSERT INTO message_dispatches (workspace_id, trace_id, current_channel, status, fallback_index, campaign_id, template_name, variables_json)
		 VALUES ($1, $2, $3, 'queued', 0, $4, $5, $6)
		 ON CONFLICT (trace_id) DO UPDATE 
		 SET trace_id = EXCLUDED.trace_id -- dummy update to return existing row
		 RETURNING id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, campaign_id, template_name, variables_json, created_at, updated_at, provider_message_id`,
		workspaceID, traceID, initialChannel, campaignID, templateName, varsJSON,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CampaignID, &d.TemplateName, &varsRaw, &d.CreatedAt, &d.UpdatedAt, &d.ProviderMessageID)
	if err != nil {
		return nil, err
	}

	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &d.VariablesJSON); err != nil {
			return nil, err
		}
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
	var varsRaw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, campaign_id, template_name, variables_json, created_at, updated_at, provider_message_id
		 FROM message_dispatches WHERE trace_id = $1`,
		traceID,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CampaignID, &d.TemplateName, &varsRaw, &d.CreatedAt, &d.UpdatedAt, &d.ProviderMessageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispatchNotFound
		}
		return nil, err
	}

	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &d.VariablesJSON); err != nil {
			return nil, err
		}
	}

	return &d, nil
}

// UpdateProviderMessageID associates an external provider message ID (e.g. wamid) with a dispatch record.
func (r *MessageDispatchRepository) UpdateProviderMessageID(ctx context.Context, id uuid.UUID, providerMessageID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE message_dispatches 
		 SET provider_message_id = $1, updated_at = now()
		 WHERE id = $2`,
		providerMessageID, id,
	)
	return err
}

// GetByProviderMessageID retrieves a message dispatch by its external provider message ID.
func (r *MessageDispatchRepository) GetByProviderMessageID(ctx context.Context, providerMessageID string) (*MessageDispatch, error) {
	var d MessageDispatch
	var varsRaw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, campaign_id, template_name, variables_json, created_at, updated_at, provider_message_id
		 FROM message_dispatches 
		 WHERE provider_message_id = $1`,
		providerMessageID,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CampaignID, &d.TemplateName, &varsRaw, &d.CreatedAt, &d.UpdatedAt, &d.ProviderMessageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispatchNotFound
		}
		return nil, err
	}

	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &d.VariablesJSON); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// UpdateStatusByProviderMessageID updates status of a message dispatch matched by its provider_message_id or id.
func (r *MessageDispatchRepository) UpdateStatusByProviderMessageID(ctx context.Context, providerMsgID string, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE message_dispatches
		 SET status = $1, updated_at = now()
		 WHERE provider_message_id = $2 OR id::text = $2`,
		status, providerMsgID,
	)
	return err
}

