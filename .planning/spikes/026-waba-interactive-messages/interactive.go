package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// InteractiveHeader represents the optional header component of a WABA interactive message.
type InteractiveHeader struct {
	Type     string `json:"type,omitempty"` // "text", "image", "document", "video"
	Text     string `json:"text,omitempty"`
	MediaURL string `json:"media_url,omitempty"`
}

// InteractiveButton represents a single quick reply button.
type InteractiveButton struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// InteractiveRow represents a single selectable item in a list message section.
type InteractiveRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// InteractiveSection represents a grouped category of rows in a list message.
type InteractiveSection struct {
	Title string           `json:"title"`
	Rows  []InteractiveRow `json:"rows"`
}

// InteractiveCTA represents Call-To-Action parameters (URL link).
type InteractiveCTA struct {
	DisplayText string `json:"display_text"`
	URL         string `json:"url"`
}

// InteractiveFlow represents Meta Flow message parameters.
type InteractiveFlow struct {
	FlowID         string                 `json:"flow_id"`
	FlowToken      string                 `json:"flow_token,omitempty"`
	FlowCTA        string                 `json:"flow_cta"`
	FlowAction     string                 `json:"flow_action,omitempty"`  // "navigate" or "data_exchange"
	FlowScreen     string                 `json:"flow_screen,omitempty"`  // e.g. "APPOINTMENT_SCREEN"
	Payload        map[string]interface{} `json:"payload,omitempty"`
}

// InteractivePayload is the unified Go payload representation for WABA interactive messages in PerGo.
type InteractivePayload struct {
	Type       string               `json:"type"`                  // "button", "list", "cta_url", "location_request", "flow"
	Header     *InteractiveHeader   `json:"header,omitempty"`
	Body       string               `json:"body"`
	Footer     string               `json:"footer,omitempty"`
	ButtonText string               `json:"button_text,omitempty"` // Button label for list or CTA
	Buttons    []InteractiveButton  `json:"buttons,omitempty"`     // 1-3 quick reply buttons
	Sections   []InteractiveSection `json:"sections,omitempty"`    // List sections
	CTA        *InteractiveCTA      `json:"cta,omitempty"`         // CTA URL parameters
	Flow       *InteractiveFlow     `json:"flow,omitempty"`        // Flow parameters
}

