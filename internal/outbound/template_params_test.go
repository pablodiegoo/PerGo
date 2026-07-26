package outbound

import (
	"encoding/json"
	"testing"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestCountTemplateVariables(t *testing.T) {
	tests := []struct {
		name       string
		components string
		expected   map[string]int
		expectErr  bool
	}{
		{
			name: "body with 2 variables",
			components: `[
				{"type": "BODY", "text": "Hello {{1}}, your order {{2}} is ready"}
			]`,
			expected:  map[string]int{"body": 2},
			expectErr: false,
		},
		{
			name: "header and body with no variables",
			components: `[
				{"type": "HEADER", "format": "TEXT", "text": "Welcome"},
				{"type": "BODY", "text": "This is a static message."}
			]`,
			expected:  map[string]int{},
			expectErr: false,
		},
		{
			name: "url button with 1 variable",
			components: `[
				{"type": "BODY", "text": "Click below"},
				{
					"type": "BUTTONS",
					"buttons": [
						{"type": "URL", "url": "https://example.com/{{1}}"}
					]
				}
			]`,
			expected:  map[string]int{"button": 1},
			expectErr: false,
		},
		{
			name: "multiple components with variables",
			components: `[
				{"type": "HEADER", "format": "TEXT", "text": "Hello {{1}}"},
				{"type": "BODY", "text": "Your order {{1}} is {{2}}"},
				{
					"type": "BUTTONS",
					"buttons": [
						{"type": "URL", "url": "https://example.com/{{1}}"}
					]
				}
			]`,
			expected:  map[string]int{"header": 1, "body": 2, "button": 1},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.components)
			actual, err := CountTemplateVariables(raw)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, actual)
			}
		})
	}
}

func TestValidateParameterCounts(t *testing.T) {
	tests := []struct {
		name      string
		provided  []domain.TemplateComponent
		expected  map[string]int
		expectErr bool
	}{
		{
			name: "matching counts",
			provided: []domain.TemplateComponent{
				{
					Type: "BODY",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "John"},
						{Type: "text", Text: "1234"},
					},
				},
			},
			expected:  map[string]int{"body": 2},
			expectErr: false,
		},
		{
			name: "mismatched counts",
			provided: []domain.TemplateComponent{
				{
					Type: "BODY",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "John"},
						{Type: "text", Text: "1234"},
						{Type: "text", Text: "extra"},
					},
				},
			},
			expected:  map[string]int{"body": 2},
			expectErr: true,
		},
		{
			name: "missing component in provided",
			provided: []domain.TemplateComponent{
				{
					Type: "HEADER",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "John"},
					},
				},
			},
			expected:  map[string]int{"body": 2},
			expectErr: true,
		},
		{
			name: "extra component in provided (ignored)",
			provided: []domain.TemplateComponent{
				{
					Type: "BODY",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "John"},
					},
				},
				{
					Type: "HEADER",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "ignored"},
					},
				},
			},
			expected:  map[string]int{"body": 1},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParameterCounts(tt.provided, tt.expected)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
