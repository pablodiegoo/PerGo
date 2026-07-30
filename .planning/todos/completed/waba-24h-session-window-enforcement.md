---
title: Implement WABA 24-Hour Session Window Pre-flight Enforcement
date: 2026-07-25
priority: high
tags: [waba, validation, session-window]
resolves_phase: 30
---

# Implement WABA 24-Hour Session Window Pre-flight Enforcement

## Context
When sending non-template WABA messages (text, media, buttons, lists, flows) via `POST /messages`, Meta rejects requests sent outside the 24-hour window from the user's last incoming message with error `131047`.

PerGo should reject these requests early at API ingestion with HTTP 422 `session_window_expired` rather than attempting a failed Meta API call.

## Implementation Tasks

1. **Database Schema (`contact_sessions`)**:
   - Create Goose migration for `contact_sessions` table: `id`, `workspace_id`, `connection_id`, `phone_number`, `last_incoming_at`, `updated_at`.
   - Index on `(workspace_id, phone_number)`.

2. **Incoming Message Ingestion**:
   - On processing any incoming webhook message from a recipient, upsert `contact_sessions.last_incoming_at = NOW()`.

3. **Pre-flight Validation**:
   - In `internal/domain/message.go` or WABA route handler:
     - Check if message `Type` is non-template (`text`, `image`, `document`, `audio`, `video`, `button`, `list`, `cta_url`, `location_request`, `flow`).
     - Query `contact_sessions` for `(workspace_id, recipient_phone)`.
     - If `last_incoming_at` is missing or `time.Since(last_incoming_at) > 24*time.Hour`, return HTTP 422 with `error: "session_window_expired"`.

4. **Unit & Integration Tests**:
   - Test active window (<24h) succeeds.
   - Test expired window (>24h) returns HTTP 422 `session_window_expired`.
   - Test template messages bypass the 24h window check.
