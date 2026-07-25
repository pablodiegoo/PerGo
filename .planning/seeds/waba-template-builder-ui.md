---
title: WABA Template Builder UI
trigger_condition: When Phase 30 (WABA Template CRUD API) is complete and validated
planted_date: 2026-07-25
context: gsd-explore session — WABA features gap analysis
---

# WABA Template Builder UI

## Idea

Build a visual template builder in the PerGo admin console (HTMX + templ) that allows both technical operators and business users to create, edit, and manage WABA message templates without leaving the platform.

## Key Design Considerations

- **Live preview panel** — show how the template will render on WhatsApp as the user builds it
- **Component-based builder** — drag/arrange header, body, footer, buttons with type-appropriate inputs
- **Inline validation** — surface local validation errors in real-time as the user types (powered by the REQ-WABA-TEMPLATE-VALIDATE endpoint)
- **Category guidance** — explain the difference between MARKETING, UTILITY, AUTHENTICATION and how it affects approval
- **Variable insertion** — helper UI for inserting `{{1}}`, `{{2}}` placeholders with sample values
- **Multi-language management** — create translations of the same template side by side
- **Version timeline** — visual display of template versions (v1 approved → v2 pending → v2 approved)
- **Status badges** — PENDING, APPROVED, REJECTED, PAUSED, DISABLED with color coding
- **Bulk actions** — sync all, delete selected, filter by status/category

## Dependencies

- Phase 30 REST API endpoints (CRUD + validate)
- `waba_templates` table with version tracking columns
- Sketch session recommended before implementation to nail the UX

## Anti-patterns to Avoid

- Don't build a generic JSON editor — business users need a guided experience
- Don't require Meta Business Manager knowledge — PerGo is the interface
- Don't show raw Meta error codes — translate to actionable guidance
