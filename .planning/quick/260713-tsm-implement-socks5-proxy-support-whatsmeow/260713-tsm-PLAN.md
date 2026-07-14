---
quick_id: "260713-tsm"
slug: "implement-socks5-proxy-support-whatsmeow"
type: quick
status: executing
---

# Plan: Implement SOCKS5/HTTP Proxy Support for WhatsMeow

## Objective
Evaluate and implement proxy configuration within the `whatsmeow` client bootstrap and pairing flow. This propagates `proxy_url` from the database connections through to `whatsmeow` clients and enables user entry of custom proxy URLs via the admin console interface.

## Tasks
1. **Task 1: Propagate Proxy URL in Session Manager & QR Pairing Flow**
   - Update `internal/session/manager.go` in `reconnectDevice` to set the `ProxyURL` field in the client config when reconnecting devices.
   - Update `internal/session/qr.go` to accept `proxyURL string` in `StartPairing` and `onPairingSuccess`. Read `proxyURL` from the database if re-pairing and not supplied. Store the proxy URL in the database connection record.
2. **Task 2: Expose Proxy URL in Admin API Handlers & UI Modal**
   - Update `internal/api/handler/admin/device.go` to parse `proxy_url` form value in `StartPairing` and pass it to `Manager.StartPairing`.
   - Update `templates/pages/devices.templ` to add a "Proxy URL (Opcional)" input field to the WhatsApp Web pairing fields in the device creation modal.
3. **Task 3: Adjust Test Callers & Write Integration Test**
   - Update `internal/session/limit_test.go`, `internal/session/qr_test.go`, and `internal/api/handler/admin/device_test.go` to match the updated `StartPairing` signature (passing empty string `""` for proxy URL).
   - Write an integration test in `internal/channel/whatsapp/proxy_test.go` using a mock SOCKS5 proxy server to verify that `whatsmeow` client traffic traverses configured proxy credentials correctly.
   - Regenerate templates and run tests to verify all pass.
