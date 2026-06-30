# Phase 8 Research: Multi-Instance Connections & Dashboard UI

To plan Phase 8 well, we need to address two main areas: database consolidation for multi-instance routing and a Notion-inspired dashboard UI with dynamic onboarding logic.

Below is the complete research findings, including database migration scripts, API changes, static adapter routing, SOCKS5 proxy integration, and layout designs.

---

## 1. Database Schema Consolidation & Migration (D-01)
We will consolidate the legacy tables `devices` and `channel_credentials` into a single unified `connections` table. 

### Unified Connections Table Schema
```sql
CREATE TABLE connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    channel TEXT NOT NULL, -- 'whatsapp' (web), 'whatsapp_cloud' (WABA), 'telegram'
    sender_identity TEXT NOT NULL, -- Unique identifier (phone number or bot username, e.g. '+5511999990001', '@bot_username')
    status TEXT NOT NULL DEFAULT 'pending',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Encrypted Credentials (JSON object representing WABA config or Telegram Token)
    credentials BYTEA,
    key_id TEXT,
    key_version INT NOT NULL DEFAULT 1,
    
    -- WhatsApp Web (whatsmeow) specific fields
    jid TEXT,
    connected_since TIMESTAMPTZ,
    
    -- Traffic isolation proxy configuration
    proxy_url TEXT, -- SOCKS5/HTTP proxy string (e.g. 'socks5://user:pass@host:port')
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (sender_identity)
);

CREATE INDEX idx_connections_workspace_id ON connections(workspace_id);
CREATE INDEX idx_connections_channel ON connections(channel);
```

### Goose Migration Script (`internal/platform/postgres/migrations/012_consolidate_connections.sql`)
The migration must automatically transfer data from legacy tables and drop them without losing session data or encryption tokens:
```sql
-- +goose Up
-- +goose StatementBegin
-- Create connections table as defined above
CREATE TABLE connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    channel TEXT NOT NULL,
    sender_identity TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    credentials BYTEA,
    key_id TEXT,
    key_version INT NOT NULL DEFAULT 1,
    jid TEXT,
    connected_since TIMESTAMPTZ,
    proxy_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (sender_identity)
);

CREATE INDEX idx_connections_workspace_id ON connections(workspace_id);
CREATE INDEX idx_connections_channel ON connections(channel);

-- Migrate existing devices (whatsmeow sessions)
INSERT INTO connections (
    id, workspace_id, name, channel, sender_identity, status, is_default, jid, connected_since, created_at, updated_at
)
SELECT 
    id,
    workspace_id,
    'WhatsApp Web - ' || COALESCE(phone, id::text),
    'whatsapp',
    COALESCE(phone, jid, id::text),
    status,
    FALSE,
    jid,
    connected_since,
    created_at,
    updated_at
FROM devices;

-- Migrate existing channel credentials (WABA and Telegram)
INSERT INTO connections (
    id, workspace_id, name, channel, sender_identity, status, is_default, credentials, key_id, key_version, created_at, updated_at
)
SELECT 
    id,
    workspace_id,
    CASE WHEN channel = 'telegram' THEN 'Telegram Bot' ELSE 'WhatsApp WABA' END,
    channel,
    'legacy_' || channel || '_' || id::text, -- Safe unique placeholder
    'active',
    TRUE,
    credentials,
    key_id,
    key_version,
    created_at,
    updated_at
FROM channel_credentials;

-- Ensure at least one default WhatsApp connection exists per workspace
WITH ranked_whatsapp AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY workspace_id ORDER BY created_at) as rn
    FROM connections
    WHERE channel = 'whatsapp'
)
UPDATE connections
SET is_default = TRUE
WHERE id IN (SELECT id FROM ranked_whatsapp WHERE rn = 1);

-- Drop legacy tables
DROP TABLE IF EXISTS devices CASCADE;
DROP TABLE IF EXISTS channel_credentials CASCADE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE channel_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    credentials BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    key_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, channel)
);

CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id),
    channel TEXT NOT NULL,
    device_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    credentials BYTEA,
    key_id TEXT,
    key_version INT DEFAULT 1,
    jid TEXT,
    phone TEXT,
    connected_since TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO devices (
    id, workspace_id, channel, device_id, status, jid, phone, connected_since, created_at, updated_at
)
SELECT 
    id,
    workspace_id,
    channel,
    id::text,
    status,
    jid,
    sender_identity,
    connected_since,
    created_at,
    updated_at
FROM connections
WHERE channel = 'whatsapp';

INSERT INTO channel_credentials (
    id, workspace_id, channel, credentials, key_id, key_version, created_at, updated_at
)
SELECT DISTINCT ON (workspace_id, channel)
    id,
    workspace_id,
    channel,
    credentials,
    key_id,
    key_version,
    created_at,
    updated_at
FROM connections
WHERE channel IN ('telegram', 'whatsapp_cloud');

DROP TABLE IF EXISTS connections CASCADE;
-- +goose StatementEnd
```

