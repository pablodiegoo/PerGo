package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestDeterministicDevWorkspaceID_Constant(t *testing.T) {
	parsed, err := uuid.Parse(DefaultDevWorkspaceID)
	if err != nil {
		t.Fatalf("DefaultDevWorkspaceID %q is not a valid UUID: %v", DefaultDevWorkspaceID, err)
	}
	expected := "a0000000-0000-0000-0000-000000000001"
	if parsed.String() != expected {
		t.Errorf("expected DefaultDevWorkspaceID to be %s, got %s", expected, parsed.String())
	}
}

func TestRandToken(t *testing.T) {
	tok1 := randToken(16)
	tok2 := randToken(16)
	if len(tok1) != 16 || len(tok2) != 16 {
		t.Fatalf("expected token length 16, got %d and %d", len(tok1), len(tok2))
	}
	if tok1 == tok2 {
		t.Errorf("expected generated tokens to be random, but they were equal: %s", tok1)
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_PERGO_VAR", "custom_val")
	if got := envOrDefault("TEST_PERGO_VAR", "fallback"); got != "custom_val" {
		t.Errorf("expected 'custom_val', got %q", got)
	}
	if got := envOrDefault("NON_EXISTENT_VAR", "fallback"); got != "fallback" {
		t.Errorf("expected 'fallback', got %q", got)
	}
}

func TestWABAConfigJSONMarshaling(t *testing.T) {
	cfg := WABAConfig{
		PhoneNumberID: "123456",
		Token:         "tok-secret",
		WABAAccountID: "waba-789",
		VerifyToken:   "pergo-verify-token",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal WABAConfig: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if parsed["phone_number_id"] != "123456" || parsed["verify_token"] != "pergo-verify-token" {
		t.Errorf("unexpected json payload: %s", string(data))
	}
}

func TestInboundPayloadJSONMarshaling(t *testing.T) {
	payload := inboundPayload{
		Event:       "inbound_message",
		TraceID:     "trace-1",
		MessageID:   "wamid.1234",
		Channel:     "whatsapp_cloud",
		Timestamp:   "2026-08-17T12:00:00Z",
		WorkspaceID: DefaultDevWorkspaceID,
		From:        "15551234567",
		To:          "15559876543",
		Body:        "Hello PerGo",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal inboundPayload: %v", err)
	}

	var parsed inboundPayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal inboundPayload: %v", err)
	}
	if parsed.WorkspaceID != DefaultDevWorkspaceID || parsed.Body != "Hello PerGo" {
		t.Errorf("unexpected unmarshaled payload: %+v", parsed)
	}
}
