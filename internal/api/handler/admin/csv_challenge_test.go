package admin

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestWriteAuditLogsCSV_EdgeCases(t *testing.T) {
	t.Run("Empty Entries", func(t *testing.T) {
		var buf bytes.Buffer
		err := writeAuditLogsCSV(&buf, []repository.AuditEntry{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse generated CSV: %v", err)
		}

		if len(records) != 1 {
			t.Fatalf("expected 1 record (header only), got %d", len(records))
		}
		expectedHeaders := []string{"timestamp", "workspace_id", "trace_id", "event_type", "payload"}
		for i, h := range expectedHeaders {
			if records[0][i] != h {
				t.Errorf("header[%d] = %q, want %q", i, records[0][i], h)
			}
		}
	})

	t.Run("Special Characters, Quotes, Newlines in Payload", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		wsID := uuid.New()
		complexJSON, _ := json.Marshal(map[string]string{
			"msg":   "Line 1\nLine 2",
			"quote": `He said "Hello!"`,
			"comma": "A, B, and C",
		})

		entries := []repository.AuditEntry{
			{
				CreatedAt:   now,
				WorkspaceID: wsID,
				TraceID:     "trace-123",
				EventType:   "inbound_message",
				Payload:     complexJSON,
			},
		}

		var buf bytes.Buffer
		err := writeAuditLogsCSV(&buf, entries)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse generated CSV with special chars: %v", err)
		}

		if len(records) != 2 {
			t.Fatalf("expected 2 records (1 header + 1 data), got %d", len(records))
		}

		row := records[1]
		if row[0] != now.Format(time.RFC3339) {
			t.Errorf("timestamp = %q, want %q", row[0], now.Format(time.RFC3339))
		}
		if row[1] != wsID.String() {
			t.Errorf("workspace_id = %q, want %q", row[1], wsID.String())
		}
		if row[2] != "trace-123" {
			t.Errorf("trace_id = %q, want %q", row[2], "trace-123")
		}
		if row[3] != "inbound_message" {
			t.Errorf("event_type = %q, want %q", row[3], "inbound_message")
		}
		if row[4] != string(complexJSON) {
			t.Errorf("payload = %q, want %q", row[4], string(complexJSON))
		}
	})
}

func TestWriteContactsCSV_EdgeCases(t *testing.T) {
	t.Run("Empty Contacts", func(t *testing.T) {
		var buf bytes.Buffer
		err := domain.WriteContactsCSV(&buf, []domain.Contact{}, func(contactID uuid.UUID) []string {
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse CSV: %v", err)
		}

		if len(records) != 1 {
			t.Fatalf("expected 1 header record, got %d", len(records))
		}
	})

	t.Run("Nil Email, Empty Identities, Tags with Commas", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		cID := uuid.New()

		contacts := []domain.Contact{
			{
				ID:        cID,
				Name:      `Jane "Doe", Jr.`,
				Email:     nil, // Nil email
				CreatedAt: now,
				// Identities is empty slice
			},
		}

		var buf bytes.Buffer
		err := domain.WriteContactsCSV(&buf, contacts, func(contactID uuid.UUID) []string {
			return []string{"VIP, Priority", "Lead"}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse CSV: %v", err)
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
	})
}

func TestWriteSkippedRowsCSV_EdgeCases(t *testing.T) {
	t.Run("Empty Skipped Rows", func(t *testing.T) {
		var buf bytes.Buffer
		err := writeSkippedRowsCSV(&buf, []domain.SkippedRow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse CSV: %v", err)
		}

		if len(records) != 1 {
			t.Fatalf("expected 1 record (header), got %d", len(records))
		}
	})

	t.Run("Special Characters and Quotes in Skipped Rows", func(t *testing.T) {
		skipped := []domain.SkippedRow{
			{
				LineNumber: 2,
				RawInput:   `551199999,"John \"Bad\" Doe",invalid`,
				Reason:     "telefone invalido (tamanho 8)",
			},
		}

		var buf bytes.Buffer
		err := writeSkippedRowsCSV(&buf, skipped)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader := csv.NewReader(&buf)
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("failed to parse CSV: %v", err)
		}

		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}

		row := records[1]
		if row[0] != "2" {
			t.Errorf("LineNumber = %q, want 2", row[0])
		}
		if row[1] != `551199999,"John \"Bad\" Doe",invalid` {
			t.Errorf("RawInput = %q", row[1])
		}
		if row[2] != "telefone invalido (tamanho 8)" {
			t.Errorf("Reason = %q", row[2])
		}
	})
}
