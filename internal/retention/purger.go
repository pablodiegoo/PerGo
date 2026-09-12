package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

// ExpungedMarker is the canonical anonymization replacement text for LGPD/GDPR compliance.
const ExpungedMarker = "[EXPURGADO]"

// MediaDeleter defines the seam for deleting media files from storage.
type MediaDeleter interface {
	DeleteMedia(ctx context.Context, mediaURL string) error
}

// WorkspaceLister defines the seam for discovering workspaces with active retention policies.
type WorkspaceLister interface {
	ListActiveRetentionPolicies(ctx context.Context) ([]repository.Workspace, error)
}

// AuditPurgerRepo defines the seam for querying and updating audit logs during data retention purging.
type AuditPurgerRepo interface {
	GetExpiredMessages(ctx context.Context, workspaceID uuid.UUID, cutoff time.Time, limit int) ([]repository.AuditEntry, error)
	AnonymizeAuditLog(ctx context.Context, id uuid.UUID, createdAt time.Time, sanitizedPayload []byte) error
}

// PurgeStats records the count of messages anonymized and media files purged.
type PurgeStats struct {
	MessagesAnonymized int `json:"messages_anonymized"`
	MediaFilesDeleted  int `json:"media_files_deleted"`
	Errors             int `json:"errors"`
}

// Purger executes scheduled data retention and media expunging per workspace policy.
type Purger struct {
	wsRepo    WorkspaceLister
	auditRepo AuditPurgerRepo
	media     MediaDeleter
	batchSize int
}

// NewPurger creates a new Purger instance.
func NewPurger(wsRepo WorkspaceLister, auditRepo AuditPurgerRepo, media MediaDeleter) *Purger {
	return &Purger{
		wsRepo:    wsRepo,
		auditRepo: auditRepo,
		media:     media,
		batchSize: 100,
	}
}

// SetBatchSize overrides the batch size for message queries.
func (p *Purger) SetBatchSize(size int) {
	if size > 0 {
		p.batchSize = size
	}
}

// PurgeAll scans all workspaces with active retention policies and executes purge routines.
func (p *Purger) PurgeAll(ctx context.Context) (map[uuid.UUID]PurgeStats, error) {
	if p.wsRepo == nil {
		return nil, nil
	}

	workspaces, err := p.wsRepo.ListActiveRetentionPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active retention policies: %w", err)
	}

	results := make(map[uuid.UUID]PurgeStats, len(workspaces))
	for _, ws := range workspaces {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		stats, err := p.PurgeWorkspace(ctx, ws)
		if err != nil {
			slog.Error("retention purge failed for workspace",
				"workspace_id", ws.ID,
				"workspace_name", ws.Name,
				"error", err,
			)
		} else if stats.MessagesAnonymized > 0 || stats.MediaFilesDeleted > 0 {
			slog.Info("retention purge completed for workspace",
				"workspace_id", ws.ID,
				"workspace_name", ws.Name,
				"messages_anonymized", stats.MessagesAnonymized,
				"media_files_deleted", stats.MediaFilesDeleted,
			)
		}
		results[ws.ID] = stats
	}

	return results, nil
}

