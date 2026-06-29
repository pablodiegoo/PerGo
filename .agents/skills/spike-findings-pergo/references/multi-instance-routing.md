# Multi-Instance Routing & Consolidation

## Requirements
- Must support multiple configurations of the same channel type (whatsmeow, WABA, Telegram) per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.

## How to Build It

### 1. Database Schema (`connections`)
Consolidate `channel_credentials` and `devices` into a single `connections` table:
```sql
CREATE TABLE connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    channel TEXT NOT NULL, -- 'whatsapp' (web), 'whatsapp_cloud' (WABA), 'telegram'
    sender_identity TEXT NOT NULL, -- phone number or bot username (e.g. '+5511999990001', '@bot_username')
    status TEXT NOT NULL DEFAULT 'pending',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Encrypted Credentials (JSON object representing WABA config or Telegram Token)
    credentials BYTEA,
    key_id TEXT,
    key_version INT NOT NULL DEFAULT 1,
    
    -- WhatsApp Web (whatsmeow) specific fields
    jid TEXT,
    connected_since TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (sender_identity)
);
```

### 2. API Validation and Ingestion Routing
Modify the `POST /api/v1/messages` handler to resolve the connection ID on ingestion:
```go
func ResolveRoute(workspaceID uuid.UUID, from, channel string, connections []Connection) (primaryID uuid.UUID, err error) {
    if from != "" {
        // Resolve directly by sender identity
        for _, conn := range connections {
            if conn.WorkspaceID == workspaceID && conn.SenderIdentity == from {
                return conn.ID, nil
            }
        }
        return uuid.Nil, errors.New("sender identity 'from' not found in workspace")
    } else if channel != "" {
        // Fallback to the default connection of that channel type
        for _, conn := range connections {
            if conn.WorkspaceID == workspaceID && conn.Channel == channel && conn.IsDefault {
                return conn.ID, nil
            }
        }
        return uuid.Nil, errors.New("no default connection found for the specified channel")
    }
    return uuid.Nil, errors.New("either 'from' or 'channel' is required")
}
```
*Tip:* To avoid SQL lookups on every API request, cache the connection metadata map `(workspace_id, sender_identity) -> connection_id` in-memory.

### 3. Static Adapters with Dynamic Instance Routing
Rather than dynamically instantiating separate adapters in memory for every connection UUID, use a static set of global dispatchers (`whatsapp`, `whatsapp_cloud`, `telegram`) and load instance state during dispatch:

#### Telegram / WABA (Stateless)
Query the database or cache on each `.Dispatch()` to load the connection credentials using the payload's connection ID:
```go
func (a *TelegramAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
    creds, err := a.connectionsRepo.GetCredentialsByID(ctx, m.ConnectionID)
    if err != nil {
        return "", channel.NewTerminalError(err)
    }
    // Perform HTTP post to Telegram Bot API with retrieved token
}
```

#### WhatsApp Web (Stateful)
Keep all paired device WebSocket connections alive in the shared `ActiveSession` registry. During dispatch, look up the active whatsmeow client using the JID of the connection:
```go
func (a *WhatsAppAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
    client := a.sessions.Get(m.SenderIdentity) // SenderIdentity is the device JID
    if client == nil {
        return "", fmt.Errorf("whatsapp device session not connected")
    }
    return client.Client().SendMessage(ctx, m.To, m.Body)
}
```

## What to Avoid
- **Avoid Dynamic Adapter Pools:** Do not create a new `TelegramAdapter` or `WABAAdapter` instance per connection. Dynamic instantiation leaks memory and creates complex lifecycle management (e.g. having to destroy/re-create adapters on credential edits).
- **Avoid calling `GetFirstDevice` in whatsmeow:** Ensure `whatsmeow` loads the specific device from its SQLStore using `container.GetDevice(jid)` rather than `container.GetFirstDevice()`.

## Constraints
- **Uniqueness:** A `sender_identity` is globally unique (e.g., a phone number or Telegram bot token cannot be configured in multiple workspaces or rows).
- **WhatsMeow DB Schema:** whatsmeow keys (`whatsmeow_device` table) must remain co-located in the same database but are accessed solely by its internal library via the device JID.

## Origin
Synthesized from spikes: 001, 002, 003
Source files available in:
- `sources/001-multi-instance-schema/`
- `sources/002-api-routing-payload/`
- `sources/003-dynamic-adapter-registry/`
