package chat

import (
	"context"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/domain"
)

// SummarizeMessages delegates to domain.SummarizeMessages to produce a structured
// deterministic conversation synopsis without an external LLM round-trip.
func SummarizeMessages(messages []domain.ChatMessage, chat *domain.Chat) domain.ChatSynopsis {
	return domain.SummarizeMessages(messages, chat)
}

// Summarize resolves a chat in workspace scope, loads up to maxMessages (default 50),
// and produces a deterministic ChatSynopsis.
func (e *Engine) Summarize(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, maxMessages int) (domain.ChatSynopsis, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
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

	return SummarizeMessages(msgs, c), nil
}