---

## 2. API Changes & Ingest Dynamic Routing
To support sending from multiple sender identities, we must modify the public message API:

### 1. Ingest Payload (`POST /api/v1/messages`)
Add the `from` field to the request object:
```go
type CreateMessageRequest struct {
    To               string              `json:"to"`
    From             string              `json:"from,omitempty"` // Phone number or bot username
    Channel          string              `json:"channel"`
    Body             string              `json:"body"`
    Media            *Media              `json:"media,omitempty"`
    Metadata         map[string]string   `json:"metadata,omitempty"`
    TTLSeconds       *int                `json:"ttl_seconds,omitempty"`
    TemplateName     string              `json:"template_name,omitempty"`
    Language         string              `json:"language,omitempty"`
    Components       []TemplateComponent `json:"components,omitempty"`
    FallbackChannels []string            `json:"fallback_channels,omitempty"`
}
```

### 2. Route Resolution on Ingestion
When a message is received, resolve the `ConnectionID` and `SenderIdentity` dynamically before publishing to NATS:
```go
func ResolveRoute(ctx context.Context, pool *pgxpool.Pool, workspaceID uuid.UUID, from, channelName string) (connID uuid.UUID, senderIdentity string, err error) {
    if from != "" {
        // Resolve directly by sender_identity
        err = pool.QueryRow(ctx, 
            `SELECT id, sender_identity FROM connections WHERE workspace_id = $1 AND sender_identity = $2`, 
            workspaceID, from,
        ).Scan(&connID, &senderIdentity)
        if err != nil {
            return uuid.Nil, "", fmt.Errorf("sender identity 'from' not found in workspace: %w", err)
        }
        return connID, senderIdentity, nil
    }
    
    // Fallback to the default connection of that channel type
    err = pool.QueryRow(ctx, 
        `SELECT id, sender_identity FROM connections WHERE workspace_id = $1 AND channel = $2 AND is_default = TRUE`, 
        workspaceID, channelName,
    ).Scan(&connID, &senderIdentity)
    if err != nil {
        return uuid.Nil, "", fmt.Errorf("no default connection found for channel %s in this workspace", channelName)
    }
    return connID, senderIdentity, nil
}
```

---

## 3. Static Adapters & Concurrency Model
To prevent memory leaks, we avoid dynamic adapter pools. We retain the static adapters registry (`whatsapp`, `whatsapp_cloud`, `telegram`) and resolve state dynamically during dispatch:

### 1. Stateless Channels (WABA / Telegram)
```go
func (a *TelegramAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
    creds, err := a.connectionsRepo.GetCredentialsByID(ctx, m.ConnectionID)
    if err != nil {
        return "", channel.NewTerminalError(err)
    }
    // Perform Telegram Bot API HTTP POST using the decrypted credentials token
}
```

### 2. Stateful Channels (whatsmeow WhatsApp Web)
```go
func (a *WhatsAppAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
    session := a.sessions.Get(m.SenderIdentity) // m.SenderIdentity carries the JID
    if session == nil {
        return "", fmt.Errorf("whatsapp device session not connected")
    }
    return session.Client().SendMessage(ctx, m.To, m.Body)
}
```

---

## 4. SOCKS5/HTTP Proxy Support for whatsmeow
```go
import (
    "net/http"
    "net/url"
    "golang.org/x/net/proxy"
)

func ConfigureProxy(client *whatsmeow.Client, proxyStr string) error {
    if proxyStr == "" {
        client.SetProxy(nil)
        return nil
    }
    
    u, err := url.Parse(proxyStr)
    if err != nil {
        return fmt.Errorf("invalid proxy URL: %w", err)
    }
    
    switch u.Scheme {
    case "socks5":
        var auth *proxy.Auth
        if u.User != nil {
            pass, _ := u.User.Password()
            auth = &proxy.Auth{
                User:     u.User.Username(),
                Password: pass,
            }
        }
        dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
        if err != nil {
            return fmt.Errorf("socks5 dialer error: %w", err)
        }
        client.SetSOCKSProxy(dialer)
        
    case "http", "https":
        client.SetProxy(http.ProxyURL(u))
        
    default:
        return fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
    }
    
    return nil
}
```

---

## 5. Notion-Style Dashboard UI & Dynamic Onboarding Checklist
- Implement cookie selection for active workspace.
- Detect onboarding completion dynamically. If `Count(APIKeys) == 0` or `Count(Connections) == 0`, render the 4-step progressive onboarding checklist; otherwise, render the operational developer metrics dashboard.

---

## 6. Workspace Connection Limits
Read `PERGO_MAX_WHATSAPP_CONNECTIONS` env variable (default = `5`). Before starting a pairing flow, check if the limit is exceeded and return HTTP 422 if so.
