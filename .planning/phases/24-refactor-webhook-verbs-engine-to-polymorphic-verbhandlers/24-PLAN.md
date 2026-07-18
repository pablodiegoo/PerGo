---
phase: 24
plan: 1
type: refactor
wave: 1
depends_on: []
files_modified:
  - internal/webhook/verbs.go
  - internal/webhook/verb_handlers.go
  - internal/webhook/verb_handlers_test.go
autonomous: true
requirements: []
---

# Phase 24, Plan 1: Extract Polymorphic VerbHandlers

<objective>
Refactor the monolithic `VerbsEngine.Execute` switch block in `internal/webhook/verbs.go` into a polymorphic `VerbHandler` interface with 6 discrete handler implementations (`replyHandler`, `waitHandler`, `forwardHandler`, `tagHandler`, `closeHandler`, `pauseBotHandler`). Handlers are statically wired in the `NewVerbsEngine` constructor via a `map[string]VerbHandler`. The `Execute` loop delegates to handlers via map lookup. The constructor signature and `Execute` method signature remain unchanged. All existing integration tests must pass without modification.
</objective>

<tasks>

## Task 1: Define VerbHandler interface and VerbContext struct in verbs.go

<read_first>
- internal/webhook/verbs.go (lines 1-91 — struct definitions, constructor, types)
- internal/webhook/dispatcher.go (lines 22-37 — interface definition patterns in webhook package)
- .planning/phases/24-refactor-webhook-verbs-engine-to-polymorphic-verbhandlers/24-CONTEXT.md (D-03 and D-04)
</read_first>

<action>
Add two new type definitions to `verbs.go`, placed between the existing params structs (after `PauseBotParams`, line ~52) and before the `VerbsEngine` struct:

1. Define `VerbHandler` interface with a single method: `Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error`

2. Define `VerbContext` struct with fields:
   - `WorkspaceID uuid.UUID`
   - `ContactID   uuid.UUID`
   - `TraceID     string`
   - `Event       inbound.InboundEventPayload`

3. Modify the `VerbsEngine` struct to replace the four direct dependency fields (`publisher`, `contactRepo`, `logsRepo`, `resolver`) with:
   - `handlers    map[string]VerbHandler`
   - `contactRepo *repository.ContactRepository` (kept for `ResolveContact` preamble)
   - `logsRepo    *repository.UserActionLogRepository` (kept for `logActionResults`)

4. Update the `NewVerbsEngine` constructor body to:
   - Build 6 handler instances using the received params: `NewReplyHandler(publisher, resolver)`, `NewWaitHandler()`, `NewForwardHandler(publisher, resolver)`, `NewTagHandler(contactRepo)`, `NewCloseHandler(contactRepo)`, `NewPauseBotHandler(contactRepo)`
   - Wire them into `map[string]VerbHandler{"reply": ..., "wait": ..., "forward": ..., "tag": ..., "close": ..., "pause_bot": ...}`
   - Return `&VerbsEngine{handlers: handlers, contactRepo: contactRepo, logsRepo: logsRepo}`
   - The constructor parameter list `(publisher outbound.Publisher, contactRepo *repository.ContactRepository, logsRepo *repository.UserActionLogRepository, resolver outbound.RouteResolver)` MUST NOT change.

5. Refactor the `Execute` method loop (currently lines 121-275) to:
   - Before the loop, build a `VerbContext{WorkspaceID: wsID, ContactID: contactID, TraceID: task.TraceID, Event: evt}`
   - In the loop, replace the switch block with: look up `handler, ok := e.handlers[verb.Action]`
   - If `!ok`, set log status to "failed" with error "unknown action", set `execErr = fmt.Errorf("verb %d unknown action: %s", i, verb.Action)`, append log, break
   - If found, call `err := handler.Execute(execCtx, vc, verb.Params)`. On error, set `log.Status = "failed"`, `log.Error = err.Error()`, set `execErr = fmt.Errorf("verb %d (%s): %w", i, verb.Action, err)`, append log, break. **CRITICAL:** Use `%w` (not `%s`) so `errors.Is(err, context.Canceled)` traverses the chain — the existing wait handler test relies on this.

