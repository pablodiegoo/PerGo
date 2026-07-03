---
status: testing
phase: 09-conversational-inbox
source: [09-VERIFICATION.md]
started: 2026-07-03T18:00:00Z
updated: 2026-07-03T18:00:00Z
---

## Current Test

number: 2
name: In-page Toast notifications on background events
expected: |
  With one chat open, send a simulated webhook to another connection;
  verify a toast notification pops up top-center and auto-dismisses in ~3.5 seconds.
awaiting: user response

## Tests

### 1. Split-pane dynamic layout scrolling and styling
expected: Verify responsive three-column layout, conversation list scrolling, chat panel message rendering, and alternating bubble alignment
result: pass
fix: commit 39723d7 — removed .content padding via :has(.inbox-shell), added responsive breakpoints for inbox-sidebar

### 2. In-page Toast notifications on background events
expected: With one chat open, send a simulated webhook to another connection; verify a toast notification pops up top-center and auto-dismisses in ~3.5 seconds
result: pending

## Summary

total: 2
passed: 1
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps

- truth: "The inbox page renders a three-column split-pane layout with sidebar, conversation list, and chat panel, responsive across screen sizes."
  status: failed
  reason: "User reported: basically 4 columns — between sidebar and chat panel have a huge space, almost as big as sidebar. Not very responsive."
  severity: major
  test: 1
  artifacts: []
  missing: []
  root_cause: ".content CSS class has padding: var(--spacing-xl) (2rem) which creates extra visual column between sidebar and inbox-shell. The inbox-shell fills the content area but padding creates a gap. Additionally, the inbox-sidebar (conv-list) uses fixed w-80 with no responsive breakpoints."
  fix: commit 39723d7 — CSS :has(.inbox-shell) removes padding, responsive breakpoints added
