package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

// SESConfig holds configuration for Amazon SES HTTP/API integration.
type SESConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	EndpointURL     string // Optional custom endpoint / mock
	HTTPClient      *http.Client
}

// SESProvider implements Provider for Amazon SES.
type SESProvider struct {
	config SESConfig
}

// NewSESProvider creates a new Amazon SES provider instance.
func NewSESProvider(cfg SESConfig) *SESProvider {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return &SESProvider{config: cfg}
}

type sesSendRequest struct {
	FromContent struct {
		FromEmailAddress string `json:"FromEmailAddress"`
	} `json:"FromContent"`
	Destination struct {
		ToAddresses []string `json:"ToAddresses"`
	} `json:"Destination"`
	Content struct {
		Simple struct {
			Subject struct {
				Data string `json:"Data"`
			} `json:"Subject"`
			Body struct {
				Text *struct {
					Data string `json:"Data"`
				} `json:"Text,omitempty"`
				Html *struct {
					Data string `json:"Data"`
				} `json:"Html,omitempty"`
			} `json:"Body"`
		} `json:"Simple"`
	} `json:"Content"`
}

// Send dispatches an email via Amazon SES HTTP v2 format.
func (s *SESProvider) Send(ctx context.Context, msg *EmailMessage) (string, error) {
	providerMsgID := msg.ID
	if providerMsgID == "" {
		providerMsgID = uuid.New().String()
	}

	from := msg.From
	if from == "" {
		return "", fmt.Errorf("from address is required for SES")
	}

	reqPayload := sesSendRequest{}
	reqPayload.FromContent.FromEmailAddress = from
	if msg.FromName != "" {
		reqPayload.FromContent.FromEmailAddress = fmt.Sprintf("%s <%s>", msg.FromName, from)
	}
	reqPayload.Destination.ToAddresses = msg.To
	reqPayload.Content.Simple.Subject.Data = msg.Subject

	if msg.TextBody != "" {
		reqPayload.Content.Simple.Body.Text = &struct {
			Data string `json:"Data"`
		}{Data: msg.TextBody}
	}
	if msg.HTMLBody != "" {
		reqPayload.Content.Simple.Body.Html = &struct {
			Data string `json:"Data"`
		}{Data: msg.HTMLBody}
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal SES request: %w", err)
	}

	endpoint := s.config.EndpointURL
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://email.%s.amazonaws.com/v2/email/outbound-emails", s.config.Region)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create SES request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.config.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("SES HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("SES returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return providerMsgID, nil
}
