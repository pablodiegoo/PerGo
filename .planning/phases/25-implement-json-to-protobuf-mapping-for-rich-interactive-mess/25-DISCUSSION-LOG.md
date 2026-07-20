# Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages (hybrid approach) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-20
**Phase:** 25-Implement JSON-to-Protobuf mapping for rich interactive messages (hybrid approach)
**Areas discussed:** Validation Strictness, Override Conflict Resolution, Fallback Degradation

---

## Validation Strictness

| Option | Description | Selected |
|--------|-------------|----------|
| Defer to adapter | The HTTP gateway only validates that the unified JSON schema is well-formed. The channel adapter enforces specific limits (e.g., max 3 buttons) and fails the dispatch if violated. | ✓ |
| Validate at gateway | The HTTP gateway knows about WhatsApp's limits and rejects the API request with a 400 Bad Request, keeping invalid messages out of the queue entirely. | |
| Truncate excess at adapter | The gateway validates JSON shape, but the adapter silently truncates any buttons/items that exceed channel limits and sends the rest. | |

**User's choice:** Defer to adapter
**Notes:** 

---

## Override Conflict Resolution

| Option | Description | Selected |
|--------|-------------|----------|
| Complete replacement | If `channel_overrides.whatsapp` is provided, it completely ignores the unified interactive components (buttons/lists) and sends the override payload as-is. | ✓ |
| Deep merge | The unified component acts as the base, and properties in `channel_overrides.whatsapp` override specific fields within it. | |
| Error on conflict | Reject the request if both a unified interactive component and an override for the same channel are provided. | |

**User's choice:** Complete replacement
**Notes:** As a premium CPaaS, predictability is key. If a developer uses an escape hatch, they want total control over the payload for that channel.

---

## Fallback Degradation

| Option | Description | Selected |
|--------|-------------|----------|
| Configurable per-message | Add a `fallback_behavior` flag (e.g., `degrade` or `fail`) to the payload. Some interactive messages are critical (fail if unsupported), while others are just enhancements (degrade to text). | ✓ |
| Always degrade gracefully | Extract the text components (header, body, footer) and send as plain text. Ensures high delivery rates. | |
| Always fail the dispatch | If the developer sent interactive elements, they are essential to the flow. Falling back to plain text breaks the UX. | |

**User's choice:** Configurable per-message
**Notes:** 

---

## the agent's Discretion

None

## Deferred Ideas

None
