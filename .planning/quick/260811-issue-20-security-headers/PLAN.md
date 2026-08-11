---
task: 04 — Security headers middleware for Echo
issue: 20
---
# Quick Plan: Issue #20 Security Headers Middleware for Echo

<task>
<files>
- internal/api/middleware/security.go
- internal/api/middleware/security_test.go
- internal/platform/echo/echo.go
</files>
<action>
1. Create `internal/api/middleware/security.go` defining `SecurityConfig` struct with configurable security headers:
   - X-Content-Type-Options: default "nosniff"
   - X-Frame-Options: default "DENY"
   - X-XSS-Protection: default "1; mode=block"
   - Strict-Transport-Security (HSTS): default "max-age=31536000; includeSubDomains"
   - Referrer-Policy: default "strict-origin-when-cross-origin"
   - Content-Security-Policy (CSP): default "" (optional/customizable)
2. Provide `SecurityHeaders()` (default config) and `SecurityHeadersWithConfig(config SecurityConfig)` middleware functions.
3. Wire `apimiddleware.SecurityHeaders()` into `echosrv.New()` in `internal/platform/echo/echo.go`.
4. Create `internal/api/middleware/security_test.go` covering default security headers, custom config overrides, skipped headers, and handler execution.
</action>
<verify>
<automated>
go test -v ./internal/api/middleware/... ./internal/platform/echo/...
</automated>
</verify>
<done>
- SecurityHeaders middleware applies standard security headers (nosniff, DENY, HSTS, Referrer-Policy, XSS protection)
- SecurityHeadersWithConfig allows setting custom header values or skipping specific headers
- echosrv.New() wires SecurityHeaders middleware by default
- Unit tests pass for middleware and echosrv packages
</done>
</task>
