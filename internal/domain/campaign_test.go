package domain

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSniffDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected rune
	}{
		{"comma", "nome,cidade,idade\n", ','},
		{"semicolon", "nome;cidade;idade\n", ';'},
		{"tab", "nome\tcidade\tidade\n", '\t'},
		{"comma fallback", "nome-cidade-idade\n", ','},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SniffDelimiter(tt.input); got != tt.expected {
				t.Errorf("SniffDelimiter() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizePhone(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantClean  string
		wantValid  bool
	}{
		{"valid standard", "+55 (11) 99999-8888", "5511999998888", true},
		{"valid raw", "5511999998888", "5511999998888", true},
		{"short invalid", "99999-8888", "999998888", false},
		{"long invalid", "5511999998888777", "5511999998888777", false},
		{"alphabetic noise", "551199abc998888", "551199998888", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotValid := SanitizePhone(tt.input)
			if gotClean != tt.wantClean || gotValid != tt.wantValid {
				t.Errorf("SanitizePhone() = (%q, %t), want (%q, %t)", gotClean, gotValid, tt.wantClean, tt.wantValid)
			}
		})
	}
}

func TestResolveVariables(t *testing.T) {
	row := map[string]string{
		"nome":   "João",
		"cidade": "São Paulo",
		"0":      "Primeiro",
		"1":      "Segundo",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard", "Olá {{nome}} de {{cidade}}!", "Olá João de São Paulo!"},
		{"case insensitive", "Olá {{Nome}} de {{Cidade}}!", "Olá João de São Paulo!"},
		{"whitespace", "Olá {{  nome  }}!", "Olá João!"},
		{"missing column", "Olá {{nome}} de {{pais}}!", "Olá João de {{pais}}!"},
		{"index based", "Item {{0}} e {{1}}", "Item Primeiro e Segundo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVariables(tt.input, row); got != tt.expected {
				t.Errorf("ResolveVariables() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCalculateDuration(t *testing.T) {
	tests := []struct {
		name         string
		totalValid   int
		batchSize    int
		delaySeconds int
		expected     int
	}{
		{"exact division", 100, 50, 5, 10},
		{"with remainder", 101, 50, 5, 15},
		{"zero valid", 0, 50, 5, 0},
		{"zero batch", 100, 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateDuration(tt.totalValid, tt.batchSize, tt.delaySeconds); got != tt.expected {
				t.Errorf("CalculateDuration() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestDeduplicateUUIDs(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	input := []uuid.UUID{id1, id2, uuid.Nil, id1, id3, id2, uuid.Nil}
	expected := []uuid.UUID{id1, id2, id3}

	result := DeduplicateUUIDs(input)
	if len(result) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("at index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}
}

type mockTagLister struct {
	contacts map[uuid.UUID][]Contact
	err      error
}

func (m *mockTagLister) ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.contacts[tagID], nil
}

func TestResolveTagRecipients(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()
	tag2 := uuid.New()

	c1ID := uuid.New()
	c2ID := uuid.New()
	c3ID := uuid.New()

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:   c1ID,
					Name: "Alice",
					Identities: []ContactIdentity{
						{SenderIdentity: "+5511999998888"},
					},
				},
				{
					ID:         c2ID,
					Name:       "5511977776666", // Name looks like phone, but no identities
					Identities: nil,
				},
			},
			tag2: {
				{
					ID:   c3ID,
					Name: "Alice Duplicate Phone",
					Identities: []ContactIdentity{
						{SenderIdentity: "5511999998888"}, // Duplicate phone of c1
					},
				},
			},
		},
	}

	records, recipients, seenPhones, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1, tag2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c2 should be skipped because no identities (name fallback removed)
	// c3 should be skipped because phone 5511999998888 is duplicate
	// Only c1 should be resolved
	if len(records) != 1 {
		t.Fatalf("expected 1 record resolved, got %d", len(records))
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient resolved, got %d", len(recipients))
	}
	if records[0].Phone != "5511999998888" {
		t.Errorf("expected phone 5511999998888, got %s", records[0].Phone)
	}
	if !seenPhones["5511999998888"] {
		t.Errorf("expected seenPhones['5511999998888'] to be true")
	}
}

