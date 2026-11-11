package llm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pablojhp.pergo/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GeminiTranscribeAndSummarize(t *testing.T) {
	audioBytes := []byte("OggS-whatsapp-opus-voice-note")

	var seenAPIKey string
	var seenInlineMime string
	var seenInlineB64 string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAPIKey = r.Header.Get("x-goog-api-key")
		require.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", r.URL.Path)

		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodyStr := string(bodyBytes)

		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(bodyStr, "inline_data") {
			var req struct {
				Contents []struct {
					Parts []struct {
						InlineData *struct {
							MimeType string `json:"mime_type"`
							Data     string `json:"data"`
						} `json:"inline_data"`
					} `json:"parts"`
				} `json:"contents"`
			}
			require.NoError(t, json.Unmarshal(bodyBytes, &req))
			seenInlineMime = req.Contents[0].Parts[0].InlineData.MimeType
			seenInlineB64 = req.Contents[0].Parts[0].InlineData.Data

			_, _ = w.Write([]byte(`{
				"candidates": [{
					"content": {
						"parts": [{"text": "Preciso renovar meu contrato anual até sexta-feira."}]
					}
				}]
			}`))
			return
		}

		// SummarizeChat JSON completion
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [{"text": "{\"summary\":\"Cliente solicitou renovação anual.\",\"customer_issues\":\"Renovar contrato até sexta-feira.\",\"promises_made\":\"Enviar proposta atualizada hoje.\",\"next_steps\":\"Aguardar assinatura do termo.\"}"}]
				}
			}]
		}`))
	}))
	defer ts.Close()

	c := llm.NewClient(llm.Config{
		GeminiAPIKey:  "gem-secret-123",
		GeminiBaseURL: ts.URL,
		HTTPClient:    ts.Client(),
	})
	require.True(t, c.Configured())

	transcript, provider, err := c.TranscribeAudio(context.Background(), audioBytes, "audio/ogg; codecs=opus")
	require.NoError(t, err)
	assert.Equal(t, "gemini", provider)
	assert.Equal(t, "Preciso renovar meu contrato anual até sexta-feira.", transcript)
	assert.Equal(t, "gem-secret-123", seenAPIKey)
	assert.Equal(t, "audio/ogg", seenInlineMime)
	assert.Equal(t, base64.StdEncoding.EncodeToString(audioBytes), seenInlineB64)

	summary, err := c.SummarizeChat(context.Background(), "[Inbound Voice]: Preciso renovar meu contrato anual até sexta-feira.\n[Agent]: Vou enviar a proposta atualizada hoje.")
	require.NoError(t, err)
	assert.Equal(t, "Cliente solicitou renovação anual.", summary.Summary)
	assert.Equal(t, "Renovar contrato até sexta-feira.", summary.CustomerIssues)
	assert.Equal(t, "Enviar proposta atualizada hoje.", summary.PromisesMade)
	assert.Equal(t, "Aguardar assinatura do termo.", summary.NextSteps)
}

func TestClient_OpenAIWhisperAndChatCompletions(t *testing.T) {
	audioBytes := []byte("ID3-mp3-sample-audio")

	var seenAuth string
	var seenModel string
	var seenFilename string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			require.NoError(t, r.ParseMultipartForm(4<<20))
			seenModel = r.FormValue("model")
			f, hdr, err := r.FormFile("file")
			require.NoError(t, err)
			defer f.Close()
			seenFilename = hdr.Filename
			got, _ := io.ReadAll(f)
			assert.Equal(t, audioBytes, got)

			_, _ = w.Write([]byte(`{"text":"Can we schedule onboarding for Monday?"}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{
				"choices": [{
					"message": {
						"content": "{\"summary\":\"Customer requested Monday onboarding.\",\"customer_issues\":\"Needs onboarding slot on Monday.\",\"promises_made\":\"None\",\"next_steps\":\"Send calendar invite for Monday.\"}"
					}
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := llm.NewClient(llm.Config{
		Provider:      "openai",
		OpenAIAPIKey:  "sk-test-openai",
		OpenAIBaseURL: ts.URL,
		HTTPClient:    ts.Client(),
	})

	transcript, provider, err := c.TranscribeAudio(context.Background(), audioBytes, "audio/mpeg")
	require.NoError(t, err)
	assert.Equal(t, "openai", provider)
	assert.Equal(t, "Can we schedule onboarding for Monday?", transcript)
	assert.Equal(t, "Bearer sk-test-openai", seenAuth)
	assert.Equal(t, "whisper-1", seenModel)
	assert.Equal(t, "audio.mp3", seenFilename)

	summary, err := c.SummarizeChat(context.Background(), "[Inbound]: Can we schedule onboarding for Monday?")
	require.NoError(t, err)
	assert.Equal(t, "Customer requested Monday onboarding.", summary.Summary)
	assert.Equal(t, "Send calendar invite for Monday.", summary.NextSteps)
}

func TestClient_FailsFastWhenUnconfigured(t *testing.T) {
	c := llm.NewClient(llm.Config{})
	assert.False(t, c.Configured())

	_, _, err := c.TranscribeAudio(context.Background(), []byte("OggS-audio"), "audio/ogg")
	require.ErrorIs(t, err, llm.ErrProviderNotConfigured)

	_, err = c.SummarizeChat(context.Background(), "Customer: Hello")
	require.ErrorIs(t, err, llm.ErrProviderNotConfigured)
}
