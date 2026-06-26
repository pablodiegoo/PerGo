package domain

import (
	"encoding/json"
	"testing"
)

func TestMessageStatusValues(t *testing.T) {
	tests := []struct {
		name     string
		status   MessageStatus
		expected string
	}{
		{"queued", StatusQueued, "queued"},
		{"sent", StatusSent, "sent"},
		{"delivered", StatusDelivered, "delivered"},
		{"read", StatusRead, "read"},
		{"failed", StatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("status %s = %q, want %q", tt.name, string(tt.status), tt.expected)
			}
		})
	}
}

func TestValidateMessageValid(t *testing.T) {
	req := &CreateMessageRequest{
		To:      "+1234567890",
		Channel: "whatsapp",
		Body:    "Hello",
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error, got %+v", err)
	}
}

func TestValidateMessageMissingTo(t *testing.T) {
	req := &CreateMessageRequest{
		Channel: "whatsapp",
		Body:    "Hello",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for missing to, got nil")
	}
	if err.Code != "invalid_payload" {
		t.Errorf("code = %q, want %q", err.Code, "invalid_payload")
	}
	if len(err.Details) != 1 || err.Details[0].Field != "to" {
		t.Errorf("expected field error for 'to', got %+v", err.Details)
	}
}

func TestValidateMessageMissingChannel(t *testing.T) {
	req := &CreateMessageRequest{
		To:   "+1234567890",
		Body: "Hello",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for missing channel, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "channel" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'channel', got %+v", err.Details)
	}
}

func TestValidateMessageInvalidChannel(t *testing.T) {
	req := &CreateMessageRequest{
		To:      "+1234567890",
		Channel: "sms",
		Body:    "Hello",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for invalid channel, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "channel" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'channel', got %+v", err.Details)
	}
}

func TestValidateMessageZeroTTL(t *testing.T) {
	zero := 0
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		TTLSeconds: &zero,
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for zero TTL, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "ttl_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'ttl_seconds', got %+v", err.Details)
	}
}

func TestValidateMessageNegativeTTL(t *testing.T) {
	neg := -5
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		TTLSeconds: &neg,
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for negative TTL, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "ttl_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'ttl_seconds', got %+v", err.Details)
	}
}

func TestValidateMessageNilTTL(t *testing.T) {
	req := &CreateMessageRequest{
		To:         "+1234567890",
		Channel:    "whatsapp",
		TTLSeconds: nil,
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error for nil TTL, got %+v", err)
	}
}

func TestValidateMessageOptionalBody(t *testing.T) {
	req := &CreateMessageRequest{
		To:      "+1234567890",
		Channel: "whatsapp",
		Body:    "",
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error for empty body, got %+v", err)
	}
}

func TestErrorResponseJSON(t *testing.T) {
	resp := ErrorResponse{
		Code:    "invalid_payload",
		Message: "request body validation failed",
		MoreInfo: "https://docs.omnigo.dev/errors/invalid_payload",
		Details: []FieldError{
			{Field: "to", Message: "is required"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal ErrorResponse: %v", err)
	}

	var decoded ErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ErrorResponse: %v", err)
	}

	if decoded.Code != resp.Code {
		t.Errorf("code = %q, want %q", decoded.Code, resp.Code)
	}
	if decoded.Message != resp.Message {
		t.Errorf("message = %q, want %q", decoded.Message, resp.Message)
	}
	if decoded.MoreInfo != resp.MoreInfo {
		t.Errorf("more_info = %q, want %q", decoded.MoreInfo, resp.MoreInfo)
	}
	if len(decoded.Details) != 1 || decoded.Details[0].Field != "to" {
		t.Errorf("details = %+v, want [{Field:to Message:is required}]", decoded.Details)
	}
}

func TestValidateMessageTemplateValid(t *testing.T) {
	req := &CreateMessageRequest{
		To:           "+1234567890",
		Channel:      "whatsapp_cloud",
		TemplateName: "welcome_template",
		Language:     "en",
	}
	if err := ValidateMessage(req); err != nil {
		t.Errorf("expected nil error for valid template request, got %+v", err)
	}
}

func TestValidateMessageTemplateInvalidChannel(t *testing.T) {
	req := &CreateMessageRequest{
		To:           "+1234567890",
		Channel:      "whatsapp",
		TemplateName: "welcome_template",
		Language:     "en",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for template on non-whatsapp_cloud channel, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "template_name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'template_name', got %+v", err.Details)
	}
}

func TestValidateMessageTemplateMissingLanguage(t *testing.T) {
	req := &CreateMessageRequest{
		To:           "+1234567890",
		Channel:      "whatsapp_cloud",
		TemplateName: "welcome_template",
	}
	err := ValidateMessage(req)
	if err == nil {
		t.Fatal("expected error for template missing language, got nil")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "language" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'language', got %+v", err.Details)
	}
}

