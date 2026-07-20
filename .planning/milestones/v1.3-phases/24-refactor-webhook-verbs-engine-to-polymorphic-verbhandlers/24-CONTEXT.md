# Phase 24: Refactor Webhook Verbs Engine to Polymorphic VerbHandlers - Context

**Gathered:** 2026-07-18
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase refactors the monolithic Webhook Verbs Engine (`VerbsEngine`) execution logic. It introduces a polymorphic `VerbHandler` interface in the `webhook` package, extracting each verb action (e.g. `reply`, `wait`, `tag`, `close`, `pause_bot`) into separate, testable handlers. It aims to improve code locality, leverage, and testability.

</domain>

<decisions>
## Implementation Decisions

### Dependency Injection (DI) Model
- **D-01:** Verb handlers are built with constructor dependency injection (Option A). Each handler class accepts only its concrete, mockable dependencies (e.g., `NewTagHandler(contactRepo)`), maximizing compile-time safety and isolation.

### Handler Registration & Routing
- **D-02:** Handlers are statically wired within the `NewVerbsEngine` constructor (Option A), mapping verb names (like `"pause_bot"`) to their respective `VerbHandler` instances. This keeps the engine stateless and setup compile-time safe.

### Execution Interface & Parsing
- **D-03:** Raw JSON delegation (Option A). The `VerbHandler` interface is defined as `Execute(ctx, verbCtx, params json.RawMessage) error`. Individual handlers unmarshal and validate their own private parameter structs, keeping the core engine generic.

### Shared Execution Context
- **D-04:** Pass shared context struct (Option A). The engine resolves the contact profile once at block execution start, passing a shared `VerbContext` struct containing `WorkspaceID`, `ContactID`, `TraceID`, and the parsed `InboundEventPayload` down the execution loop, avoiding redundant DB queries.

### File Structure
- **D-05:** Place handlers in the same package (Option A). All handlers and the `VerbHandler` interface reside in the `webhook` package, consolidated within `internal/webhook/verb_handlers.go` to prevent circular import issues and maintain a flat package layout.

### the agent's Discretion
None - all core structural patterns are locked by design decisions above.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Domain Glossary
- `CONTEXT.md` — Defines Webhook Verbs Engine, Webhook Dispatcher, and other domain terms.

### Webhook Verbs Engine Core
- `internal/webhook/verbs.go` — Current implementation of verbs engine execution block.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `repository.ContactRepository`: Used by tag, close, and pause_bot handlers.
- `outbound.Publisher` and `outbound.RouteResolver`: Used by reply and forward handlers.

### Established Patterns
- Constructor dependency injection is widely used across all other package handlers.

### Integration Points
- `internal/webhook/verbs.go`: The execution entrypoint `Execute` will loop over the handler map rather than using the switch block.

</code_context>

<specifics>
## Specific Ideas
No specific requirements — open to standard Go implementations matching the decisions.

</specifics>

<deferred>
## Deferred Ideas
None — discussion stayed within phase scope.

</deferred>

---

*Phase: 24-refactor-webhook-verbs-engine-to-polymorphic-verbhandlers*
*Context gathered: 2026-07-18*
