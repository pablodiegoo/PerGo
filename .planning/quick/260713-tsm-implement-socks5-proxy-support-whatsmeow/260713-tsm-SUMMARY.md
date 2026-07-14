---
quick_id: "260713-tsm"
status: complete
---

# Summary: Implement SOCKS5 proxy support in whatsmeow

We have implemented SOCKS5/HTTP proxy support in the `whatsmeow` client bootstrap and pairing flow.

## Actions taken:
1. **Task 1: Propagate Proxy URL in Session Manager & QR Pairing Flow**
   - Updated [internal/session/manager.go](file:///home/pablo/Coding/PerGo/internal/session/manager.go) in `reconnectDevice` to set the `ProxyURL` field in the client config when reconnecting devices.
   - Updated [internal/session/qr.go](file:///home/pablo/Coding/PerGo/internal/session/qr.go) to accept `proxyURL string` in `StartPairing` and `onPairingSuccess`. Read `proxyURL` from the database if re-pairing and not supplied, and stored it in the database connection record.

2. **Task 2: Expose Proxy URL in Admin API Handlers & UI Modal**
   - Updated [internal/api/handler/admin/device.go](file:///home/pablo/Coding/PerGo/internal/api/handler/admin/device.go) to parse `proxy_url` form value in `StartPairing` and pass it to `Manager.StartPairing`.
   - Updated [templates/pages/devices.templ](file:///home/pablo/Coding/PerGo/templates/pages/devices.templ) to add a "Proxy URL (Opcional)" input field to the WhatsApp Web pairing fields in the device creation modal, making sure the field is not required.

3. **Task 3: Adjust Test Callers & Write Integration Test**
   - Updated test callers ([internal/session/limit_test.go](file:///home/pablo/Coding/PerGo/internal/session/limit_test.go)) to match the new signature of `StartPairing`.
   - Wrote an integration test in [internal/channel/whatsapp/proxy_test.go](file:///home/pablo/Coding/PerGo/internal/channel/whatsapp/proxy_test.go) using a mock SOCKS5 proxy server to verify that `whatsmeow` client traffic traverses configured proxy credentials correctly.
   - Regenerated templates ([templates/pages/devices_templ.go](file:///home/pablo/Coding/PerGo/templates/pages/devices_templ.go)) and ran the tests.

## Commits:
- `eba1b3b` - feat: propagate proxy URL in session manager and QR pairing flow
- `1456cc7` - feat: expose proxy URL in admin API handler and UI modal
- `da25e9b` - test: adjust test callers and add SOCKS5 integration test
