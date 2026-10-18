package typebot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

type Forwarder struct {
	sessionRepo *repository.TypebotSessionRepository
	integRepo   *repository.IntegrationRepository
	ingestor    outbound.OutboundProcessor
	client      *Client
	activity    *activity.Recorder
}

func NewForwarder(
	sessionRepo *repository.TypebotSessionRepository,
	integRepo *repository.IntegrationRepository,
	publisher inbound.Publisher,
) *Forwarder {
	var rec *activity.Recorder
	var ingestor outbound.OutboundProcessor
	if sessionRepo != nil && sessionRepo.Pool() != nil {
		rec = activity.New(sessionRepo.Pool(), nil, nil, nil)
		if publisher != nil {
			connRepo := repository.NewConnectionRepository(sessionRepo.Pool(), nil)
			contactRepo := repository.NewContactRepository(sessionRepo.Pool())
			ingestor = outbound.NewProcessor(nil, nil, connRepo, publisher).
				SetContactRepository(contactRepo).
				SetActivity(rec)
		}
	}
	return &Forwarder{
		sessionRepo: sessionRepo,
		integRepo:   integRepo,
		ingestor:    ingestor,
		client:      NewClient(),
		activity:    rec,
	}
}

func (f *Forwarder) WithActivity(rec *activity.Recorder) *Forwarder {
	if f == nil {
		return nil
	}
	f.activity = rec
	if proc, ok := f.ingestor.(*outbound.Processor); ok && proc != nil {
		proc.SetActivity(rec)
	}
	return f
}

func (f *Forwarder) WithIngestor(ingestor outbound.OutboundProcessor) *Forwarder {
	if f == nil {
		return nil
	}
	f.ingestor = ingestor
	return f
}

