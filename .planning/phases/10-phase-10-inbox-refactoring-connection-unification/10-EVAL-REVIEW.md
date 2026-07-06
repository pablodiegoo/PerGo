# EVAL-REVIEW — Phase 10: Inbox Refactoring & Connection Unification

**Audit Date:** 2026-07-06
**AI-SPEC Present:** No — audit conducted against general best practices
**Overall Score:** 42/100
**Verdict:** SIGNIFICANT GAPS

---

> **Context:** Phase 10 is a Go backend + HTMX admin UI phase. There are no LLM/generative AI components.
> The applicable evaluation framework is **systems evaluation** (correctness, contract validity, behavioral safety,
> integration coverage), not model evaluation. Dimensions are drawn from `ai-evals.md`'s applicable categories
> reframed for a messaging platform: output structure validity, task completion, tool use correctness, policy
> compliance, and production monitoring.

---

## Dimension Coverage

| # | Dimension | Status | Measurement | Finding |
|---|-----------|--------|-------------|---------|
| 1 | **Output structure validity** — HTTP response schemas (204 vs 200, OOB HTML, NATS payload) | COVERED | Code-based (inbox_test.go: `PollMessages_NoContent`, `PollMessages_NewMessages`, `QueueMessage_Serialization`) | ✅ 204/200 response contracts tested; OOB anchor verified; QueueMessage round-trip tested |
| 2 | **Task completion** — WABA 24h window enforcement | COVERED | Code-based (`TestInboxHandler_WabaWindowCheck` — 5 table cases, within/outside/nil) | ✅ Business rule fully covered with table-driven tests |
| 3 | **Tool use correctness** — Template NATS publish | PARTIAL | Code-based (`TestInboxHandler_NewMessageSend_Template`) | ⚠️ Test asserts serialization only; does not verify actual `Publisher.Publish()` is called on the live handler path (mock publisher not wired — tests the domain struct, not the HTTP handler) |
| 4 | **Output structure validity** — Connection dashboard unified listing | PARTIAL | Build-only (`go build ./...`, `templ generate`) | ⚠️ DeviceHandler.List and connections table verified only by build success. No HTTP handler test asserts that `ConnectionRepository.ListByWorkspace` is called with correct workspace ID |
| 5 | **Policy compliance** — WhatsApp Web ban warning visibility | MISSING | None found | ❌ Ban warning in the creation modal is a UI-only compliance requirement; no test or spec verifies it renders for the `whatsapp_web` channel selection |
| 6 | **Task completion** — Playground decommission (route removal) | MISSING | Build-only | ❌ No test asserts that `GET /admin/playground` returns 404. Build success does not prove route is absent — a misconfigured router could re-expose it |
| 7 | **Safety** — WebSocket endpoint NATS log streaming (no auth bypass) | MISSING | None found | ❌ `/admin/devices/test/ws` WebSocket endpoint has no test verifying authentication is enforced; an unauthenticated WS connection could read internal NATS dispatch logs |
| 8 | **Production monitoring / observability** — Debug server pprof/expvar | PARTIAL | `obs_test.go` + `cmd/pergo/main.go` | ⚠️ `obs.StartDebugServer` is present and tested; however no expvar counter is updated for Phase 10 paths (inbox poll count, WABA blocked rate, template sends). Metrics exist but do not capture new business-critical flows |

**Coverage Score:** 4/8 (50%)

---

## Infrastructure Audit

| Component | Status | Finding |
|-----------|--------|---------|
| Eval tooling | Partial | `go test ./... -short` in Makefile. `make test-race` exists. No external eval framework (Promptfoo, Braintrust) — appropriate for a Go backend. Makefile targets present but not wired to CI |
| Reference dataset | Missing | No reference test fixtures for messaging flows. DB integration tests use live Postgres (ports 5433/5432 fallback) with no seed dataset. No `testcontainers` usage — tests skip when Postgres is unavailable |
| CI/CD integration | Missing | No `.github/workflows/` directory exists. `make test` and `make lint` are Makefile targets only — not automated on push/PR |
| Online guardrails | Partial | Rate limiting middleware exists (`internal/api/middleware/ratelimit.go`, tested). WABA 24h window check is implemented in handler. WebSocket endpoint auth is not verified. Queue backpressure exists in JetStream layer |
| Tracing | Partial | `pprof` + `expvar` debug server present and wired. `slog` structured logging used throughout. No trace propagation into new Phase 10 handler paths (WabaWindowCheck, NewMessageSend) — no `slog.Info` calls with `trace_id` in these code paths verified |

**Infrastructure Score:** 30/100

---

## Critical Gaps

### GAP-01 — BLOCKER: No CI/CD automation
**Dimension:** All
**Severity:** BLOCKER
**Detail:** No `.github/workflows/` directory. Every test and lint gate is manual. Any commit can silently break contracts without automated enforcement.

