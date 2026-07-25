---
spike: 026
name: waba-interactive-messages
type: standard
validates: "Given an interactive request payload (button, list, cta_url, location_request, flow), when processed by PerGo, then valid Meta Cloud API interactive JSON payloads are generated and previewed in real-time."
verdict: VALIDATED
related: [002, 009, 025]
tags: [waba, interactive, api, ui, meta]
---

# Spike 026: WABA Interactive Messages Engine

## What This Validates

- **Given** an interactive request payload (`button`, `list`, `cta_url`, `location_request`, `flow`),
- **When** transformed by PerGo's WABA Cloud API adapter,
- **Then** valid Meta WhatsApp Cloud API v25.0 `type: "interactive"` JSON payloads are produced matching all Meta structural constraints (e.g. 1-3 buttons, max 10 list rows, mandatory body text).

---

## Research & Architecture

### Reference Findings from `context/inspiration/` (`evolution-go` & Meta Docs)

1. **Meta Interactive Engine Structure (`type: "interactive"`)**:
   - Meta Cloud API (`POST /v21.0/{phone_number_id}/messages`) accepts 5 primary interactive message types:
     - `button` — 1 to 3 quick reply buttons (`action.buttons`)
     - `list` — Sectioned single-select menu (`action.button` + `action.sections`) with up to 10 rows
     - `cta_url` — Call-to-Action link button (`action.name: "cta_url"`, `action.parameters.url`)
     - `location_request_message` — Native GPS location request button (`action.name: "send_location"`)
     - `flow` — Meta Flow interactive forms (`action.name: "flow"`)
   - Header options: `text`, `image`, `document`, `video`.
   - Body is mandatory across all interactive types.
   - Footer is optional string across all types.

2. **Validation Rules & Rendering Constraints**:
   - **Buttons (`button`)**: Minimum 1 button, maximum 3 buttons. Each button title $\le 20$ chars. Button IDs must be unique per message.
   - **List (`list`)**: `button_text` trigger $\le 20$ chars. Maximum 10 rows total across all sections. Row title $\le 24$ chars, description $\le 72$ chars.
   - **CTA Link (`cta_url`)**: `url` must start with `http://` or `https://`.
   - **Flows (`flow`)**: Requires `flow_id`, `flow_cta`, `flow_message_version: "3"`.

3. **Incoming Webhook Callbacks (`interactive`)**:
   - When a customer clicks a button or selects a list item, WABA delivers an `interactive` webhook event with `type: "button_reply"`, `type: "list_reply"`, or `type: "nfm_reply"` containing button/row ID and title.

---

## Unified Go Model Proposed for PerGo (`internal/domain`)

```go
type InteractiveHeader struct {
    Type     string `json:"type,omitempty"` // "text", "image", "document", "video"
    Text     string `json:"text,omitempty"`
    MediaURL string `json:"media_url,omitempty"`
}

type InteractiveButton struct {
    ID    string `json:"id"`
    Title string `json:"title"`
}

type InteractiveRow struct {
    ID          string `json:"id"`
    Title       string `json:"title"`
    Description string `json:"description,omitempty"`
}

type InteractiveSection struct {
    Title string           `json:"title"`
    Rows  []InteractiveRow `json:"rows"`
}

type InteractiveCTA struct {
    DisplayText string `json:"display_text"`
    URL         string `json:"url"`
}

type InteractiveFlow struct {
    FlowID     string                 `json:"flow_id"`
    FlowToken  string                 `json:"flow_token,omitempty"`
    FlowCTA    string                 `json:"flow_cta"`
    FlowAction string                 `json:"flow_action,omitempty"`
    FlowScreen string                 `json:"flow_screen,omitempty"`
    Payload    map[string]interface{} `json:"payload,omitempty"`
}

type InteractivePayload struct {
    Type       string               `json:"type"` // "button", "list", "cta_url", "location_request", "flow"
    Header     *InteractiveHeader   `json:"header,omitempty"`
    Body       string               `json:"body"`
    Footer     string               `json:"footer,omitempty"`
    ButtonText string               `json:"button_text,omitempty"`
    Buttons    []InteractiveButton  `json:"buttons,omitempty"`
    Sections   []InteractiveSection `json:"sections,omitempty"`
    CTA        *InteractiveCTA      `json:"cta,omitempty"`
    Flow       *InteractiveFlow     `json:"flow,omitempty"`
}
```

---

## How to Run & Verify

### 1. Run Go Unit Tests
```bash
go test -v ./.planning/spikes/026-waba-interactive-messages/...
```

### 2. Experience Live Interactive Workbench
Open `workbench.html` in any web browser to interactively build and preview WABA messages:
```bash
xdg-open ./.planning/spikes/026-waba-interactive-messages/workbench.html
```

---

## Results & Verdict

- **Verdict:** `VALIDATED`
- **Payload Translation:** 100% verified against Meta Cloud API v21.0 specification across all 5 interactive types.
- **Constraints Enforced:** Button limits ($\le 3$), list row limits ($\le 10$), CTA URL formatting, and character length limits validated.
- **Live Preview:** Interactive test bench (`workbench.html`) provides real-time WhatsApp phone screen rendering and Meta Cloud API JSON inspection.