6. Delete the `executeReply` method (lines 283-324) and `executeForward` method (lines 326-362) from `verbs.go` — their logic moves into the handler implementations in Task 2.

Keep all params structs (`ReplyParams`, `WaitParams`, `ForwardParams`, `TagParams`, `CloseParams`, `PauseBotParams`), `Verb`, `ExecutedVerbLog`, `VerbsExecutionMetadata`, and `logActionResults` exactly where they are in `verbs.go`.
</action>

<acceptance_criteria>
- `verbs.go` contains `type VerbHandler interface {` with method `Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error`
- `verbs.go` contains `type VerbContext struct {` with fields `WorkspaceID`, `ContactID`, `TraceID`, `Event`
- `VerbsEngine` struct has field `handlers map[string]VerbHandler`
- `VerbsEngine` struct retains `contactRepo *repository.ContactRepository` and `logsRepo *repository.UserActionLogRepository`
- `VerbsEngine` struct does NOT have `publisher` or `resolver` fields
- `NewVerbsEngine` function signature is unchanged: `func NewVerbsEngine(publisher outbound.Publisher, contactRepo *repository.ContactRepository, logsRepo *repository.UserActionLogRepository, resolver outbound.RouteResolver) *VerbsEngine`
- `Execute` method signature is unchanged: `func (e *VerbsEngine) Execute(ctx context.Context, task WebhookDeliveryTask, verbs []Verb) error`
- `Execute` method contains no `switch verb.Action` statement
- `executeReply` and `executeForward` private methods are deleted
- `logActionResults` method is still present and unchanged
- All params structs are still exported and present in `verbs.go`
</acceptance_criteria>

<verify>
- `go build ./internal/webhook/...` compiles (will fail until Task 2 provides handler constructors — verify after Task 2)
</verify>

## Task 2: Implement 6 VerbHandler implementations in verb_handlers.go

<read_first>
- internal/webhook/verbs.go (full file — see current inline logic for each verb case)
- internal/webhook/dispatcher_test.go (lines 291-324 — mockPublisher and mockRouteResolver definitions for testing patterns)
- .planning/phases/24-refactor-webhook-verbs-engine-to-polymorphic-verbhandlers/24-RESEARCH.md (section 2 — verb types inventory, handler-specific logic details)
</read_first>

<action>
Create new file `internal/webhook/verb_handlers.go` with package `webhook`.

Imports needed: `context`, `encoding/json`, `fmt`, `time`, `github.com/google/uuid`, `github.com/pablojhp.pergo/internal/domain`, `github.com/pablojhp.pergo/internal/outbound`, `github.com/pablojhp.pergo/internal/repository`

Implement 6 unexported handler structs with exported constructors:

**1. replyHandler**
- Struct: `type replyHandler struct { publisher outbound.Publisher; resolver outbound.RouteResolver }`
- Constructor: `func NewReplyHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler`
- `Execute` method: unmarshal `ReplyParams` from params. If `publisher == nil || resolver == nil`, return nil (skip). Resolve connection via `resolver.GetBySenderIdentity(ctx, vc.WorkspaceID, vc.Event.To)`, fallback to `resolver.GetDefaultChannelConnection(ctx, vc.WorkspaceID, vc.Event.Channel)`. Build `domain.QueueMessage` with WorkspaceID, conn.ID, conn.SenderIdentity, vc.TraceID, To=vc.Event.From, Channel=vc.Event.Channel, Body=p.Body, QueuedAt=time.Now().UTC(). Marshal and publish to "messages.outbound".
- Error wrapping: unmarshal errors return `fmt.Errorf("invalid params: %w", err)`. Connection resolution errors return `fmt.Errorf("cannot resolve connection for reply: %w", err)`.

**2. waitHandler**
- Struct: `type waitHandler struct{}` (no deps)
- Constructor: `func NewWaitHandler() VerbHandler`
- `Execute` method: unmarshal `WaitParams`. Parse duration. Cap at 10s, clamp negative to 0. `select` on `time.After(d)` vs `ctx.Done()`. On ctx cancel, return `ctx.Err()`.
- Error wrapping: unmarshal errors return `fmt.Errorf("invalid params: %w", err)`. Duration parse errors return `fmt.Errorf("invalid duration '%s': %w", p.Duration, err)`.

