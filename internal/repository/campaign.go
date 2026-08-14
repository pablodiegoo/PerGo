package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

	if c.Status == "" {
		c.Status = domain.CampaignStatusDraft
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.DelaySeconds <= 0 {
		c.DelaySeconds = 5
	}

	var dbCampaign domain.Campaign
	err = r.pool.QueryRow(ctx,
		`INSERT INTO campaigns (
			workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
			template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
			recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		 RETURNING id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		           template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		           recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, created_at, updated_at`,
		c.WorkspaceID, c.ConnectionID, c.ConnectionSlug, c.Name, c.Status, c.BatchSize, c.DelaySeconds,
		c.TemplateName, c.MessageBody, c.Channel, c.TagID, c.TotalRecipients, c.SentRecipients, c.FailedRecipients,
		recipientsJSON, skippedJSON, c.ScheduledAt, c.TagIDs, c.RateLimitPerMin,
	).Scan(
		&dbCampaign.ID, &dbCampaign.WorkspaceID, &dbCampaign.ConnectionID, &dbCampaign.ConnectionSlug, &dbCampaign.Name, &dbCampaign.Status,
		&dbCampaign.BatchSize, &dbCampaign.DelaySeconds, &dbCampaign.TemplateName, &dbCampaign.MessageBody, &dbCampaign.Channel, &dbCampaign.TagID,
		&dbCampaign.TotalRecipients, &dbCampaign.SentRecipients, &dbCampaign.FailedRecipients,
		&recipientsJSON, &skippedJSON, &dbCampaign.ScheduledAt, &dbCampaign.TagIDs, &dbCampaign.RateLimitPerMin, &dbCampaign.CreatedAt, &dbCampaign.UpdatedAt,
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
		`SELECT id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		        template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		        recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, created_at, updated_at
		 FROM campaigns WHERE id = $1`,
		id,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ConnectionSlug, &c.Name, &c.Status,
		&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.MessageBody, &c.Channel, &c.TagID,
		&c.TotalRecipients, &c.SentRecipients, &c.FailedRecipients,
		&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.TagIDs, &c.RateLimitPerMin, &c.CreatedAt, &c.UpdatedAt,
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

func (r *CampaignRepository) UpdateCounters(ctx context.Context, campaignID uuid.UUID, sentInc, failedInc int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE campaigns 
		 SET sent_recipients = sent_recipients + $1, 
		     failed_recipients = failed_recipients + $2, 
		     updated_at = now() 
		 WHERE id = $3`,
		sentInc, failedInc, campaignID,
	)
	return err
}

func (r *CampaignRepository) AddRecipients(ctx context.Context, campaignID uuid.UUID, recipients []domain.CampaignRecipientRecord) error {
	if len(recipients) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, rec := range recipients {
		varsJSON, err := json.Marshal(rec.Variables)
		if err != nil {
			return fmt.Errorf("marshal recipient variables: %w", err)
		}
		status := rec.Status
		if status == "" {
			status = domain.RecipientStatusPending
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO campaign_recipients (campaign_id, contact_id, phone, variables, status, error_message, sent_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (campaign_id, phone) DO NOTHING`,
			campaignID, rec.ContactID, rec.Phone, varsJSON, status, rec.ErrorMessage, rec.SentAt,
		)
		if err != nil {
			return fmt.Errorf("insert campaign recipient: %w", err)
		}
	}

	// Update campaign total_recipients count
	_, err = tx.Exec(ctx,
		`UPDATE campaigns SET total_recipients = (SELECT COUNT(*) FROM campaign_recipients WHERE campaign_id = $1), updated_at = now() WHERE id = $1`,
		campaignID,
	)
	if err != nil {
		return fmt.Errorf("update campaign total_recipients: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (r *CampaignRepository) ListRecipients(ctx context.Context, campaignID uuid.UUID, status *domain.RecipientStatus, limit int) ([]domain.CampaignRecipientRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, campaign_id, contact_id, phone, variables, status, error_message, sent_at, created_at 
	          FROM campaign_recipients WHERE campaign_id = $1`
	args := []any{campaignID}

	if status != nil && *status != "" {
		query += ` AND status = $2 ORDER BY created_at ASC LIMIT $3`
		args = append(args, *status, limit)
	} else {
		query += ` ORDER BY created_at ASC LIMIT $2`
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []domain.CampaignRecipientRecord
	for rows.Next() {
		var rec domain.CampaignRecipientRecord
		var varsJSON []byte
		err := rows.Scan(
			&rec.ID, &rec.CampaignID, &rec.ContactID, &rec.Phone, &varsJSON,
			&rec.Status, &rec.ErrorMessage, &rec.SentAt, &rec.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(varsJSON, &rec.Variables); err != nil {
			rec.Variables = make(map[string]string)
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}

func (r *CampaignRepository) UpdateRecipientStatus(ctx context.Context, id uuid.UUID, status domain.RecipientStatus, errorMsg *string) error {
	var sentAt *time.Time
	if status == domain.RecipientStatusSent {
		now := time.Now()
		sentAt = &now
	}

	_, err := r.pool.Exec(ctx,
		`UPDATE campaign_recipients SET status = $1, error_message = $2, sent_at = $3 WHERE id = $4`,
		status, errorMsg, sentAt, id,
	)
	return err
}

func (r *CampaignRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Campaign, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		        template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		        recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, created_at, updated_at
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
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ConnectionSlug, &c.Name, &c.Status,
			&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.MessageBody, &c.Channel, &c.TagID,
			&c.TotalRecipients, &c.SentRecipients, &c.FailedRecipients,
			&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.TagIDs, &c.RateLimitPerMin, &c.CreatedAt, &c.UpdatedAt,
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
