---
phase: 10-inbox-refactoring-connection-unification
plan: 02
subsystem: admin-ui
tags: [go, templ, htmx, postgres, admin, connections]

requires:
  - phase: 10-inbox-refactoring-connection-unification
    plan: 01
    provides: Out-of-band cursor swaps and stable message polling

provides:
  - Repurposed DeviceHandler with ConnectionRepository support
  - Unified connections listing (`GET /admin/devices`) rendering WABA, Telegram, and WhatsApp Web in one place
  - Modal-based connection creation form (`POST /admin/devices/create`) for Telegram and WABA
  - Real-time connection testing modal (`GET /admin/devices/test`) with test outbound trigger (`POST /admin/devices/test`)
  - WebSocket streaming endpoint (`GET /admin/devices/test/ws`) that feeds live NATS logs to the test modal
  - Disconnect/Delete handler (`DELETE /admin/devices/:id`) calling repository connection delete
  - Decommissioned Developer Playground, removing all routes, handlers, templates, and links

affects: [10-inbox-refactoring-connection-unification]

tech-stack:
  added: []
  patterns: [WebSocket logs streaming, Unified multi-channel table layout]

key-files:
  created: []
  modified:
    - internal/api/handler/admin/device.go
    - templates/pages/devices.templ
    - templates/layout/sidebar.templ
    - cmd/pergo/main.go
    - internal/api/handler/admin/device_test.go
  deleted:
    - internal/api/handler/admin/playground.go
    - templates/pages/playground.templ

key-decisions:
  - "Repurposed DeviceHandler to be the general connections controller to reuse the existing devices namespace/route structure with minimal disruption."
  - "Decommissioned playground.go completely and removed the playground option from the sidebar to streamline the console's UI."
  - "Added /admin/devices/test/ws WebSocket endpoint using coder/websocket that subscribes to messages.> and inbound/webhooks topics to stream live dispatch logs during connectivity tests."
  - "Unified connections table listing page to accept a Connections slice rather than active whatsmeow sessions."

patterns-established:
  - "Connection testing logs streaming over WebSocket from NATS topics directly to modal views."

requirements-completed:
  - repurpose_device_handler_connections
  - refactor_devices_template_connections
  - decommission_developer_playground
  - update_devices_handler_tests

coverage:
  - id: U1
    description: "DeviceHandler.List queries ConnectionRepository for workspace connections"
    requirement: repurpose_device_handler_connections
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false
  - id: U2
    description: "DeviceListPage accepts connection slices and renders WABA/Telegram/WhatsApp rows"
    requirement: refactor_devices_template_connections
    verification:
      - kind: build
        ref: "templ generate"
        status: pass
    human_judgment: true
  - id: U3
    description: "Playground route, handlers, and sidebar navigation links are retired"
    requirement: decommission_developer_playground
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: true
  - id: U4
    description: "Connection deletion/disconnect calls Connections repository Delete method"
    requirement: repurpose_device_handler_connections
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false
  - id: U5
    description: "WebSocket endpoint streams logs from NATS and renders row template"
    requirement: repurpose_device_handler_connections
    verification:
      - kind: build
        ref: "go test -v ./internal/api/handler/admin -run TestDevice"
        status: pass
    human_judgment: false
