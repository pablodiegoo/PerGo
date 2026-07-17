package typebot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// BotConfig represents a single bot configuration inside the integrations table
type BotConfig struct {
	ConnectionID   string   `json:"connection_id"`
	BotID          string   `json:"bot_id"`
	PublicToken    string   `json:"public_token"`
	TriggerWords   []string `json:"trigger_words"`
	IsDefault      bool     `json:"is_default"`
	SessionTimeout int      `json:"session_timeout_minutes"` // e.g. 30
}

// Config represents the stored encrypted config for Typebot
type Config struct {
	APIURL string      `json:"api_url"`
	Bots   []BotConfig `json:"bots"`
}

type Message struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // "text", "image", "audio", "video", "custom"
	Content struct {
		Type     string        `json:"type"` // "richText", "url", etc
		RichText []interface{} `json:"richText,omitempty"`
		URL      string        `json:"url,omitempty"`
	} `json:"content"`
}

type StartChatRequest struct {
	PrefilledVariables map[string]string `json:"prefilledVariables,omitempty"`
}

type ContinueChatRequest struct {
	Message ContinueChatMessage `json:"message"`
}

type ContinueChatMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type StartChatResponse struct {
	SessionID string    `json:"sessionId"`
	Messages  []Message `json:"messages"`
}

type ContinueChatResponse struct {
	Messages []Message `json:"messages"`
}

type Client struct {
	client *http.Client
}

func NewClient() *Client {
	return &Client{client: &http.Client{}}
}

func (c *Client) StartChat(ctx context.Context, apiURL, botID, publicToken string, prefilledVariables map[string]string) (string, []Message, error) {
	url := fmt.Sprintf("%s/api/v1/typebots/%s/startChat", apiURL, botID)
	
	reqBody := StartChatRequest{
		PrefilledVariables: prefilledVariables,
	}
	b, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if publicToken != "" {
		req.Header.Set("Authorization", "Bearer "+publicToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("typebot API error (startChat): status %d body %s", resp.StatusCode, string(body))
	}

	var res StartChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", nil, err
	}

	return res.SessionID, res.Messages, nil
}

func (c *Client) ContinueChat(ctx context.Context, apiURL, sessionID, publicToken string, messageText string) ([]Message, error) {
	url := fmt.Sprintf("%s/api/v1/sessions/%s/continueChat", apiURL, sessionID)
	
	reqBody := ContinueChatRequest{
		Message: ContinueChatMessage{
			Type: "text",
			Text: messageText,
		},
	}
	b, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if publicToken != "" {
		req.Header.Set("Authorization", "Bearer "+publicToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session expired: status 404")
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("typebot API error (continueChat): status %d body %s", resp.StatusCode, string(body))
	}

	var res ContinueChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return res.Messages, nil
}
