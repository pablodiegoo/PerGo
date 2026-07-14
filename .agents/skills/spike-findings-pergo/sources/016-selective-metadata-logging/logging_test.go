package logging

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Workspace represents client settings, including compliance toggles.
type Workspace struct {
	ID                uuid.UUID
	SaveMessageBodies bool // If false, redact plaintext message bodies/media URLs before database insert.
}

// MessagePayload represents the raw message payload sent or received.
type MessagePayload struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Body      string `json:"body,omitempty"`
	MediaURL  string `json:"media_url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// AuditLogEntry is the final data written to the database.
type AuditLogEntry struct {
	WorkspaceID uuid.UUID
	EventType   string
	Payload     json.RawMessage
	CreatedAt   time.Time
}

// RedactEvent processes a raw message payload according to workspace settings.
func RedactEvent(ws Workspace, eventType string, raw []byte) ([]byte, error) {
	if ws.SaveMessageBodies {
		return raw, nil
	}

	// Message body saving is disabled. Perform selective logging.
	var msg MessagePayload
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}

	// Construct the redacted metadata-only payload
	redacted := map[string]any{
		"from":        msg.From,
		"to":          msg.To,
		"body_length": len(msg.Body),
	}

	if msg.MediaType != "" {
		redacted["media_type"] = msg.MediaType
	}

	return json.Marshal(redacted)
}

func TestRedactEvent(t *testing.T) {
	wsID := uuid.New()

	t.Run("SaveMessageBodies enabled preserves full payload", func(t *testing.T) {
		ws := Workspace{ID: wsID, SaveMessageBodies: true}
		payload := MessagePayload{
			From:      "+5511999990000",
			To:        "+5511988881111",
			Body:      "Secret bank transfer detail: 12345",
			MediaURL:  "https://pergo.io/receipt.pdf",
			MediaType: "document",
		}
		raw, _ := json.Marshal(payload)

		redacted, err := RedactEvent(ws, "message_sent", raw)
		if err != nil {
			t.Fatalf("redact failed: %v", err)
		}

		var result MessagePayload
		json.Unmarshal(redacted, &result)

		if result.Body != payload.Body {
			t.Errorf("got body %s, want %s", result.Body, payload.Body)
		}
		if result.MediaURL != payload.MediaURL {
			t.Errorf("got media_url %s, want %s", result.MediaURL, payload.MediaURL)
		}
	})

	t.Run("SaveMessageBodies disabled redacts PII but preserves metadata", func(t *testing.T) {
		ws := Workspace{ID: wsID, SaveMessageBodies: false}
		payload := MessagePayload{
			From:      "+5511999990000",
			To:        "+5511988881111",
			Body:      "Secret bank transfer detail: 12345",
			MediaURL:  "https://pergo.io/receipt.pdf",
			MediaType: "document",
		}
		raw, _ := json.Marshal(payload)

		redacted, err := RedactEvent(ws, "message_sent", raw)
		if err != nil {
			t.Fatalf("redact failed: %v", err)
		}

		var result map[string]any
		json.Unmarshal(redacted, &result)

		// Assert body and media_url are missing
		if _, exists := result["body"]; exists {
			t.Errorf("body field should be redacted/missing")
		}
		if _, exists := result["media_url"]; exists {
			t.Errorf("media_url field should be redacted/missing")
		}

		// Assert metadata is retained
		if val, ok := result["body_length"].(float64); !ok || val != float64(len(payload.Body)) {
			t.Errorf("got body_length %v, want %d", result["body_length"], len(payload.Body))
		}
		if val, ok := result["media_type"].(string); !ok || val != payload.MediaType {
			t.Errorf("got media_type %v, want %s", result["media_type"], payload.MediaType)
		}
		if val, ok := result["from"].(string); !ok || val != payload.From {
			t.Errorf("got from %v, want %s", result["from"], payload.From)
		}
	})
}
