# Quick Task Summary: Issue #19 SSRF Prevention for Outbound HTTP

## Overview
Implemented `internal/platform/netpolicy` to prevent SSRF (Server-Side Request Forgery) attacks on outbound HTTP requests.

## Deliverables
- `internal/platform/netpolicy/netpolicy.go`: Exports `NewPublicHTTPClient`, `RestrictedIPChecker`, `ErrRestrictedIP`, and options (`WithAllowedIPs`, `WithAllowlist`, `WithTimeout`).
- Intercepts destination IP addresses at the TCP level via `net.Dialer.ControlContext` prior to socket connection (`SYN`).
- Blocks private, loopback, link-local, CGNAT (`100.64.0.0/10`), multicast, and unspecified IP ranges.
- Handles IPv4-mapped IPv6 address unmapping (`.Unmap()`).
- Supports configurable allowlists bypassing restriction for explicit IPs/CIDRs.
- `internal/platform/netpolicy/netpolicy_test.go`: Unit tests covering all blocked IP categories, public IP passthrough, allowlist overrides, and `http.Client` execution.