**3. forwardHandler**
- Struct: `type forwardHandler struct { publisher outbound.Publisher; resolver outbound.RouteResolver }`
- Constructor: `func NewForwardHandler(publisher outbound.Publisher, resolver outbound.RouteResolver) VerbHandler`
- `Execute` method: unmarshal `ForwardParams`. If `publisher == nil || resolver == nil`, return nil. Resolve default channel connection via `resolver.GetDefaultChannelConnection`. Build `domain.QueueMessage` with To=p.To, Channel=p.Channel, Body=vc.Event.Body. Marshal and publish.
- Error wrapping: same pattern as reply. Connection errors return `fmt.Errorf("cannot resolve default connection for forward channel '%s': %w", channel, err)`.

**4. tagHandler**
- Struct: `type tagHandler struct { contactRepo *repository.ContactRepository }`
- Constructor: `func NewTagHandler(contactRepo *repository.ContactRepository) VerbHandler`
- `Execute` method: unmarshal `TagParams`. Check `vc.ContactID == uuid.Nil` and return `fmt.Errorf("contact not resolved")`. Call `contactRepo.AddTags(ctx, vc.WorkspaceID, vc.ContactID, p.Tags)`.
- Error wrapping: unmarshal errors return `fmt.Errorf("invalid params: %w", err)`. DB errors return `fmt.Errorf("db update failed: %w", err)`.

**5. closeHandler**
- Struct: `type closeHandler struct { contactRepo *repository.ContactRepository }`
- Constructor: `func NewCloseHandler(contactRepo *repository.ContactRepository) VerbHandler`
- `Execute` method: check `vc.ContactID == uuid.Nil` and return `fmt.Errorf("contact not resolved")`. Call `contactRepo.CloseThread(ctx, vc.WorkspaceID, vc.ContactID)`.
- Error wrapping: DB errors return `fmt.Errorf("db update failed: %w", err)`.

**6. pauseBotHandler**
- Struct: `type pauseBotHandler struct { contactRepo *repository.ContactRepository }`
- Constructor: `func NewPauseBotHandler(contactRepo *repository.ContactRepository) VerbHandler`
- `Execute` method: check `vc.ContactID == uuid.Nil` and return `fmt.Errorf("contact not resolved")`. If `len(params) > 0 && string(params) != "null"`, unmarshal `PauseBotParams`. Compute `pausedAt := time.Now().UTC()`. If `p.Duration != ""`, parse duration, compute `pausedAt = time.Now().UTC().Add(-12 * time.Hour).Add(d)`. Call `contactRepo.UpdateBotState(ctx, vc.WorkspaceID, vc.ContactID, false, &pausedAt)`.
- Error wrapping: unmarshal errors return `fmt.Errorf("invalid params: %w", err)`. Duration parse errors return `fmt.Errorf("invalid duration '%s': %w", p.Duration, err)`. DB errors return `fmt.Errorf("db update failed: %w", err)`.

**IMPORTANT error message format**: The `Execute` loop in `verbs.go` wraps handler errors as `fmt.Errorf("verb %d (%s) %s", i, verb.Action, err.Error())`. Handler errors must NOT include the verb index or action name — the engine loop adds those. The existing tests match against specific error patterns; handler errors should only contain the detail portion (e.g., "invalid params:", "contact not resolved", "db update failed:").

**CRITICAL**: Study the error message patterns in the original `verbs.go` switch block carefully. The existing tests in `verbs_test.go` check for `err == nil` or `err != nil` — they do NOT string-match specific error messages. The dispatcher_test.go integration test also only checks `err != nil`. So the exact error wording can change as long as errors are returned when they should be.
</action>

