# Phase 21: Chatwoot Integration - Pattern Map

**Mapped:** 2026-07-17
**Files analyzed:** 15
**Analogs found:** 15 / 15

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/platform/postgres/migrations/027_create_integrations_and_chatwoot_mappings.sql` | config | N/A | `internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql` | High |
| `internal/repository/integration.go` | service/repository | CRUD | `internal/repository/connection.go` | High |
| `internal/repository/chatwoot_mapping.go` | service/repository | CRUD | `internal/repository/recipient_session.go` | High |
| `internal/integration/chatwoot/client.go` | service/component | request-response | `internal/channel/telegram/telegram.go` | High |
| `internal/integration/chatwoot/syncer.go` | service/component | event-driven | `internal/inbound/processor.go` | High |
| `internal/api/handler/chatwoot_webhook.go` | controller/handler | request-response | `internal/api/handler/telegram_webhook.go` | High |
| `internal/api/handler/admin/integration.go` | controller/handler | CRUD | `internal/api/handler/admin/waba_template.go` | High |
| `internal/repository/integration_test.go` | test | CRUD | `internal/repository/credentials_test.go` | High |
| `internal/repository/chatwoot_mapping_test.go` | test | CRUD | `internal/repository/recipient_session_test.go` | High |
| `internal/integration/chatwoot/client_test.go` | test | request-response | `internal/channel/telegram/telegram_test.go` | High |
| `internal/integration/chatwoot/syncer_test.go` | test | event-driven | `internal/inbound/processor_test.go` | High |
| `internal/api/handler/chatwoot_webhook_test.go` | test | request-response | `internal/api/handler/telegram_webhook_test.go` | High |
| `internal/api/handler/admin/integration_test.go` | test | CRUD | `internal/api/handler/admin/waba_template_test.go` | High |
| `internal/inbound/processor.go` (Modified) | service | event-driven | `internal/inbound/processor.go` (Self) | High |
| `cmd/pergo/main.go` (Modified) | config | N/A | `cmd/pergo/main.go` (Self) | High |

---

## Pattern Assignments

### 1. Database Migration SQL
**Analog:** `internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql`

* **Core pattern:**
  ```sql
  -- +goose Up
  CREATE TABLE integrations (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      name TEXT NOT NULL,
      provider TEXT NOT NULL, -- e.g., 'chatwoot', 'typebot'
      active BOOLEAN NOT NULL DEFAULT TRUE,
      config BYTEA NOT NULL,
      key_id TEXT NOT NULL,
      key_version INT NOT NULL DEFAULT 1,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      UNIQUE (workspace_id, provider)
  );

  CREATE TABLE chatwoot_mappings (
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
      connection_id UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
      chatwoot_contact_id BIGINT NOT NULL,
      chatwoot_conversation_id BIGINT NOT NULL,
      channel TEXT NOT NULL,
      sender_identity TEXT NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      PRIMARY KEY (workspace_id, contact_id, connection_id)
  );

  CREATE INDEX idx_integrations_workspace ON integrations(workspace_id);
  CREATE INDEX idx_chatwoot_mappings_contact ON chatwoot_mappings(contact_id);

  -- +goose Down
  DROP TABLE chatwoot_mappings;
  DROP TABLE integrations;
  ```

---

### 2. Integration Repository (`integration.go`)
**Analog:** `internal/repository/connection.go` (with elements from `credentials.go`)

* **Imports pattern:**
  ```go
  import (
      "context"
      "database/sql"
      "encoding/json"
      "errors"
      "fmt"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/jackc/pgx/v5/pgxpool"
  )
  ```
* **Auth/Guard/Encryption pattern:**
  Use `CredentialProvider` to encrypt the `config` JSON payload before writing to the database:
  ```go
  ciphertext, keyID, keyVersion, err := r.provider.Encrypt(plaintextJSON)
  ```
* **Core pattern:**
  ```go
  type Integration struct {
      ID          uuid.UUID
      WorkspaceID uuid.UUID
      Name        string
      Provider    string
      Active      bool
      Config      []byte
      CreatedAt   time.Time
      UpdatedAt   time.Time
  }

  type IntegrationRepository struct {
      pool     *pgxpool.Pool
      provider CredentialProvider
  }

  func (r *IntegrationRepository) Save(ctx context.Context, i *Integration) error {
      // Encrypt and Exec INSERT/UPDATE ON CONFLICT
  }

  func (r *IntegrationRepository) GetByProvider(ctx context.Context, workspaceID uuid.UUID, provider string) (*Integration, error) {
      // QueryRow, Scan, and Decrypt
  }
  ```

---

### 3. Chatwoot Mapping Repository (`chatwoot_mapping.go`)
**Analog:** `internal/repository/recipient_session.go`

* **Core pattern:**
  ```go
  type ChatwootMapping struct {
      WorkspaceID            uuid.UUID
      ContactID              uuid.UUID
      ConnectionID           uuid.UUID
      ChatwootContactID      int64
      ChatwootConversationID int64
      Channel                string
      SenderIdentity         string
      CreatedAt              time.Time
      UpdatedAt              time.Time
  }

  type ChatwootMappingRepository struct {
      pool *pgxpool.Pool
  }

  func (r *ChatwootMappingRepository) Upsert(ctx context.Context, m *ChatwootMapping) error {
      _, err := r.pool.Exec(ctx, `
          INSERT INTO chatwoot_mappings (workspace_id, contact_id, connection_id, chatwoot_contact_id, chatwoot_conversation_id, channel, sender_identity)
          VALUES ($1, $2, $3, $4, $5, $6, $7)
          ON CONFLICT (workspace_id, contact_id, connection_id)
          DO UPDATE SET 
              chatwoot_contact_id = EXCLUDED.chatwoot_contact_id,
              chatwoot_conversation_id = EXCLUDED.chatwoot_conversation_id,
              channel = EXCLUDED.channel,
              sender_identity = EXCLUDED.sender_identity,
              updated_at = NOW()
      `, m.WorkspaceID, m.ContactID, m.ConnectionID, m.ChatwootContactID, m.ChatwootConversationID, m.Channel, m.SenderIdentity)
      return err
  }

  func (r *ChatwootMappingRepository) GetByContactAndConnection(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) (*ChatwootMapping, error) {
      // QueryRow mapping with WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
      return nil, nil
  }

  func (r *ChatwootMappingRepository) Delete(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) error {
      // Exec DELETE FROM chatwoot_mappings WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
      return nil
  }
  ```

---

### 4. Chatwoot REST Client (`client.go`)
**Analog:** `internal/channel/telegram/telegram.go`

* **Imports pattern:**
  ```go
  import (
      "bytes"
      "context"
      "encoding/json"
      "fmt"
      "io"
      "net/http"
      "time"
  )
  ```
* **Core pattern:**
  ```go
  type ChatwootClient struct {
      client  *http.Client
      apiURL  string
      token   string
      accountID int64
  }

  func (c *ChatwootClient) SearchContact(ctx context.Context, identifier string) (int64, error) {
      // GET /api/v1/accounts/{account_id}/contacts/search?q={identifier}
  }

  func (c *ChatwootClient) CreateContact(ctx context.Context, identifier, name string) (int64, error) {
      // POST /api/v1/accounts/{account_id}/contacts
  }

  func (c *ChatwootClient) CreateConversation(ctx context.Context, contactID int64, inboxID int64) (int64, error) {
      // POST /api/v1/accounts/{account_id}/conversations
  }

  func (c *ChatwootClient) PostMessage(ctx context.Context, conversationID int64, content string, private bool) error {
      // POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
  }
  ```
* **Error Handling pattern:**
  Use structured HTTP response verification and return terminal failures for client errors (`4xx` code errors) and retriable ones for server errors (`5xx` code errors or timeouts):
  ```go
  resp, err := c.client.Do(req)
  if err != nil {
      return err
  }
  defer resp.Body.Close()
  if resp.StatusCode >= 400 && resp.StatusCode < 500 {
      return fmt.Errorf("terminal error: http status %d", resp.StatusCode)
  }
  ```

---

### 5. Chatwoot Inbound Syncer (`syncer.go`)
**Analog:** `internal/inbound/processor.go`

* **Core pattern:**
  ```go
  type ChatwootSyncer struct {
      integrationRepo *repository.IntegrationRepository
      mappingRepo     *repository.ChatwootMappingRepository
      // HTTP client helper or factory to instantiate ChatwootClient
  }

  func (s *ChatwootSyncer) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error {
      // 1. Fetch integration configuration for workspaceID and check if Active
      // 2. Decrypt configuration details (api_url, token, inbox_id, account_id)
      // 3. Lookup mapping repository for (workspaceID, contact.ID, ev.ConnectionID) using GetByContactAndConnection
      // 4. If mapped:
      //      - Post message directly to Chatwoot Conversation ID
      //      - If post returns 404 (ErrNotFound), delete local mapping using mappingRepo.Delete(ctx, workspaceID, contact.ID, ev.ConnectionID), and proceed to step 5 (re-resolve & recreate)
      // 5. If not mapped:
      //      - Search contact in Chatwoot using contact.ID (UUID string) as "identifier"
      //      - If not found, create contact in Chatwoot
      //      - Create conversation in Chatwoot
      //      - Save mapping (Upsert with connection_id and sender_identity)
      //      - Post message to the new Conversation ID
  }
  ```

---

### 6. Webhook Endpoint Handler (`chatwoot_webhook.go`)
**Analog:** `internal/api/handler/telegram_webhook.go`

* **Auth/Guard pattern:**
  Rely on `AuthMiddleware` key validation injected via request Context (using `?token=YOUR_API_KEY` mapped to standard credentials).
* **Core pattern:**
  ```go
  type ChatwootWebhookHandler struct {
      publisher         inbound.Publisher
      mappingRepo       *repository.ChatwootMappingRepository
      outboundProcessor *outbound.Processor
  }

  func (h *ChatwootWebhookHandler) Handle(c *echo.Context) error {
      // 1. Extract workspace ID from tenant context
      workspaceID, ok := tenant.WorkspaceID(c.Request().Context())
      if !ok {
          return c.NoContent(http.StatusUnauthorized)
      }

      // 2. Read body
      body, err := io.ReadAll(c.Request().Body)
      if err != nil {
          return c.NoContent(http.StatusBadRequest)
      }

      // 3. Parse ChatwootWebhookPayload
      var payload ChatwootWebhookPayload
      if err := json.Unmarshal(body, &payload); err != nil {
          return c.NoContent(http.StatusBadRequest)
      }

      // 4. Validate filter (D-06): message_type == "outgoing" && private == false && sender.type == "user"
      if payload.MessageType != "outgoing" || payload.Private || payload.Sender.Type != "user" {
          return c.NoContent(http.StatusOK) // Return 200 to acknowledge webhook
      }

      // 5. Query mappingRepo by both workspace ID and Chatwoot Conversation ID to retrieve local contact ID and channel (ensuring tenant isolation)
      // 6. Push message out through NATS messages.outbound or outboundProcessor
      return c.NoContent(http.StatusOK)
  }
  ```

---

### 7. Admin Configuration Endpoint (`admin/integration.go`)
**Analog:** `internal/api/handler/admin/waba_template.go`

* **Core pattern:**
  ```go
  type ChatwootAdminHandler struct {
      integrationRepo *repository.IntegrationRepository
  }

  func (h *ChatwootAdminHandler) GetSettings(c *echo.Context) error {
      // Render HTMX form for configuration settings (fields: ApiURL, AccessToken, InboxID, AccountID)
  }

  func (h *ChatwootAdminHandler) PostSettings(c *echo.Context) error {
      // Bind inputs, validate parameters, encrypt config, and save in integrations table
  }
  ```

---

## Shared Patterns

- **Authentication/guards:**
  All public API requests under `/api/integrations/*` run through `AuthMiddleware` which intercepts the API query parameter `?token=YOUR_API_KEY` (mapped internally to `api_key` hashes) to load `WorkspaceID` context.
  
- **Error handling wrappers:**
  Repositories must return custom errors such as `ErrIntegrationNotFound` (similar to `ErrConnectionNotFound`) which translate to specific HTTP status codes inside handlers. Use `errors.Is` to check database result errors.

- **Logging patterns:**
  Standard library `log/slog` structured logging must be used at all handlers, models, and clients:
  ```go
  slog.Info("chatwoot webhook received", "workspace_id", workspaceID, "conversation_id", payload.Conversation.ID)
  slog.Error("failed to sync message to chatwoot", "error", err, "contact_id", contactID)
  ```

- **Response formatting:**
  Public webhook handlers respond with standard empty status code bodies (`c.NoContent(http.StatusOK)` or `http.StatusBadRequest`). Admin configuration endpoints verify HTMX headers via `mw.IsHTMX(c)` and render raw templated HTML components or fragments via `mw.Render()`.
