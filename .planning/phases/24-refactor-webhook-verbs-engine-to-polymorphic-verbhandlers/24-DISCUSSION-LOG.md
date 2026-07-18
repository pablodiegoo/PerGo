# Phase 24: Refactor Webhook Verbs Engine to Polymorphic VerbHandlers - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-18
**Phase:** 24-refactor-webhook-verbs-engine-to-polymorphic-verbhandlers
**Areas discussed:** Dependency Injection Model, Handler Registration, Execution Interface, Shared Execution Context, File Structure

---

## Dependency Injection Model

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Constructor Injection | ✓ |
| Option B | Context Object | |

**User's choice:** Option A
**Notes:** Decided to use constructor injection to keep handler dependencies isolated and ease testing.

---

## Handler Registration

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Wired directly in constructor/map | ✓ |
| Option B | Dynamic registration | |

**User's choice:** Option A
**Notes:** Keep handler mapping static within `NewVerbsEngine` to avoid mutable runtime registries.

---

## Execution Interface & Parsing

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Raw JSON delegation (`json.RawMessage`) | ✓ |
| Option B | Map-based parsing | |

**User's choice:** Option A
**Notes:** Handlers unmarshal their own params, keeping the engine code generic.

---

## Shared Execution Context

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Pass a Shared `VerbContext` struct | ✓ |
| Option B | Handlers resolve independently | |

**User's choice:** Option A
**Notes:** Pass pre-resolved IDs to prevent duplicate database calls during a single webhook execution sequence.

---

## File Structure

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Same package (`webhook`), single file | ✓ |
| Option B | Separate sub-package (`webhook/handlers`) | |

**User's choice:** Option A
**Notes:** Group handlers in `internal/webhook/verb_handlers.go` to keep imports flat and clean.

---

## the agent's Discretion
None.

## Deferred Ideas
None.