<acceptance_criteria>
- File `internal/webhook/verb_handlers.go` exists with `package webhook`
- Contains 6 unexported structs: `replyHandler`, `waitHandler`, `forwardHandler`, `tagHandler`, `closeHandler`, `pauseBotHandler`
- Contains 6 exported constructors: `NewReplyHandler`, `NewWaitHandler`, `NewForwardHandler`, `NewTagHandler`, `NewCloseHandler`, `NewPauseBotHandler`
- Each constructor returns `VerbHandler` interface type
- `replyHandler` and `forwardHandler` hold `publisher outbound.Publisher` and `resolver outbound.RouteResolver`
- `tagHandler`, `closeHandler`, `pauseBotHandler` hold `contactRepo *repository.ContactRepository`
- `waitHandler` has no dependency fields
- `pauseBotHandler.Execute` preserves the `pausedAt = time.Now().UTC().Add(-12 * time.Hour).Add(d)` offset formula
- `waitHandler.Execute` preserves the 10s cap and negative-to-0 clamp
- `replyHandler.Execute` and `forwardHandler.Execute` return nil when `publisher == nil || resolver == nil`
- `go build ./internal/webhook/...` exits 0
- `go build ./...` exits 0
</acceptance_criteria>

<verify>
- `go build ./internal/webhook/...` exits 0
- `go build ./...` exits 0
- `go vet ./internal/webhook/...` exits 0
</verify>

## Task 3: Verify existing integration tests pass without modification

<read_first>
- internal/webhook/verbs_test.go (full file — all 7 test cases)
- internal/webhook/dispatcher_test.go (lines 326-550 — TestDefaultDispatcher_VerbsIntegration)
- internal/webhook/verbs.go (updated version from Task 1)
- internal/webhook/verb_handlers.go (new file from Task 2)
</read_first>

<action>
Run the full existing test suite for the webhook package. Do NOT modify any test files. The refactoring must be fully transparent to the existing tests.

If any tests fail due to error message formatting changes, adjust the error wrapping format in `verbs.go`'s Execute loop to match the original pattern. The original patterns are:
- `"verb %d (reply) invalid params: %w"` — verb index, action name in parens, specific error detail
- `"verb %d (reply) execution failed: %w"` — verb index, action name, "execution failed"
- `"verb %d (wait) invalid params: %w"` — same pattern
- `"verb %d (wait) invalid duration '%s': %w"` — includes the bad duration value
- `"verb %d (tag) contact resolution failed"` — no wrapped error
- `"verb %d (pause_bot) invalid params: %w"`
- `"verb %d unknown action: %s"` — no parens around action

Since the existing tests only check `err == nil` or `err != nil` (not exact string matches), the wrapping format `"verb %d (%s) %s"` should be sufficient. However, the `context.Canceled` error propagation for the wait handler test MUST be preserved — the test checks `errors.Is(err, context.Canceled)`. The wait handler must return `ctx.Err()` directly (unwrapped) when context is done, and the engine loop must propagate it with `%w` wrapping so `errors.Is` traverses the chain.

Specific concern: The context cancellation test (line 261) uses `errors.Is(err, context.Canceled)`. Currently `execErr` is set to `execCtx.Err()` directly when context is cancelled. In the refactored version, two paths exist:
1. The loop's `select case <-execCtx.Done()` check before handler call — set `execErr = execCtx.Err()` directly (unchanged)
2. The wait handler returns `ctx.Err()` — the loop wraps it as `fmt.Errorf("verb %d (%s) ...: %w", ...)` — must use `%w` so `errors.Is` works

Ensure the loop uses `%w` (not `%s`) for wrapping handler errors so error chain traversal works.
</action>

<acceptance_criteria>
- `go test ./internal/webhook/ -run TestVerbsEngine -v` exits 0 with all 7 subtests passing
- `go test ./internal/webhook/ -run TestDefaultDispatcher_VerbsIntegration -v` exits 0
- `go test ./internal/webhook/ -v` exits 0 (all tests in the package)
- No test files have been modified (verbs_test.go, dispatcher_test.go remain identical to their pre-refactor state)
</acceptance_criteria>

<verify>
- `go test ./internal/webhook/ -v -count=1` exits 0
- `git diff --name-only internal/webhook/verbs_test.go internal/webhook/dispatcher_test.go` shows no changes
</verify>

