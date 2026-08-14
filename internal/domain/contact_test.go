package domain_test

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestWriteContactsCSV_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := domain.WriteContactsCSV(&buf, []domain.Contact{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 header record, got %d", len(records))
	}

	expectedHeader := []string{"id", "name", "email", "channel", "sender_identity", "tags", "created_at"}
	if len(records[0]) != len(expectedHeader) {
		t.Fatalf("expected header length %d, got %d: %v", len(expectedHeader), len(records[0]), records[0])
	}
	for i, h := range expectedHeader {
		if records[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, records[0][i], h)
		}
	}
}

func TestWriteContactsCSV_Standard(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	cID := uuid.New()
	email := "alice@example.com"

	contacts := []domain.Contact{
		{
			ID:          cID,
			WorkspaceID: uuid.New(),
			Name:        "Alice Silva",
			Email:       &email,
			Tags:        []string{"VIP", "Customer"},
			CreatedAt:   now,
			Identities: []domain.ContactIdentity{
				{
					Channel:        "whatsapp",
					SenderIdentity: "5511999990001",
				},
			},
		},
	}

	var buf bytes.Buffer
	err := domain.WriteContactsCSV(&buf, contacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	row := records[1]
	if row[0] != cID.String() {
		t.Errorf("id = %q, want %q", row[0], cID.String())
	}
	if row[1] != "Alice Silva" {
		t.Errorf("name = %q, want %q", row[1], "Alice Silva")
	}
	if row[2] != "alice@example.com" {
		t.Errorf("email = %q, want %q", row[2], "alice@example.com")
	}
	if row[3] != "whatsapp" {
		t.Errorf("channel = %q, want %q", row[3], "whatsapp")
	}
	if row[4] != "5511999990001" {
		t.Errorf("sender_identity = %q, want %q", row[4], "5511999990001")
	}
	if row[5] != "VIP,Customer" {
		t.Errorf("tags = %q, want %q", row[5], "VIP,Customer")
	}
	if row[6] != now.Format(time.RFC3339) {
		t.Errorf("created_at = %q, want %q", row[6], now.Format(time.RFC3339))
	}
}

func TestWriteContactsCSV_CustomAttributes(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	cID1 := uuid.New()
	cID2 := uuid.New()

	contacts := []domain.Contact{
		{
			ID:        cID1,
			Name:      "Contact 1",
			Tags:      []string{"Lead"},
			CreatedAt: now,
			Attributes: map[string]string{
				"plan": "Enterprise",
				"city": "Sao Paulo",
			},
		},
		{
			ID:        cID2,
			Name:      "Contact 2",
			Tags:      nil,
			CreatedAt: now,
			Attributes: map[string]string{
				"city":    "Rio de Janeiro",
				"segment": "Tech",
			},
		},
	}

	var buf bytes.Buffer
	err := domain.WriteContactsCSV(&buf, contacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Attribute keys sorted: "city", "plan", "segment"
	expectedHeaders := []string{
		"id", "name", "email", "channel", "sender_identity", "tags", "created_at",
		"city", "plan", "segment",
	}

	if len(records[0]) != len(expectedHeaders) {
		t.Fatalf("expected %d headers, got %d: %v", len(expectedHeaders), len(records[0]), records[0])
	}
	for i, h := range expectedHeaders {
		if records[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, records[0][i], h)
		}
	}

	// Check row 1
	row1 := records[1]
	if row1[7] != "Sao Paulo" || row1[8] != "Enterprise" || row1[9] != "" {
		t.Errorf("row1 attrs = city:%q, plan:%q, segment:%q; want city:Sao Paulo, plan:Enterprise, segment:\"\"", row1[7], row1[8], row1[9])
	}

	// Check row 2
	row2 := records[2]
	if row2[7] != "Rio de Janeiro" || row2[8] != "" || row2[9] != "Tech" {
		t.Errorf("row2 attrs = city:%q, plan:%q, segment:%q; want city:Rio de Janeiro, plan:\"\", segment:Tech", row2[7], row2[8], row2[9])
	}
}

func TestWriteContactsCSV_EdgeCases(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	cID := uuid.New()

	contacts := []domain.Contact{
		{
			ID:        cID,
			Name:      `Jane "Doe", Jr.`,
			Email:     nil,
			Tags:      []string{"VIP, Priority", "Lead"},
			CreatedAt: now,
			Attributes: map[string]string{
				"notes": "line1\nline2, with comma",
			},
		},
	}

	var buf bytes.Buffer
	err := domain.WriteContactsCSV(&buf, contacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	row := records[1]
	if row[0] != cID.String() {
		t.Errorf("id = %q, want %q", row[0], cID.String())
	}
	if row[1] != `Jane "Doe", Jr.` {
		t.Errorf("name = %q, want %q", row[1], `Jane "Doe", Jr.`)
	}
	if row[2] != "" {
		t.Errorf("email = %q, want empty string", row[2])
	}
	if row[3] != "" || row[4] != "" {
		t.Errorf("channel/identity = %q/%q, want empty strings", row[3], row[4])
	}
	if row[5] != "VIP, Priority,Lead" {
		t.Errorf("tags = %q, want %q", row[5], "VIP, Priority,Lead")
	}
	if row[7] != "line1\nline2, with comma" {
		t.Errorf("notes attr = %q, want %q", row[7], "line1\nline2, with comma")
	}
}
