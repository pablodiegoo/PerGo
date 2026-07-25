package slug_test

import (
	"testing"

	"github.com/pablojhp.pergo/internal/pkg/slug"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name with space",
			input:    "Vendas SP",
			expected: "vendas-sp",
		},
		{
			name:     "special characters and accents",
			input:    "Suporte_Técnico!!",
			expected: "suporte-tcnico",
		},
		{
			name:     "multiple underscores and spaces",
			input:    "  WhatsApp___Cloud   Device  ",
			expected: "whatsapp-cloud-device",
		},
		{
			name:     "leading and trailing hyphens",
			input:    "---Telegram Bot---",
			expected: "telegram-bot",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "numbers and letters",
			input:    "Channel 123 Test",
			expected: "channel-123-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slug.Generate(tt.input)
			if got != tt.expected {
				t.Errorf("Generate(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}