### GAP-02 — BLOCKER: WebSocket endpoint authentication not tested
**Dimension:** Safety
**Severity:** BLOCKER
**Detail:** `GET /admin/devices/test/ws` streams live NATS messages. No test or audit verifies that an unauthenticated request is rejected. The admin auth middleware wraps this route in `cmd/pergo/main.go` (per SUMMARY), but no test probes this endpoint without a valid session cookie. An auth misconfiguration would expose internal dispatch logs.

### GAP-03 — BLOCKER: Playground route decommission not asserted
**Dimension:** Policy compliance
**Severity:** BLOCKER
**Detail:** SUMMARY confirms `playground.go` was deleted and routes removed. Build passes. But no test asserts `GET /admin/playground` returns 404. A future refactor could silently re-expose this route.

### GAP-04 — WARNING: Template send does not test actual HTTP handler
**Dimension:** Tool use correctness
**Severity:** WARNING
**Detail:** `TestInboxHandler_NewMessageSend_Template` tests `domain.QueueMessage` struct serialization directly. It does not instantiate `InboxHandler` with a mock `Publisher` and fire `POST /admin/inbox/new-message-send`. The HTTP parsing path (form params → QueueMessage → Publish) is not covered.

### GAP-05 — WARNING: No reference dataset / seeded test fixtures
**Dimension:** Task completion
**Severity:** WARNING
**Detail:** DB integration tests for inbox polling connect to live Postgres using port-fallback heuristics. When neither port is available, these tests silently skip. No `testcontainers` or seed fixture approach guarantees deterministic coverage in CI.

---

## Remediation Plan

### Must fix before production:

**1. Add GitHub Actions CI workflow** (GAP-01)

```yaml
# .github/workflows/ci.yml
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env: { POSTGRES_PASSWORD: postgres }
      nats:
        image: nats:2.10-alpine
        args: ["-js"]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go test ./... -race -count=1
      - run: golangci-lint run
```

**2. Add WebSocket auth test** (GAP-02)

```go
// internal/api/handler/admin/device_test.go
func TestDeviceHandler_WS_RequiresAuth(t *testing.T) {
    // Fire GET /admin/devices/test/ws without session cookie
    // Assert 401 or redirect, NOT 101 Switching Protocols
}
```

**3. Assert playground route returns 404** (GAP-03)

```go
// cmd/pergo/admin_test.go
func TestPlaygroundRouteDecommissioned(t *testing.T) {
    resp := testServer.GET("/admin/playground")
    assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
```

### Should fix soon:

**4. Add HTTP handler test for NewMessageSend** (GAP-04)

```go
func TestInboxHandler_NewMessageSend_HTTP(t *testing.T) {
    // Use echotest.NewRequest, inject mock Publisher
    // POST form with template_name=welcome_optin&param_1=Carlos
    // Assert Publisher.Publish was called once with correct TemplateName
}
```

**5. Switch DB integration tests to testcontainers** (GAP-05)

```go
func TestMain(m *testing.M) {
    ctx := context.Background()
    pg, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{Image: "postgres:16", ...},
        Started: true,
    })
    // set DSN, run migrations, run m.Run()
}
```

### Nice to have:

- **slog trace_id** — Instrument `WabaWindowCheck`, `NewMessageSend`, and `DeviceHandler.WS` with `slog.Info("...", "trace_id", traceID)` so new flows appear in structured logs.
- **expvar counters** — `waba_blocked_count`, `template_sends_total`, `ws_connections_active` — pairs with existing pprof/expvar debug server.
- **Ban warning smoke test** — A simple templ unit test or snapshot asserting the WABA channel option renders the ban warning string.

---

## Files Found

**Test files relevant to Phase 10:**
- `internal/api/handler/admin/inbox_test.go` — 13 test functions (PollMessages, WabaWindowCheck, NewMessageSend, etc.)
- `internal/api/handler/admin/device_test.go` — 4 test functions (Construction, GetQR, DatabaseFlows, StartPairing)

**Eval/observability infrastructure:**
- `internal/api/middleware/ratelimit.go` + `ratelimit_test.go` — Rate limiting guardrail
- `internal/platform/queue/jetstream.go` + `jetstream_test.go` — Backpressure (MaxMsgs/DiscardNew)
- `cmd/pergo/obs_test.go` — pprof/expvar debug server test
- `Makefile` — `test`, `test-race`, `lint` targets (manual only)

**Missing:**
- `.github/workflows/*.yml` — No CI/CD pipeline
- `testcontainers` usage — None found
- Promptfoo/Braintrust/RAGAS — None (not applicable for this system type)
- Reference test dataset — None found
