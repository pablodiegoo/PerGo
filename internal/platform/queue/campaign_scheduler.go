package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// DueTriggerer defines the contract for triggering due scheduled campaigns.
type DueTriggerer interface {
	TriggerDue(ctx context.Context) ([]uuid.UUID, error)
}

// CampaignScheduler is a background ticker daemon that monitors and triggers scheduled campaigns
// by delegating to a DueTriggerer.
type CampaignScheduler struct {
	triggerer DueTriggerer
	interval  time.Duration
}

// NewCampaignScheduler creates a new CampaignScheduler instance.
func NewCampaignScheduler(triggerer DueTriggerer) *CampaignScheduler {
	return &CampaignScheduler{
		triggerer: triggerer,
		interval:  5 * time.Second,
	}
}

// SetInterval sets the polling interval for testing or custom configuration.
func (s *CampaignScheduler) SetInterval(interval time.Duration) {
	if interval > 0 {
		s.interval = interval
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

// CheckDueCampaigns delegates directly to s.triggerer.TriggerDue(ctx)
// and returns the number of triggered campaigns.
func (s *CampaignScheduler) CheckDueCampaigns(ctx context.Context) (int, error) {
	if s.triggerer == nil {
		return 0, nil
	}

	ids, err := s.triggerer.TriggerDue(ctx)
	if err != nil {
		return 0, err
	}

	return len(ids), nil
}

