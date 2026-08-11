package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestRedactingHandler_FlatRedaction(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(jsonHandler))

	logger.Info("user payload",
		"phone", "+15551234567",
		"email", "user@example.com",
		"sender", "5511999999999",
		"recipient", "5511888888888",
		"contact", "John Doe",
		"body", "Hello Secret World",
		"text", "Some text",
		"content", "Sensitive content",
		"message_body", "Raw message body",
		"payload", "Secret payload",
		"user_id", "usr_12345",
		"status", "delivered",
	)

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	sensitiveKeys := []string{
		"phone", "email", "sender", "recipient", "contact",
		"body", "text", "content", "message_body", "payload",
	}

	for _, k := range sensitiveKeys {
		val, ok := output[k]
		if !ok {
			t.Errorf("expected key %q in log output", k)
			continue
		}
		if val != RedactionPlaceholder {
			t.Errorf("expected key %q to be %q, got %v", k, RedactionPlaceholder, val)
		}
	}

	if output["user_id"] != "usr_12345" {
		t.Errorf("expected user_id 'usr_12345', got %v", output["user_id"])
	}
	if output["status"] != "delivered" {
		t.Errorf("expected status 'delivered', got %v", output["status"])
	}
}

func TestRedactingHandler_CaseInsensitivity(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(jsonHandler))

	logger.Info("case test",
		"Phone", "+15550000",
		"PHONE_NUMBER", "+15551111",
		"RECIPIENT", "alice",
		"EmAiL", "test@test.com",
		"NormalKey", "normal_value",
	)

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	for _, k := range []string{"Phone", "PHONE_NUMBER", "RECIPIENT", "EmAiL"} {
		if val, ok := output[k]; !ok || val != RedactionPlaceholder {
			t.Errorf("expected %q to be redacted, got %v", k, val)
		}
	}

	if output["NormalKey"] != "normal_value" {
		t.Errorf("expected NormalKey to be 'normal_value', got %v", output["NormalKey"])
	}
}

func TestRedactingHandler_GroupRecursion(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(jsonHandler))

	logger.Info("group test",
		slog.Group("contact_info",
			slog.String("email", "nested@example.com"),
			slog.String("name", "Bob"),
			slog.Group("deep_nest",
				slog.String("phone", "+19998887777"),
				slog.String("city", "San Francisco"),
			),
		),
	)

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	contactInfo, ok := output["contact_info"].(map[string]any)
	if !ok {
		t.Fatalf("expected contact_info group in log output")
	}

	if contactInfo["email"] != RedactionPlaceholder {
		t.Errorf("expected contact_info.email to be redacted, got %v", contactInfo["email"])
	}
	if contactInfo["name"] != "Bob" {
		t.Errorf("expected contact_info.name to be 'Bob', got %v", contactInfo["name"])
	}

	deepNest, ok := contactInfo["deep_nest"].(map[string]any)
	if !ok {
		t.Fatalf("expected deep_nest subgroup in log output")
	}

	if deepNest["phone"] != RedactionPlaceholder {
		t.Errorf("expected deep_nest.phone to be redacted, got %v", deepNest["phone"])
	}
	if deepNest["city"] != "San Francisco" {
		t.Errorf("expected deep_nest.city to be 'San Francisco', got %v", deepNest["city"])
	}
}

func TestRedactingHandler_WithAttrsAndWithGroup(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(jsonHandler)).
		With("phone", "+12345", "app", "pergo").
		WithGroup("request_details")

	logger.Info("with attrs & group", "body", "secret body", "ip", "127.0.0.1")

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	if output["phone"] != RedactionPlaceholder {
		t.Errorf("expected top-level attached phone to be redacted, got %v", output["phone"])
	}
	if output["app"] != "pergo" {
		t.Errorf("expected top-level app to be 'pergo', got %v", output["app"])
	}

	reqDetails, ok := output["request_details"].(map[string]any)
	if !ok {
		t.Fatalf("expected request_details group in output")
	}

	if reqDetails["body"] != RedactionPlaceholder {
		t.Errorf("expected request_details.body to be redacted, got %v", reqDetails["body"])
	}
	if reqDetails["ip"] != "127.0.0.1" {
		t.Errorf("expected request_details.ip to be '127.0.0.1', got %v", reqDetails["ip"])
	}
}

func TestRedactingHandler_CustomKeysOptions(t *testing.T) {
	t.Run("WithSensitiveKeys overrides default keys", func(t *testing.T) {
		var buf bytes.Buffer
		jsonHandler := slog.NewJSONHandler(&buf, nil)
		handler := NewRedactingHandler(jsonHandler, WithSensitiveKeys("ssn", "credit_card"))
		logger := slog.New(handler)

		logger.Info("custom keys override",
			"ssn", "000-00-0000",
			"phone", "+123456",
			"credit_card", "4111111111111111",
		)

		var output map[string]any
		if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
			t.Fatalf("failed to unmarshal log JSON: %v", err)
		}

		if output["ssn"] != RedactionPlaceholder {
			t.Errorf("expected ssn to be redacted, got %v", output["ssn"])
		}
		if output["credit_card"] != RedactionPlaceholder {
			t.Errorf("expected credit_card to be redacted, got %v", output["credit_card"])
		}
		if output["phone"] != "+123456" {
			t.Errorf("expected phone to be unredacted when overridden, got %v", output["phone"])
		}
	})

	t.Run("WithExtraSensitiveKeys extends default keys", func(t *testing.T) {
		var buf bytes.Buffer
		jsonHandler := slog.NewJSONHandler(&buf, nil)
		handler := NewRedactingHandler(jsonHandler, WithExtraSensitiveKeys("ssn", "secret_token"))
		logger := slog.New(handler)

		logger.Info("custom extra keys",
			"phone", "+123456",
			"ssn", "000-00-0000",
			"secret_token", "tok_xyz",
			"normal", "value",
		)

		var output map[string]any
		if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
			t.Fatalf("failed to unmarshal log JSON: %v", err)
		}

		if output["phone"] != RedactionPlaceholder {
			t.Errorf("expected phone to be redacted, got %v", output["phone"])
		}
		if output["ssn"] != RedactionPlaceholder {
			t.Errorf("expected ssn to be redacted, got %v", output["ssn"])
		}
		if output["secret_token"] != RedactionPlaceholder {
			t.Errorf("expected secret_token to be redacted, got %v", output["secret_token"])
		}
		if output["normal"] != "value" {
			t.Errorf("expected normal to be 'value', got %v", output["normal"])
		}
	})
}

func TestNewLoggerWithWriter_DefaultRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter("trace-123", &buf)

	logger.Info("incoming request", "email", "secret@domain.com", "path", "/api/v1/messages")

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	if output["trace_id"] != "trace-123" {
		t.Errorf("expected trace_id 'trace-123', got %v", output["trace_id"])
	}
	if output["email"] != RedactionPlaceholder {
		t.Errorf("expected email to be redacted by NewLoggerWithWriter, got %v", output["email"])
	}
	if output["path"] != "/api/v1/messages" {
		t.Errorf("expected path to be '/api/v1/messages', got %v", output["path"])
	}
}

func TestRedactingHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewRedactingHandler(jsonHandler)

	ctx := context.Background()

	if handler.Enabled(ctx, slog.LevelInfo) {
		t.Errorf("expected Enabled(LevelInfo) to return false when inner handler minimum is Warn")
	}
	if !handler.Enabled(ctx, slog.LevelWarn) {
		t.Errorf("expected Enabled(LevelWarn) to return true")
	}
}
