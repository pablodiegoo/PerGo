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
						{Channel: "whatsapp", SenderIdentity: "+5511999998888"},
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
						{Channel: "whatsapp", SenderIdentity: "5511999998888"}, // Duplicate phone of c1
					},
				},
			},
		},
	}

	records, recipients, seenPhones, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1, tag2}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c1 should be resolved as pending
	// c2 should be resolved as skipped (no identities)
	// c3 should be skipped by deduplication against c1
	if len(records) != 2 {
		t.Fatalf("expected 2 records resolved (1 pending, 1 skipped), got %d", len(records))
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient resolved, got %d", len(recipients))
	}
	if records[0].Phone != "5511999998888" || records[0].Status != RecipientStatusPending {
		t.Errorf("expected c1 to be pending with 5511999998888, got phone %s, status %s", records[0].Phone, records[0].Status)
	}
	if records[1].Status != RecipientStatusSkipped {
		t.Errorf("expected c2 to be skipped, got status %s", records[1].Status)
	}
	if !seenPhones["5511999998888"] {
		t.Errorf("expected seenPhones['5511999998888'] to be true")
	}
}

func TestResolveTagRecipients_ChannelFilter(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()

	c1ID := uuid.New()
	c2ID := uuid.New()

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:   c1ID,
					Name: "Alice",
					Identities: []ContactIdentity{
						{Channel: "whatsapp", SenderIdentity: "+5511999998888"},
						{Channel: "telegram", SenderIdentity: "alice_tg"},
					},
				},
				{
					ID:   c2ID,
					Name: "Bob",
					Identities: []ContactIdentity{
						{Channel: "telegram", SenderIdentity: "5521988887777"},
					},
				},
			},
		},
	}

	// Filter by whatsapp:
	// - Alice matches whatsapp -> Status: RecipientStatusPending
	// - Bob lacks whatsapp identity -> Status: RecipientStatusSkipped
	// - recipients slice contains ONLY Alice
	records, recipients, seenPhones, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records (1 pending, 1 skipped), got %d", len(records))
	}
	if records[0].Phone != "5511999998888" || records[0].Status != RecipientStatusPending {
		t.Errorf("expected records[0] to be Alice pending, got phone %s, status %s", records[0].Phone, records[0].Status)
	}
	if records[1].Phone != "5521988887777" || records[1].Status != RecipientStatusSkipped {
		t.Errorf("expected records[1] to be Bob skipped, got phone %s, status %s", records[1].Phone, records[1].Status)
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient for batch dispatch, got %d", len(recipients))
	}
	if recipients[0].To != "5511999998888" {
		t.Errorf("expected recipient Alice, got %s", recipients[0].To)
	}
	if !seenPhones["5511999998888"] || !seenPhones["5521988887777"] {
		t.Errorf("expected seenPhones to contain both Alice and Bob's identities")
	}

	// Empty filter — both should match as pending if they have valid phones
	records2, recipients2, _, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records2) != 2 {
		t.Fatalf("expected 2 records for empty filter, got %d", len(records2))
	}
	if len(recipients2) != 2 {
		t.Fatalf("expected 2 recipients for empty filter, got %d", len(recipients2))
	}
}

func TestResolveTagRecipients_SkippedNoIdentities(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()

	c1ID := uuid.New()
	email := "carol@example.com"

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:         c1ID,
					Name:       "Carol",
					Email:      &email,
					Identities: nil,
				},
			},
		},
	}

	records, recipients, seenPhones, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 skipped record, got %d", len(records))
	}
	if records[0].Status != RecipientStatusSkipped {
		t.Errorf("expected status skipped, got %s", records[0].Status)
	}
	if records[0].Phone != "carol@example.com" {
		t.Errorf("expected identity carol@example.com, got %s", records[0].Phone)
	}
	if len(recipients) != 0 {
		t.Errorf("expected 0 recipients, got %d", len(recipients))
	}
	if !seenPhones["carol@example.com"] {
		t.Errorf("expected seenPhones to contain carol@example.com")
	}
}
