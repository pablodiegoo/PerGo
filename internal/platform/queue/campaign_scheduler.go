package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
)

// Publisher defines the contract for publishing events to JetStream.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ScheduledCampaignClaimer defines the contract for querying and claiming due scheduled campaigns.
type ScheduledCampaignClaimer interface {
	ClaimDueScheduledCampaigns(ctx context.Context, now time.Time, limit int) ([]domain.Campaign, error)
	RollbackClaim(ctx context.Context, id uuid.UUID) error
}

// CampaignScheduler is a background ticker daemon that monitors and triggers scheduled campaigns
// when their scheduled_at timestamp arrives.
type CampaignScheduler struct {
	claimer     ScheduledCampaignClaimer
	publisher   Publisher
	auditWriter audit.Writer
	interval    time.Duration
	batchLimit  int
}

// NewCampaignScheduler creates a new CampaignScheduler instance.
func NewCampaignScheduler(
	claimer ScheduledCampaignClaimer,
	publisher Publisher,
	auditWriter audit.Writer,
) *CampaignScheduler {
	return &CampaignScheduler{
		claimer:     claimer,
		publisher:   publisher,
		auditWriter: auditWriter,
		interval:    5 * time.Second,
		batchLimit:  50,
	}
}

// SetInterval sets the polling interval for testing or custom configuration.
func (s *CampaignScheduler) SetInterval(interval time.Duration) {
	if interval > 0 {
		s.interval = interval
	}
}

// SetBatchLimit sets the max number of campaigns claimed per tick.
func (s *CampaignScheduler) SetBatchLimit(limit int) {
	if limit > 0 {
		s.batchLimit = limit
	}
}

// Run starts the background scheduler loop and blocks until ctx is cancelled.
func (s *CampaignScheduler) Run(ctx context.Context) {
	slog.Info("campaign scheduler started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial check on startup
	if _, err := s.CheckDueCampaigns(ctx); err != nil {
		slog.Error("campaign scheduler: error on initial check", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("campaign scheduler stopped")
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if _, err := s.CheckDueCampaigns(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("campaign scheduler: error during check", "error", err)
			}
		}
	}
}

// CheckDueCampaigns queries the database for due scheduled campaigns, atomically claims them,
// transitions them to 'sending', publishes their CampaignStartTask to NATS JetStream (campaigns.start),
// and records an audit log event.
func (s *CampaignScheduler) CheckDueCampaigns(ctx context.Context) (int, error) {
	if s.claimer == nil || s.publisher == nil {
		return 0, nil
	}

	now := time.Now().UTC()
	claimed, err := s.claimer.ClaimDueScheduledCampaigns(ctx, now, s.batchLimit)
	if err != nil {
		return 0, fmt.Errorf("claim due campaigns: %w", err)
	}

	if len(claimed) == 0 {
		return 0, nil
	}

	slog.Info("campaign scheduler: claimed due campaigns", "count", len(claimed))

	triggeredCount := 0
	for _, camp := range claimed {
		startTask := domain.CampaignStartTask{
			CampaignID:  camp.ID,
			WorkspaceID: camp.WorkspaceID,
		}
		payload, err := json.Marshal(startTask)
		if err != nil {
			slog.Error("campaign scheduler: failed to marshal CampaignStartTask", "campaign_id", camp.ID, "error", err)
			continue
		}

		traceID := fmt.Sprintf("campaign_%s_start", camp.ID.String())
		if err := s.publisher.Publish(ctx, "campaigns.start", payload, traceID); err != nil {
			slog.Error("campaign scheduler: failed to publish start task to JetStream, rolling back claim to scheduled", "campaign_id", camp.ID, "error", err)
			if rollbackErr := s.claimer.RollbackClaim(ctx, camp.ID); rollbackErr != nil {
				slog.Error("campaign scheduler: failed to rollback campaign claim", "campaign_id", camp.ID, "error", rollbackErr)
			}
			continue
		}

		// Emit campaign.dispatch.scheduled_triggered audit event
		if s.auditWriter != nil {
			channel := "whatsapp"
			if camp.Channel != nil {
				channel = *camp.Channel
			}

			auditPayload := map[string]any{
				"workspace_id": camp.WorkspaceID,
				"trace_id":     traceID,
				"campaign_id":  camp.ID,
				"channel":      channel,
				"status":       "sending",
				"scheduled_at": camp.ScheduledAt,
				"triggered_at": time.Now().UTC(),
			}
			payloadBytes, err := json.Marshal(auditPayload)
			if err == nil {
				if auditErr := s.auditWriter.Write(audit.NewEvent(
					camp.WorkspaceID,
					traceID,
					"campaign.dispatch.scheduled_triggered",
					payloadBytes,
				)); auditErr != nil {
					slog.Error("campaign scheduler: failed to write scheduled_triggered audit event", "campaign_id", camp.ID, "trace_id", traceID, "error", auditErr)
				}
			} else {
				slog.Error("campaign scheduler: failed to marshal audit payload", "campaign_id", camp.ID, "error", err)
			}
		}

		slog.Info("campaign scheduler: triggered scheduled campaign",
			"campaign_id", camp.ID,
			"workspace_id", camp.WorkspaceID,
			"scheduled_at", camp.ScheduledAt,
		)
		triggeredCount++
	}

	return triggeredCount, nil
}
