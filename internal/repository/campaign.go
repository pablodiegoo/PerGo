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
	if c == nil || c.WorkspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	recipientsJSON, err := json.Marshal(c.Recipients)
	if err != nil {
		return nil, fmt.Errorf("marshal recipients: %w", err)
	}
	skippedJSON, err := json.Marshal(c.SkippedRows)
	if err != nil {
		return nil, fmt.Errorf("marshal skipped rows: %w", err)
	}

	if c.Status == "" {
		if c.ScheduledAt != nil && !c.ScheduledAt.IsZero() {
			c.Status = domain.CampaignStatusScheduled
		} else {
			c.Status = domain.CampaignStatusDraft
		}
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.DelaySeconds <= 0 {
		c.DelaySeconds = 5
	}

	fallbackChannels := c.FallbackChannels
	if fallbackChannels == nil {
		fallbackChannels = []string{}
	}

	var interactiveJSON []byte
	if c.Interactive != nil {
		var err error
		interactiveJSON, err = json.Marshal(c.Interactive)
		if err != nil {
			return nil, fmt.Errorf("marshal interactive payload: %w", err)
		}
	}

	var dbCampaign domain.Campaign
	var returnedInteractiveJSON []byte
	err = r.pool.QueryRow(ctx,
		`INSERT INTO campaigns (
			workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
			template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
			recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, fallback_channels,
			interactive, fallback_behavior
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		 RETURNING id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		           template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		           recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, fallback_channels,
		           interactive, fallback_behavior, created_at, updated_at`,
		c.WorkspaceID, c.ConnectionID, c.ConnectionSlug, c.Name, c.Status, c.BatchSize, c.DelaySeconds,
		c.TemplateName, c.MessageBody, c.Channel, c.TagID, c.TotalRecipients, c.SentRecipients, c.FailedRecipients,
		recipientsJSON, skippedJSON, c.ScheduledAt, c.TagIDs, c.RateLimitPerMin, fallbackChannels,
		interactiveJSON, c.FallbackBehavior,
	).Scan(
		&dbCampaign.ID, &dbCampaign.WorkspaceID, &dbCampaign.ConnectionID, &dbCampaign.ConnectionSlug, &dbCampaign.Name, &dbCampaign.Status,
		&dbCampaign.BatchSize, &dbCampaign.DelaySeconds, &dbCampaign.TemplateName, &dbCampaign.MessageBody, &dbCampaign.Channel, &dbCampaign.TagID,
		&dbCampaign.TotalRecipients, &dbCampaign.SentRecipients, &dbCampaign.FailedRecipients,
		&recipientsJSON, &skippedJSON, &dbCampaign.ScheduledAt, &dbCampaign.TagIDs, &dbCampaign.RateLimitPerMin, &dbCampaign.FallbackChannels,
		&returnedInteractiveJSON, &dbCampaign.FallbackBehavior, &dbCampaign.CreatedAt, &dbCampaign.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert campaign: %w", err)
	}

	if err := json.Unmarshal(recipientsJSON, &dbCampaign.Recipients); err != nil {
		return nil, fmt.Errorf("unmarshal recipients: %w", err)
	}
	if err := json.Unmarshal(skippedJSON, &dbCampaign.SkippedRows); err != nil {
		return nil, fmt.Errorf("unmarshal skipped rows: %w", err)
	}
	inter, err := unmarshalInteractive(returnedInteractiveJSON)
	if err != nil {
		return nil, err
	}
	dbCampaign.Interactive = inter

	return &dbCampaign, nil
}

