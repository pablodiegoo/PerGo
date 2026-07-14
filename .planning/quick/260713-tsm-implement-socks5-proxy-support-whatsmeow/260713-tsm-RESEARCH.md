# Research: SOCKS5/HTTP Proxy Support for WhatsMeow

## Executive Summary
This document provides actionable research and implementation details for integrating SOCKS5/HTTP proxy support within the `whatsmeow` WhatsApp Web client manager. The database connection consolidation (migration `012`) and `Connection` models already support the `proxy_url` field. We detail how to propagate this configuration through the pairing flow and session manager, address common pitfalls, and lay out an offline integration test using a mock SOCKS5 proxy server.

## 1. Primary Source Investigations (whatsmeow Proxy APIs)
`whatsmeow` provides native support for setting proxy endpoints using its `SetProxyAddress` client method:
```go
func (cli *Client) SetProxyAddress(addr string, opts ...SetProxyOptions) error
```
- **How it works:** Under the hood, `SetProxyAddress` parses the address string and invokes `SetProxy(httpProxyFunc)` for `http`/`https` schemes, or `SetSOCKSProxy(socksDialer)` for `socks5` schemes.
- **Authentication:** It natively parses username and password credentials encoded directly inside the URI (e.g. `socks5://user:pass@host:port` or `http://user:pass@host:port`).
- **Timing Constraint:** The proxy configuration *must* be set on the client before calling `client.Connect()`. Any proxy configuration change applied after connection will not affect the active socket; the connection must be disconnected, the proxy updated, and then connected.

## 2. Common Pitfalls & Mitigations
- **Pitfall: Environment Variables Override.** By default, if no proxy is configured, whatsmeow reads proxy settings from standard environment variables (like `HTTP_PROXY`, `HTTPS_PROXY`).
  - *Mitigation:* Ensure that if a connection specifies no proxy URL, we explicitly call `client.SetProxy(nil)` to override any global/environment-level proxy variables.
- **Pitfall: SOCKS5 DNS Leakage / Failures.** Default SOCKS5 dialers in Go might resolve DNS queries locally rather than forwarding them to the proxy server.
  - *Mitigation:* `whatsmeow` handles DNS resolution safely when configured via `SetProxyAddress` as it uses `golang.org/x/net/proxy.FromURL` for SOCKS5, which uses remote DNS resolution by default.
- **Pitfall: Blocking Handshakes.** If the proxy server goes offline or becomes unresponsive, the WebSocket handshake inside `client.Connect()` will block.
  - *Mitigation:* Run the connection flow within a context that has a sensible timeout (e.g. 30 seconds) to prevent goroutine leaks.

## 3. Integration Plan & Affected Files

The database schema (migration `012_consolidate_connections.sql`) and connection model (`internal/repository/connection.go`) already contain the `proxy_url` plain text column mapping. We only need to wire the value through the initialization and pairing code.

### 3.1. `internal/channel/whatsapp/client.go`
The proxy helper `ConfigureProxy` is already implemented:
```go
func ConfigureProxy(client *whatsmeow.Client, proxyStr string) error {
	if proxyStr == "" {
		client.SetProxy(nil)
		return nil
	}
	return client.SetProxyAddress(proxyStr)
}
```
And `NewWhatsAppClient` correctly calls it if `cfg.ProxyURL != ""`.

### 3.2. `internal/session/manager.go`
In `reconnectDevice`, propagate the `ProxyURL` stored in the database connection record to the client configuration:
```diff
 	cfg := whatsapp.ClientConfig{
 		DB:        m.db,
 		WAVersion: m.waVersion,
 	}
+	if d.ProxyURL != nil {
+		cfg.ProxyURL = *d.ProxyURL
+	}
```

### 3.3. `internal/session/qr.go`
1. Update `StartPairing` signature to accept `proxyURL string`:
```go
func (m *Manager) StartPairing(ctx context.Context, workspaceID uuid.UUID, phone string, proxyURL string, existingConnID *uuid.UUID) (<-chan QRPairingEvent, error)
```
2. In `StartPairing`, load the existing connection proxy URL if it is a re-pairing flow and no `proxyURL` was supplied:
```go
	if proxyURL == "" && existingConnID != nil {
		if conn, err := m.repo.GetByID(ctx, *existingConnID); err == nil && conn != nil && conn.ProxyURL != nil {
			proxyURL = *conn.ProxyURL
		}
	}
```
3. Set the proxy URL in `ClientConfig`:
```go
	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
		ProxyURL:  proxyURL,
	}
```
4. Pass `proxyURL` to `onPairingSuccess` (which must be updated to accept the string):
```go
func (m *Manager) onPairingSuccess(ctx context.Context, wc *whatsapp.WhatsAppClient, workspaceID uuid.UUID, phone string, proxyURL string, existingConnID *uuid.UUID) error
```
5. In `onPairingSuccess`, store `proxyURL` when creating or updating the connection:
```go
	var proxyURLVal *string
	if proxyURL != "" {
		proxyURLVal = &proxyURL
	}
	// For UPDATE: set proxy_url = $6
	// For INSERT: set proxy_url = proxyURLVal
```

### 3.4. `internal/api/handler/admin/device.go`
1. In `StartPairing` HTTP handler, read the `proxy_url` form value:
```go
	phone := c.FormValue("phone")
	proxyURL := c.FormValue("proxy_url")
```
2. Pass `proxyURL` to `h.Manager.StartPairing`.

### 3.5. `templates/pages/devices.templ`
Add a `Proxy URL` input field inside the WhatsApp Web section of the pairing modal:
```html
					<!-- WhatsApp Web Fields -->
					<div id="fields-whatsapp" class="space-y-4 mb-4">
						@components.BanWarning()
						<div class="form-group">
							<label for="phone" class="block text-sm font-medium text-zinc-700 mb-1">Número de Telefone</label>
							<input type="tel" id="phone" name="phone" required class="form-input w-full border border-zinc-300 rounded-md p-2 focus:outline-none focus:border-zinc-950" placeholder="+5511999999999"/>
						</div>
						<div class="form-group">
							<label for="proxy_url" class="block text-sm font-medium text-zinc-700 mb-1">Proxy URL (Opcional)</label>
							<input type="text" id="proxy_url" name="proxy_url" class="form-input w-full border border-zinc-300 rounded-md p-2 focus:outline-none focus:border-zinc-950" placeholder="socks5://user:pass@host:port ou http://host:port"/>
						</div>
					</div>
```

### 3.6. Compiler Adjustments (Tests)
Ensure other files calling `StartPairing` are updated:
- `internal/session/limit_test.go`
- `internal/session/qr_test.go`
- `internal/api/handler/admin/device_test.go`
Update them to pass an empty string `""` for the `proxyURL` argument.

## 4. Integration Test Plan
To test SOCKS5 proxy support offline without making network connections to WhatsApp servers, we will write a unit test in `internal/channel/whatsapp/proxy_test.go` using a lightweight mock SOCKS5 TCP listener. The listener intercepts the connection request from `whatsmeow` and validates that the proxy configuration is applied.
