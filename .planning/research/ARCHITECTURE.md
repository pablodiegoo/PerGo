# Architecture Research: WABA Deep Integration

## Integration Points

1. **Data Access & Repository Layer (`internal/repository/`)**:
   - `internal/repository/waba_template.go`: Owns PostgreSQL persistence (`waba_templates` table) for storing template definitions (`id`, `workspace_id`, `connection_id`, `meta_template_id`, `name`, `language`, `status`, `category`, `components`). Provides `Create`, `Upsert`, `GetByID`, `GetByNameAndLanguage`, `ListByWorkspace`, `ListByConnection`, `UpdateStatus`, `Delete`.
   - `internal/repository/recipient_session.go`: Owns `recipient_sessions` table, tracking customer service 24-hour windows (`last_inbound_at`) per workspace/recipient/channel combination.
   - `internal/repository/contact.go`: Owns `contacts` and `contact_identities` tables for unified contact resolution across channels (`ResolveContact`).

2. **Channel Adapter Layer (`internal/channel/whatsapp/`)**:
   - `WABAAdapter` (`waba.go`): Implements `channel.Dispatcher`. Responsible for outbound message dispatching, credential decryption, pre-flight 24-hour session window validation, template parameter mapping, and Meta Graph API POST execution.
   - `waba_template_client.go` (Meta Graph API Client): Calls Meta Graph API endpoint (`/{waba_account_id}/message_templates`) to perform remote CRUD and sync actions.
   - `waba_inbound.go`: Webhook handler parsing Meta Cloud API inbound payloads (messages, status updates, template status changes, Flow completion responses `nfm_reply`, and Commerce order events).

3. **Admin Console Layer (`internal/admin/`)**:
   - Templ + HTMX admin handlers: Surface template management UI (listing, template creation form, status badging, template deletion, and manual Meta Graph API sync trigger).

4. **Queue & Webhook Infrastructure (`internal/queue/` & `internal/webhook/`)**:
   - Inbound webhooks trigger state updates (e.g. updating 24-hour window timestamp in DB and updating template approval statuses), then broadcast events to workspace webhook subscribers.

---

## New Components

1. **Meta Graph API Template Client (`waba_template_client.go` in `internal/channel/whatsapp/`)**:
   - Encapsulates direct HTTP calls to Meta's Graph API for remote template creation, deletion, and fetching template lists from Meta WABA accounts.

2. **In-Memory Template Cache (`template_cache.go` in `internal/channel/whatsapp/`)**:
   - Thread-safe (`sync.RWMutex`) in-memory cache keyed by `connection_id:name:language`.
   - Eliminates database round-trips during hot-path outbound message dispatch (`POST /messages`).
   - Invalidated on template create/update/delete operations and when Meta fires `message_template_status_update` webhooks.

3. **WABA Interactive & Flow Transformers (`waba_interactive.go` in `internal/channel/whatsapp/`)**:
   - Dedicated payload transformers for Meta Flows (`nfm_reply` and `flow` action) and Commerce/Catalog messages (`product`, `product_list`, `catalog_message`).

4. **Session Window Checker (`waba_session_manager.go` in `internal/channel/whatsapp/`)**:
   - Concrete implementation of the `WindowChecker` interface backed by `RecipientSessionRepository`, validating whether the 24-hour messaging window is open for a target recipient.

---

## Data Flow Changes

### Outbound Dispatch Flow
`POST /messages` → Ingestion Gateway → NATS JetStream → WABA Channel Worker (`WABAAdapter.Dispatch`):
1. **Credential & Connection Lookup**: Fetch WABA credentials (`phone_number_id`, `token`, `waba_account_id`) from `connections` repository and decrypt AES-256-GCM payload.
2. **Session Window Check vs Template Dispatch**:
   - **Freeform Messages (`template_name == ""`)**: Check 24-hour session window via `windowChecker.IsWindowOpen(...)`. If expired (`time.Since(last_inbound_at) > 24h`), abort immediately with terminal 422 error (`"customer service window expired"`), preventing wasteful Meta API calls (Meta error 131047 / error code 470).
   - **Template Messages (`template_name != ""`)**: Lookup template definition in `TemplateCache` (falling back to `waba_templates` DB). Build Meta template component parameters.
   - **Interactive Messages (Flows / Commerce)**: Invoke `waba_interactive.go` transformers to structure Meta Graph API `interactive` object payload.
3. **Graph API Execution**: POST request sent to `https://graph.facebook.com/v25.0/{phone_number_id}/messages`.

### Inbound Webhook Flow
Meta Webhook → `POST /webhooks/waba` → `waba_inbound.go`:
1. **Inbound User Messages**:
   - Resolve contact via `ContactRepository.ResolveContact(...)`.
   - Update session timestamp via `RecipientSessionRepository.Upsert(...)` to open/extend the 24-hour window (`last_inbound_at = time.Now()`).
   - Extract message content (text, media, `nfm_reply` flow data, or `order` catalog data) and publish unified message to inbound processing queue.
2. **Template Status Update Webhook (`message_template_status_update`)**:
   - Extract template ID, status (`APPROVED`, `REJECTED`, `PAUSED`), and rejection reasons.
   - Update `waba_templates` record in PostgreSQL via `WABATemplateRepository.UpdateStatus(...)`.
   - Invalidate corresponding entry in `TemplateCache`.
   - Emit event to workspace webhook subscribers.

---

## Suggested Build Order

1. **Phase 1: 24-Hour Session Window & Identity Alignment**
   - Wire `RecipientSessionRepository` with `waba_inbound.go` to update `last_inbound_at` on every incoming message.
   - Integrate `windowChecker.IsWindowOpen` into `WABAAdapter.Dispatch` for freeform message validation.

2. **Phase 2: Template CRUD, Graph API Sync & In-Memory Caching**
   - Implement `waba_template_client.go` for Meta Graph API template operations.
   - Build `TemplateCache` in `internal/channel/whatsapp/` with DB fallback and webhook invalidation.
   - Connect admin console HTMX views to template repository & Graph API sync service.

3. **Phase 3: Interactive & Meta Flows Integration**
   - Extend `internal/domain/message.go` interactive schemas to support Meta Flow action properties.
   - Build `waba_interactive.go` flow transformers for outbound dispatch and `nfm_reply` inbound webhook parser.

4. **Phase 4: Commerce & Catalog Integration**
   - Extend interactive transformers for Single Product (`product`), Multi-Product (`product_list`), and Catalog messages.
   - Implement inbound `order` webhook payload parsing and event emission.

---

## Sources

- PerGo Codebase Architecture: `docs/architecture/01-architectural-summary.md`, `02-technical-decisions.md`
- Existing PerGo Repositories & Adapters:
  - `internal/channel/whatsapp/waba.go`
  - `internal/channel/whatsapp/waba_inbound.go`
  - `internal/repository/waba_template.go`
  - `internal/repository/recipient_session.go`
  - `internal/repository/contact.go`
  - `internal/domain/message.go`
- Meta WhatsApp Business Platform Cloud API Documentation (v25.0: Messages, Message Templates, Meta Flows, Commerce & Catalogs).
