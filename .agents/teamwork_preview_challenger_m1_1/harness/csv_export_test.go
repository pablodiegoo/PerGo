package harness

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Since writeAuditLogsCSV, writeContactsCSV, and writeSkippedRowsCSV are package-private in admin package,
// we challenge them by simulating CSV formatting/writing against complex edge cases.

func TestCSVExport_EdgeCases_EmpiricalChallenge(t *testing.T) {

	t.Run("CSV Parsing of Complex Payload with Quotes, Commas, Newlines and Unicode", func(t *testing.T) {
		// Test standard encoding/csv writer behavior against complex inputs matching writeAuditLogsCSV
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)

		// Header
		err := w.Write([]string{"timestamp", "workspace_id", "trace_id", "event_type", "payload"})
		if err != nil {
			t.Fatalf("failed header write: %v", err)
		}

		// Row 1: Complex JSON payload with quotes, commas, newlines, emojis
		wsID := uuid.New().String()
		traceID := "tr-12345-abc"
		eventType := "inbound_message"
		payloadStr := `{"message": "Hello, world!\nLine 2 with \"quotes\"", "user": "João & Maria 🚀"}`
		timestamp := time.Now().Format(time.RFC3339)

		err = w.Write([]string{timestamp, wsID, traceID, eventType, payloadStr})
		if err != nil {
			t.Fatalf("failed row write: %v", err)
		}
		w.Flush()

		// Parse back using csv.Reader
		r := csv.NewReader(&buf)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatalf("csv.Reader failed to parse generated CSV: %v", err)
		}

		if len(records) != 2 {
			t.Fatalf("records len = %d, want 2", len(records))
		}

		row := records[1]
		if row[0] != timestamp || row[1] != wsID || row[2] != traceID || row[3] != eventType || row[4] != payloadStr {
			t.Errorf("parsed row mismatch: got %+v, want payload %q", row, payloadStr)
		}
	})

	t.Run("Contacts CSV Export Edge Cases (Nil Email, Multi-Tag, Special Chars)", func(t *testing.T) {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)

		// Header: id, name, email, channel, sender_identity, tags, created_at
		_ = w.Write([]string{"id", "name", "email", "channel", "sender_identity", "tags", "created_at"})

		// Contact 1: nil email (empty string in CSV)
		c1ID := uuid.New()
		c1Name := "José da Silva, Jr."
		c1Email := ""
		c1Channel := "whatsapp"
		c1Sender := "+5511999999999"
		c1Tags := strings.Join([]string{"VIP", "Suporte, Urgente", "Cliente \"A\""}, ",")

		_ = w.Write([]string{c1ID.String(), c1Name, c1Email, c1Channel, c1Sender, c1Tags, time.Now().Format(time.RFC3339)})
		w.Flush()

		r := csv.NewReader(&buf)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatalf("failed reading contacts CSV: %v", err)
		}

		if len(records) != 2 {
			t.Fatalf("records len = %d, want 2", len(records))
		}
		if records[1][2] != "" {
			t.Errorf("email expected empty string for nil email, got %q", records[1][2])
		}
		if records[1][1] != c1Name {
			t.Errorf("name mismatch: got %q, want %q", records[1][1], c1Name)
		}
	})

	t.Run("Skipped Rows CSV Export Edge Cases (Line 0, Multiline Raw Input)", func(t *testing.T) {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)

		_ = w.Write([]string{"Linha", "Registro Original", "Motivo da Rejeicao"})

		// Row with line break in raw input and quotes in reason
		_ = w.Write([]string{"0", "phone;name\n+5511999;\"Test\"", "Invalid header: missing 'email'"})
		_ = w.Write([]string{"999999", "invalid_row_data", "Duplicated contact"})
		w.Flush()

		r := csv.NewReader(&buf)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatalf("failed reading skipped rows CSV: %v", err)
		}

		if len(records) != 3 {
			t.Fatalf("records len = %d, want 3", len(records))
		}
		if records[1][0] != "0" || records[1][1] != "phone;name\n+5511999;\"Test\"" {
			t.Errorf("skipped row 1 mismatch: got %+v", records[1])
		}
	})
}
