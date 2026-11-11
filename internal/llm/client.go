// Package llm provides a lightweight, zero-SDK multi-provider client for audio transcription (STT)
// and structured JSON completions across Google Gemini and OpenAI.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/pablojhp.pergo/internal/config"
)

const (
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com"
	defaultOpenAIBaseURL = "https://api.openai.com"
	defaultGeminiModel   = "gemini-2.5-flash"
	defaultOpenAIModel   = "gpt-4o-mini"
	defaultWhisperModel  = "whisper-1"

	maxAudioBytes    = 25 * 1024 * 1024
	maxResponseBytes = 2 * 1024 * 1024
)

// ErrProviderNotConfigured is returned when no LLM/STT API key has been configured.
var ErrProviderNotConfigured = errors.New("llm provider is not configured")

// ErrEmptyAudioPayload is returned when an empty audio slice is passed to TranscribeAudio.
var ErrEmptyAudioPayload = errors.New("audio payload cannot be empty")

// ErrAudioPayloadTooLarge is returned when audio exceeds the 25MB provider limit.
var ErrAudioPayloadTooLarge = errors.New("audio payload exceeds 25MB limit")

// ChatSummaryLLM represents a structured executive summary of a customer conversation.
type ChatSummaryLLM struct {
	Summary        string `json:"summary"`
	CustomerIssues string `json:"customer_issues"`
	PromisesMade   string `json:"promises_made"`
	NextSteps      string `json:"next_steps"`
}

// Config configures the multi-provider LLM and STT client.
type Config struct {
	Provider      string
	GeminiAPIKey  string
	OpenAIAPIKey  string
	GeminiBaseURL string
	OpenAIBaseURL string
	GeminiModel   string
	OpenAIModel   string
	WhisperModel  string
	HTTPClient    *http.Client
}

// Client executes audio transcription and structured chat summarization requests.
type Client struct {
	provider      string
	geminiAPIKey  string
	openAIAPIKey  string
	geminiBaseURL string
	openAIBaseURL string
	geminiModel   string
	openAIModel   string
	whisperModel  string
	httpClient    *http.Client
}

// NewClient constructs a new multi-provider Client.
func NewClient(cfg Config) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 45 * time.Second}
	}
	gemBase := strings.TrimRight(strings.TrimSpace(cfg.GeminiBaseURL), "/")
	if gemBase == "" {
		gemBase = defaultGeminiBaseURL
	}
	oaiBase := strings.TrimRight(strings.TrimSpace(cfg.OpenAIBaseURL), "/")
	if oaiBase == "" {
		oaiBase = defaultOpenAIBaseURL
	}
	gemModel := strings.TrimSpace(cfg.GeminiModel)
	if gemModel == "" {
		gemModel = defaultGeminiModel
	}
	oaiModel := strings.TrimSpace(cfg.OpenAIModel)
	if oaiModel == "" {
		oaiModel = defaultOpenAIModel
	}
	whisperModel := strings.TrimSpace(cfg.WhisperModel)
	if whisperModel == "" {
		whisperModel = defaultWhisperModel
	}

	return &Client{
		provider:      strings.ToLower(strings.TrimSpace(cfg.Provider)),
		geminiAPIKey:  strings.TrimSpace(cfg.GeminiAPIKey),
		openAIAPIKey:  strings.TrimSpace(cfg.OpenAIAPIKey),
		geminiBaseURL: gemBase,
		openAIBaseURL: oaiBase,
		geminiModel:   gemModel,
		openAIModel:   oaiModel,
		whisperModel:  whisperModel,
		httpClient:    hc,
	}
}

// NewClientFromConfig constructs a Client from PerGo's root Config.
func NewClientFromConfig(cfg *config.Config) *Client {
	if cfg == nil {
		return NewClient(Config{})
	}
	return NewClient(Config{
		Provider:     cfg.LLMProvider,
		GeminiAPIKey: cfg.GeminiAPIKey,
		OpenAIAPIKey: cfg.OpenAIAPIKey,
	})
}

// Configured reports whether at least one provider API key is available.
func (c *Client) Configured() bool {
	if c == nil {
		return false
	}
	return c.geminiAPIKey != "" || c.openAIAPIKey != ""
}

func (c *Client) providerOrder() []string {
	if c == nil {
		return nil
	}
	var order []string
	switch c.provider {
	case "openai":
		if c.openAIAPIKey != "" {
			order = append(order, "openai")
		}
		if c.geminiAPIKey != "" {
			order = append(order, "gemini")
		}
	default:
		if c.geminiAPIKey != "" {
			order = append(order, "gemini")
		}
		if c.openAIAPIKey != "" {
			order = append(order, "openai")
		}
	}
	return order
}

// TranscribeAudio transcribes audio bytes (such as WhatsApp/Telegram audio/ogg Opus voice notes,
// MP3, WAV, or MP4) using Gemini multimodal Flash or OpenAI Whisper.
func (c *Client) TranscribeAudio(ctx context.Context, data []byte, mimeType string) (string, string, error) {
	if len(data) == 0 {
		return "", "", ErrEmptyAudioPayload
	}
	if len(data) > maxAudioBytes {
		return "", "", ErrAudioPayloadTooLarge
	}
	providers := c.providerOrder()
	if len(providers) == 0 {
		return "", "", ErrProviderNotConfigured
	}

	normalizedMime := normalizeAudioMime(data, mimeType)

	var lastErr error
	for _, p := range providers {
		switch p {
		case "gemini":
			text, err := c.transcribeGemini(ctx, data, normalizedMime)
			if err == nil {
				return text, "gemini", nil
			}
			lastErr = err
		case "openai":
			text, err := c.transcribeOpenAI(ctx, data, normalizedMime)
			if err == nil {
				return text, "openai", nil
			}
			lastErr = err
		}
	}
	return "", "", lastErr
}

