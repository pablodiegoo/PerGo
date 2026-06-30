---
title: Implement SOCKS5/HTTP Proxy Support for WhatsMeow
date: 2026-06-29
priority: medium
---

# Todo: Implement SOCKS5/HTTP Proxy Support for WhatsMeow

## Description
Evaluate and implement proxy configuration within the `whatsmeow` client bootstrap. This is necessary to support outbound traffic routing through dedicated proxy IPs, preventing main server IP blacklisting.

## Action Items
- [ ] Research `whatsmeow` custom dialer configuration. The library allows passing a custom dialer (e.g. `proxy.SOCKS5` or `net/http` proxy dialer) during WebSocket connection.
- [ ] Add `proxy_url` (optional, encrypted) column to the `connections` table in the database schema.
- [ ] Modify `internal/channel/whatsapp/client.go` to parse the `proxy_url` and inject the custom dialer into the client config.
- [ ] Write integration test verifying that a whatsmeow client can connect through a local/mock SOCKS5 proxy server.
