package whatsapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestPhoneToJID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"5511999999999", "5511999999999@s.whatsapp.net"},
		{"+55 11 99999-9999", "5511999999999@s.whatsapp.net"},
		{"(11) 99999-9999", "11999999999@s.whatsapp.net"},
		{"1234567890", "1234567890@s.whatsapp.net"},
	}
	for _, tt := range tests {
		got := phoneToJID(tt.input)
		if got != tt.want {
			t.Errorf("phoneToJID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsTerminalWhatsAppError(t *testing.T) {
	tests := []struct {
		err      string
		terminal bool
	}{
		{"connection failed: 403 forbidden", true},
		{"logged out from another device", true},
		{"device unpaired", true},
		{"connection timeout", false},
		{"temporary network error", false},
	}
	for _, tt := range tests {
		got := isTerminalWhatsAppError(errors.New(tt.err))
		if got != tt.terminal {
			t.Errorf("isTerminalWhatsAppError(%q) = %v, want %v", tt.err, got, tt.terminal)
		}
	}
}

func TestStaggerBounds(t *testing.T) {
	if minStagger != 1*time.Second {
		t.Errorf("minStagger = %v, want 1s", minStagger)
	}
	if maxStagger != 3*time.Second {
		t.Errorf("maxStagger = %v, want 3s", maxStagger)
	}
}

func TestNewWhatsAppAdapter(t *testing.T) {
	a := NewWhatsAppAdapter(nil, nil)
	if a == nil {
		t.Error("expected adapter")
	}
}

func TestIsTerminalWhatsAppErrorNil(t *testing.T) {
	if isTerminalWhatsAppError(nil) {
		t.Error("nil error should not be terminal")
	}
}

func TestBuildInteractiveOrOverrideMsg_Override(t *testing.T) {
	payload := &channel.MessagePayload{
		ChannelOverrides: map[string]json.RawMessage{
			"whatsapp": json.RawMessage(`{"conversation": "override text"}`),
		},
	}
	msg, err := buildInteractiveOrOverrideMsg(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil || msg.Conversation == nil || *msg.Conversation != "override text" {
		t.Errorf("expected conversation 'override text', got %+v", msg)
	}
}

func TestBuildInteractiveOrOverrideMsg_InteractiveButton(t *testing.T) {
	payload := &channel.MessagePayload{
		Interactive: &domain.Interactive{
			Type: "button",
			Body: domain.TextContent{Text: "body text"},
			Action: domain.Action{
				Buttons: []domain.Button{
					{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Yes"}},
					{Type: "reply", Reply: domain.Reply{ID: "2", Title: "No"}},
				},
			},
		},
	}
	msg, err := buildInteractiveOrOverrideMsg(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil || msg.ButtonsMessage == nil {
		t.Fatalf("expected ButtonsMessage")
	}
	if len(msg.ButtonsMessage.Buttons) != 2 {
		t.Errorf("expected 2 buttons, got %d", len(msg.ButtonsMessage.Buttons))
	}
}

func TestBuildInteractiveOrOverrideMsg_Degrade(t *testing.T) {
	payload := &channel.MessagePayload{
		FallbackBehavior: "degrade",
		Interactive: &domain.Interactive{
			Type: "button",
			Body: domain.TextContent{Text: "body text"},
			Action: domain.Action{
				Buttons: []domain.Button{
					{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Yes"}},
					{Type: "reply", Reply: domain.Reply{ID: "2", Title: "No"}},
					{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Maybe"}},
					{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Too many"}},
				},
			},
		},
	}
	msg, err := buildInteractiveOrOverrideMsg(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil || msg.Conversation == nil {
		t.Fatalf("expected degraded text conversation")
	}
	if !strings.Contains(*msg.Conversation, "Too many") {
		t.Errorf("expected degraded text to contain button titles: %s", *msg.Conversation)
	}
}

func TestBuildInteractiveOrOverrideMsg_Fail(t *testing.T) {
	payload := &channel.MessagePayload{
		FallbackBehavior: "fail",
		Interactive: &domain.Interactive{
			Type: "button",
			Body: domain.TextContent{Text: "body text"},
			Action: domain.Action{
				Buttons: []domain.Button{
					{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Yes"}},
					{Type: "reply", Reply: domain.Reply{ID: "2", Title: "No"}},
					{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Maybe"}},
					{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Too many"}},
				},
			},
		},
	}
	_, err := buildInteractiveOrOverrideMsg(payload)
	if err == nil {
		t.Fatalf("expected error on fallback fail")
	}
	if _, ok := err.(*channel.TerminalError); !ok {
		t.Errorf("expected TerminalError, got %T: %v", err, err)
	}
}
