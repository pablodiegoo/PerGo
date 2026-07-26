package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockPublisher struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.published = append(m.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject: subject, data: data, traceID: traceID})
	return nil
}

func TestSessionTicker_CheckExpiringSessions(t *testing.T) {
	wsID := uuid.New()
	phone := "+5511999990001"
	channelName := "whatsapp_cloud"
	recIdentity := "+5511999990002"
	now := time.Now().UTC()

	// Simulate standard session at 23h5m ago
	sessStandard := repository.RecipientSession{
		WorkspaceID:       wsID,
		RecipientPhone:    phone,
		Channel:           channelName,
		RecipientIdentity: recIdentity,
		LastInboundAt:     now.Add(-23 * time.Hour).Add(-2 * time.Minute),
		EntryPointType:    "standard",
	}

	pub := &mockPublisher{}
	// Test ExpiringSessionEvent marshalling
	evt := ExpiringSessionEvent{
		Event:          "session.expiring_soon",
		TraceID:        uuid.New().String(),
		WorkspaceID:    sessStandard.WorkspaceID.String(),
		RecipientPhone: sessStandard.RecipientPhone,
		Channel:        sessStandard.Channel,
		EntryPointType: sessStandard.EntryPointType,
		ExpiresAt:      sessStandard.LastInboundAt.Add(24 * time.Hour).Format(time.RFC3339),
		Timestamp:      now.Format(time.RFC3339),
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	if err := pub.Publish(context.Background(), "webhooks.events", payload, evt.TraceID); err != nil {
		t.Fatalf("failed to publish event: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}

	if pub.published[0].subject != "webhooks.events" {
		t.Errorf("got subject %s, want webhooks.events", pub.published[0].subject)
	}

	var parsed ExpiringSessionEvent
	if err := json.Unmarshal(pub.published[0].data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if parsed.Event != "session.expiring_soon" {
		t.Errorf("got Event %s, want session.expiring_soon", parsed.Event)
	}
	if parsed.EntryPointType != "standard" {
		t.Errorf("got EntryPointType %s, want standard", parsed.EntryPointType)
	}
}