func (r *CampaignRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	var c domain.Campaign
	var recipientsJSON, skippedJSON, interactiveJSON []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		        template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		        recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, fallback_channels,
		        interactive, fallback_behavior, created_at, updated_at
		 FROM campaigns WHERE id = $1`,
		id,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ConnectionSlug, &c.Name, &c.Status,
		&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.MessageBody, &c.Channel, &c.TagID,
		&c.TotalRecipients, &c.SentRecipients, &c.FailedRecipients,
		&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.TagIDs, &c.RateLimitPerMin, &c.FallbackChannels,
		&interactiveJSON, &c.FallbackBehavior, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCampaignNotFound
		}
		return nil, fmt.Errorf("query campaign by id: %w", err)
	}

	if err := json.Unmarshal(recipientsJSON, &c.Recipients); err != nil {
		return nil, fmt.Errorf("unmarshal recipients: %w", err)
	}
	if err := json.Unmarshal(skippedJSON, &c.SkippedRows); err != nil {
		return nil, fmt.Errorf("unmarshal skipped rows: %w", err)
	}
	inter, err := unmarshalInteractive(interactiveJSON)
	if err != nil {
		return nil, err
	}
	c.Interactive = inter

	return &c, nil
}

func (r *CampaignRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.CampaignStatus) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE campaigns SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update campaign status: %w", err)
	}
	return nil
}

func (r *CampaignRepository) UpdateRecipients(ctx context.Context, id uuid.UUID, recipients []domain.CampaignRecipient, skipped []domain.SkippedRow) error {
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return fmt.Errorf("marshal recipients: %w", err)
	}
	skippedJSON, err := json.Marshal(skipped)
	if err != nil {
		return fmt.Errorf("marshal skipped rows: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`UPDATE campaigns SET recipients = $1, skipped_rows = $2, updated_at = now() WHERE id = $3`,
		recipientsJSON, skippedJSON, id,
	)
	if err != nil {
		return fmt.Errorf("update campaign recipients: %w", err)
	}
	return nil
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
	if err != nil {
		return fmt.Errorf("update campaign counters: %w", err)
	}
	return nil
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
		return nil, fmt.Errorf("query campaign recipients: %w", err)
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
			return nil, fmt.Errorf("scan campaign recipient: %w", err)
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
	if err != nil {
		return fmt.Errorf("update recipient status: %w", err)
	}
	return nil
}

func (r *CampaignRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Campaign, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, connection_slug, name, status, batch_size, delay_seconds, 
		        template_name, message_body, channel, tag_id, total_recipients, sent_recipients, failed_recipients, 
		        recipients, skipped_rows, scheduled_at, tag_ids, rate_limit_per_min, fallback_channels,
		        interactive, fallback_behavior, created_at, updated_at
		 FROM campaigns WHERE workspace_id = $1 ORDER BY created_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list campaigns by workspace: %w", err)
	}
	defer rows.Close()

	var campaigns []domain.Campaign
	for rows.Next() {
		var c domain.Campaign
		var recipientsJSON, skippedJSON, interactiveJSON []byte
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ConnectionSlug, &c.Name, &c.Status,
			&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.MessageBody, &c.Channel, &c.TagID,
			&c.TotalRecipients, &c.SentRecipients, &c.FailedRecipients,
			&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.TagIDs, &c.RateLimitPerMin, &c.FallbackChannels,
			&interactiveJSON, &c.FallbackBehavior, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan campaign: %w", err)
		}
		if err := json.Unmarshal(recipientsJSON, &c.Recipients); err != nil {
			return nil, fmt.Errorf("unmarshal recipients: %w", err)
		}
		if err := json.Unmarshal(skippedJSON, &c.SkippedRows); err != nil {
			return nil, fmt.Errorf("unmarshal skipped rows: %w", err)
		}
		inter, err := unmarshalInteractive(interactiveJSON)
		if err != nil {
			return nil, err
		}
		c.Interactive = inter
		campaigns = append(campaigns, c)
	}

	return campaigns, rows.Err()
}

func (r *CampaignRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM campaigns WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete campaign: %w", err)
	}
	return nil
}

// ClaimDueScheduledCampaigns atomically selects due scheduled campaigns (status = 'scheduled' AND scheduled_at <= now),
// transitions them to 'sending', and returns them. Uses FOR UPDATE SKIP LOCKED to prevent race conditions
// across multiple concurrent scheduler worker instances.
func (r *CampaignRepository) ClaimDueScheduledCampaigns(ctx context.Context, now time.Time, limit int) ([]domain.Campaign, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `WITH due_campaigns AS (
		SELECT id
		FROM campaigns
		WHERE status = 'scheduled' AND scheduled_at <= $1
		ORDER BY scheduled_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	)
	UPDATE campaigns c
	SET status = 'sending',
	    updated_at = now()
	FROM due_campaigns
	WHERE c.id = due_campaigns.id
	RETURNING c.id, c.workspace_id, c.connection_id, c.connection_slug, c.name, c.status, 
	          c.batch_size, c.delay_seconds, c.template_name, c.message_body, c.channel, c.tag_id, 
	          c.total_recipients, c.sent_recipients, c.failed_recipients, 
	          c.recipients, c.skipped_rows, c.scheduled_at, c.tag_ids, c.rate_limit_per_min, 
	          c.fallback_channels, c.interactive, c.fallback_behavior, c.created_at, c.updated_at`

	rows, err := r.pool.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due scheduled campaigns: %w", err)
	}
	defer rows.Close()

	var campaigns []domain.Campaign
	for rows.Next() {
		var c domain.Campaign
		var recipientsJSON, skippedJSON, interactiveJSON []byte
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ConnectionSlug, &c.Name, &c.Status,
			&c.BatchSize, &c.DelaySeconds, &c.TemplateName, &c.MessageBody, &c.Channel, &c.TagID,
			&c.TotalRecipients, &c.SentRecipients, &c.FailedRecipients,
			&recipientsJSON, &skippedJSON, &c.ScheduledAt, &c.TagIDs, &c.RateLimitPerMin,
			&c.FallbackChannels, &interactiveJSON, &c.FallbackBehavior, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan claimed campaign: %w", err)
		}
		if err := json.Unmarshal(recipientsJSON, &c.Recipients); err != nil {
			return nil, fmt.Errorf("unmarshal recipients: %w", err)
		}
		if err := json.Unmarshal(skippedJSON, &c.SkippedRows); err != nil {
			return nil, fmt.Errorf("unmarshal skipped rows: %w", err)
		}
		inter, err := unmarshalInteractive(interactiveJSON)
		if err != nil {
			return nil, err
		}
		c.Interactive = inter
		campaigns = append(campaigns, c)
	}

	return campaigns, rows.Err()
}

// RollbackClaim transitions a campaign back to 'scheduled' if publishing its start task failed.
func (r *CampaignRepository) RollbackClaim(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE campaigns SET status = $1, updated_at = now() WHERE id = $2 AND status = $3`,
		domain.CampaignStatusScheduled, id, domain.CampaignStatusSending,
	)
	if err != nil {
		return fmt.Errorf("rollback claimed campaign: %w", err)
	}
	return nil
}

func unmarshalInteractive(raw []byte) (*domain.Interactive, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var inter domain.Interactive
	if err := json.Unmarshal(raw, &inter); err != nil {
		return nil, fmt.Errorf("unmarshal interactive payload: %w", err)
	}
	return &inter, nil
}