// CompleteJSON sends a system and user prompt and returns a raw JSON object string from the active provider.
func (c *Client) CompleteJSON(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	providers := c.providerOrder()
	if len(providers) == 0 {
		return "", ErrProviderNotConfigured
	}

	var lastErr error
	for _, p := range providers {
		switch p {
		case "gemini":
			raw, err := c.completeJSONGemini(ctx, systemPrompt, userPrompt)
			if err == nil {
				return cleanJSONFence(raw), nil
			}
			lastErr = err
		case "openai":
			raw, err := c.completeJSONOpenAI(ctx, systemPrompt, userPrompt)
			if err == nil {
				return cleanJSONFence(raw), nil
			}
			lastErr = err
		}
	}
	return "", lastErr
}

// SummarizeChat produces a structured executive summary for a formatted conversation transcript.
func (c *Client) SummarizeChat(ctx context.Context, formattedTranscript string) (*ChatSummaryLLM, error) {
	trimmed := strings.TrimSpace(formattedTranscript)
	if trimmed == "" {
		return nil, errors.New("conversation transcript cannot be empty")
	}

	const systemPrompt = `You are an operational CRM copilot summarizing an omnichannel customer conversation (including transcribed voice notes).
Write concise, factual fields in the SAME language used in the conversation.
Respond strictly with a JSON object matching this schema:
{
  "summary": "1-2 sentence executive overview of the conversation",
  "customer_issues": "Main questions, pain points, or requests raised by the contact",
  "promises_made": "Commitments, deadlines, or proposals promised by the operator/team (or 'None' / 'Nenhuma' if none)",
  "next_steps": "Concrete recommended next action and current status"
}`

	rawJSON, err := c.CompleteJSON(ctx, systemPrompt, trimmed)
	if err != nil {
		return nil, err
	}

	var out ChatSummaryLLM
	if err := json.Unmarshal([]byte(rawJSON), &out); err != nil {
		return nil, fmt.Errorf("decode llm chat summary json: %w", err)
	}
	return &out, nil
}

func (c *Client) transcribeGemini(ctx context.Context, data []byte, mimeType string) (string, error) {
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.geminiBaseURL, c.geminiModel)

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{
						"inline_data": map[string]string{
							"mime_type": mimeType,
							"data":      base64.StdEncoding.EncodeToString(data),
						},
					},
					{
						"text": "Transcribe this audio recording verbatim in its original spoken language. Return only the spoken words without timestamps, markdown formatting, or extra commentary.",
					},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature": 0.1,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal gemini transcription request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create gemini transcription request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.geminiAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini transcription http error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read gemini transcription response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini transcription returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	return extractGeminiText(respBytes)
}

func (c *Client) completeJSONGemini(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.geminiBaseURL, c.geminiModel)

	payload := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]string{
				{"text": systemPrompt},
			},
		},
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": userPrompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.2,
			"responseMimeType": "application/json",
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal gemini completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create gemini completion request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.geminiAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini completion http error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read gemini completion response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini completion returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	return extractGeminiText(respBytes)
}

func extractGeminiText(respBytes []byte) (string, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode gemini response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini response contained no candidate text parts")
	}
	var sb strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", errors.New("gemini returned empty text")
	}
	return text, nil
}

func (c *Client) transcribeOpenAI(ctx context.Context, data []byte, mimeType string) (string, error) {
	endpoint := c.openAIBaseURL + "/v1/audio/transcriptions"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", c.whisperModel); err != nil {
		return "", fmt.Errorf("write whisper model field: %w", err)
	}

	filename := "audio." + audioExtForMime(mimeType)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create whisper form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write whisper audio bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("close whisper multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", fmt.Errorf("create openai transcription request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.openAIAPIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai transcription http error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read openai transcription response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai transcription returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode openai transcription response: %w", err)
	}
	text := strings.TrimSpace(parsed.Text)
	if text == "" {
		return "", errors.New("openai whisper returned empty transcript")
	}
	return text, nil
}

func (c *Client) completeJSONOpenAI(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	endpoint := c.openAIBaseURL + "/v1/chat/completions"

	payload := map[string]any{
		"model": c.openAIModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal openai completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create openai completion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.openAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai completion http error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read openai completion response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai completion returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode openai completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("openai completion returned no choices")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("openai completion returned empty content")
	}
	return text, nil
}

func normalizeAudioMime(data []byte, raw string) string {
	mime := strings.ToLower(strings.TrimSpace(raw))
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = strings.TrimSpace(mime[:idx])
	}

	switch mime {
	case "audio/ogg", "application/ogg", "audio/opus", "voice", "audio":
		return "audio/ogg"
	case "audio/mpeg", "audio/mp3":
		return "audio/mpeg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio/wav"
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac":
		return "audio/mp4"
	case "audio/webm":
		return "audio/webm"
	}

	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "audio/ogg"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "audio/wav"
	}
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return "audio/mpeg"
	}
	if len(data) >= 8 && string(data[4:8]) == "ftyp" {
		return "audio/mp4"
	}
	if mime != "" && strings.HasPrefix(mime, "audio/") {
		return mime
	}
	return "audio/ogg"
}

func audioExtForMime(mime string) string {
	switch mime {
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/mp4":
		return "m4a"
	case "audio/webm":
		return "webm"
	default:
		return "ogg"
	}
}

func cleanJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.Index(s, "\n"); nl != -1 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}
