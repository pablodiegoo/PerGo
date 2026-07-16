package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
)

var (
	ErrCampaignNotFound = errors.New("campaign not found")
)

type CampaignRepository struct {
	pool *pgxpool.Pool
}

func NewCampaignRepository(pool *pgxpool.Pool) *CampaignRepository {
	return &CampaignRepository{pool: pool}
}

func (r *CampaignRepository) Create(ctx context.Context, c *domain.Campaign) (*domain.Campaign, error) {
	recipientsJSON, err := json.Marshal(c.Recipients)
	if err != nil {
		return nil, err
	}
	skippedJSON, err := json.Marshal(c.SkippedRows)
	if err != nil {
		return nil, err
	}

	var dbCampaign domain.Campaign
	err = r.pool.QueryRow(ctx,
		`INSERT INTO campaigns (workspace_id, connection_id, name, status, batch_size, delay_seconds, template_name, channel, recipients, skipped_rows, scheduled_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, workspace_id, connection_id, name, status, batch_size, delay_seconds, template_name, channel, recipients, skipped_rows, scheduled_at, created_at, updated_at`,
		c.WorkspaceID, c.ConnectionID, c.Name, c.Status, c.BatchSize, c.DelaySeconds, c.TemplateName, c.Channel, recipientsJSON, skippedJSON, c.ScheduledAt,
	).Scan(
		&dbCampaign.ID, &dbCampaign.WorkspaceID, &dbCampaign.ConnectionID, &dbCampaign.Name, &dbCampaign.Status,
		&dbCampaign.BatchSize, &dbCampaign.DelaySeconds, &dbCampaign.TemplateName, &dbCampaign.Channel,
		&recipientsJSON, &skippedJSON, &dbCampaign.ScheduledAt, &dbCampaign.CreatedAt, &dbCampaign.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(recipientsJSON, &dbCampaign.Recipients); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(skippedJSON, &dbCampaign.SkippedRows); err != nil {
		return nil, err
	}

	return &dbCampaign, nil
}

func (r *CampaignRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	var c domain.Campaign
	var recipientsJSON, skippedJSON []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, name, status, batch_size, delay_seconds, template_name, channel, recipients, skipped_rows, scheduled_at, created_at, updated_at
		 FROM campaigns WHERE id = $1`,
		id,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.Name, &c.Status,
		&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.Channel,
		&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCampaignNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(recipientsJSON, &c.Recipients); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(skippedJSON, &c.SkippedRows); err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *CampaignRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.CampaignStatus) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE campaigns SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	return err
}

func (r *CampaignRepository) UpdateRecipients(ctx context.Context, id uuid.UUID, recipients []domain.CampaignRecipient, skipped []domain.SkippedRow) error {
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return err
	}
	skippedJSON, err := json.Marshal(skipped)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`UPDATE campaigns SET recipients = $1, skipped_rows = $2, updated_at = now() WHERE id = $3`,
		recipientsJSON, skippedJSON, id,
	)
	return err
}

func (r *CampaignRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Campaign, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, name, status, batch_size, delay_seconds, template_name, channel, recipients, skipped_rows, scheduled_at, created_at, updated_at
		 FROM campaigns WHERE workspace_id = $1 ORDER BY created_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []domain.Campaign
	for rows.Next() {
		var c domain.Campaign
		var recipientsJSON, skippedJSON []byte
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.Name, &c.Status,
			&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.Channel,
			&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(recipientsJSON, &c.Recipients); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(skippedJSON, &c.SkippedRows); err != nil {
			return nil, err
		}
		campaigns = append(campaigns, c)
	}

	return campaigns, rows.Err()
}

func (r *CampaignRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM campaigns WHERE id = $1`, id)
	return err
}
