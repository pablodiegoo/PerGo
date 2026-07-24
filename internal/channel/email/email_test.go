package email

import (
	"context"
	"encoding/json"
	"net/smtp"
	"strings"
	"testing"

	"github.com/pablojhp.pergo/internal/channel"
)

type mockProvider struct {
	lastMsg *EmailMessage
	err     error
}

func (m *mockProvider) Send(ctx context.Context, msg *EmailMessage) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.lastMsg = msg
	return "mock-email-id-123", nil
}

func TestEmailAdapterDispatch(t *testing.T) {
	mock := &mockProvider{}
	adapter := NewEmailAdapter(mock)

	payload := &channel.MessagePayload{
		MessageID: "msg-999",
		To:        "user@example.com",
		Body:      "Hello Email",
		Metadata: map[string]string{
			"subject":     "Welcome to PerGo",
			"from":        "sender@pergo.dev",
			"from_name":   "PerGo Team",
			"track_opens": "true",
		},
	}

	id, err := adapter.Dispatch(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "mock-email-id-123" {
		t.Errorf("expected mock id, got %s", id)
	}
	if mock.lastMsg.Subject != "Welcome to PerGo" {
		t.Errorf("expected subject 'Welcome to PerGo', got '%s'", mock.lastMsg.Subject)
	}
	if mock.lastMsg.From != "sender@pergo.dev" {
		t.Errorf("expected from 'sender@pergo.dev', got '%s'", mock.lastMsg.From)
	}
	if !mock.lastMsg.TrackOpens {
		t.Errorf("expected TrackOpens true")
	}
}

func TestEmailAdapterChannelOverrides(t *testing.T) {
	mock := &mockProvider{}
	adapter := NewEmailAdapter(mock)

	overrideJSON, _ := json.Marshal(map[string]string{
		"subject":   "Overridden Subject",
		"html_body": "<h1>HTML Content</h1>",
	})

	payload := &channel.MessagePayload{
		MessageID: "msg-100",
		To:        "user@example.com",
		Body:      "Plain body",
		ChannelOverrides: map[string]json.RawMessage{
			"email": overrideJSON,
		},
	}

	_, err := adapter.Dispatch(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastMsg.Subject != "Overridden Subject" {
		t.Errorf("expected 'Overridden Subject', got '%s'", mock.lastMsg.Subject)
	}
	if mock.lastMsg.HTMLBody != "<h1>HTML Content</h1>" {
		t.Errorf("expected HTML body, got '%s'", mock.lastMsg.HTMLBody)
	}
}

func TestBuildMIMEPayload(t *testing.T) {
	msg := &EmailMessage{
		ID:       "test-id-12345678",
		To:       []string{"recipient@example.com"},
		Subject:  "Test Subject",
		TextBody: "Text body",
		HTMLBody: "<p>HTML body</p>",
	}

	buf := BuildMIMEPayload("sender@pergo.dev", "Sender Name", msg, msg.ID)
	content := buf.String()

	if !strings.Contains(content, "From: Sender Name <sender@pergo.dev>") {
		t.Errorf("missing From header in MIME output: %s", content)
	}
	if !strings.Contains(content, "Subject: Test Subject") {
		t.Errorf("missing Subject header in MIME output: %s", content)
	}
	if !strings.Contains(content, "multipart/alternative") {
		t.Errorf("missing multipart/alternative content type in MIME output: %s", content)
	}
}

func TestSMTPProviderSendMock(t *testing.T) {
	var sentMail bool
	oldSendMail := sendMail
	defer func() { sendMail = oldSendMail }()

	sendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		sentMail = true
		if addr != "smtp.example.com:587" {
			t.Errorf("unexpected addr: %s", addr)
		}
		if from != "admin@pergo.dev" {
			t.Errorf("unexpected from: %s", from)
		}
		return nil
	}

	provider := NewSMTPProvider(SMTPConfig{
		Host:        "smtp.example.com",
		Port:        587,
		FromAddress: "admin@pergo.dev",
	})

	id, err := provider.Send(context.Background(), &EmailMessage{
		ID:       "id-123",
		To:       []string{"target@example.com"},
		Subject:  "Test",
		TextBody: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "id-123" {
		t.Errorf("expected id-123, got %s", id)
	}
	if !sentMail {
		t.Errorf("sendMail was not called")
	}
}
