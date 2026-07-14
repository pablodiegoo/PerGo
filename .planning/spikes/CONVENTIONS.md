# Spike Conventions

Patterns and design choices established across spike sessions.

## Stack

### Backend
- **Database:** PostgreSQL (with `pgcrypto` for transparent credentials encryption).
- **Language:** Go 1.25+ with Echo v5 (router) and pgx/v5 (database connectivity).

### Admin UI
- **Styling:** Tailwind CSS (CDN) + DaisyUI v4 (CDN) — matches the real admin panel stack.
- **Interactivity:** HTMX 2.x for all server-driven fragment updates (partial replacement, polling, form submits).
- **Templates:** a-h/templ for type-safe HTML components.
- **UI spikes** are self-contained HTML files with CDN dependencies — no build step, open directly in browser.

## Structure
- Spike artifacts are stored under `.planning/spikes/<NNN>-<name>/` containing a `README.md` and the core source files.
- Local compose-based containers run PostgreSQL on port `5433` and NATS on port `4222`.
- UI spikes: single `index.html` with mock data, open with `xdg-open` or directly in browser.

## Patterns

### Backend
- **Unified Connections Table:** Consolidate `devices` and `channel_credentials` into a single `connections` table, using a globally unique `sender_identity` column as the business routing key.
- **Dynamic Instance Routing:** Keep dispatchers statically registered. The worker/API passes connection ID/identities in the payload; dispatchers resolve credentials from DB or in-memory sessions at dispatch time.
- **API `from` Field Routing:** `POST /api/v1/messages` resolves the target connection using the `from` parameter. Falls back to `is_default = true` connection for the channel.
- **Conversational Sessions:** Persist bidirectional conversation states in a `recipient_sessions` table mapping unique composite keys `(workspace_id, recipient_phone, channel, recipient_identity)`.
- **Durable Webhook Queueing:** Dispatch webhooks asynchronously via NATS JetStream to avoid blocking protocol socket reader loops.
- **Payload Signing:** Enforce security by computing an HMAC-SHA256 signature using the workspace secret, transmitted via `X-PerGo-Signature` in the format `t=<timestamp>,v1=<signature>`.
- **Messaging Flow Verbs:** Client webhooks can return declarative JSON arrays containing reply/wait/forward commands to offload complex routing states from client logic.
- **Selective Logging compliance:** If save message bodies is disabled, scrub PII (message text/media link content) from database audit entries and webhooks, retaining metadata indicators.
- **Omnichannel Contact Merging:** Unify distinct channel endpoints under unified Contact profiles, providing atomic merge methods to consolidate channel threads.


### Admin UI (HTMX)
- **Partial replacement pattern:** `hx-get="/fragment" hx-target="#panel-id" hx-swap="innerHTML"` — used for all panel transitions (chat open, filter change). Sidebar and surrounding layout stay in place.
- **Polling — append-only:** `hx-trigger="every 3s" hx-swap="beforeend scroll:bottom"` — for chat message updates. Server returns only new items; empty response = no DOM change.
- **Polling — full replace:** `hx-trigger="every 5s" hx-swap="innerHTML"` — for conversation list updates (simpler, acceptable for list refreshes).
- **ID cursor for polling:** Always pass the last-seen row UUID as `?after=<id>`, never a timestamp — avoids clock skew race conditions in concurrent writes.
- **Toast for background events:** Fixed top-center toast (auto-dismiss 3.5s) for events in non-active views. HX-Trigger response header can fire client-side JS events.
- **Three-column inbox layout:** sidebar (220px, sticky) | list panel (300px) | main panel (flex-1). All managed via CSS flexbox with `overflow: hidden` on the root and `overflow-y: auto` on scrollable inner panels.
- **Nested Configurations Sidebar:** The left navigation sidebar exposes three top-level navigation hubs: Visão Geral, Inbox, and Configurações. The Configurações hub contains an inline, collapsible submenu with options for Conexões, Logs, Workspaces, Webhooks, and Telemetry.
- **Settings Submenu Persistence:** If the active page corresponds to any of the settings sub-options, the settings submenu is rendered in an expanded state (max-height set, arrow chevron rotated 180 degrees) on page load. Otherwise, it defaults to collapsed.
- **Unified Settings Page Layouts:** Redundant top tab switchers (e.g., Workspace/Webhooks/Telemetry) are removed from settings sub-pages since the sidebar serves this purpose. All configuration page headers share a uniform typography hierarchy and button spacing.
