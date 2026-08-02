package handler_test

import (
	"bytes"
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/*.json
var testdataFS embed.FS

func loadFixture(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + filename)
	require.NoError(t, err, "failed to load fixture %s", filename)
	return data
}

func assertJSONMatch(t *testing.T, fixtureFilename string, actualJSON []byte) {
	t.Helper()
	fixtureBytes := loadFixture(t, fixtureFilename)
	assert.JSONEq(t, string(fixtureBytes), string(actualJSON), "JSON payload must match fixture %s", fixtureFilename)
}

func TestPerGoProviderContract_CreateMessageRequest_Deserialization(t *testing.T) {
	fixtureBytes := loadFixture(t, "send_message_request.json")

	// 1. Verify JSON payload binding via Echo HTTP request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(fixtureBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var payload domain.CreateMessageRequest
	err := c.Bind(&payload)
	require.NoError(t, err, "Echo context Bind must succeed on valid consumer request payload")

	// 2. Assert all fields expected by PerGo handler are correctly populated
	assert.Equal(t, "+5511999999999", payload.To)
	assert.Equal(t, "+5511888888888", payload.From)
	assert.Equal(t, "whatsapp_cloud", payload.Channel)
	assert.Equal(t, "Olá! Escolha uma opção abaixo:", payload.Body)
	require.NotNil(t, payload.Media)
	assert.Equal(t, "https://example.com/image.png", payload.Media.MediaURL)
	assert.Equal(t, "interview_2026", payload.Metadata["campaign"])
	require.NotNil(t, payload.TTLSeconds)
	assert.Equal(t, 3600, *payload.TTLSeconds)
	assert.Equal(t, "welcome_template", payload.TemplateName)
	assert.Equal(t, "pt_BR", payload.Language)
	require.Len(t, payload.Components, 1)
	assert.Equal(t, "body", payload.Components[0].Type)
}

func TestPerGoProviderContract_CreateMessageResponse_Serialization(t *testing.T) {
	msgID := uuid.MustParse("98765432-1111-2222-3333-987654321000")
	queuedAt, _ := time.Parse(time.RFC3339, "2026-08-02T12:00:00Z")

	resp := domain.CreateMessageResponse{
		MessageID: msgID,
		Status:    domain.StatusQueued,
		QueuedAt:  queuedAt,
	}

	actualJSON, err := json.MarshalIndent(resp, "", "  ")
	require.NoError(t, err)

	assertJSONMatch(t, "send_message_response_202.json", actualJSON)
}

func TestPerGoProviderContract_ErrorResponse_Serialization(t *testing.T) {
	errResp := domain.ErrorResponse{
		Code:     "invalid_payload",
		Message:  "request body validation failed",
		MoreInfo: "https://docs.pergo.dev/errors/invalid_payload",
		Details: []domain.FieldError{
			{
				Field:   "to",
				Message: "is required and must be in E.164 format",
			},
		},
	}

	actualJSON, err := json.MarshalIndent(errResp, "", "  ")
	require.NoError(t, err)

	assertJSONMatch(t, "error_response.json", actualJSON)
}

func TestPerGoProviderContract_InboundEventPayload_Serialization(t *testing.T) {
	payload := inbound.InboundEventPayload{
		Event:       "message.inbound",
		TraceID:     "trc_123456789",
		MessageID:   "msg_inbound_001",
		Channel:     "whatsapp_cloud",
		Timestamp:   "1770033600",
		WorkspaceID: "ws_ecoar_ai",
		From:        "+5511999999999",
		To:          "+5511888888888",
		Body:        "Olá! Gostaria de participar da pesquisa.",
		Media: &inbound.EventMedia{
			MediaURL:  "https://example.com/audio.mp3",
			MediaType: "audio/mp3",
			Filename:  "audio.mp3",
			Caption:   "Áudio de resposta",
		},
	}

	actualJSON, err := json.MarshalIndent(payload, "", "  ")
	require.NoError(t, err)

	assertJSONMatch(t, "inbound_event_message.json", actualJSON)
}
