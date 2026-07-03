---
status: testing
phase: 09-conversational-inbox
source: [09-VERIFICATION.md]
started: 2026-07-03T18:00:00Z
updated: 2026-07-03T18:00:00Z
---

## Current Test

number: 1
name: Split-pane dynamic layout scrolling and styling
expected: |
  The inbox page at /admin/inbox renders a three-column split-pane layout:
  - Left sidebar (collapsible) with navigation
  - Conversation list panel (300px) with channel filter tabs and 5s auto-refresh
  - Chat panel (flex-grow) with message bubbles, auto-grow textarea, 3s polling
  Verify responsive resizing, scrolling behavior, and bubble alignment
  (inbound left-aligned white, outbound right-aligned blue #3b82f6).
awaiting: user response

## Tests

### 1. Split-pane dynamic layout scrolling and styling
expected: Verify responsive three-column layout, conversation list scrolling, chat panel message rendering, and alternating bubble alignment
result: pending

### 2. In-page Toast notifications on background events
expected: With one chat open, send a simulated webhook to another connection; verify a toast notification pops up top-center and auto-dismisses in ~3.5 seconds
result: pending

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
