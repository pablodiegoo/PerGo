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

### Admin UI (HTMX)
- **Partial replacement pattern:** `hx-get="/fragment" hx-target="#panel-id" hx-swap="innerHTML"` — used for all panel transitions (chat open, filter change). Sidebar and surrounding layout stay in place.
- **Polling — append-only:** `hx-trigger="every 3s" hx-swap="beforeend scroll:bottom"` — for chat message updates. Server returns only new items; empty response = no DOM change.
- **Polling — full replace:** `hx-trigger="every 5s" hx-swap="innerHTML"` — for conversation list updates (simpler, acceptable for list refreshes).
- **ID cursor for polling:** Always pass the last-seen row UUID as `?after=<id>`, never a timestamp — avoids clock skew race conditions in concurrent writes.
- **Toast for background events:** Fixed top-center toast (auto-dismiss 3.5s) for events in non-active views. HX-Trigger response header can fire client-side JS events.
- **Three-column inbox layout:** sidebar (220px, sticky) | list panel (300px) | main panel (flex-1). All managed via CSS flexbox with `overflow: hidden` on the root and `overflow-y: auto` on scrollable inner panels.
