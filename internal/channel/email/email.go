package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pablojhp.pergo/internal/channel"
)

// Attachment represents a file attachment for an outbound email.
type Attachment struct {
	Filename    string
	ContentType string
	Content     io.Reader
}

// EmailMessage is the internal domain representation of an outbound email.
type EmailMessage struct {
	ID          string
	From        string
	FromName    string
	To          []string
	ReplyTo     string
	Subject     string
	TextBody    string
	HTMLBody    string
	Attachments []Attachment
	Headers     map[string]string
	TrackOpens  bool
	TrackClicks bool
}

// Provider defines the internal seam for email dispatch adapters (SMTP, SES, Mautic).
type Provider interface {
	Send(ctx context.Context, msg *EmailMessage) (string, error)
}

// EmailAdapter implements channel.Dispatcher for email channels.
type EmailAdapter struct {
	provider Provider
}

// NewEmailAdapter creates a new EmailAdapter wrapping a concrete email Provider.
func NewEmailAdapter(p Provider) *EmailAdapter {
	return &EmailAdapter{provider: p}
}

// Dispatch satisfies the channel.Dispatcher interface.
func (a *EmailAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
	if a.provider == nil {
		return "", channel.NewTerminalError(errors.New("no email provider configured"))
	}
	if m.To == "" {
		return "", channel.NewTerminalError(errors.New("email recipient (To) is empty"))
	}

	emailMsg := &EmailMessage{
		ID:       m.MessageID,
		To:       []string{m.To},
		TextBody: m.Body,
		Subject:  "Message from PerGo",
		Headers:  make(map[string]string),
	}

	// Extract subject and options from Metadata if present
	if m.Metadata != nil {
		if subj, ok := m.Metadata["subject"]; ok && subj != "" {
			emailMsg.Subject = subj
		}
		if from, ok := m.Metadata["from"]; ok && from != "" {
			emailMsg.From = from
		}
		if fromName, ok := m.Metadata["from_name"]; ok && fromName != "" {
			emailMsg.FromName = fromName
		}
		if replyTo, ok := m.Metadata["reply_to"]; ok && replyTo != "" {
			emailMsg.ReplyTo = replyTo
		}
		if trackOpens, ok := m.Metadata["track_opens"]; ok && (trackOpens == "true" || trackOpens == "1") {
			emailMsg.TrackOpens = true
		}
		if trackClicks, ok := m.Metadata["track_clicks"]; ok && (trackClicks == "true" || trackClicks == "1") {
			emailMsg.TrackClicks = true
		}
	}

	// Parse ChannelOverrides for email specific overrides
	if m.ChannelOverrides != nil {
		if raw, ok := m.ChannelOverrides["email"]; ok && len(raw) > 0 {
			var emailOverride struct {
				Subject  string `json:"subject"`
				HTMLBody string `json:"html_body"`
				TextBody string `json:"text_body"`
				From     string `json:"from"`
				FromName string `json:"from_name"`
				ReplyTo  string `json:"reply_to"`
			}
			if err := json.Unmarshal(raw, &emailOverride); err == nil {
				if emailOverride.Subject != "" {
					emailMsg.Subject = emailOverride.Subject
				}
				if emailOverride.HTMLBody != "" {
					emailMsg.HTMLBody = emailOverride.HTMLBody
				}
				if emailOverride.TextBody != "" {
					emailMsg.TextBody = emailOverride.TextBody
				}
				if emailOverride.From != "" {
					emailMsg.From = emailOverride.From
				}
				if emailOverride.FromName != "" {
					emailMsg.FromName = emailOverride.FromName
				}
				if emailOverride.ReplyTo != "" {
					emailMsg.ReplyTo = emailOverride.ReplyTo
				}
			}
		}
	}

	// If body looks like HTML and no separate HTML body is set, treat Body as HTMLBody
	if emailMsg.HTMLBody == "" && strings.HasPrefix(strings.TrimSpace(emailMsg.TextBody), "<") {
		emailMsg.HTMLBody = emailMsg.TextBody
	}

	msgID, err := a.provider.Send(ctx, emailMsg)
	if err != nil {
		return "", fmt.Errorf("email provider dispatch failed: %w", err)
	}

	return msgID, nil
}
