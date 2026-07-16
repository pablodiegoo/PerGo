---
phase: 18-omnichannel-contact-merging
plan: 18-P02
subsystem: ui
tags: [inbox, htmx, templates, routing, integration-testing]

# Dependency graph
requires:
  - phase: 18-omnichannel-contact-merging
    plan: 18-P01
    provides: Database schema and repositories for contacts and contact identities
provides:
  - Unification of chat histories grouping by contact ID in the repository
  - Inbox API endpoints for contact search and contact merging
  - Front-end views, picker selectors, and inline HTMX contact merging inputs
affects: [18-omnichannel-contact-merging]

# Tech tracking
tech-stack:
  added: []
  patterns: [Cross-channel conversation UNION query, HTMX inline type-ahead dropdown, Contact merge confirmation flows]

key-files:
  created:
    - cmd/pergo/admin_contact_merge_test.go
  modified:
    - internal/repository/audit.go
    - internal/api/handler/admin/inbox.go
    - templates/pages/inbox.templ
    - templates/components/chat_panel.templ
    - templates/components/conv_item.templ
    - templates/components/conv_list.templ
    - templates/components/message_bubble.templ
    - cmd/pergo/main.go
    - internal/platform/queue/jetstream.go
    - internal/platform/queue/webhook_worker.go
  deleted:
    - internal/repository/telegram_contact.go
    - internal/repository/telegram_contact_test.go

key-decisions:
  - "Grouping the inbox list by contact_id via CTE query to consolidate WhatsApp and Telegram threads into single rows"
  - "Querying message history dynamically by looking up all identities mapped to a contact ID and unioning their inbound/outbound records"
  - "Strict workspace scoping check in the merging handlers to protect workspace boundaries"
  - "HTMX-driven type-ahead search with a delayed keyup trigger to fetch contact candidates"

patterns-established:
  - "Consolidated conversation list and unified thread queries in the AuditRepository"

requirements-completed: ["CONT-01", "CONT-02", "CONT-03", "CONT-04"]

coverage:
  - id: D6
    description: "Audit repository grouping conversation summary card by contact_id and list thread history using all identities"
    requirement: "CONT-01"
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_contact_merge_test.go#TestAdminContactMerge"
        status: pass
    human_judgment: false
  - id: D7
    description: "Contact search and merging routes registered and handlers implemented in the Inbox controller"
    requirement: "CONT-02"
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_contact_merge_test.go#TestAdminContactMerge"
        status: pass
    human_judgment: false
  - id: D8
    description: "HTMX components for conversation list item linking contact_id, chat panel details card, and inline search-and-merge overlay dropdown"
    requirement: "CONT-02"
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_contact_merge_test.go#TestAdminContactMerge"
        status: pass
    human_judgment: false
  - id: D9
    description: "Complete deletion of legacy telegram_contact files and references"
    requirement: "CONT-01"
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_contact_merge_test.go#TestAdminContactMerge"
        status: pass
    human_judgment: false

# Metrics
duration: 25min
completed: 2026-07-16
status: complete
---

# Phase 18: Omnichannel Contact Merging - Plan P02 Summary

This summary details the work done to finalize Phase 18 Wave 2 implementation of Omnichannel Contact Merging in PerGo.

## 1. Accomplished Tasks

- **Thread Consolidation**: Refactored `ListConversations` and `ListThreadByContact` in `internal/repository/audit.go` to group and list conversation logs under unified contacts instead of raw addresses.
- **Search & Merge Endpoints**: Added GET `/admin/contacts/search` and POST `/admin/contacts/merge` to `internal/api/handler/admin/inbox.go`. Handled full transaction safety in `MergeContacts` and logged the operation in `webhook_dlq` / audit action logs under `contact.merge`.
- **UI Views refactoring**: Updated `inbox.templ`, `chat_panel.templ`, `conv_item.templ`, and `conv_list.templ` to:
  - Display unified contact card header.
  - Implement a compose box picker dropdown for replying.
  - Embed the HTMX type-ahead contact merging UI dropdown.
- **Legacy cleanup**: Permanently deleted the files `telegram_contact.go` and `telegram_contact_test.go` from the `internal/repository` package and removed references in `cmd/pergo/main.go`.
- **Integration Tests**: Added `cmd/pergo/admin_contact_merge_test.go` with Docker integration test containers validating successful merge operations, thread unification, workspace boundaries enforcement, and transaction rollback.

## 2. Verification Run

- Compiled templates with `~/go/bin/templ generate`.
- Verified compilation with `go build ./cmd/pergo`.
- Verified test suite passes successfully:
  - `go test -v ./cmd/pergo -run TestAdminContactMerge` (PASS)
  - `go test -v ./internal/...` (PASS)
