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

// Standard metadata keys for message events.
const (
	MetaIsGroup        = "is_group"
	MetaParticipant    = "participant"
	MetaChatJID        = "chat_jid"
	MetaSenderPushName = "sender_push_name"
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

// TotalRows returns the total count of rows across all sections.
func (i *Interactive) TotalRows() int {
	count := 0
	for _, s := range i.Action.Sections {
		count += len(s.Rows)
	}
	return count
}

// DegradeToText converts an interactive component into a formatted numbered text menu.
func (i *Interactive) DegradeToText() string {
	var sb strings.Builder
	if i.Header != nil && i.Header.Text != "" {
		sb.WriteString(i.Header.Text)
		sb.WriteString("\n\n")
	}
	sb.WriteString(i.Body.Text)

	if len(i.Action.Buttons) > 0 {
		for idx, b := range i.Action.Buttons {
			sb.WriteString(fmt.Sprintf("\n%d. %s", idx+1, b.Reply.Title))
		}
	}

	if len(i.Action.Sections) > 0 {
		rowNum := 1
		for _, s := range i.Action.Sections {
			if s.Title != "" {
				sb.WriteString("\n\n*")
				sb.WriteString(s.Title)
				sb.WriteString("*")
			}
			for _, r := range s.Rows {
				sb.WriteString(fmt.Sprintf("\n%d. %s", rowNum, r.Title))
				if r.Description != "" {
					sb.WriteString(fmt.Sprintf(" - %s", r.Description))
				}
				rowNum++
			}
		}
	}

	if i.Type == "flow" && i.Action.FlowCTA != "" {
		sb.WriteString(fmt.Sprintf("\n\n[%s]", i.Action.FlowCTA))
	}

	if i.Footer != nil && i.Footer.Text != "" {
		sb.WriteString("\n\n")
		sb.WriteString(i.Footer.Text)
	}

	return sb.String()
}

// TextContent represents text within an interactive component.
type TextContent struct {
	Text string `json:"text"`
}

// Action represents the interactive elements.
type Action struct {
	Button            string                 `json:"button,omitempty"` // For list messages
	Buttons           []Button               `json:"buttons,omitempty"`
	Sections          []Section              `json:"sections,omitempty"`
	FlowToken         string                 `json:"flow_token,omitempty"`
	FlowID            string                 `json:"flow_id,omitempty"`
	FlowCTA           string                 `json:"flow_cta,omitempty"`
	FlowAction        string                 `json:"flow_action,omitempty"`
	FlowActionPayload map[string]interface{} `json:"flow_action_payload,omitempty"`
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
const (
	MessageTypeProduct     = "product"
	MessageTypeProductList = "product_list"
)

type Row struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// ProductItem represents a single product item in a section or catalog.
type ProductItem struct {
	ProductRetailerID string  `json:"product_retailer_id"`
	ItemPrice         float64 `json:"item_price,omitempty"`
	Currency          string  `json:"currency,omitempty"`
	Quantity          int     `json:"quantity,omitempty"`
}

// ProductSection represents a collection of product items within a multi-product message.
type ProductSection struct {
	Title        string        `json:"title,omitempty"`
	ProductItems []ProductItem `json:"product_items,omitempty"`
}

// ProductPayload represents the single product or product list payload for WhatsApp catalog messages.
type ProductPayload struct {
	CatalogID         string           `json:"catalog_id,omitempty"`
	ProductRetailerID string           `json:"product_retailer_id,omitempty"`
	Header            string           `json:"header,omitempty"`
	Body              string           `json:"body,omitempty"`
	Footer            string           `json:"footer,omitempty"`
	Sections          []ProductSection `json:"sections,omitempty"`
}

// CreateMessageRequest is the JSON payload for POST /messages.
type CreateMessageRequest struct {
	To               string                     `json:"to"`
	From             string                     `json:"from,omitempty"`
	Channel          string                     `json:"channel"`
	Body             string                     `json:"body"`
	Media            *Media                     `json:"media,omitempty"`
	Metadata         map[string]string          `json:"metadata,omitempty"`
	TTLSeconds       *int                       `json:"ttl_seconds,omitempty"`
	TemplateName     string                     `json:"template_name,omitempty"`
	Language         string                     `json:"language,omitempty"`
	Components       []TemplateComponent        `json:"components,omitempty"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
	Interactive      *Interactive               `json:"interactive,omitempty"`
	ChannelOverrides map[string]json.RawMessage `json:"channel_overrides,omitempty"`
	FallbackBehavior string                     `json:"fallback_behavior,omitempty"`
	Type             string                     `json:"type,omitempty"`
	Product          *ProductPayload            `json:"product,omitempty"`
}

// QueueMessage wraps the published payload for JetStream queues.
type QueueMessage struct {
	WorkspaceID      uuid.UUID                  `json:"workspace_id"`
	ConnectionID     uuid.UUID                  `json:"connection_id"`
	SenderIdentity   string                     `json:"sender_identity"`
	TraceID          string                     `json:"trace_id"`
	To               string                     `json:"to"`
	Channel          string                     `json:"channel"`
	Body             string                     `json:"body"`
	Media            *Media                     `json:"media,omitempty"`
	Metadata         map[string]string          `json:"metadata,omitempty"`
	TTLSeconds       *int                       `json:"ttl_seconds,omitempty"`
	QueuedAt         time.Time                  `json:"queued_at"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
	TemplateName     string                     `json:"template_name,omitempty"`
	Language         string                     `json:"language,omitempty"`
	Components       []TemplateComponent        `json:"components,omitempty"`
	CampaignID       *uuid.UUID                 `json:"campaign_id,omitempty"`
	VariablesJSON    map[string]string          `json:"variables_json,omitempty"`
	Interactive      *Interactive               `json:"interactive,omitempty"`
	ChannelOverrides map[string]json.RawMessage `json:"channel_overrides,omitempty"`
	FallbackBehavior string                     `json:"fallback_behavior,omitempty"`
	Type             string                     `json:"type,omitempty"`
	Product          *ProductPayload            `json:"product,omitempty"`
}

// TemplateComponent represents a template component payload.
type TemplateComponent struct {
	Type       string              `json:"type"` // "header", "body", "buttons", etc.
	Parameters interface{} `json:"parameters"`
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
	}

	if req.TTLSeconds != nil && *req.TTLSeconds <= 0 {
		details = append(details, FieldError{
			Field:   "ttl_seconds",
			Message: "must be a positive integer",
		})
	}

	if req.TemplateName != "" {
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
	} else if req.TemplateName == "" && req.Body == "" && req.Interactive == nil && req.Product == nil && req.Type != MessageTypeProduct && req.Type != MessageTypeProductList {
		details = append(details, FieldError{
			Field:   "body",
			Message: "either body, media, interactive, or product is required",
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

	hasProductError := false
	if req.Type == MessageTypeProduct || req.Type == MessageTypeProductList || req.Product != nil {
		pDetails := ValidateProductPayload(req.Product, req.Type)
		if len(pDetails) > 0 {
			hasProductError = true
			details = append(details, pDetails...)
		}
	}

	if len(details) > 0 {
		code := "invalid_payload"
		if hasProductError {
			code = "invalid_product_payload"
		}
		return &ErrorResponse{
			Code:    code,
			Message: "request body validation failed",
			Details: details,
		}
	}

	return nil
}

// ValidateProductPayload validates product message bounds and constraints according to Meta API rules.
func ValidateProductPayload(product *ProductPayload, msgType string) []FieldError {
	var details []FieldError

	if product == nil {
		return append(details, FieldError{
			Field:   "product",
			Message: "is required",
		})
	}

	// Single product validation
	if msgType == MessageTypeProduct || (msgType == "" && len(product.Sections) == 0) {
		if product.ProductRetailerID == "" {
			details = append(details, FieldError{
				Field:   "product.product_retailer_id",
				Message: "is required",
			})
		}
		return details
	}

	// Product list validation
	if msgType == MessageTypeProductList || (msgType == "" && len(product.Sections) > 0) {
		if len(product.Sections) < 1 || len(product.Sections) > 10 {
			details = append(details, FieldError{
				Field:   "product.sections",
				Message: "must contain between 1 and 10 sections",
			})
		}

		totalItems := 0
		for i, section := range product.Sections {
			if len([]rune(section.Title)) > 24 {
				details = append(details, FieldError{
					Field:   fmt.Sprintf("product.sections[%d].title", i),
					Message: "cannot exceed 24 characters",
				})
			}

			totalItems += len(section.ProductItems)
			for j, item := range section.ProductItems {
				if item.ProductRetailerID == "" {
					details = append(details, FieldError{
						Field:   fmt.Sprintf("product.sections[%d].product_items[%d].product_retailer_id", i, j),
						Message: "is required",
					})
				}
			}
		}

		if totalItems > 30 {
			details = append(details, FieldError{
				Field:   "product.sections",
				Message: "cannot exceed 30 items total across all sections",
			})
		}
	}

	return details
}
