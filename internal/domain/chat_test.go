package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestChatDomain(t *testing.T) {
	chatID := uuid.New()
	wsID := uuid.New()
	contactID := uuid.New()

	chat := &domain.Chat{
		ID:            chatID,
		WorkspaceID:   wsID,
		ContactID:     contactID,
		Status:        string(domain.ChatStatusOpen),
		Tags:          []string{"vip", "onboarding"},
		UnreadCount:   2,
		LastMessageAt: time.Now().UTC(),
		Metadata:      map[string]interface{}{"source": "web"},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	data, err := json.Marshal(chat)
	if err != nil {
		t.Fatalf("failed to marshal Chat: %v", err)
	}

	var decoded domain.Chat
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Chat: %v", err)
	}

	if decoded.ID != chatID || decoded.Status != string(domain.ChatStatusOpen) {
		t.Errorf("mismatch decoded chat: %+v", decoded)
	}
}

func TestChatMessageDomain(t *testing.T) {
	msgID := uuid.New()
	chatID := uuid.New()
	wsID := uuid.New()

	msg := &domain.ChatMessage{
		ID:          msgID,
		ChatID:      chatID,
		WorkspaceID: wsID,
		UID:         "msg-12345",
		Direction:   string(domain.DirectionInbound),
		SenderType:  string(domain.SenderTypeContact),
		Body:        "Hello there!",
		Reactions: []domain.Reaction{
			{Emoji: "👍", Sender: "agent1", CreatedAt: time.Now().UTC()},
		},
		Metadata:  map[string]interface{}{"confidence": 0.98},
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal ChatMessage: %v", err)
	}

	var decoded domain.ChatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ChatMessage: %v", err)
	}

	if decoded.UID != "msg-12345" || len(decoded.Reactions) != 1 || decoded.Reactions[0].Emoji != "👍" {
		t.Errorf("mismatch decoded message: %+v", decoded)
	}
}