// PurgeWorkspace purges expired messages and corresponding media files for a single workspace.
func (p *Purger) PurgeWorkspace(ctx context.Context, ws repository.Workspace) (PurgeStats, error) {
	var stats PurgeStats
	if ws.MediaRetentionDays <= 0 || p.auditRepo == nil {
		return stats, nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -ws.MediaRetentionDays)

	for {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		entries, err := p.auditRepo.GetExpiredMessages(ctx, ws.ID, cutoff, p.batchSize)
		if err != nil {
			return stats, fmt.Errorf("get expired messages: %w", err)
		}
		if len(entries) == 0 {
			break
		}

		for _, entry := range entries {
			var payload map[string]any
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				slog.Warn("retention purger: malformed audit payload",
					"entry_id", entry.ID,
					"workspace_id", ws.ID,
					"error", err,
				)
				stats.Errors++
				continue
			}

			// 1. Identify and delete corresponding media files from MinIO/S3
			mediaURLs := extractMediaURLs(payload)
			for _, mURL := range mediaURLs {
				if mURL != "" && p.media != nil {
					if err := p.media.DeleteMedia(ctx, mURL); err != nil {
						slog.Warn("retention purger: failed to delete media from storage",
							"workspace_id", ws.ID,
							"media_url", mURL,
							"error", err,
						)
					} else {
						stats.MediaFilesDeleted++
					}
				}
			}

			// 2. Anonymize message text and sensitive PII in the payload ([EXPURGADO])
			anonymizePayload(payload)

			sanitizedBytes, err := json.Marshal(payload)
			if err != nil {
				slog.Error("retention purger: failed to marshal sanitized payload",
					"entry_id", entry.ID,
					"error", err,
				)
				stats.Errors++
				continue
			}

			// 3. Update database record in place, preserving timestamps & event types for aggregate metrics
			if err := p.auditRepo.AnonymizeAuditLog(ctx, entry.ID, entry.CreatedAt, sanitizedBytes); err != nil {
				slog.Error("retention purger: failed to anonymize audit log",
					"entry_id", entry.ID,
					"error", err,
				)
				stats.Errors++
				continue
			}

			stats.MessagesAnonymized++
		}

		// If returned batch was smaller than requested limit, we've exhausted all current expired records
		if len(entries) < p.batchSize {
			break
		}
	}

	return stats, nil
}

// extractMediaURLs collects all media proxy URLs present in the event payload.
func extractMediaURLs(payload map[string]any) []string {
	var urls []string

	// Check payload.media.media_url
	if mediaRaw, ok := payload["media"].(map[string]any); ok {
		if u, ok := mediaRaw["media_url"].(string); ok && u != "" && u != ExpungedMarker {
			urls = append(urls, u)
		}
	}

	// Check payload.request.media_url or payload.request.media.media_url
	if reqRaw, ok := payload["request"].(map[string]any); ok {
		if u, ok := reqRaw["media_url"].(string); ok && u != "" && u != ExpungedMarker {
			urls = append(urls, u)
		}
		if reqMedia, ok := reqRaw["media"].(map[string]any); ok {
			if u, ok := reqMedia["media_url"].(string); ok && u != "" && u != ExpungedMarker {
				urls = append(urls, u)
			}
		}
	}

	// Check top-level media_url
	if u, ok := payload["media_url"].(string); ok && u != "" && u != ExpungedMarker {
		urls = append(urls, u)
	}

	return urls
}

// anonymizePayload replaces all sensitive message content and PII with [EXPURGADO].
func anonymizePayload(payload map[string]any) {
	// Top-level message body
	if _, ok := payload["body"]; ok {
		payload["body"] = ExpungedMarker
	}

	// Inbound media details
	if mediaRaw, ok := payload["media"].(map[string]any); ok {
		if _, ok := mediaRaw["caption"]; ok {
			mediaRaw["caption"] = ExpungedMarker
		}
		if _, ok := mediaRaw["media_url"]; ok {
			mediaRaw["media_url"] = ExpungedMarker
		}
	}

	// Outbound request message details
	if reqRaw, ok := payload["request"].(map[string]any); ok {
		if _, ok := reqRaw["body"]; ok {
			reqRaw["body"] = ExpungedMarker
		}
		if _, ok := reqRaw["media_url"]; ok {
			reqRaw["media_url"] = ExpungedMarker
		}
		if reqMedia, ok := reqRaw["media"].(map[string]any); ok {
			if _, ok := reqMedia["caption"]; ok {
				reqMedia["caption"] = ExpungedMarker
			}
			if _, ok := reqMedia["media_url"]; ok {
				reqMedia["media_url"] = ExpungedMarker
			}
		}
	}

	// Story event details
	if storyRaw, ok := payload["story_event"].(map[string]any); ok {
		if _, ok := storyRaw["media_url"]; ok {
			storyRaw["media_url"] = ExpungedMarker
		}
	}

	// Purge marker and audit timestamp
	payload["purged"] = "true"
	payload["purged_at"] = time.Now().UTC().Format(time.RFC3339)
}
