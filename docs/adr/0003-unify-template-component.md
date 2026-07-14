# ADR-0003: Unify TemplateComponent type in domain

**Status:** Proposed  
**Date:** 2026-07-03

## Context

`TemplateComponent` and `TemplateParameter` are defined identically in both `internal/domain/message.go` and `internal/channel/dispatcher.go`. The worker's `convertTemplateComponents` function copies fields one-to-one between them — pure pass-through. Every new template field requires updating both structs.

## Decision

Delete the channel-layer copies. `channel.MessagePayload.Components` changes from `[]TemplateComponent` to `[]domain.TemplateComponent`. The `convertTemplateComponents` function in worker.go is removed. The types live once in `domain`.

### Files changed

- `internal/channel/dispatcher.go` — delete `TemplateComponent` and `TemplateParameter` definitions; update `MessagePayload.Components` type
- `internal/platform/queue/worker.go` — delete `convertTemplateComponents`; use `qMsg.Components` directly
- `internal/channel/whatsapp/waba_test.go` — update type references (`channel.TemplateComponent` → `domain.TemplateComponent`)

### Risk assessment

- `channel` already imports `domain` for `Media` — no new dependency
- `domain` already imported in `internal/channel/whatsapp/waba_test.go`
- No other references to `channel.TemplateComponent` or `channel.TemplateParameter` exist
- No circular dependency possible

## Consequences

- **Locality:** template schema changes in one place
- **Leverage:** delete 14 lines of conversion code + duplicated type definitions
