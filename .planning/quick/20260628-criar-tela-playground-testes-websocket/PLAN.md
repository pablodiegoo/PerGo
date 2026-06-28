---
status: incomplete
date: 2026-06-28
description: Create Developer Playground messaging verification screen with real-time HTMX WebSockets
---

# Plan - Developer Playground with WebSockets

Implement a live developer testing screen to view and send messages inside the administration panel.

## Tasks

### 1. Template Layout and Pages
- Create [playground.templ](file:///home/pablo/Coding/OmniGo/templates/pages/playground.templ):
  - `Playground` main page.
  - Sidebar integration.
  - Left panel: **Test Message Dispatcher** (select Workspace, Channel, Destination, Body) -> submits via HTMX to `/admin/playground/send`.
  - Right panel: **Live Event Stream** (connects using `hx-ext="ws"` to `/admin/playground/ws` and has a container `#playground-events` where new logs/messages are appended at the top).
- Modify [sidebar.templ](file:///home/pablo/Coding/OmniGo/templates/layout/sidebar.templ) to add a navigation link to "/admin/playground".

### 2. Handlers and Routes
- Create [playground.go](file:///home/pablo/Coding/OmniGo/internal/api/handler/admin/playground.go):
  - Struct `PlaygroundHandler` with `WorkspaceRepository`, `JetStreamPublisher` and `nats.Conn`.
  - `Page`: Renders the Playground templates.
  - `Send`: Receives form inputs, constructs a `domain.QueueMessage`, generates a trace ID, and publishes to `"messages.outbound"`.
  - `WS`: Upgrades the connection to WebSockets, subscribes to:
    - `messages.>`
    - `inbound.events.>`
    - `webhooks.events`
    and pushes serialized HTML representation of the events to the websocket client.
- Register routes in [main.go](file:///home/pablo/Coding/OmniGo/cmd/omnigo/main.go):
  - `GET /admin/playground`
  - `POST /admin/playground/send`
  - `GET /admin/playground/ws`

### 3. Verification
- Compile the code and run the test suite (`make test-race`).
- Ensure the assets and WS connections work.
