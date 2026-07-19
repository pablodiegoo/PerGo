package typebot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

type Forwarder struct {
	sessionRepo *repository.TypebotSessionRepository
	integRepo   *repository.IntegrationRepository
	publisher   inbound.Publisher
	client      *Client
}

func NewForwarder(
	sessionRepo *repository.TypebotSessionRepository,
	integRepo *repository.IntegrationRepository,
	publisher inbound.Publisher,
) *Forwarder {
	return &Forwarder{
		sessionRepo: sessionRepo,
		integRepo:   integRepo,
		publisher:   publisher,
		client:      NewClient(),
	}
}

func (f *Forwarder) SyncInboundMessage(ctx context.Context, contact *domain.Contact, event *inbound.InboundEvent) error {
	if !contact.BotActive {
		slog.Debug("Skipping Typebot forward: bot is inactive for contact", "contact_id", contact.ID)
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
			messages, contErr = f.client.ContinueChat(ctx, cfg.APIURL, session.TypebotSessionID, activeBot.PublicToken, event.Body)
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

		prefilled := map[string]string{
			"Name":  contact.Name,
			"Phone": event.From, // Or derived from contact
		}
		
		var startErr error
		typebotSessionID, messages, startErr = f.client.StartChat(ctx, cfg.APIURL, matchingBot.BotID, matchingBot.PublicToken, prefilled)
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

	// 5. Publish returned messages from Typebot to messages.outbound
	for _, m := range messages {
		traceID := uuid.New().String()
		
		// Create an outbound QueueMessage
		outMsg := domain.QueueMessage{
			WorkspaceID:    event.WorkspaceID,
			Channel:        event.Channel,
			To:             event.From,
			Body:           extractText(m),
			ConnectionID:   event.ConnectionID,
			SenderIdentity: event.To,
			TraceID:        traceID,
		}

		// Ensure body is not empty before sending
		if outMsg.Body != "" {
			b, err := json.Marshal(outMsg)
			if err != nil {
				slog.Error("failed to marshal outbound message from typebot", "error", err)
				continue
			}

			err = f.publisher.Publish(ctx, "messages.outbound", b, traceID)
			if err != nil {
				slog.Error("failed to publish outbound message from typebot", "error", err)
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
