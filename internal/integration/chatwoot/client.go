package chatwoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrTerminal = errors.New("terminal client error")
)

// ChatwootClient wraps Chatwoot REST v1 API endpoints.
type ChatwootClient struct {
	client    *http.Client
	apiURL    string
	token     string
	accountID int64
}

// NewChatwootClient creates a new ChatwootClient instance.
func NewChatwootClient(apiURL string, token string, accountID int64, client *http.Client) *ChatwootClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	apiURL = strings.TrimSuffix(apiURL, "/")
	return &ChatwootClient{
		client:    client,
		apiURL:    apiURL,
		token:     token,
		accountID: accountID,
	}
}

// SearchContact queries GET /api/v1/accounts/{account_id}/contacts/search?q={identifier}
func (c *ChatwootClient) SearchContact(ctx context.Context, identifier string) (int64, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/search?q=%s", c.apiURL, c.accountID, identifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return 0, err
	}

	var res struct {
		Payload []struct {
			ID         int64  `json:"id"`
			Identifier string `json:"identifier"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, err
	}

	for _, item := range res.Payload {
		if item.Identifier == identifier {
			return item.ID, nil
		}
	}

	if len(res.Payload) > 0 {
		return res.Payload[0].ID, nil
	}

	return 0, ErrNotFound
}

// CreateContact queries POST /api/v1/accounts/{account_id}/contacts
func (c *ChatwootClient) CreateContact(ctx context.Context, identifier string, name string, phoneNumber string, customAttributes map[string]interface{}) (int64, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%d/contacts", c.apiURL, c.accountID)

	reqBody := map[string]interface{}{
		"name": name,
	}
	if identifier != "" {
		reqBody["identifier"] = identifier
	}
	if phoneNumber != "" {
		reqBody["phone_number"] = phoneNumber
	}
	if len(customAttributes) > 0 {
		reqBody["custom_attributes"] = customAttributes
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return 0, err
	}

	var res struct {
		Payload struct {
			Contact struct {
				ID int64 `json:"id"`
			} `json:"contact"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, err
	}

	return res.Payload.Contact.ID, nil
}

// UpdateContact queries PUT /api/v1/accounts/{account_id}/contacts/{contact_id}
func (c *ChatwootClient) UpdateContact(ctx context.Context, chatwootContactID int64, name string, phoneNumber string, customAttributes map[string]interface{}) (int64, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/%d", c.apiURL, c.accountID, chatwootContactID)

	reqBody := map[string]interface{}{
		"name": name,
	}
	if phoneNumber != "" {
		reqBody["phone_number"] = phoneNumber
	}
	if len(customAttributes) > 0 {
		reqBody["custom_attributes"] = customAttributes
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return 0, err
	}

	var res struct {
		Payload struct {
			Contact struct {
				ID int64 `json:"id"`
			} `json:"contact"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, err
	}

	return res.Payload.Contact.ID, nil
}

// CreateConversation queries POST /api/v1/accounts/{account_id}/conversations
func (c *ChatwootClient) CreateConversation(ctx context.Context, contactID int64, inboxID int64) (int64, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%d/conversations", c.apiURL, c.accountID)

	reqBody := map[string]interface{}{
		"inbox_id":   inboxID,
		"contact_id": contactID,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return 0, err
	}

	var res struct {
		ID int64 `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, err
	}

	return res.ID, nil
}

// PostMessage queries POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
func (c *ChatwootClient) PostMessage(ctx context.Context, conversationID int64, content string, private bool) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/messages", c.apiURL, c.accountID, conversationID)

	reqBody := map[string]interface{}{
		"content":      content,
		"message_type": "incoming",
		"private":      private,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

func (c *ChatwootClient) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	errMsg := string(bodyBytes)
	if errMsg == "" {
		errMsg = resp.Status
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("%w: status %d, body: %s", ErrTerminal, resp.StatusCode, errMsg)
	}

	return fmt.Errorf("server error: status %d, body: %s", resp.StatusCode, errMsg)
}