func (f *Forwarder) SyncInboundMessage(ctx context.Context, contact *domain.Contact, event *inbound.InboundEvent) error {
	if event != nil && event.IsGroup() {
		slog.Debug("Skipping Typebot forward: group message ignored", "workspace_id", event.WorkspaceID, "from", event.From)
		return nil
	}

	if contact == nil || event == nil {
		return nil
	}

	if f.activity != nil {
		state, _, err := f.activity.Snapshot(ctx, event.WorkspaceID, activity.ChatOf(event.ConnectionID, contact.ID))
		if err != nil {
			return fmt.Errorf("failed to snapshot chat state for typebot: %w", err)
		}
		if !state.AI.Effective(time.Now().UTC()).IsEngaged() {
			slog.Debug("Skipping Typebot forward: AI is not engaged for chat", "contact_id", contact.ID, "connection_id", event.ConnectionID)
			return nil
		}
	}

	if f.integRepo == nil {
		return nil
	}

	// 1. Look up if workspace has active typebot integration
	integ, err := f.integRepo.GetByProvider(ctx, event.WorkspaceID, "typebot")
	if err != nil {
		if errors.Is(err, repository.ErrIntegrationNotFound) {
			return nil // No active integration
		}
		return fmt.Errorf("failed to get typebot integration: %w", err)
	}

	if !integ.Active || len(integ.Config) == 0 {
		return nil
	}

	var cfg Config
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal typebot config: %w", err)
	}

	// 2. Find matching BotConfig for this connection
	var matchingBot *BotConfig
	bodyLower := strings.ToLower(strings.TrimSpace(event.Body))

	// Find bot based on connection ID
	var defaultBot *BotConfig
	for _, b := range cfg.Bots {
		if b.ConnectionID == event.ConnectionID.String() {
			if b.IsDefault {
				defaultBot = &b
			}
			for _, tw := range b.TriggerWords {
				if strings.ToLower(tw) == bodyLower {
					bot := b
					matchingBot = &bot
					break
				}
			}
		}
		if matchingBot != nil {
			break
		}
	}

	var messageText string
	var pergoMetadata map[string]any

	if event.Media != nil {
		messageText = "[Media Attachment]"
		pergoMetadata = map[string]any{
			"media_url":  event.Media.MediaURL,
			"media_type": event.Media.MediaType,
		}
	} else if event.Location != nil {
		messageText = "[Unsupported message: location]"
	} else if len(event.Contacts) > 0 {
		messageText = "[Unsupported message: contact]"
	} else if event.Interactive != nil {
		switch event.Interactive.Type {
		case "button_reply":
			if event.Interactive.ButtonReply != nil {
				if event.Interactive.ButtonReply.Title != "" {
					messageText = event.Interactive.ButtonReply.Title
				} else {
					messageText = event.Interactive.ButtonReply.ID
				}
				pergoMetadata = map[string]any{
					"button_id":    event.Interactive.ButtonReply.ID,
					"button_title": event.Interactive.ButtonReply.Title,
				}
			}
		case "list_reply":
			if event.Interactive.ListReply != nil {
				if event.Interactive.ListReply.Title != "" {
					messageText = event.Interactive.ListReply.Title
				} else {
					messageText = event.Interactive.ListReply.ID
				}
				pergoMetadata = map[string]any{
					"list_id":          event.Interactive.ListReply.ID,
					"list_title":       event.Interactive.ListReply.Title,
					"list_description": event.Interactive.ListReply.Description,
				}
			}
		case "nfm_reply":
			if event.Interactive.NFMReply != nil {
				messageText = event.Body
				pergoMetadata = map[string]any{
					"flow_token": event.Interactive.NFMReply.FlowToken,
					"screen":     event.Interactive.NFMReply.Screen,
					"flow_data":  event.Interactive.NFMReply.Data,
				}
			}
		case "order":
			if event.Interactive.Order != nil {
				messageText = event.Body
				pergoMetadata = map[string]any{
					"catalog_id":    event.Interactive.Order.CatalogID,
					"product_items": event.Interactive.Order.ProductItems,
					"total_price":   event.Interactive.Order.TotalPrice,
					"currency":      event.Interactive.Order.Currency,
				}
			}
		}
		if messageText == "" {
			messageText = event.Body
		}
	} else {
		messageText = event.Body
	}

	sessionId := fmt.Sprintf("%s:%s", event.ConnectionID.String(), event.From)

	// 3. Check for active session
	session, err := f.sessionRepo.GetSession(ctx, event.WorkspaceID, contact.ID, event.ConnectionID)
	if err != nil && !errors.Is(err, repository.ErrTypebotSessionNotFound) {
		return fmt.Errorf("failed to get typebot session: %w", err)
	}

	var messages []Message
	var typebotSessionID string

	// If a customer is in an active session, D-04 dictates we ignore trigger keyword and route to active session.
	if session != nil {
		// Identify which bot config this session belongs to (if multiple bots per connection, we use defaultBot or the matching bot if we can.
		// Actually, we need to pass public_token, so we need the bot config. For now just grab the first matching connection bot.)
		// Wait, we don't store which BotID the session belongs to! We should, or we just rely on one bot per connection.
		// Since connectionID maps to a bot, let's use the first one for this connection to get the token.
		var activeBot *BotConfig
		for _, b := range cfg.Bots {
			if b.ConnectionID == event.ConnectionID.String() {
				activeBot = &b
				break
			}
		}

		if activeBot != nil {
			var contErr error
			messages, contErr = f.client.ContinueChat(ctx, cfg.APIURL, session.TypebotSessionID, activeBot.PublicToken, messageText)
			if contErr != nil {
				if strings.Contains(contErr.Error(), "404") {
					// Session expired, delete it and retry with StartChat
					_ = f.sessionRepo.DeleteSession(ctx, event.WorkspaceID, contact.ID, event.ConnectionID)
					session = nil // Fall through to start chat
				} else {
					return contErr
				}
			} else {
				// Update session updated_at
				_ = f.sessionRepo.UpsertSession(ctx, session)
			}
		}
	}

	// If no active session, try to start a new one if it matches trigger or default bot
	if session == nil {
		if matchingBot == nil {
			matchingBot = defaultBot
		}
		if matchingBot == nil {
			return nil // No matching bot and no default bot
		}

		prefilled := map[string]any{
			"Name":  contact.Name,
			"Phone": event.From, // Or derived from contact
		}
		if pergoMetadata != nil {
			prefilled["pergo_metadata"] = pergoMetadata
		}

		req := StartChatRequest{
			SessionID:          sessionId,
			Message:            messageText,
			PrefilledVariables: prefilled,
		}

		var startErr error
		typebotSessionID, messages, startErr = f.client.StartChat(ctx, cfg.APIURL, matchingBot.BotID, matchingBot.PublicToken, req)
		if startErr != nil {
			return fmt.Errorf("failed to start typebot chat: %w", startErr)
		}

		// Save new session
		err = f.sessionRepo.UpsertSession(ctx, &repository.TypebotSession{
			WorkspaceID:      event.WorkspaceID,
			ContactID:        contact.ID,
			ConnectionID:     event.ConnectionID,
			TypebotSessionID: typebotSessionID,
		})
		if err != nil {
			slog.Error("failed to upsert typebot session", "error", err)
		}
	}

	// 5. Route returned messages from Typebot through OutboundProcessor.Ingest
	for _, m := range messages {
		body := extractText(m)
		if body == "" {
			continue
		}
		traceID := uuid.New().String()
		connID := event.ConnectionID
		contactID := contact.ID
		req := &domain.CreateMessageRequest{
			To:      event.From,
			Channel: event.Channel,
			Body:    body,
			ConversationalIngestFields: domain.ConversationalIngestFields{
				SenderType:   string(domain.SenderTypeAIAgent),
				SenderName:   "Typebot",
				ConnectionID: &connID,
				ContactID:    &contactID,
			},
		}
		if f.ingestor != nil {
			if _, err := f.ingestor.Ingest(ctx, event.WorkspaceID, traceID, req); err != nil {
				slog.Error("failed to ingest outbound message from typebot", "error", err)
			}
		}
	}

	return nil
}

func extractText(m Message) string {
	if m.Type == "text" {
		if m.Content.Type == "richText" {
			// Naive extraction for now
			var out []string
			for _, rt := range m.Content.RichText {
				b, _ := json.Marshal(rt)
				var block struct {
					Children []struct {
						Text string `json:"text"`
					} `json:"children"`
				}
				if err := json.Unmarshal(b, &block); err == nil {
					for _, child := range block.Children {
						out = append(out, child.Text)
					}
				}
			}
			return strings.Join(out, "\n")
		}
	}
	return ""
}
