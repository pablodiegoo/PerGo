package webhook

import (
	"testing"
)

func TestMatchEvent(t *testing.T) {
	tests := []struct {
		pattern   string
		eventName string
		expected  bool
	}{
		{"*", "message.sent", true},
		{"*", "connection.status", true},
		{"message.*", "message.sent", true},
		{"message.*", "message.received", true},
		{"message.*", "connection.status", false},
		{"message.sent", "message.sent", true},
		{"message.sent", "message.received", false},
		{"connection.*", "connection.status", true},
		{"connection.*", "message.sent", false},
	}

	for _, tc := range tests {
		result := MatchEvent(tc.pattern, tc.eventName)
		if result != tc.expected {
			t.Errorf("MatchEvent(%q, %q) = %t; want %t", tc.pattern, tc.eventName, result, tc.expected)
		}
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		patterns  []string
		eventName string
		expected  bool
	}{
		{[]string{"message.*", "connection.status"}, "message.sent", true},
		{[]string{"message.*", "connection.status"}, "connection.status", true},
		{[]string{"message.*", "connection.*"}, "message.failed", true},
		{[]string{"message.sent", "connection.status"}, "message.received", false},
		{[]string{"*"}, "anything", true},
		{[]string{}, "message.sent", false},
	}

	for _, tc := range tests {
		result := MatchesAny(tc.patterns, tc.eventName)
		if result != tc.expected {
			t.Errorf("MatchesAny(%v, %q) = %t; want %t", tc.patterns, tc.eventName, result, tc.expected)
		}
	}
}
