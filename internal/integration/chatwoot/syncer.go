package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// ChatwootSyncer coordinates inbound message synchronization from PerGo to Chatwoot.
type ChatwootSyncer struct {
	integrationRepo *repository.IntegrationRepository
	mappingRepo     *repository.ChatwootMappingRepository
	httpClient      *http.Client
}

// NewChatwootSyncer creates a new ChatwootSyncer.
func NewChatwootSyncer(
	integrationRepo *repository.IntegrationRepository,
	mappingRepo *repository.ChatwootMappingRepository,
	httpClient *http.Client,
) *ChatwootSyncer {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &ChatwootSyncer{
		integrationRepo: integrationRepo,
		mappingRepo:     mappingRepo,
		httpClient:      httpClient,
	}
}

// SyncInboundMessage processes the incoming customer message and posts/syncs it to Chatwoot.
func (s *ChatwootSyncer) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *inbound.InboundEvent) error {
	integration, err := s.integrationRepo.GetByProvider(ctx, ev.WorkspaceID, "chatwoot")
	if err != nil {
		if errors.Is(err, repository.ErrIntegrationNotFound) {
			slog.Debug("Chatwoot integration not configured for workspace", "workspace_id", ev.WorkspaceID)
			return nil
		}
		return fmt.Errorf("failed to fetch chatwoot integration: %w", err)
	}

	if !integration.Active {
		slog.Debug("Chatwoot integration is inactive for workspace", "workspace_id", ev.WorkspaceID)
		return nil
	}

	var cfg struct {
		APIURL      string `json:"api_url"`
		AccessToken string `json:"access_token"`
		InboxID     int64  `json:"inbox_id"`
		AccountID   int64  `json:"account_id"`
	}
	if err := json.Unmarshal(integration.Config, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal chatwoot integration config: %w", err)
	}

	if cfg.APIURL == "" || cfg.AccessToken == "" || cfg.AccountID == 0 {
		return fmt.Errorf("invalid chatwoot configuration parameters")
	}

	client := NewChatwootClient(cfg.APIURL, cfg.AccessToken, cfg.AccountID, s.httpClient)

	content := ev.Body
	if ev.Media != nil && ev.Media.MediaURL != "" {
		mediaURLText := fmt.Sprintf("\nMedia: %s", ev.Media.MediaURL)
		if ev.Media.Caption != "" {
			mediaURLText = fmt.Sprintf("\nMedia: %s (Caption: %s)", ev.Media.MediaURL, ev.Media.Caption)
		}
		content = content + mediaURLText
	}
	content = strings.TrimSpace(content)

	mapping, err := s.mappingRepo.GetByContactAndConnection(ctx, ev.WorkspaceID, contact.ID, ev.ConnectionID)
	if err == nil {
		err = client.PostMessage(ctx, mapping.ChatwootConversationID, content, false)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				slog.Warn("Chatwoot conversation not found. Deleting mapping and recreating.", "conversation_id", mapping.ChatwootConversationID)
				_ = s.mappingRepo.Delete(ctx, ev.WorkspaceID, contact.ID, ev.ConnectionID)
			} else {
				return fmt.Errorf("failed to post message to mapped chatwoot conversation: %w", err)
			}
		} else {
			return nil
		}
	} else if !errors.Is(err, repository.ErrChatwootMappingNotFound) {
		return fmt.Errorf("failed to fetch chatwoot mapping: %w", err)
	}

	var chatwootContactID int64
	chatwootContactID, err = client.SearchContact(ctx, contact.ID.String())

	var phoneNumber string
	for _, ident := range contact.Identities {
		if ident.Channel == "whatsapp" || ident.Channel == "whatsapp_cloud" {
			phoneNumber = ident.SenderIdentity
			break
		}
	}
	if phoneNumber == "" {
		if ev.Channel == "whatsapp" || ev.Channel == "whatsapp_cloud" {
			phoneNumber = ev.From
		}
	}

	customAttributes := map[string]interface{}{
		"pergo_contact_id": contact.ID.String(),
	}

	if err != nil {
		if errors.Is(err, ErrNotFound) {
			chatwootContactID, err = client.CreateContact(ctx, contact.ID.String(), contact.Name, phoneNumber, customAttributes)
			if err != nil {
				return fmt.Errorf("failed to create chatwoot contact: %w", err)
			}
		} else {
			return fmt.Errorf("failed to search chatwoot contact: %w", err)
		}
	} else {
		_, updateErr := client.UpdateContact(ctx, chatwootContactID, contact.Name, phoneNumber, customAttributes)
		if updateErr != nil {
			slog.Warn("failed to update Chatwoot contact attributes", "error", updateErr, "chatwoot_contact_id", chatwootContactID)
		}
	}

	var chatwootConversationID int64
	chatwootConversationID, err = client.CreateConversation(ctx, chatwootContactID, cfg.InboxID)
	if err != nil {
		return fmt.Errorf("failed to create chatwoot conversation: %w", err)
	}

	newMapping := &repository.ChatwootMapping{
		WorkspaceID:            ev.WorkspaceID,
		ContactID:              contact.ID,
		ConnectionID:           ev.ConnectionID,
		ChatwootContactID:      chatwootContactID,
		ChatwootConversationID: chatwootConversationID,
		Channel:                ev.Channel,
		SenderIdentity:         ev.To,
	}
	err = s.mappingRepo.Upsert(ctx, newMapping)
	if err != nil {
		return fmt.Errorf("failed to save chatwoot mapping: %w", err)
	}

	err = client.PostMessage(ctx, chatwootConversationID, content, false)
	if err != nil {
		return fmt.Errorf("failed to post message to new chatwoot conversation: %w", err)
	}

	return nil
}
