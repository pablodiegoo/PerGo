package whatsapp

import (
	"errors"
	"testing"
	"time"
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
