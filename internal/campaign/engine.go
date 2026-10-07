package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// Publisher defines the port for publishing messages to the message broker.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// CreateCampaignParams holds all input parameters for creating a campaign.
type CreateCampaignParams struct {
	Name             string                     `json:"name"`
	ConnectionSlug   string                     `json:"connection_slug"`
	ConnectionID     *uuid.UUID                 `json:"connection_id,omitempty"`
	BatchSize        int                        `json:"batch_size"`
	DelaySeconds     int                        `json:"delay_seconds"`
	RateLimitPerMin  *int                       `json:"rate_limit_per_min,omitempty"`
	ScheduledAt      *time.Time                 `json:"scheduled_at,omitempty"`
	TemplateName     *string                    `json:"template_name,omitempty"`
	MessageBody      *string                    `json:"message_body,omitempty"`
	Channel          *string                    `json:"channel,omitempty"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
	Interactive      *domain.Interactive        `json:"interactive,omitempty"`
	FallbackBehavior *string                    `json:"fallback_behavior,omitempty"`
	TagID            *uuid.UUID                 `json:"tag_id,omitempty"`
	TagIDs           []uuid.UUID                `json:"tag_ids,omitempty"`
	Recipients       []domain.CampaignRecipient `json:"recipients,omitempty"`
	SkippedRows      []domain.SkippedRow        `json:"skipped_rows,omitempty"`
}

// BroadcasterEngine owns the campaign lifecycle end-to-end.
type BroadcasterEngine interface {
	Create(ctx context.Context, scope domain.WorkspaceScope, params CreateCampaignParams) (*domain.Campaign, error)
	Start(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
	Pause(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
	Resume(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
	Cancel(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
	Delete(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) error
	TriggerDue(ctx context.Context) ([]uuid.UUID, error)
	ProcessStartTask(ctx context.Context, task domain.CampaignStartTask) error
	ProcessBatchTask(ctx context.Context, task domain.CampaignBatchTask) error
}

type defaultEngine struct {
	campaignRepo   *repository.CampaignRepository
	connectionRepo *repository.ConnectionRepository
	dispatchRepo   *repository.MessageDispatchRepository
	publisher      Publisher
	auditWriter    audit.Writer
	tagLister      domain.TagContactLister
}

// NewBroadcasterEngine constructs a new BroadcasterEngine.
func NewBroadcasterEngine(
	campaignRepo *repository.CampaignRepository,
	connectionRepo *repository.ConnectionRepository,
	dispatchRepo *repository.MessageDispatchRepository,
	publisher Publisher,
	auditWriter audit.Writer,
	tagLister domain.TagContactLister,
) BroadcasterEngine {
	return &defaultEngine{
		campaignRepo:   campaignRepo,
		connectionRepo: connectionRepo,
		dispatchRepo:   dispatchRepo,
		publisher:      publisher,
		auditWriter:    auditWriter,
		tagLister:      tagLister,
	}
}

// Create validates scope, connection, parameters, defaults, and creates a campaign in draft (or scheduled).
func (e *defaultEngine) Create(ctx context.Context, scope domain.WorkspaceScope, params CreateCampaignParams) (*domain.Campaign, error) {
	workspaceID := scope.WorkspaceID()
	if workspaceID == uuid.Nil {
		return nil, domain.ErrMissingScope
	}
	if !scope.Matches(workspaceID) {
		return nil, repository.ErrCampaignNotFound
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("campaign name is required")
	}

	if params.RateLimitPerMin != nil && *params.RateLimitPerMin <= 0 {
		return nil, errors.New("rate_limit_per_min must be greater than 0")
	}

	if params.FallbackBehavior != nil && *params.FallbackBehavior != "" {
		fb := *params.FallbackBehavior
		if fb != string(domain.FallbackBehaviorDegrade) && fb != string(domain.FallbackBehaviorFail) {
			return nil, errors.New(`fallback_behavior must be either "degrade" or "fail"`)
		}
	}

	if params.Interactive != nil {
		if params.Interactive.Type == "" {
			return nil, errors.New("interactive.type is required")
		}
		if params.Interactive.Body.Text == "" {
			return nil, errors.New("interactive.body.text is required")
		}
		if params.Interactive.Type == "button" && len(params.Interactive.Action.Buttons) == 0 {
			return nil, errors.New("interactive.action.buttons is required when type is button")
		}
		if params.Interactive.Type == "list" && len(params.Interactive.Action.Sections) == 0 {
			return nil, errors.New("interactive.action.sections is required when type is list")
		}
	}

	// Collect and deduplicate target tag IDs
	var targetTagIDs []uuid.UUID
	if params.TagID != nil && *params.TagID != uuid.Nil {
		targetTagIDs = append(targetTagIDs, *params.TagID)
	}
	for _, tid := range params.TagIDs {
		if tid != uuid.Nil {
			targetTagIDs = append(targetTagIDs, tid)
		}
	}
	targetTagIDs = domain.DeduplicateUUIDs(targetTagIDs)

	if len(targetTagIDs) == 0 && len(params.Recipients) == 0 {
		return nil, errors.New("campaign requires at least one recipient or a valid tag")
	}

	// Connection resolution & validation
	var connID uuid.UUID
	var connSlug string
	var channel string

	if e.connectionRepo != nil {
		var conn *repository.Connection
		var err error

		if params.ConnectionID != nil && *params.ConnectionID != uuid.Nil {
			conn, err = e.connectionRepo.GetByID(ctx, *params.ConnectionID)
		} else if params.ConnectionSlug != "" {
			conn, err = e.connectionRepo.GetBySlug(ctx, workspaceID, params.ConnectionSlug)
		} else if params.Channel != nil && *params.Channel != "" {
			conn, err = e.connectionRepo.GetDefaultChannelConnection(ctx, workspaceID, *params.Channel)
		}

		if err != nil || conn == nil {
			return nil, fmt.Errorf("connection not found: %w", err)
		}
		if !scope.Matches(conn.WorkspaceID) {
			return nil, repository.ErrConnectionNotFound
		}
		if conn.Status != "active" {
			return nil, fmt.Errorf("connection %q is currently %s", conn.Slug, conn.Status)
		}

		connID = conn.ID
		connSlug = conn.Slug
		channel = conn.Channel
	} else {
		if params.ConnectionID != nil {
			connID = *params.ConnectionID
		}
		connSlug = params.ConnectionSlug
		if params.Channel != nil {
			channel = *params.Channel
		} else {
			channel = "whatsapp"
		}
	}

	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	delaySeconds := params.DelaySeconds
	if delaySeconds <= 0 {
		delaySeconds = 5
	}

	var primaryTagID *uuid.UUID
	if params.TagID != nil && *params.TagID != uuid.Nil {
		primaryTagID = params.TagID
	} else if len(targetTagIDs) > 0 {
		primaryTagID = &targetTagIDs[0]
	}

	status := domain.CampaignStatusDraft
	if params.ScheduledAt != nil && !params.ScheduledAt.IsZero() {
		status = domain.CampaignStatusScheduled
	}

	camp := &domain.Campaign{
		WorkspaceID:      workspaceID,
		ConnectionID:     &connID,
		ConnectionSlug:   &connSlug,
		Name:             name,
		Status:           status,
		BatchSize:        batchSize,
		DelaySeconds:     delaySeconds,
		RateLimitPerMin:  params.RateLimitPerMin,
		ScheduledAt:      params.ScheduledAt,
		TemplateName:     params.TemplateName,
		MessageBody:      params.MessageBody,
		Channel:          &channel,
		FallbackChannels: params.FallbackChannels,
		Interactive:      params.Interactive,
		FallbackBehavior: params.FallbackBehavior,
		TagID:            primaryTagID,
		TagIDs:           targetTagIDs,
		TotalRecipients:  len(params.Recipients),
		Recipients:       params.Recipients,
		SkippedRows:      params.SkippedRows,
	}

	return e.campaignRepo.Create(ctx, camp)
}

// Start transitions a draft campaign to sending and publishes a start task.
func (e *defaultEngine) Start(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error) {
	camp, err := e.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if !scope.Matches(camp.WorkspaceID) {
		return nil, repository.ErrCampaignNotFound
	}

	if camp.Status != domain.CampaignStatusDraft && camp.Status != domain.CampaignStatusScheduled {
		return nil, domain.ErrInvalidCampaignTransition{
			From: camp.Status,
			To:   domain.CampaignStatusSending,
		}
	}

	if err := e.campaignRepo.UpdateStatus(ctx, campaignID, domain.CampaignStatusSending); err != nil {
		return nil, fmt.Errorf("update status to sending: %w", err)
	}
	camp.Status = domain.CampaignStatusSending

	startTask := domain.CampaignStartTask{
		CampaignID:  campaignID,
		WorkspaceID: camp.WorkspaceID,
	}
	payload, err := json.Marshal(startTask)
	if err != nil {
		return nil, fmt.Errorf("marshal start task: %w", err)
	}

	traceID := fmt.Sprintf("campaign_%s_start", campaignID.String())
	if err := e.publisher.Publish(ctx, "campaigns.start", payload, traceID); err != nil {
		return nil, fmt.Errorf("publish start task: %w", err)
	}

	return camp, nil
}

// Pause transitions a sending campaign to paused.
func (e *defaultEngine) Pause(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error) {
	camp, err := e.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if !scope.Matches(camp.WorkspaceID) {
		return nil, repository.ErrCampaignNotFound
	}

	if camp.Status != domain.CampaignStatusSending {
		return nil, domain.ErrInvalidCampaignTransition{
			From: camp.Status,
			To:   domain.CampaignStatusPaused,
		}
	}

	if err := e.campaignRepo.UpdateStatus(ctx, campaignID, domain.CampaignStatusPaused); err != nil {
		return nil, fmt.Errorf("update status to paused: %w", err)
	}
	camp.Status = domain.CampaignStatusPaused

	return camp, nil
}

// Resume transitions a paused campaign to sending and publishes remaining pending recipients.
// If zero pending recipients remain, it completes the campaign immediately.
func (e *defaultEngine) Resume(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error) {
	camp, err := e.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if !scope.Matches(camp.WorkspaceID) {
		return nil, repository.ErrCampaignNotFound
	}

	if camp.Status != domain.CampaignStatusPaused {
		return nil, domain.ErrInvalidCampaignTransition{
			From: camp.Status,
			To:   domain.CampaignStatusSending,
		}
	}

	if err := e.campaignRepo.UpdateStatus(ctx, campaignID, domain.CampaignStatusSending); err != nil {
		return nil, fmt.Errorf("update status to sending: %w", err)
	}
	camp.Status = domain.CampaignStatusSending

	pending, err := e.campaignRepo.ListPendingRecipients(ctx, campaignID)
	if err != nil {
		return nil, fmt.Errorf("list pending recipients: %w", err)
	}

	if len(pending) == 0 && len(camp.Recipients) > 0 && camp.SentRecipients < len(camp.Recipients) {
		pending = camp.Recipients[camp.SentRecipients:]
	}

	if len(pending) == 0 {
		if err := e.campaignRepo.UpdateStatus(ctx, campaignID, domain.CampaignStatusCompleted); err != nil {
			return nil, fmt.Errorf("update status to completed: %w", err)
		}
		camp.Status = domain.CampaignStatusCompleted
		return camp, nil
	}

	batchSize := camp.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	var batches [][]domain.CampaignRecipient
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batches = append(batches, pending[i:end])
	}

	totalBatches := len(batches)
	for idx, batch := range batches {
		batchTask := domain.CampaignBatchTask{
			CampaignID:       campaignID,
			WorkspaceID:      camp.WorkspaceID,
			BatchIndex:       idx + 1,
			TotalBatches:     totalBatches,
			Recipients:       batch,
			DelaySeconds:     camp.DelaySeconds,
			RateLimitPerMin:  camp.RateLimitPerMin,
			FallbackChannels: camp.FallbackChannels,
		}
		payload, err := json.Marshal(batchTask)
		if err != nil {
			return nil, fmt.Errorf("marshal batch task: %w", err)
		}
		traceID := fmt.Sprintf("campaign_%s_batch_%d", campaignID.String(), idx+1)
		if err := e.publisher.Publish(ctx, "campaigns.batches", payload, traceID); err != nil {
			return nil, fmt.Errorf("publish batch task: %w", err)
		}
	}

	return camp, nil
}

// Cancel transitions a scheduled, sending, or paused campaign to cancelled.
func (e *defaultEngine) Cancel(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error) {
	camp, err := e.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if !scope.Matches(camp.WorkspaceID) {
		return nil, repository.ErrCampaignNotFound
	}

	if camp.Status != domain.CampaignStatusScheduled &&
		camp.Status != domain.CampaignStatusSending &&
		camp.Status != domain.CampaignStatusPaused {
		return nil, domain.ErrInvalidCampaignTransition{
			From: camp.Status,
			To:   domain.CampaignStatusCancelled,
		}
	}

	if err := e.campaignRepo.UpdateStatus(ctx, campaignID, domain.CampaignStatusCancelled); err != nil {
		return nil, fmt.Errorf("update status to cancelled: %w", err)
	}
	camp.Status = domain.CampaignStatusCancelled

	return camp, nil
}

// Delete removes a draft campaign.
func (e *defaultEngine) Delete(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) error {
	camp, err := e.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return err
	}
	if !scope.Matches(camp.WorkspaceID) {
		return repository.ErrCampaignNotFound
	}

	if camp.Status != domain.CampaignStatusDraft {
		return fmt.Errorf("cannot delete campaign in %s status", camp.Status)
	}

	return e.campaignRepo.Delete(ctx, campaignID)
}

// TriggerDue claims scheduled campaigns whose scheduled_at has arrived, publishes start tasks,
// and rolls back on publish failure.
func (e *defaultEngine) TriggerDue(ctx context.Context) ([]uuid.UUID, error) {
	claimed, err := e.campaignRepo.ClaimDueScheduledCampaigns(ctx, time.Now().UTC(), 50)
	if err != nil {
		return nil, fmt.Errorf("claim due scheduled campaigns: %w", err)
	}

	var triggered []uuid.UUID
	for _, camp := range claimed {
		startTask := domain.CampaignStartTask{
			CampaignID:  camp.ID,
			WorkspaceID: camp.WorkspaceID,
		}
		payload, err := json.Marshal(startTask)
		if err != nil {
			slog.Error("failed to marshal start task for scheduled campaign", "campaign_id", camp.ID, "error", err)
			continue
		}

		traceID := fmt.Sprintf("campaign_%s_start", camp.ID.String())
		if err := e.publisher.Publish(ctx, "campaigns.start", payload, traceID); err != nil {
			slog.Error("failed to publish start task, rolling back claim to scheduled", "campaign_id", camp.ID, "error", err)
			if rollbackErr := e.campaignRepo.RollbackClaim(ctx, camp.ID); rollbackErr != nil {
				slog.Error("failed to rollback claim", "campaign_id", camp.ID, "error", rollbackErr)
			}
			continue
		}

		channel := "whatsapp"
		if camp.Channel != nil && *camp.Channel != "" {
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
		if err == nil && e.auditWriter != nil {
			_ = e.auditWriter.Write(audit.NewEvent(
				camp.WorkspaceID,
				traceID,
				"campaign.dispatch.scheduled_triggered",
				payloadBytes,
			))
		}

		triggered = append(triggered, camp.ID)
	}

	return triggered, nil
}

// ProcessStartTask dynamically resolves tag recipients, merges with CSV, persists recipients,
// emits audit events, and publishes batches.
func (e *defaultEngine) ProcessStartTask(ctx context.Context, task domain.CampaignStartTask) error {
	camp, err := e.campaignRepo.GetByID(ctx, task.CampaignID)
	if err != nil {
		return fmt.Errorf("get campaign: %w", err)
	}

	if camp.Status == domain.CampaignStatusCancelled || camp.Status == domain.CampaignStatusFailed {
		slog.Info("campaign is inactive, skipping start task", "campaign_id", task.CampaignID, "status", camp.Status)
		return nil
	}

	channel := "whatsapp"
	if camp.Channel != nil && *camp.Channel != "" {
		channel = *camp.Channel
	}

	// 1. Resolve tag recipients with channel filter
	tagRes, err := domain.ResolveTagRecipients(ctx, e.tagLister, camp.WorkspaceID, camp.TagIDs, channel)
	if err != nil {
		_ = e.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusFailed)
		return fmt.Errorf("resolve tag recipients: %w", err)
	}

	// 2. Merge with static CSV recipients
	allRecords, mergedRecipients := domain.MergeTagAndCSVRecipients(tagRes, camp.Recipients)

	// 3. Handle empty audience
	if len(allRecords) == 0 {
		_ = e.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		traceID := fmt.Sprintf("campaign_%s_start", task.CampaignID.String())
		_ = e.emitAuditLog(auditDispatchEvent{
			WorkspaceID: camp.WorkspaceID,
			TraceID:     traceID,
			EventType:   "campaign.dispatch.completed_empty",
			Status:      "completed_empty",
			Recipient:   "system",
			CampaignID:  task.CampaignID,
			Channel:     channel,
		})
		return nil
	}

	// 4. Persist recipients
	if err := e.campaignRepo.AddRecipients(ctx, task.CampaignID, allRecords); err != nil {
		_ = e.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusFailed)
		return fmt.Errorf("persist campaign recipients: %w", err)
	}

	// 5. Emit audit logs for skipped contacts
	for _, rec := range allRecords {
		if rec.Status == domain.RecipientStatusSkipped {
			recipTraceID := fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), rec.Phone)
			_ = e.emitAuditLog(auditDispatchEvent{
				WorkspaceID: camp.WorkspaceID,
				TraceID:     recipTraceID,
				EventType:   "campaign.dispatch.skipped",
				Status:      "skipped",
				Recipient:   rec.Phone,
				CampaignID:  task.CampaignID,
				Channel:     channel,
			})
		}
	}

	// 6. Handle case where all contacts were skipped
	if len(mergedRecipients) == 0 {
		traceID := fmt.Sprintf("campaign_%s_start", task.CampaignID.String())
		_ = e.emitAuditLog(auditDispatchEvent{
			WorkspaceID: camp.WorkspaceID,
			TraceID:     traceID,
			EventType:   "campaign.dispatch.completed_empty",
			Status:      "completed_empty",
			Recipient:   "system",
			CampaignID:  task.CampaignID,
			Channel:     channel,
		})
		_ = e.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		return nil
	}

	// 7. Update status to sending
	if err := e.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusSending); err != nil {
		return fmt.Errorf("update campaign status to sending: %w", err)
	}

	// 8. Slice into batches and publish CampaignBatchTask
	batchSize := camp.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	var batches [][]domain.CampaignRecipient
	for i := 0; i < len(mergedRecipients); i += batchSize {
		end := i + batchSize
		if end > len(mergedRecipients) {
			end = len(mergedRecipients)
		}
		batches = append(batches, mergedRecipients[i:end])
	}

	totalBatches := len(batches)
	for idx, batch := range batches {
		batchTask := domain.CampaignBatchTask{
			CampaignID:       task.CampaignID,
			WorkspaceID:      camp.WorkspaceID,
			BatchIndex:       idx + 1,
			TotalBatches:     totalBatches,
			Recipients:       batch,
			DelaySeconds:     camp.DelaySeconds,
			RateLimitPerMin:  camp.RateLimitPerMin,
			FallbackChannels: camp.FallbackChannels,
		}
		payload, err := json.Marshal(batchTask)
		if err != nil {
			return fmt.Errorf("marshal batch task: %w", err)
		}
		traceID := fmt.Sprintf("campaign_%s_batch_%d", task.CampaignID.String(), idx+1)
		if err := e.publisher.Publish(ctx, "campaigns.batches", payload, traceID); err != nil {
			return fmt.Errorf("publish batch task: %w", err)
		}
	}

	return nil
}

// ProcessBatchTask processes a single batch: rate limits, interpolates message/interactive content,
// publishes outbound queue messages, updates recipient status and counters, sleeps jittered delay,
// and atomically detects completion.
func (e *defaultEngine) ProcessBatchTask(ctx context.Context, task domain.CampaignBatchTask) error {
	camp, err := e.campaignRepo.GetByID(ctx, task.CampaignID)
	if err != nil {
		return fmt.Errorf("get campaign: %w", err)
	}

	if camp.Status == domain.CampaignStatusCancelled || camp.Status == domain.CampaignStatusFailed {
		slog.Info("campaign is inactive, skipping batch", "campaign_id", task.CampaignID, "status", camp.Status)
		return nil
	}

	if camp.Status == domain.CampaignStatusPaused {
		slog.Info("campaign is paused, exiting batch cleanly", "campaign_id", task.CampaignID)
		return nil
	}

	channel := "whatsapp"
	if camp.Channel != nil && *camp.Channel != "" {
		channel = *camp.Channel
	}

	var connID uuid.UUID
	var senderIdentity string
	if camp.ConnectionID != nil && e.connectionRepo != nil {
		connID = *camp.ConnectionID
		if conn, err := e.connectionRepo.GetByID(ctx, connID); err == nil && conn != nil {
			senderIdentity = conn.SenderIdentity
		}
	}

	rateLimit := task.RateLimitPerMin
	if camp.RateLimitPerMin != nil && *camp.RateLimitPerMin > 0 {
		rateLimit = camp.RateLimitPerMin
	}
	limiter := createRateLimiter(rateLimit, task.DelaySeconds)

	for _, recipient := range task.Recipients {
		if err := limiter.Wait(ctx); err != nil {
			return nil
		}

		traceID := fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), recipient.To)

		var templateName *string
		variablesJSON := recipient.Variables

		fallbackChannels := task.FallbackChannels
		if len(fallbackChannels) == 0 && camp != nil {
			fallbackChannels = camp.FallbackChannels
		}

		fallbackBehavior := string(domain.FallbackBehaviorDegrade)
		if camp.FallbackBehavior != nil && *camp.FallbackBehavior != "" {
			fallbackBehavior = *camp.FallbackBehavior
		}

		qMsg := domain.QueueMessage{
			WorkspaceID:      task.WorkspaceID,
			ConnectionID:     connID,
			SenderIdentity:   senderIdentity,
			TraceID:          traceID,
			To:               recipient.To,
			Channel:          channel,
			QueuedAt:         time.Now(),
			FallbackChannels: fallbackChannels,
			CampaignID:       &task.CampaignID,
			VariablesJSON:    variablesJSON,
			FallbackBehavior: fallbackBehavior,
		}

		if camp.Interactive != nil {
			interpolated := domain.InterpolateInteractive(camp.Interactive, recipient.Variables)
			if limitErr := domain.ValidateInteractiveLimits(interpolated); limitErr != nil {
				if fallbackBehavior == string(domain.FallbackBehaviorFail) {
					qMsg.Interactive = interpolated
					qMsg.Body = interpolated.DegradeToText()
				} else {
					qMsg.Body = interpolated.DegradeToText()
					qMsg.Interactive = nil
				}
			} else {
				qMsg.Interactive = interpolated
				if qMsg.Body == "" {
					qMsg.Body = interpolated.Body.Text
				}
			}
		} else {
			if channel == "whatsapp_cloud" && camp.TemplateName != nil {
				templateName = camp.TemplateName
				qMsg.TemplateName = *camp.TemplateName
				qMsg.Language = "pt_BR"

				var params []domain.TemplateParameter
				for i := 1; ; i++ {
					val, ok := recipient.Variables[fmt.Sprintf("%d", i)]
					if !ok {
						break
					}
					params = append(params, domain.TemplateParameter{
						Type: "text",
						Text: val,
					})
				}
				if len(params) > 0 {
					qMsg.Components = []domain.TemplateComponent{
						{
							Type:       "body",
							Parameters: params,
						},
					}
				}
			}

			if camp.MessageBody != nil {
				qMsg.Body = domain.ResolveVariables(*camp.MessageBody, recipient.Variables)
			} else if camp.TemplateName != nil {
				qMsg.Body = domain.ResolveVariables(*camp.TemplateName, recipient.Variables)
			}
		}

		if e.dispatchRepo != nil {
			dispatch, err := e.dispatchRepo.GetOrCreateDispatch(
				ctx,
				task.WorkspaceID,
				traceID,
				channel,
				&task.CampaignID,
				templateName,
				variablesJSON,
			)
			if err != nil {
				e.recordRecipientFailure(ctx, task.WorkspaceID, task.CampaignID, recipient.To, channel, traceID, err)
				continue
			}

			if dispatch != nil && (dispatch.Status == "delivered" || dispatch.Status == "sent") {
				_ = e.campaignRepo.UpdateRecipientStatusByPhone(ctx, task.CampaignID, recipient.To, domain.RecipientStatusSent, nil)
				_ = e.emitAuditLog(auditDispatchEvent{
					WorkspaceID: task.WorkspaceID,
					TraceID:     traceID,
					EventType:   "campaign.dispatch." + dispatch.Status,
					Status:      dispatch.Status,
					Recipient:   recipient.To,
					CampaignID:  task.CampaignID,
					Channel:     channel,
				})
				continue
			}
		}

		payload, err := json.Marshal(qMsg)
		if err != nil {
			e.recordRecipientFailure(ctx, task.WorkspaceID, task.CampaignID, recipient.To, channel, traceID, err)
			continue
		}

		if err := e.publisher.Publish(ctx, "messages.outbound", payload, traceID); err != nil {
			e.recordRecipientFailure(ctx, task.WorkspaceID, task.CampaignID, recipient.To, channel, traceID, err)
			continue
		}

		// Recipient sent successfully
		_ = e.campaignRepo.UpdateRecipientStatusByPhone(ctx, task.CampaignID, recipient.To, domain.RecipientStatusSent, nil)
		_ = e.campaignRepo.UpdateCounters(ctx, task.CampaignID, 1, 0)
		_ = e.emitAuditLog(auditDispatchEvent{
			WorkspaceID: task.WorkspaceID,
			TraceID:     traceID,
			EventType:   "campaign.dispatch.sent",
			Status:      "sent",
			Recipient:   recipient.To,
			CampaignID:  task.CampaignID,
			Channel:     channel,
		})
	}

	// Dynamic sleep with jitter
	if task.DelaySeconds > 0 {
		sleepDur := CalculateJitteredDelay(task.DelaySeconds, nil)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}
	}

	// Atomic completion detection
	_, _ = e.campaignRepo.CompleteIfDone(ctx, task.CampaignID)

	return nil
}

// CalculateJitteredDelay computes delaySeconds + uniform random jitter in [-0.5s, +0.5s].
// If delaySeconds <= 0, returns 0.
func CalculateJitteredDelay(delaySeconds int, rng *rand.Rand) time.Duration {
	if delaySeconds <= 0 {
		return 0
	}
	var r float64
	if rng != nil {
		r = rng.Float64()
	} else {
		r = rand.Float64()
	}
	jitter := (r - 0.5) * 1.0 // uniform in [-0.5, +0.5]
	sleepDur := time.Duration((float64(delaySeconds) + jitter) * float64(time.Second))
	if sleepDur < 0 {
		return 0
	}
	return sleepDur
}

func createRateLimiter(rateLimitPerMin *int, delaySeconds int) *rate.Limiter {
	if rateLimitPerMin != nil && *rateLimitPerMin > 0 {
		interval := time.Minute / time.Duration(*rateLimitPerMin)
		return rate.NewLimiter(rate.Every(interval), 1)
	}
	if delaySeconds <= 0 {
		return rate.NewLimiter(rate.Inf, 1)
	}
	return rate.NewLimiter(rate.Every(time.Duration(delaySeconds)*time.Second), 1)
}

type auditDispatchEvent struct {
	WorkspaceID uuid.UUID
	TraceID     string
	EventType   string
	Status      string
	Recipient   string
	CampaignID  uuid.UUID
	Channel     string
	ErrStr      string
}

func (e *defaultEngine) recordRecipientFailure(
	ctx context.Context,
	workspaceID, campaignID uuid.UUID,
	recipientTo, channel, traceID string,
	failureErr error,
) {
	errStr := failureErr.Error()
	_ = e.campaignRepo.UpdateCounters(ctx, campaignID, 0, 1)
	_ = e.campaignRepo.UpdateRecipientStatusByPhone(ctx, campaignID, recipientTo, domain.RecipientStatusFailed, &errStr)
	_ = e.emitAuditLog(auditDispatchEvent{
		WorkspaceID: workspaceID,
		TraceID:     traceID,
		EventType:   "campaign.dispatch.failed",
		Status:      "failed",
		Recipient:   recipientTo,
		CampaignID:  campaignID,
		Channel:     channel,
		ErrStr:      errStr,
	})
}

func (e *defaultEngine) emitAuditLog(event auditDispatchEvent) error {
	if e.auditWriter == nil {
		return nil
	}
	payload := map[string]any{
		"workspace_id": event.WorkspaceID,
		"trace_id":     event.TraceID,
		"campaign_id":  event.CampaignID,
		"recipient_id": event.Recipient,
		"recipient":    event.Recipient,
		"status":       event.Status,
		"channel":      event.Channel,
	}
	if event.ErrStr != "" {
		payload["error"] = event.ErrStr
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return e.auditWriter.Write(audit.NewEvent(event.WorkspaceID, event.TraceID, event.EventType, payloadBytes))
}
