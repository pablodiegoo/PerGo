package session

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestJIDToPhone(t *testing.T) {
	tests := []struct {
		jid  types.JID
		want string
	}{
		{types.JID{User: "5511999999999", Server: "s.whatsapp.net"}, "5511999999999"},
		{types.JID{User: "1234567890", Server: "s.whatsapp.net"}, "1234567890"},
	}
	for _, tt := range tests {
		got := JIDToPhone(tt.jid)
		if got != tt.want {
			t.Errorf("JIDToPhone(%v) = %q, want %q", tt.jid, got, tt.want)
		}
	}
}

func TestDeviceStatusValues(t *testing.T) {
	if DeviceStatusConnected != "connected" {
		t.Errorf("unexpected connected value: %q", DeviceStatusConnected)
	}
	if DeviceStatusDisconnected != "disconnected" {
		t.Errorf("unexpected disconnected value: %q", DeviceStatusDisconnected)
	}
	if DeviceStatusTerminal != "terminal" {
		t.Errorf("unexpected terminal value: %q", DeviceStatusTerminal)
	}
}
