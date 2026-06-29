---
spike: 1
name: multi-instance-schema
type: standard
validates: "Given a workspace with 2 Telegram bots and 2 WABA configurations, when the database is migrated to a unified connections schema, then we can store, query, and encrypt/decrypt distinct credentials/sessions cleanly without key collision."
verdict: VALIDATED
related: []
tags: [db, schema]
---

# Spike 001: Multi-Instance Schema Redesign

## What This Validates
This spike validates that we can consolidate the existing `channel_credentials` and `devices` tables into a unified `connections` table that supports multiple configurations of the same channel type per workspace, while maintaining secure AES-256-GCM encryption at rest.

## Research

### Current Architecture
Currently, PerGo uses:
- `devices` table: For WhatsApp Web (whatsmeow) sessions, containing fields like `jid`, `phone`, `connected_since` alongside connection `status`.
- `channel_credentials` table: For Telegram and WABA configurations. It has a strict `UNIQUE (workspace_id, channel)` constraint, limiting workspaces to a single connection per channel.

### Proposed Consolidated Schema: `connections`
We will replace both tables with a unified `connections` table.
```sql
CREATE TABLE connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    channel TEXT NOT NULL, -- 'whatsapp' (web), 'whatsapp_cloud' (WABA), 'telegram'
    sender_identity TEXT NOT NULL, -- phone number for WABA/Web, bot username for Telegram
    status TEXT NOT NULL DEFAULT 'pending',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Encrypted Credentials Payload (JSON serialized bytes)
    credentials BYTEA,
    key_id TEXT,
    key_version INT NOT NULL DEFAULT 1,
    
    -- WhatsApp Web Specifics
    jid TEXT,
    connected_since TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Constraints
    UNIQUE (sender_identity) -- A sender identity is globally unique
);
```

### Gotchas & Decisions
- **whatsmeow JID and SQLStore:** whatsmeow stores its device keys in its own tables (transparently encrypted via view/triggers in `whatsmeow_device`). Since whatsmeow keys are tied to the `jid` primary key, our `connections` table stores the `jid` as a lookup field. To connect a specific whatsmeow device, we load the whatsmeow client using `container.GetDevice(jid)` instead of `GetFirstDevice()`.
- **API `from` Field Routing:** The public API `POST /api/v1/messages` will look up connections based on `(workspace_id, sender_identity)`. If the `from` request field is empty, it uses the connection where `is_default = TRUE` for that channel.

## How to Run
We implemented a Go test in `connections_spike_test.go` that:
1. Performs mock/temporary table creation representing the `connections` schema.
2. Creates multiple connections (2 Telegram bots, 2 WABA) in a single workspace.
3. Encrypts different credentials JSON payloads per connection.
4. Verifies we can query, decrypt, and match them via the `sender_identity` (equivalent to `from` routing).

To execute the test:
```bash
PERGO_DATABASE_URL=postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable go test -v ./internal/repository/ -run TestConnectionsSpike
```

## What to Expect
- Successful schema setup without constraint violations.
- Multiple active connections of the same type (`telegram` / `whatsapp_cloud`) can coexist under one workspace.
- Verification of correct decryption and routing for each connection instance.

## Investigation Trail
- Formulated the consolidated `connections` schema.
- Discovered that whatsmeow's SQLStore naturally supports multiple devices in the database since the primary key of `whatsmeow_device` is the `jid` string. We just need to load them via `container.GetDevice(jid)` instead of the hardcoded `container.GetFirstDevice()`.
- Implemented and verified the database migration and query routing in `connections_spike_test.go`.

## Results
- **Verdict:** VALIDATED ✓
- **Evidence:** The test successfully ran against a local PostgreSQL instance:
  ```
  === RUN   TestConnectionsSpike
  2026/06/29 19:40:33 OK   001_create_schema.sql (64.04ms)
  ...
  2026/06/29 19:40:33 OK   011_encrypt_whatsmeow_device.sql (34.61ms)
  goose: successfully migrated database to version: 11
  --- PASS: TestConnectionsSpike (0.48s)
  PASS
  ok  	github.com/pablojhp.pergo/internal/repository	0.483s
  ```
- **Gotchas resolved:** Caching connection mapping of `(workspace_id, sender_identity)` in memory is recommended on the public API ingestion to keep lookup speeds sub-millisecond without database overhead.
