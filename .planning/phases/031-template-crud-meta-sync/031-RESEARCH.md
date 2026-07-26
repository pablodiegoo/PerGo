# Phase 31: Template CRUD, Meta Graph API Sync & Local Cache - Research

**Objective:** What do I need to know to PLAN this phase well?

## 1. Existing Patterns
- **Repository Pattern:** `internal/repository/waba_template.go` already defines `WABATemplateRepository` with standard CRUD operations (`Create`, `Upsert`, `GetByID`, `UpdateStatus`, etc.). The `WABATemplate` struct lacks `RejectionReason` and `QualityScore` fields which must be added.
- **Cache Pattern:** In Phase 29, an in-memory map cache guarded by `sync.RWMutex` was added directly inside the `ConnectionRepository`. We will replicate this pattern in `WABATemplateRepository`, caching templates by `ConnectionID`.
- **Meta API Client:** `internal/api/handler/admin/device.go` contains an inline method `syncTemplatesFromMeta` that makes a `GET` request to Meta's `/v18.0/{waba_id}/message_templates`. This will serve as the basis for a dedicated Meta API client.
- **Webhook Pattern:** `internal/api/handler/waba_webhook.go` already handles verification and incoming WhatsApp events. It uses a switch case to route event types. We need to catch the `message_template_status_update` event.
- **Admin UI Pattern:** Standard HTMX + DaisyUI server-side rendered Go templates. Forms and tables fit into a Notion-inspired monochromatic UI layout.

## 2. Integration Points
- **Schema Migration:** A new migration file (e.g., `032_add_quality_and_rejection_to_waba_templates.sql`) is required to append `rejection_reason` (TEXT) and `quality_score` (VARCHAR) to the existing `waba_templates` table.
- **Webhook Handler Extension:** `waba_webhook.go` needs to process `message_template_status_update` events, calling `WABATemplateRepository.UpdateStatus` and updating quality score / rejection reason, followed by invalidating/updating the local cache.
- **UI Routes & Controllers:** Add endpoints under `/api/v1/waba/templates` and new Admin UI handlers for `/connections/:slug/templates` mimicking existing patterns.
- **Meta Graph Client Extraction:** A dedicated client package/struct is needed to encapsulate `Create`, `Delete`, and `Sync` logic so that it can be invoked by both the REST API, Admin UI, and webhooks.

## 3. Technical Approach
1. **Database Schema & Repo Update (TMPL-04, TMPL-06, TMPL-07):** 
   - Create SQL migration for `rejection_reason` and `quality_score`. 
   - Update the `WABATemplate` struct in `waba_template.go` to include these fields.
   - Update SQL statements in `WABATemplateRepository` methods (`Create`, `Upsert`, `UpdateStatus`).
2. **In-Memory Cache (TMPL-02):**
   - Add a `sync.RWMutex` and an inner map cache (`map[uuid.UUID]map[string]*WABATemplate` indexed by ConnectionID and Name+Language) inside `WABATemplateRepository`.
   - Implement an eager warm-up query to populate the cache on system startup.
   - Modify repository read methods to use the cache, avoiding DB hits.
3. **Meta API Client Extraction (TMPL-03):**
   - Extract `syncTemplatesFromMeta` from `DeviceHandler` into a generic WABA Meta client `waba_template_client.go`.
   - Implement rate-limiting (e.g., using a time map or Redis/cache key for the 15-minute window) for the sync trigger.
4. **Webhook Processing (TMPL-05, TMPL-07):**
   - Hook into `waba_webhook.go` to intercept `message_template_status_update`.
   - Update DB and immediately invalidate/refresh the in-memory cache for that connection.
   - Trigger a Toast notification / Log if the quality score drops from GREEN to YELLOW or RED.
5. **REST API & Admin UI (TMPL-01, TMPL-08, TMPL-09):**
   - Build Echo REST endpoints for template CRUD.
   - Build Admin UI screens: A listing table with status badges (using standard DaisyUI colors), and a creation form organized by Meta's component blocks (Header, Body with `{{X}}` params, Footer, Buttons).
   - Implement a live side-panel WhatsApp-style visual preview using HTMX debounced partials that triggers when the operator alters the form body.

## 4. Dependencies & Risks
- **Meta API Constraints:** Meta only allows deleting and recreating templates if they are approved. Edits are only permitted on rejected/paused templates. The backend must enforce these rules based on the cached status.
- **Rate Limiting:** The manual sync button (TMPL-03) must strictly enforce the 15-minute limit to prevent Meta API throttling.
- **Cache Staleness & Broadcast:** If PerGo runs in multiple instances, an in-memory cache updated by a webhook hit on one instance will leave other instances stale. As PerGo uses NATS JetStream, we may need to broadcast cache invalidation events over NATS if multi-instance is fully realized. For MVP single-instance, eager DB warm-up + direct map mutation is sufficient.
- **Cold-Start DB Hammering:** A 100k message burst right after restart would hammer the DB if the cache isn't eagerly populated on boot. Startup MUST pre-fetch all templates into memory.

## 5. Validation Architecture
- **TMPL-01 & TMPL-09:** Write unit/integration tests for the REST API endpoints simulating a mock Meta Graph API response for template creation.
- **TMPL-02:** Validate that the `WABATemplateRepository` cache accurately mirrors the mock DB state and serves queries without executing new SQL `SELECT`s.
- **TMPL-03:** Write a test simulating two sync requests within 15 minutes, expecting the second to return a rate-limit error (HTTP 429).
- **TMPL-04:** Create multiple templates with the same name but different languages (`en_US`, `pt_BR`) and assert they are stored and cached uniquely.
- **TMPL-05 & TMPL-06:** Submit a mock `message_template_status_update` webhook with `event: "REJECTED"` and a `reason`, and assert the local cache and DB are correctly updated.
- **TMPL-07:** Submit a mock webhook with a `quality_score` drop to RED and verify the alert log is triggered.
- **TMPL-08:** Manual visual validation of the Admin UI preview panel to ensure it correctly interpolates `{{1}}` style parameters into standard UI bubbles.

## 6. Code Excerpts

**Cache Pattern Example:**
```go
type WABATemplateRepository struct {
	pool      *pgxpool.Pool
	cache     map[uuid.UUID]map[string]*WABATemplate // connectionID -> "name_language" -> template
	mu        sync.RWMutex
}
```

**Client Interface:**
```go
type WABAMetaClient interface {
    CreateTemplate(ctx context.Context, token, wabaID string, tpl *WABATemplate) (*MetaResponse, error)
    DeleteTemplate(ctx context.Context, token, wabaID, name string) error
    SyncTemplates(ctx context.Context, token, wabaID string) ([]WABATemplate, error)
}
```

## RESEARCH COMPLETE
