# Phase 31: Template CRUD, Meta Graph API Sync & Local Cache - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-26
**Phase:** 31-Template CRUD, Meta Graph API Sync & Local Cache
**Areas discussed:** Template creation flow, Cache architecture, Admin UI template builder, Visual template previewer

---

## Template Creation Flow

### Q1: Meta submission on create

| Option | Description | Selected |
|--------|-------------|----------|
| Submit to Meta immediately | POST to PerGo creates template in Meta Graph API and stores locally (PENDING) | ✓ |
| Local-first with explicit submit | Operator creates a draft locally, reviews, then submits to Meta | |
| Passthrough only | PerGo doesn't create templates — sync only | |

**User's choice:** Submit to Meta immediately
**Notes:** Single action, operator sees Meta's response including errors.

### Q2: Update/delete behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Unified CRUD | PUT updates in Meta first, DELETE removes from Meta | |
| Read-heavy with sync | Only CREATE and DELETE go to Meta, edits by delete+recreate | |
| You decide | Follow Meta's actual API constraints | ✓ |

**User's choice:** You decide — follow Meta's actual API constraints

### Q3: Template ownership model

| Option | Description | Selected |
|--------|-------------|----------|
| Scoped by connection | Each WABA connection manages its own templates, connection_id required | ✓ |
| Workspace-level with connection binding | Templates logically per-workspace, bound to connection at creation | |

**User's choice:** Scoped by connection

### Q4: Meta API error handling

| Option | Description | Selected |
|--------|-------------|----------|
| Standard JSON error response | Return Meta's error code in PerGo's standard format | |
| You decide | Follow existing error pattern in other handlers | ✓ |

**User's choice:** You decide — follow existing patterns

---

## Cache Architecture

### Q1: Cache key structure

| Option | Description | Selected |
|--------|-------------|----------|
| Connection-scoped cache | Key: connection_id:name:language | |
| Workspace-level cache | Key: workspace_id:connection_id:name:language | |
| You decide | Align with connection-scoped model | ✓ |

**User's choice:** You decide — align with connection-scoped ownership

### Q2: Invalidation strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Webhook-driven only | Cache fresh via message_template_status_update webhooks, manual sync as escape hatch | ✓ |
| Webhook + periodic refresh | Webhooks plus background sync every 6 hours | |
| Webhook + TTL | Cache entries expire, lazy refresh on read | |
| You decide | Webhook + manual sync fallback | |

**User's choice:** Webhook-driven invalidation only

### Q3: Cache warmup

| Option | Description | Selected |
|--------|-------------|----------|
| Lazy load | Cache starts empty, populates on first access | |
| Eager warm on startup | Load all templates from PostgreSQL into memory at boot | |
| You decide | Pick for expected template count | ✓ |

**User's choice:** You decide — noted that 100k message bursts are possible, cold-start misses could hammer DB

### Q4: Sync rate limit scope

| Option | Description | Selected |
|--------|-------------|----------|
| Per-workspace | One sync per workspace per 15 min | |
| Per-connection | Each connection syncs independently every 15 min | ✓ |
| You decide | Follow per-connection scoping | |

**User's choice:** Per-connection rate limit

---

## Admin UI Template Builder

### Q1: Form richness

| Option | Description | Selected |
|--------|-------------|----------|
| Full structured form | Dedicated sections for HEADER/BODY/FOOTER/BUTTONS, like Meta's builder | ✓ |
| Simplified form | Minimal fields + raw JSON editor for advanced components | |
| You decide | Build for operator console | |

**User's choice:** Full structured form

### Q2: Form location

| Option | Description | Selected |
|--------|-------------|----------|
| Inline page | New page at /templates/new or /templates/{id}/edit | ✓ |
| Slide-over panel | Drawer slides in from right | |
| Modal dialog | Large modal overlay | |
| You decide | Follow existing pattern for complex forms | |

**User's choice:** Inline page

### Q3: Template listing

| Option | Description | Selected |
|--------|-------------|----------|
| Color-coded status badges | Table with APPROVED(green)/PENDING(amber)/REJECTED(red)/PAUSED(gray)/DISABLED(gray-strikethrough), filterable | ✓ |
| Card grid | Each template as a card | |
| You decide | Match existing listing pattern | |

**User's choice:** Color-coded status badges in table

### Q4: Navigation placement

| Option | Description | Selected |
|--------|-------------|----------|
| Grouped under connection | Connections → [connection] → Templates | ✓ |
| Top-level Templates page | /templates with connection filter dropdown | |
| You decide | Fit existing nav structure | |

**User's choice:** Grouped under connection

### Q5: Multi-locale variants

| Option | Description | Selected |
|--------|-------------|----------|
| Tabbed locale variants | Language tab bar in creation form | |
| Sequential creation | One language at a time, Add Language button | |
| You decide | Design based on Meta's model | ✓ |

**User's choice:** You decide — based on Meta's name+language model

### Q6: Quality alerts

| Option | Description | Selected |
|--------|-------------|----------|
| Toast notification | Standard toast for quality changes | |
| Dedicated alerts section | Quality alerts panel on template list | |
| You decide | Use existing toast pattern | ✓ |

**User's choice:** You decide — use existing toast pattern

### Q7: Rejection reason display

| Option | Description | Selected |
|--------|-------------|----------|
| Inline rejection display | Rejection reason in list row (expandable) + detail page | ✓ |
| You decide | Make clearly visible wherever appropriate | |

**User's choice:** Inline rejection display

---

## Visual Template Previewer

### Q1: Preview interaction

| Option | Description | Selected |
|--------|-------------|----------|
| Live side-panel preview | WhatsApp chat bubble to right of form, updates in real-time | ✓ |
| Preview on separate action | Preview button opens modal | |
| You decide | Appropriate for templ+HTMX stack | |

**User's choice:** Live side-panel preview

### Q2: Implementation approach

| Option | Description | Selected |
|--------|-------------|----------|
| HTMX debounced partial | hx-trigger="input changed delay:300ms", server-side render | |
| Client-side JS preview | Vanilla JS reads form, updates DOM directly | |
| You decide | Best UX within templ+HTMX constraints | ✓ |

**User's choice:** You decide

### Q3: Parameter interpolation

| Option | Description | Selected |
|--------|-------------|----------|
| Sample values | {{1}} → 'John', {{2}} → '12345', with optional custom samples | ✓ |
| Placeholder markers | Raw {{1}} as styled pill badges | |
| You decide | Best look in WhatsApp-style bubble | |

**User's choice:** Sample values with optional custom input

### Q4: Visual fidelity

| Option | Description | Selected |
|--------|-------------|----------|
| Full WhatsApp styling | Green bubble, WhatsApp typography, pixel-match | |
| Simplified chat bubble | Captures essence, consistent with PerGo's Notion aesthetic | ✓ |
| You decide | Balance WhatsApp-style with design system | |

**User's choice:** Simplified chat bubble, consistent with PerGo's design

---

## Agent's Discretion

- D-02: Edit/delete mutation logic (follow Meta's API constraints)
- D-04: Error response format (follow existing handler patterns)
- D-05: Cache key structure (connection-scoped)
- D-07: Cache warmup strategy (100k msg burst context → likely eager)
- D-13: Multi-locale variant UX (Meta's name+language model)
- D-14: Quality alert delivery (toast, existing pattern)
- D-17: Preview rendering approach (HTMX partial vs client-side JS)

## Deferred Ideas

None — discussion stayed within phase scope
