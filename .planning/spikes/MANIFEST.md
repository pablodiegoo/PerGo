# Spike Manifest

## Idea
Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID.

## Requirements
- Must support multiple configurations of the same channel type per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.

## Spikes

| # | Name | Type | Validates | Verdict | Tags |
|---|------|------|-----------|---------|------|
| 001 | multi-instance-schema | standard | Given a workspace with multiple configurations, when migrated to a unified connections schema, then we can store and encrypt distinct credentials/sessions cleanly. | VALIDATED | db, schema |
| 002 | api-routing-payload | standard | Given a message request, when multiple instances exist, then we can route it dynamically via the `from` field with fallback support. | PENDING | api, routing |
| 003 | dynamic-adapter-registry | standard | Given a running server, when connection credentials change, then the registry can dynamically instantiate/update dispatchers in memory. | PENDING | concurrency, registry |
