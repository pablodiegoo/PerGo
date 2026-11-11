package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/domain"
)

// SummarizerFunc generates a structured ChatSynopsis for a chat thread using an LLM or custom backend.
type SummarizerFunc func(ctx context.Context, messages []domain.ChatMessage, chat *domain.Chat) (*domain.ChatSynopsis, error)

// SummarizeMessages delegates to domain.SummarizeMessages to produce a structured
// deterministic conversation synopsis without an external LLM round-trip.
func SummarizeMessages(messages []domain.ChatMessage, chat *domain.Chat) domain.ChatSynopsis {
	return domain.SummarizeMessages(enrichMessagesWithTranscripts(messages), chat)
}

func enrichMessagesWithTranscripts(messages []domain.ChatMessage) []domain.ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	enriched := make([]domain.ChatMessage, len(messages))
	copy(enriched, messages)
	for i := range enriched {
		if enriched[i].Metadata == nil {
			continue
		}
		raw, ok := enriched[i].Metadata["transcript"]
		if !ok || raw == nil {
			continue
		}
		transcript, ok := raw.(string)
		if !ok {
			continue
		}
		transcript = strings.TrimSpace(transcript)
		if transcript == "" {
			continue
		}
		body := strings.TrimSpace(enriched[i].Body)
		if body == "" {
			enriched[i].Body = "[Audio transcript]: " + transcript
		} else if !strings.Contains(body, transcript) {
			enriched[i].Body = body + " [Audio transcript: " + transcript + "]"
		}
	}
	return enriched
}

// Summarize resolves a chat in workspace scope, loads up to maxMessages (default 50),
// and produces a ChatSynopsis via the configured LLM summarizer, falling back to
// deterministic heuristic summarization on error or when no LLM summarizer is set.
func (e *Engine) Summarize(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, maxMessages int) (domain.ChatSynopsis, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && ref.ContactID != uuid.Nil {
		c, err = e.EnsureChatForContact(ctx, workspaceID, ref.ContactID, false)
	}
	if err != nil {
		return domain.ChatSynopsis{}, err
	}

	if maxMessages <= 0 || maxMessages > 500 {
		maxMessages = 50
	}

	msgs, err := e.chatRepo.ListChatMessages(ctx, c.WorkspaceID, c.ID, "", "", maxMessages)
	if err != nil {
		return domain.ChatSynopsis{}, err
	}

	enriched := enrichMessagesWithTranscripts(msgs)

	if e.summarizer != nil && len(enriched) > 0 {
		syn, sErr := e.summarizer(ctx, enriched, c)
		if sErr != nil {
			slog.Warn("chat engine: LLM summarizer failed, falling back to deterministic synopsis", "error", sErr, "chat_id", c.ID)
		} else if syn != nil && (strings.TrimSpace(syn.CustomerIssues) != "" || strings.TrimSpace(syn.PromisesMade) != "" || strings.TrimSpace(syn.CurrentStatus) != "") {
			return *syn, nil
		}
	}

	return domain.SummarizeMessages(enriched, c), nil
}
