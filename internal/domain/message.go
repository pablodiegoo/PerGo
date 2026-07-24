// Package domain defines the core message types, validation rules, and
// error contracts for the PerGo message ingestion API.
package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MessageStatus represents the lifecycle state of a message.
type MessageStatus string

const (
	StatusQueued    MessageStatus = "queued"
	StatusSent      MessageStatus = "sent"
	StatusDelivered MessageStatus = "delivered"
	StatusRead      MessageStatus = "read"
	StatusFailed    MessageStatus = "failed"
)

// ValidChannels defines the set of accepted channel values.
var ValidChannels = map[string]bool{
	"whatsapp":       true,
	"whatsapp_cloud": true,
	"telegram":       true,
	"instagram":      true,
	"email":          true,
	"email_ses":      true,
	"email_smtp":     true,
	"email_mautic":   true,
}

// Media represents media payload (URL, type, filename, caption).
type Media struct {
	MediaURL  string `json:"media_url"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
}

// Interactive represents the unified schema for rich interactive messages.
type Interactive struct {
	Type   string       `json:"type"` // "button", "list", etc.
	Header *TextContent `json:"header,omitempty"`
	Body   TextContent  `json:"body"`
	Footer *TextContent `json:"footer,omitempty"`
	Action Action       `json:"action"`
}

// TextContent represents text within an interactive component.
type TextContent struct {
	Text string `json:"text"`
}

// Action represents the interactive elements.
type Action struct {
	Button   string    `json:"button,omitempty"` // For list messages
	Buttons  []Button  `json:"buttons,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

// Button represents a reply button.
type Button struct {
	Type  string `json:"type"` // e.g. "reply"
	Reply Reply  `json:"reply"`
}

// Reply holds the content of a button.
type Reply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Section represents a section of a list message.
type Section struct {
	Title string `json:"title,omitempty"`
	Rows  []Row  `json:"rows"`
}

// Row represents an item in a list message section.
type Row struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// CreateMessageRequest is the JSON payload for POST /messages.
type CreateMessageRequest struct {
	To               string              `json:"to"`
	From             string              `json:"from,omitempty"`
	Channel          string              `json:"channel"`
	Body             string              `json:"body"`
	Media            *Media              `json:"media,omitempty"`
	Metadata         map[string]string   `json:"metadata,omitempty"`
	TTLSeconds       *int                `json:"ttl_seconds,omitempty"`
	TemplateName     string              `json:"template_name,omitempty"`
	Language         string              `json:"language,omitempty"`
	Components       []TemplateComponent        `json:"components,omitempty"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
	Interactive      *Interactive               `json:"interactive,omitempty"`
	ChannelOverrides map[string]json.RawMessage `json:"channel_overrides,omitempty"`
	FallbackBehavior string                     `json:"fallback_behavior,omitempty"`
}

// QueueMessage wraps the published payload for JetStream queues.
type QueueMessage struct {
	WorkspaceID      uuid.UUID           `json:"workspace_id"`
	ConnectionID     uuid.UUID           `json:"connection_id"`
	SenderIdentity   string              `json:"sender_identity"`
	TraceID          string              `json:"trace_id"`
	To               string              `json:"to"`
	Channel          string              `json:"channel"`
	Body             string              `json:"body"`
	Media            *Media              `json:"media,omitempty"`
	Metadata         map[string]string   `json:"metadata,omitempty"`
	TTLSeconds       *int                `json:"ttl_seconds,omitempty"`
	QueuedAt         time.Time           `json:"queued_at"`
	FallbackChannels []string            `json:"fallback_channels,omitempty"`
	TemplateName     string              `json:"template_name,omitempty"`
	Language         string              `json:"language,omitempty"`
	Components       []TemplateComponent `json:"components,omitempty"`
	CampaignID       *uuid.UUID                 `json:"campaign_id,omitempty"`
	VariablesJSON    map[string]string          `json:"variables_json,omitempty"`
	Interactive      *Interactive               `json:"interactive,omitempty"`
	ChannelOverrides map[string]json.RawMessage `json:"channel_overrides,omitempty"`
	FallbackBehavior string                     `json:"fallback_behavior,omitempty"`
}

// TemplateComponent represents a template component payload.
type TemplateComponent struct {
	Type       string              `json:"type"` // "header", "body", "buttons", etc.
	Parameters []TemplateParameter `json:"parameters"`
}

// TemplateParameter represents a template parameter payload.
type TemplateParameter struct {
	Type string `json:"type"` // "text", etc.
	Text string `json:"text,omitempty"`
}

// CreateMessageResponse is returned on successful enqueue (HTTP 202).
type CreateMessageResponse struct {
	MessageID uuid.UUID     `json:"message_id"`
	Status    MessageStatus `json:"status"`
	QueuedAt  time.Time     `json:"queued_at"`
}

// ErrorResponse is the structured error format per API-04.
type ErrorResponse struct {
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	MoreInfo string       `json:"more_info,omitempty"`
	Details  []FieldError `json:"details,omitempty"`
}

// FieldError provides field-level validation error details.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateMessage checks a CreateMessageRequest for correctness and returns
// an ErrorResponse if any validation rule fails. Returns nil when valid.
func ValidateMessage(req *CreateMessageRequest) *ErrorResponse {
	var details []FieldError

	if req.To == "" {
		details = append(details, FieldError{
			Field:   "to",
			Message: "is required",
		})
	}

	if req.Channel == "" {
		details = append(details, FieldError{
			Field:   "channel",
			Message: "is required",
		})
	} else if !ValidChannels[req.Channel] {
		details = append(details, FieldError{
			Field:   "channel",
			Message: "must be one of: whatsapp, whatsapp_cloud, telegram, instagram, email, email_ses, email_smtp, email_mautic",
		})
	}

	if req.TTLSeconds != nil && *req.TTLSeconds <= 0 {
		details = append(details, FieldError{
			Field:   "ttl_seconds",
			Message: "must be a positive integer",
		})
	}

	if req.TemplateName != "" {
		if req.Channel != "whatsapp_cloud" {
			details = append(details, FieldError{
				Field:   "template_name",
				Message: "templates are only supported for whatsapp_cloud channel",
			})
		}
		if req.Language == "" {
			details = append(details, FieldError{
				Field:   "language",
				Message: "is required when template_name is specified",
			})
		}
	}

	// Validate fallback channels
	seen := make(map[string]bool)
	if req.Channel != "" {
		seen[req.Channel] = true
	}
	for i, fb := range req.FallbackChannels {
		if !ValidChannels[fb] {
			details = append(details, FieldError{
				Field:   fmt.Sprintf("fallback_channels[%d]", i),
				Message: "unsupported channel: must be one of: whatsapp, whatsapp_cloud, telegram, instagram, email, email_ses, email_smtp, email_mautic",
			})
		}
		if seen[fb] {
			details = append(details, FieldError{
				Field:   fmt.Sprintf("fallback_channels[%d]", i),
				Message: "duplicate channel entry",
			})
		}
		seen[fb] = true
	}

	// Validate Body & Media payload presence
	if req.Media != nil {
		if req.Media.MediaType != "image" && req.Media.MediaType != "document" && req.Media.MediaType != "audio" && req.Media.MediaType != "video" {
			details = append(details, FieldError{
				Field:   "media.media_type",
				Message: "must be one of: image, document, audio, video",
			})
		}

		// MediaURL validation: check empty and scheme
		hasValidPrefix := false
		lowerURL := strings.ToLower(req.Media.MediaURL)
		if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
			hasValidPrefix = true
		}
		if req.Media.MediaURL == "" {
			details = append(details, FieldError{
				Field:   "media.media_url",
				Message: "is required when media is provided",
			})
		} else if !hasValidPrefix {
			details = append(details, FieldError{
				Field:   "media.media_url",
				Message: "must be a valid HTTP/HTTPS URL",
			})
		}

		if req.Media.MediaType == "document" && req.Media.Filename == "" {
			details = append(details, FieldError{
				Field:   "media.filename",
				Message: "is required when media_type is document",
			})
		}
	} else if req.TemplateName == "" && req.Body == "" && req.Interactive == nil {
		details = append(details, FieldError{
			Field:   "body",
			Message: "either body, media, or interactive is required",
		})
	}

	if req.FallbackBehavior != "" && req.FallbackBehavior != "degrade" && req.FallbackBehavior != "fail" {
		details = append(details, FieldError{
			Field:   "fallback_behavior",
			Message: `must be either "degrade" or "fail"`,
		})
	}

	if req.Interactive != nil {
		if req.Interactive.Type == "" {
			details = append(details, FieldError{
				Field:   "interactive.type",
				Message: "is required",
			})
		}
		if req.Interactive.Body.Text == "" {
			details = append(details, FieldError{
				Field:   "interactive.body.text",
				Message: "is required",
			})
		}
		if req.Interactive.Type == "button" && len(req.Interactive.Action.Buttons) == 0 {
			details = append(details, FieldError{
				Field:   "interactive.action.buttons",
				Message: "is required when type is button",
			})
		}
		if req.Interactive.Type == "list" && len(req.Interactive.Action.Sections) == 0 {
			details = append(details, FieldError{
				Field:   "interactive.action.sections",
				Message: "is required when type is list",
			})
		}
	}

	if len(details) > 0 {
		return &ErrorResponse{
			Code:    "invalid_payload",
			Message: "request body validation failed",
			Details: details,
		}
	}

	return nil
}
