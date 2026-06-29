---
name: spike-findings-pergo
description: Implementation blueprint from spike experiments. Requirements, proven patterns, and verified knowledge for building PerGo. Auto-loaded during implementation work.
---

<context>
## Project: PerGo

Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID.

Spike sessions wrapped: 2026-06-29
</context>

<requirements>
## Requirements

- Must support multiple configurations of the same channel type per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.
</requirements>

<findings_index>
## Feature Areas

| Area | Reference | Key Finding |
|------|-----------|-------------|
| Multi-Instance Routing & Consolidation | [multi-instance-routing.md](file:///.agents/skills/spike-findings-pergo/references/multi-instance-routing.md) | Consolidate credentials/devices into a single connections table and route dynamically using static adapters to avoid memory leaks. |

## Source Files

Original spike source files are preserved in `sources/` for complete reference:
- [sources/001-multi-instance-schema/](file:///.agents/skills/spike-findings-pergo/sources/001-multi-instance-schema/)
- [sources/002-api-routing-payload/](file:///.agents/skills/spike-findings-pergo/sources/002-api-routing-payload/)
- [sources/003-dynamic-adapter-registry/](file:///.agents/skills/spike-findings-pergo/sources/003-dynamic-adapter-registry/)
</findings_index>

<metadata>
## Processed Spikes

- 001-multi-instance-schema
- 002-api-routing-payload
- 003-dynamic-adapter-registry
</metadata>