## Task 4: Add unit tests for individual VerbHandler implementations

<read_first>
- internal/webhook/verb_handlers.go (implementations from Task 2)
- internal/webhook/verbs.go (VerbHandler interface and VerbContext struct)
- internal/webhook/dispatcher_test.go (lines 291-324 — mockPublisher and mockRouteResolver patterns)
- internal/webhook/verbs_test.go (getTestPoolWithMigrations helper for DB-backed tests)
</read_first>

<action>
Create new file `internal/webhook/verb_handlers_test.go` with package `webhook_test`.

Add focused unit tests for each handler's `Execute` method. Reuse the existing `mockPublisher` and `mockRouteResolver` structs already defined in `dispatcher_test.go` (they're in the same test package `webhook_test`). For `contactRepo`-dependent handlers, use the existing `getTestPoolWithMigrations` pattern and real PostgreSQL.

Required test cases:

**TestReplyHandler:**
1. "publishes to messages.outbound" — wire mockPublisher + mockRouteResolver, call Execute with valid ReplyParams JSON `{"body": "test reply"}`, assert `mockPublisher.published` has 1 entry with subject "messages.outbound" and body in the payload matching "test reply"
2. "skips when publisher is nil" — `NewReplyHandler(nil, nil)`, call Execute, assert no error returned
3. "returns error on invalid params" — pass malformed JSON `{bad`, assert error is non-nil

**TestWaitHandler:**
1. "waits for specified duration" — params `{"duration": "50ms"}`, measure elapsed time, assert >= 40ms
2. "caps at 10 seconds" — params `{"duration": "30s"}`, use context with 100ms timeout, assert error (context timeout) triggers before 30s
3. "returns error on invalid duration" — params `{"duration": "invalid"}`, assert error is non-nil

**TestForwardHandler:**
1. "publishes forward message" — wire mockPublisher + mockRouteResolver, call Execute with ForwardParams `{"to": "+5511999", "channel": "telegram"}`, assert published message contains correct "to" field
2. "skips when publisher is nil" — assert no error

**TestTagHandler:**
1. "adds tags to contact" — use real contactRepo, resolve a contact, call handler.Execute with valid VerbContext containing the real ContactID + TagParams JSON `{"tags": ["test-tag"]}`, query DB to verify tags contain "test-tag"
2. "fails when contact not resolved" — pass VerbContext with ContactID=uuid.Nil, assert error is non-nil

**TestCloseHandler:**
1. "closes thread" — use real contactRepo, resolve a contact, call handler.Execute, query DB to verify closed_at is non-nil
2. "fails when contact not resolved" — pass VerbContext with ContactID=uuid.Nil, assert error is non-nil

**TestPauseBotHandler:**
1. "pauses indefinitely" — call Execute with `{}` params, verify bot_active=false, bot_paused_at within 10 seconds of now
2. "pauses with duration offset" — call Execute with `{"duration": "2h"}`, verify bot_paused_at is offset by ~10h from now (elapsed between 9h and 11h)
3. "fails on invalid duration" — pass `{"duration": "bad"}`, assert error is non-nil
4. "fails when contact not resolved" — pass VerbContext with ContactID=uuid.Nil, assert error is non-nil
</action>

<acceptance_criteria>
- File `internal/webhook/verb_handlers_test.go` exists with `package webhook_test`
- Contains at least 14 test cases across 6 handler test functions
- `go test ./internal/webhook/ -run TestReplyHandler -v` exits 0
- `go test ./internal/webhook/ -run TestWaitHandler -v` exits 0
- `go test ./internal/webhook/ -run TestForwardHandler -v` exits 0
- `go test ./internal/webhook/ -run TestTagHandler -v` exits 0
- `go test ./internal/webhook/ -run TestCloseHandler -v` exits 0
- `go test ./internal/webhook/ -run TestPauseBotHandler -v` exits 0
- `go test ./internal/webhook/ -v -count=1` exits 0 (all tests including existing ones)
</acceptance_criteria>

