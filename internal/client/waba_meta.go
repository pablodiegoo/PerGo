package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

var (
	// ErrSyncRateLimited is returned when a template sync is requested within the 15-minute rate limit window.
	ErrSyncRateLimited = errors.New("template sync is rate limited: please wait 15 minutes between sync requests")
)

// WABAMetaClient provides operations for interacting with the Meta Graph API for WABA templates.
type WABAMetaClient struct {
	httpClient *http.Client
	baseURL    string

	mu           sync.Mutex
	lastSyncTime map[uuid.UUID]time.Time
}

// NewWABAMetaClient creates a new WABAMetaClient.
func NewWABAMetaClient(httpClient *http.Client, baseURL string) *WABAMetaClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://graph.facebook.com/v18.0"
	}
	return &WABAMetaClient{
		httpClient:   httpClient,
		baseURL:      baseURL,
		lastSyncTime: make(map[uuid.UUID]time.Time),
	}
}

// MetaTemplateItem represents a single template item returned by Meta Graph API.
type MetaTemplateItem struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Language        string            `json:"language"`
	Status          string            `json:"status"`
	Category        string            `json:"category"`
	Components      []json.RawMessage `json:"components"`
	RejectionReason *string           `json:"rejected_reason,omitempty"`
	QualityScore    *string           `json:"quality_score,omitempty"`
}

// CreateTemplate submits a template creation request to Meta Graph API.
func (c *WABAMetaClient) CreateTemplate(ctx context.Context, wabaAccountID, token, name, language, category string, components json.RawMessage) (string, string, error) {
	metaURL := fmt.Sprintf("%s/%s/message_templates", c.baseURL, wabaAccountID)
	payload := map[string]interface{}{
		"name":       name,
		"language":   language,
		"category":   category,
		"components": components,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to serialize template payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metaURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to connect to Meta API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		type metaErrResponse struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		var metaErr metaErrResponse
		if err := json.Unmarshal(respBytes, &metaErr); err == nil && metaErr.Error.Message != "" {
			return "", "", fmt.Errorf("Meta API error: %s (code %d)", metaErr.Error.Message, metaErr.Error.Code)
		}
		return "", "", fmt.Errorf("Meta API returned HTTP status %d: %s", resp.StatusCode, string(respBytes))
	}

	type metaResponse struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	var metaResp metaResponse
	if err := json.Unmarshal(respBytes, &metaResp); err != nil || metaResp.ID == "" {
		return "", "", fmt.Errorf("failed to parse Meta API response: %w", err)
	}

	status := metaResp.Status
	if status == "" {
		status = "PENDING"
	}
	return metaResp.ID, status, nil
}

// DeleteTemplate sends a deletion request to Meta Graph API by template name.
func (c *WABAMetaClient) DeleteTemplate(ctx context.Context, wabaAccountID, token, name string) error {
	metaURL := fmt.Sprintf("%s/%s/message_templates?name=%s", c.baseURL, wabaAccountID, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, metaURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Meta API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Meta API delete returned HTTP status %d: %s", resp.StatusCode, string(respBytes))
	}
	return nil
}

// SyncTemplates fetches all templates for a WABA account from Meta Graph API and updates the local repository, enforcing a 15-minute rate limit per connection.
func (c *WABAMetaClient) SyncTemplates(ctx context.Context, connectionID uuid.UUID, wabaAccountID, token string, workspaceID uuid.UUID, repo *repository.WABATemplateRepository, bypassRateLimit bool) ([]repository.WABATemplate, error) {
	if !bypassRateLimit {
		c.mu.Lock()
		if lastTime, exists := c.lastSyncTime[connectionID]; exists {
			if time.Since(lastTime) < 15*time.Minute {
				c.mu.Unlock()
				return nil, ErrSyncRateLimited
			}
		}
		c.mu.Unlock()
	}

	metaURL := fmt.Sprintf("%s/%s/message_templates?limit=100", c.baseURL, wabaAccountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Meta API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Meta API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from Meta: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		type metaError struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		type metaErrorResponse struct {
			Error metaError `json:"error"`
		}
		var metaErr metaErrorResponse
		if err := json.Unmarshal(respBytes, &metaErr); err == nil && metaErr.Error.Message != "" {
			return nil, fmt.Errorf("Meta API error: %s (code %d)", metaErr.Error.Message, metaErr.Error.Code)
		}
		return nil, fmt.Errorf("Meta API returned HTTP status %d", resp.StatusCode)
	}

	type metaTemplatesResponse struct {
		Data []MetaTemplateItem `json:"data"`
	}

	var metaResp metaTemplatesResponse
	if err := json.Unmarshal(respBytes, &metaResp); err != nil {
		return nil, fmt.Errorf("failed to parse Meta response: %w", err)
	}

	slog.Info("syncing templates from Meta", "count", len(metaResp.Data), "connection_id", connectionID)

	var synced []repository.WABATemplate
	for _, t := range metaResp.Data {
		componentsJSON, err := json.Marshal(t.Components)
		if err != nil {
			slog.Error("failed to marshal components", "error", err, "template", t.Name)
			continue
		}

		dbTmpl := &repository.WABATemplate{
			WorkspaceID:     workspaceID,
			ConnectionID:    connectionID,
			MetaTemplateID:  t.ID,
			Name:            t.Name,
			Language:        t.Language,
			Status:          t.Status,
			Category:        t.Category,
			Components:      componentsJSON,
			RejectionReason: t.RejectionReason,
			QualityScore:    t.QualityScore,
		}

		if repo != nil {
			saved, err := repo.Upsert(ctx, dbTmpl)
			if err != nil {
				slog.Error("failed to upsert template in local DB", "error", err, "template", t.Name)
				return nil, fmt.Errorf("failed to save template %s in local DB: %w", t.Name, err)
			}
			synced = append(synced, *saved)
		} else {
			synced = append(synced, *dbTmpl)
		}
	}

	c.mu.Lock()
	c.lastSyncTime[connectionID] = time.Now()
	c.mu.Unlock()

	return synced, nil
}
