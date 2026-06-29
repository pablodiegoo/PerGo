package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Simulates the channel message payload with connection routing fields
type MockMessagePayload struct {
	ConnectionID   uuid.UUID
	SenderIdentity string
	To             string
	Body           string
}

// Simulates the whatsmeow client wrapper
type MockWhatsMeowClient struct {
	JID  string
	Sent []string
}

func (c *MockWhatsMeowClient) SendMessage(to, body string) error {
	c.Sent = append(c.Sent, to+":"+body)
	return nil
}

// In-memory active whatsmeow sessions
type MockActiveSessionRegistry struct {
	sessions map[string]*MockWhatsMeowClient
}

func (r *MockActiveSessionRegistry) Get(jid string) *MockWhatsMeowClient {
	return r.sessions[jid]
}

// Simulated WhatsApp Web Adapter (using the single global adapter pattern)
type MockWhatsAppWebAdapter struct {
	sessions *MockActiveSessionRegistry
}

func (a *MockWhatsAppWebAdapter) Dispatch(ctx context.Context, m *MockMessagePayload) error {
	// Dynamically look up the active session by JID (which matches the SenderIdentity)
	client := a.sessions.Get(m.SenderIdentity)
	if client == nil {
		return errors.New("device session not connected for JID: " + m.SenderIdentity)
	}
	return client.SendMessage(m.To, m.Body)
}

// Simulated Telegram Adapter (using single global adapter with DB credential lookup)
type MockTelegramAdapter struct {
	credentials map[uuid.UUID]string // Mock DB: connection_id -> decrypted token
}

func (a *MockTelegramAdapter) Dispatch(ctx context.Context, m *MockMessagePayload) (string, error) {
	// Dynamically load credentials by ConnectionID
	token, ok := a.credentials[m.ConnectionID]
	if !ok {
		return "", errors.New("credentials not found for connection ID")
	}
	// Simulate HTTP send using the token
	return "sent via Telegram token: " + token, nil
}

func TestRegistrySpike(t *testing.T) {
	ctx := context.Background()

	// --- Test WhatsApp Web (Stateful Session Routing) ---
	sessions := &MockActiveSessionRegistry{
		sessions: map[string]*MockWhatsMeowClient{
			"jid_device_1": {JID: "jid_device_1"},
			"jid_device_2": {JID: "jid_device_2"},
		},
	}

	// Single adapter manages all devices
	webAdapter := &MockWhatsAppWebAdapter{sessions: sessions}

	// Dispatch message 1 through device 1
	err := webAdapter.Dispatch(ctx, &MockMessagePayload{
		SenderIdentity: "jid_device_1",
		To:             "client_a",
		Body:           "hello device 1",
	})
	if err != nil {
		t.Fatalf("dispatch 1 failed: %v", err)
	}

	// Dispatch message 2 through device 2
	err = webAdapter.Dispatch(ctx, &MockMessagePayload{
		SenderIdentity: "jid_device_2",
		To:             "client_b",
		Body:           "hello device 2",
	})
	if err != nil {
		t.Fatalf("dispatch 2 failed: %v", err)
	}

	// Assert correct routing
	if len(sessions.sessions["jid_device_1"].Sent) != 1 || sessions.sessions["jid_device_1"].Sent[0] != "client_a:hello device 1" {
		t.Errorf("device 1 sent unexpected: %v", sessions.sessions["jid_device_1"].Sent)
	}
	if len(sessions.sessions["jid_device_2"].Sent) != 1 || sessions.sessions["jid_device_2"].Sent[0] != "client_b:hello device 2" {
		t.Errorf("device 2 sent unexpected: %v", sessions.sessions["jid_device_2"].Sent)
	}

	// Dispatch through non-existent device
	err = webAdapter.Dispatch(ctx, &MockMessagePayload{
		SenderIdentity: "jid_device_non_existent",
		To:             "client_c",
		Body:           "hello",
	})
	if err == nil {
		t.Error("expected error for non-existent device session, got nil")
	}

	// --- Test Telegram (Stateless Credential Routing) ---
	connID1 := uuid.New()
	connID2 := uuid.New()

	telegramAdapter := &MockTelegramAdapter{
		credentials: map[uuid.UUID]string{
			connID1: "token-bot-1",
			connID2: "token-bot-2",
		},
	}

	// Dispatch through bot 1
	res1, err := telegramAdapter.Dispatch(ctx, &MockMessagePayload{
		ConnectionID: connID1,
		To:           "chat_x",
		Body:         "msg to bot 1",
	})
	if err != nil {
		t.Fatalf("telegram dispatch 1 failed: %v", err)
	}
	if res1 != "sent via Telegram token: token-bot-1" {
		t.Errorf("got response = %q, want expected with token-bot-1", res1)
	}

	// Dispatch through bot 2
	res2, err := telegramAdapter.Dispatch(ctx, &MockMessagePayload{
		ConnectionID: connID2,
		To:           "chat_y",
		Body:         "msg to bot 2",
	})
	if err != nil {
		t.Fatalf("telegram dispatch 2 failed: %v", err)
	}
	if res2 != "sent via Telegram token: token-bot-2" {
		t.Errorf("got response = %q, want expected with token-bot-2", res2)
	}
}