// MetaInteractiveMessage represents Meta WhatsApp Cloud API v25.0 outbound request payload structure.
type MetaInteractiveMessage struct {
	MessagingProduct string          `json:"messaging_product"`
	RecipientType    string          `json:"recipient_type"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Interactive      MetaInteractive `json:"interactive"`
}

// MetaInteractive contains the nested interactive structure for Meta Cloud API.
type MetaInteractive struct {
	Type   string                `json:"type"`
	Header *MetaHeader           `json:"header,omitempty"`
	Body   MetaBody              `json:"body"`
	Footer *MetaFooter           `json:"footer,omitempty"`
	Action MetaAction            `json:"action"`
}

type MetaHeader struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Image    *MetaMediaLink `json:"image,omitempty"`
	Document *MetaMediaLink `json:"document,omitempty"`
	Video    *MetaMediaLink `json:"video,omitempty"`
}

type MetaMediaLink struct {
	Link string `json:"link"`
}

type MetaBody struct {
	Text string `json:"text"`
}

type MetaFooter struct {
	Text string `json:"text"`
}

type MetaAction struct {
	Button     string                   `json:"button,omitempty"`     // List button text
	Buttons    []MetaReplyButton        `json:"buttons,omitempty"`    // Quick reply buttons
	Sections   []MetaSection            `json:"sections,omitempty"`   // List sections
	Name       string                   `json:"name,omitempty"`       // "cta_url", "send_location", "flow"
	Parameters map[string]interface{}  `json:"parameters,omitempty"` // Parameters for CTA/Flow
}

type MetaReplyButton struct {
	Type  string             `json:"type"` // "reply"
	Reply MetaButtonContent `json:"reply"`
}

type MetaButtonContent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type MetaSection struct {
	Title string    `json:"title"`
	Rows  []MetaRow `json:"rows"`
}

type MetaRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Validate checks structural constraints required by Meta Cloud API for interactive messages.
func (p *InteractivePayload) Validate() error {
	if strings.TrimSpace(p.Body) == "" {
		return errors.New("body text is required for interactive messages")
	}

	switch strings.ToLower(p.Type) {
	case "button":
		if len(p.Buttons) == 0 {
			return errors.New("button interactive message requires at least 1 button")
		}
		if len(p.Buttons) > 3 {
			return errors.New("button interactive message allows maximum 3 buttons")
		}
		for i, btn := range p.Buttons {
			if strings.TrimSpace(btn.ID) == "" {
				return fmt.Errorf("button[%d] id cannot be empty", i)
			}
			if strings.TrimSpace(btn.Title) == "" {
				return fmt.Errorf("button[%d] title cannot be empty", i)
			}
			if len(btn.Title) > 20 {
				return fmt.Errorf("button[%d] title exceeds maximum length of 20 characters", i)
			}
		}

	case "list":
		if strings.TrimSpace(p.ButtonText) == "" {
			return errors.New("list interactive message requires button_text (menu trigger label)")
		}
		if len(p.ButtonText) > 20 {
			return errors.New("list button_text exceeds maximum length of 20 characters")
		}
		if len(p.Sections) == 0 {
			return errors.New("list interactive message requires at least 1 section")
		}
		totalRows := 0
		for sIdx, sec := range p.Sections {
			if len(sec.Rows) == 0 {
				return fmt.Errorf("section[%d] must contain at least 1 row", sIdx)
			}
			for rIdx, row := range sec.Rows {
				if strings.TrimSpace(row.ID) == "" {
					return fmt.Errorf("section[%d].row[%d] id cannot be empty", sIdx, rIdx)
				}
				if strings.TrimSpace(row.Title) == "" {
					return fmt.Errorf("section[%d].row[%d] title cannot be empty", sIdx, rIdx)
				}
				if len(row.Title) > 24 {
					return fmt.Errorf("section[%d].row[%d] title exceeds 24 characters limit", sIdx, rIdx)
				}
				if len(row.Description) > 72 {
					return fmt.Errorf("section[%d].row[%d] description exceeds 72 characters limit", sIdx, rIdx)
				}
				totalRows++
			}
		}
		if totalRows > 10 {
			return fmt.Errorf("list message allows maximum 10 rows across all sections (got %d)", totalRows)
		}

	case "cta_url":
		if p.CTA == nil {
			return errors.New("cta_url interactive message requires cta parameters")
		}
		if strings.TrimSpace(p.CTA.DisplayText) == "" {
			return errors.New("cta display_text cannot be empty")
		}
		if strings.TrimSpace(p.CTA.URL) == "" {
			return errors.New("cta url cannot be empty")
		}
		if !strings.HasPrefix(p.CTA.URL, "http://") && !strings.HasPrefix(p.CTA.URL, "https://") {
			return errors.New("cta url must start with http:// or https://")
		}

	case "location_request", "location_request_message":
		// No extra required parameters beyond body

	case "flow":
		if p.Flow == nil {
			return errors.New("flow interactive message requires flow parameters")
		}
		if strings.TrimSpace(p.Flow.FlowID) == "" {
			return errors.New("flow flow_id cannot be empty")
		}
		if strings.TrimSpace(p.Flow.FlowCTA) == "" {
			return errors.New("flow flow_cta text cannot be empty")
		}

	default:
		return fmt.Errorf("unsupported interactive message type: %s", p.Type)
	}

	return nil
}

// ToMetaJSON converts the unified InteractivePayload into Meta WhatsApp Cloud API v21.0 payload format.
func (p *InteractivePayload) ToMetaJSON(toPhone string) (*MetaInteractiveMessage, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	meta := &MetaInteractiveMessage{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               toPhone,
		Type:             "interactive",
		Interactive: MetaInteractive{
			Body: MetaBody{Text: p.Body},
		},
	}

	// Format Footer
	if strings.TrimSpace(p.Footer) != "" {
		meta.Interactive.Footer = &MetaFooter{Text: p.Footer}
	}

	// Format Header
	if p.Header != nil && strings.TrimSpace(p.Header.Type) != "" {
		hType := strings.ToLower(p.Header.Type)
		header := &MetaHeader{Type: hType}

		switch hType {
		case "text":
			header.Text = p.Header.Text
		case "image":
			header.Image = &MetaMediaLink{Link: p.Header.MediaURL}
		case "document":
			header.Document = &MetaMediaLink{Link: p.Header.MediaURL}
		case "video":
			header.Video = &MetaMediaLink{Link: p.Header.MediaURL}
		}
		meta.Interactive.Header = header
	}

	// Format Action per type
	normalizedType := strings.ToLower(p.Type)
	switch normalizedType {
	case "button":
		meta.Interactive.Type = "button"
		btns := make([]MetaReplyButton, len(p.Buttons))
		for i, btn := range p.Buttons {
			btns[i] = MetaReplyButton{
				Type: "reply",
				Reply: MetaButtonContent{
					ID:    btn.ID,
					Title: btn.Title,
				},
			}
		}
		meta.Interactive.Action.Buttons = btns

	case "list":
		meta.Interactive.Type = "list"
		meta.Interactive.Action.Button = p.ButtonText

		secs := make([]MetaSection, len(p.Sections))
		for i, sec := range p.Sections {
			rows := make([]MetaRow, len(sec.Rows))
			for j, r := range sec.Rows {
				rows[j] = MetaRow{
					ID:          r.ID,
					Title:       r.Title,
					Description: r.Description,
				}
			}
			secs[i] = MetaSection{
				Title: sec.Title,
				Rows:  rows,
			}
		}
		meta.Interactive.Action.Sections = secs

	case "cta_url":
		meta.Interactive.Type = "cta_url"
		meta.Interactive.Action.Name = "cta_url"
		meta.Interactive.Action.Parameters = map[string]interface{}{
			"display_text": p.CTA.DisplayText,
			"url":          p.CTA.URL,
		}

	case "location_request", "location_request_message":
		meta.Interactive.Type = "location_request_message"
		meta.Interactive.Action.Name = "send_location"

	case "flow":
		meta.Interactive.Type = "flow"
		meta.Interactive.Action.Name = "flow"

		flowToken := p.Flow.FlowToken
		if flowToken == "" {
			flowToken = "unused_token"
		}
		flowAction := p.Flow.FlowAction
		if flowAction == "" {
			flowAction = "navigate"
		}

		params := map[string]interface{}{
			"flow_message_version": "3",
			"flow_token":           flowToken,
			"flow_id":              p.Flow.FlowID,
			"flow_cta":             p.Flow.FlowCTA,
			"flow_action":          flowAction,
		}

		if p.Flow.FlowScreen != "" || len(p.Flow.Payload) > 0 {
			payloadObj := map[string]interface{}{}
			if p.Flow.FlowScreen != "" {
				payloadObj["screen"] = p.Flow.FlowScreen
			}
			for k, v := range p.Flow.Payload {
				payloadObj[k] = v
			}
			params["flow_action_payload"] = payloadObj
		}

		meta.Interactive.Action.Parameters = params
	}

	return meta, nil
}

// InteractiveWebhookResponse models an incoming customer interactive response callback from WABA webhook.
type InteractiveWebhookResponse struct {
	Type        string `json:"type"`         // "button_reply", "list_reply", "nfm_reply" (flow)
	ButtonReply *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"button_reply,omitempty"`
	ListReply *struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
	} `json:"list_reply,omitempty"`
	NFMReply *struct {
		Name           string                 `json:"name"`
		Body           string                 `json:"body"`
		ResponseJSON   string                 `json:"response_json"`
		ParsedResponse map[string]interface{} `json:"-"`
	} `json:"nfm_reply,omitempty"`
}