<verify>
- `go test ./internal/webhook/ -v -count=1` exits 0
- `go test ./internal/webhook/ -run "TestReplyHandler|TestWaitHandler|TestForwardHandler|TestTagHandler|TestCloseHandler|TestPauseBotHandler" -v` exits 0
</verify>

</tasks>

<verification>

## Overall Verification

1. **Full build**: `go build ./...` exits 0
2. **All webhook tests**: `go test ./internal/webhook/ -v -count=1` exits 0
3. **Race detector**: `go test ./internal/webhook/ -race -count=1` exits 0
4. **Vet check**: `go vet ./internal/webhook/...` exits 0
5. **Constructor signature preserved**: `grep -n "func NewVerbsEngine" internal/webhook/verbs.go` shows exact signature `func NewVerbsEngine(publisher outbound.Publisher, contactRepo *repository.ContactRepository, logsRepo *repository.UserActionLogRepository, resolver outbound.RouteResolver) *VerbsEngine`
6. **Execute signature preserved**: `grep -n "func (e \*VerbsEngine) Execute" internal/webhook/verbs.go` shows `func (e *VerbsEngine) Execute(ctx context.Context, task WebhookDeliveryTask, verbs []Verb) error`
7. **No switch block**: `grep -c "switch verb.Action" internal/webhook/verbs.go` returns 0
8. **Handler map exists**: `grep "handlers.*map\[string\]VerbHandler" internal/webhook/verbs.go` matches
9. **No test files modified**: `git diff --name-only internal/webhook/verbs_test.go internal/webhook/dispatcher_test.go` shows no output
10. **main.go unchanged**: `git diff --name-only cmd/pergo/main.go` shows no output

</verification>

<success_criteria>

## must_haves

These are derived from the phase goal "Refactor Webhook Verbs Engine to Polymorphic VerbHandlers":

1. **D-03: VerbHandler interface defined** — `type VerbHandler interface { Execute(ctx context.Context, vc VerbContext, params json.RawMessage) error }` exists in `verbs.go` (raw JSON delegation)
2. **D-04: VerbContext struct defined** — carries WorkspaceID, ContactID, TraceID, Event (shared context struct)
3. **D-01: 6 handler implementations with constructor DI** — replyHandler, waitHandler, forwardHandler, tagHandler, closeHandler, pauseBotHandler all implement VerbHandler in `verb_handlers.go`, each accepting only its concrete dependencies
4. **D-02: Static handler registration** — `NewVerbsEngine` constructor builds `map[string]VerbHandler` with all 6 handlers wired statically
5. **Switch block eliminated** — `Execute` loop uses map lookup instead of switch
6. **Public API preserved** — `NewVerbsEngine` constructor signature and `Execute` method signature are unchanged
7. **Behavioral equivalence** — all existing integration tests pass without modification
8. **New unit tests** — at least 14 handler-level unit tests in `verb_handlers_test.go`
9. **D-05: Same package layout** — all handlers and VerbHandler interface reside in `internal/webhook/` package

</success_criteria>

## Artifacts this phase produces

### New Files

| File | Symbols |
|------|---------|
| `internal/webhook/verb_handlers.go` | `replyHandler` (struct), `NewReplyHandler` (func), `waitHandler` (struct), `NewWaitHandler` (func), `forwardHandler` (struct), `NewForwardHandler` (func), `tagHandler` (struct), `NewTagHandler` (func), `closeHandler` (struct), `NewCloseHandler` (func), `pauseBotHandler` (struct), `NewPauseBotHandler` (func) |
| `internal/webhook/verb_handlers_test.go` | `TestReplyHandler`, `TestWaitHandler`, `TestForwardHandler`, `TestTagHandler`, `TestCloseHandler`, `TestPauseBotHandler` |

### Modified Files

| File | Changes |
|------|---------|
| `internal/webhook/verbs.go` | Added `VerbHandler` (interface), `VerbContext` (struct). Modified `VerbsEngine` struct (replaced `publisher`/`resolver` with `handlers map[string]VerbHandler`). Refactored `Execute` loop (map lookup replaces switch). Deleted `executeReply` and `executeForward` private methods. |
