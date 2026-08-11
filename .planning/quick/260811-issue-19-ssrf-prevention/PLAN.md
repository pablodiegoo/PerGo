---
task: 03 — SSRF prevention for outbound HTTP
issue: 19
---
# Quick Plan: Issue #19 SSRF Prevention for Outbound HTTP

<task>
<files>
- internal/platform/netpolicy/netpolicy.go
- internal/platform/netpolicy/netpolicy_test.go
</files>
<action>
1. Create `internal/platform/netpolicy/netpolicy.go` with `NewPublicHTTPClient` and IP validation logic.
2. Define `ErrRestrictedIP = errors.New("restricted IP address blocked by netpolicy")`.
3. Implement IP checks at `ControlContext` TCP level for private (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16), loopback (127.0.0.0/8, ::1), link-local (169.254.0.0/16, fe80::/10), CGNAT (100.64.0.0/10), multicast, and unspecified (0.0.0.0, ::) addresses, handling IPv4-mapped IPv6 addresses via `.Unmap()`.
4. Support functional options `WithAllowedIPs` / `WithAllowlist` and `WithTimeout` for configurable allowlist override.
5. Create `internal/platform/netpolicy/netpolicy_test.go` covering blocked ranges, public IP passthrough, allowlist overrides, and HTTP client integration.
</action>
<verify>
<automated>
go test -v ./internal/platform/netpolicy/...
</automated>
</verify>
<done>
- NewPublicHTTPClient returns an *http.Client with ControlContext TCP level SSRF filtering
- Private, loopback, link-local, CGNAT, multicast, and unspecified IPs are blocked
- Allowlist options override blocked ranges when explicitly specified
- Unit tests pass for netpolicy package
</done>
</task>