// ToMetaJSONChunks converts the payload into one or more Meta Cloud API message payloads.
// If buttons exceed 3 or list rows exceed 10, it automatically chunks the payload into multiple messages
// instead of failing validation, ensuring smooth delivery without rejecting developer requests.
func (p *InteractivePayload) ToMetaJSONChunks(toPhone string) ([]*MetaInteractiveMessage, error) {
	normalizedType := strings.ToLower(p.Type)

	// Strategy 1: Auto-convert 4..10 buttons to a List message (single message UX)
	if normalizedType == "button" && len(p.Buttons) > 3 && len(p.Buttons) <= 10 {
		converted := &InteractivePayload{
			Type:       "list",
			Header:     p.Header,
			Body:       p.Body,
			Footer:     p.Footer,
			ButtonText: "Ver Opções",
		}
		rows := make([]InteractiveRow, len(p.Buttons))
		for i, btn := range p.Buttons {
			rows[i] = InteractiveRow{
				ID:    btn.ID,
				Title: btn.Title,
			}
		}
		converted.Sections = []InteractiveSection{
			{Title: "Opções disponíveis", Rows: rows},
		}
		meta, err := converted.ToMetaJSON(toPhone)
		if err != nil {
			return nil, err
		}
		return []*MetaInteractiveMessage{meta}, nil
	}

	// Strategy 2: Auto-chunking for >3 buttons (>10 buttons case)
	if normalizedType == "button" && len(p.Buttons) > 3 {
		var chunks []*MetaInteractiveMessage
		total := len(p.Buttons)
		for start := 0; start < total; start += 3 {
			end := start + 3
			if end > total {
				end = total
			}

			subButtons := p.Buttons[start:end]
			bodyText := p.Body
			if start > 0 {
				bodyText = fmt.Sprintf("%s\n\n*(Continuação - parte %d)*", p.Body, (start/3)+1)
			}

			subPayload := &InteractivePayload{
				Type:    "button",
				Body:    bodyText,
				Footer:  p.Footer,
				Buttons: subButtons,
			}
			if start == 0 {
				subPayload.Header = p.Header
			}

			meta, err := subPayload.ToMetaJSON(toPhone)
			if err != nil {
				return nil, err
			}
			chunks = append(chunks, meta)
		}
		return chunks, nil
	}

	// Strategy 3: Auto-chunking for >10 list rows
	if normalizedType == "list" {
		totalRows := 0
		for _, sec := range p.Sections {
			totalRows += len(sec.Rows)
		}

		if totalRows > 10 {
			var chunks []*MetaInteractiveMessage
			var currentRows []InteractiveRow
			chunkIdx := 1

			// Flatten and chunk rows by 10
			for _, sec := range p.Sections {
				for _, row := range sec.Rows {
					currentRows = append(currentRows, row)
					if len(currentRows) == 10 {
						bodyText := p.Body
						if chunkIdx > 1 {
							bodyText = fmt.Sprintf("%s\n\n*(Continuação - parte %d)*", p.Body, chunkIdx)
						}

						subPayload := &InteractivePayload{
							Type:       "list",
							Body:       bodyText,
							Footer:     p.Footer,
							ButtonText: p.ButtonText,
							Sections: []InteractiveSection{
								{Title: fmt.Sprintf("Opções (%d-%d)", (chunkIdx-1)*10+1, chunkIdx*10), Rows: currentRows},
							},
						}
						if chunkIdx == 1 {
							subPayload.Header = p.Header
						}

						meta, err := subPayload.ToMetaJSON(toPhone)
						if err != nil {
							return nil, err
						}
						chunks = append(chunks, meta)
						currentRows = nil
						chunkIdx++
					}
				}
			}

			if len(currentRows) > 0 {
				bodyText := p.Body
				if chunkIdx > 1 {
					bodyText = fmt.Sprintf("%s\n\n*(Continuação - parte %d)*", p.Body, chunkIdx)
				}
				subPayload := &InteractivePayload{
					Type:       "list",
					Body:       bodyText,
					Footer:     p.Footer,
					ButtonText: p.ButtonText,
					Sections: []InteractiveSection{
						{Title: "Outras Opções", Rows: currentRows},
					},
				}
				if chunkIdx == 1 {
					subPayload.Header = p.Header
				}
				meta, err := subPayload.ToMetaJSON(toPhone)
				if err != nil {
					return nil, err
				}
				chunks = append(chunks, meta)
			}

			return chunks, nil
		}
	}

	// Standard single message case
	meta, err := p.ToMetaJSON(toPhone)
	if err != nil {
		return nil, err
	}
	return []*MetaInteractiveMessage{meta}, nil
}

// ParseInteractiveWebhook unpacks incoming customer interactive responses.
func ParseInteractiveWebhook(rawJSON []byte) (*InteractiveWebhookResponse, error) {
	var resp InteractiveWebhookResponse
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal interactive webhook response: %w", err)
	}

	if resp.NFMReply != nil && resp.NFMReply.ResponseJSON != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(resp.NFMReply.ResponseJSON), &parsed); err == nil {
			resp.NFMReply.ParsedResponse = parsed
		}
	}

	return &resp, nil
}
