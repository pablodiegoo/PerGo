---
phase: 10-inbox-refactoring-connection-unification
plan: 03
subsystem: admin-ui
tags: [go, templ, htmx, postgres, admin, waba, templates]

requires:
  - phase: 10-inbox-refactoring-connection-unification
    plan: 02
    provides: Unified connections dashboard, decommissioned playground

provides:
  - InboxHandler updated to include WABATemplateRepository dependency
  - Meta templates route `/admin/inbox/new-message-modal` and `/admin/inbox/new-message-send` registered in cmd/pergo/main.go
  - WABA 24-hour service window check implemented in ChatPanel and PollMessages
  - isWabaBlocked conditional formatting in templates/components/chat_panel.templ, disabling standard inputs and displaying a warning banner
  - WABA template composer dropdown and parameters form modal (`components.NewChatModal`)
  - Novo Chat initiator button and modal allowing free-text for Telegram/WhatsApp Web and forcing templates for WABA
  - Expanded unit tests asserting window checks, template payload formatting, and NATS queue message structure

affects: [10-inbox-refactoring-connection-unification]

tech-stack:
  added: []
  patterns: [WABA 24h Window Check, Meta Templates composition, HTMX modal swap]

key-files:
  created:
    - templates/components/new_chat_modal.templ
    - templates/components/new_chat_modal_templ.go
  modified:
    - internal/api/handler/admin/inbox.go
    - templates/components/chat_panel.templ
    - templates/components/chat_panel_templ.go
    - templates/pages/inbox.templ
    - templates/pages/inbox_templ.go
    - cmd/pergo/main.go
    - internal/api/handler/admin/inbox_test.go

key-decisions:
  - "Calculated isWabaBlocked in both ChatPanel (initial load) and PollMessages (polling) using h.Sessions.Get to check if LastInboundAt is older than 24h or if the session does not exist (ErrSessionNotFound)."
  - "Enforced template selection in the Novo Chat modal for WABA by hiding the standard textarea when the whatsapp_cloud option is selected."
  - "Constructed WABA template parameters list using domain.TemplateComponent and domain.TemplateParameter structures for NATS outbound QueueMessage serialization."
  - "Handled bracket-text interpolation issue in Templ by outputting literal string blocks like '{{\"{{1}}\"}}' so the compiler doesn't mistake it for a Go template tag."

patterns-established:
  - "24-hour customer service window enforcement on official WhatsApp API channels."
  - "String literal escaping for double curly braces in Templ components."

requirements-completed:
  - waba_24h_service_window_enforcement
  - waba_template_composer_modal
  - new_chat_initiator_modal
  - update_inbox_handler_tests

coverage:
  - id: U1
    description: "isWabaBlocked set to true when last inbound message is older than 24h"
    requirement: waba_24h_service_window_enforcement
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false
  - id: U2
    description: "isWabaBlocked set to true when session is not found in database"
    requirement: waba_24h_service_window_enforcement
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false
  - id: U3
    description: "NewMessageModal renders NewChatModal template component with templates list"
    requirement: waba_template_composer_modal
    verification:
      - kind: build
        ref: "templ generate"
        status: pass
    human_judgment: true
  - id: U4
    description: "NewMessageSend parses template params and enqueues QueueMessage with TemplateName and Components"
    requirement: waba_template_composer_modal
    verification:
      - kind: build
        ref: "go test -v ./internal/api/handler/admin -run TestInbox"
        status: pass
    human_judgment: false
  - id: U5
    description: "Novo Chat button opens modal with dynamic form hiding textarea for WABA"
    requirement: new_chat_initiator_modal
    verification:
      - kind: build
        ref: "templ generate"
        status: pass
    human_judgment: true
