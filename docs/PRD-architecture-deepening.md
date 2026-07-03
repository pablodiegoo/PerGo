# PRD: Architecture Deepening — Worker, Inbound, Types, Credentials

## Problem Statement

PerGo's outbound dispatch pipeline, inbound message processing, and credential handling are spread across shallow modules with oversized interfaces. Testing any of these subsystems requires wiring 5–8 real dependencies (PostgreSQL, NATS, whatsmeow, S3, AES-256-GCM). Bug fixes in the fallback loop or inbound deduplication require manual end-to-end validation because there's no test surface. Type duplication adds friction to every template schema change.

## Solution

Deepen four modules so each has a small interface and a large, testable implementation behind it. The test surface becomes the module interface — no real infrastructure needed for tests.

## User Stories

1. As a developer, I want to test the entire dispatch pipeline (idempotency, TTL, fallback, retry) through a single interface, so that I can verify routing behavior without running PostgreSQL, NATS, or live channel adapters.
2. As a developer, I want to test inbound WhatsApp message processing (media, dedup, PII, publish) without a live whatsmeow client, so that I can catch inbound bugs before deploying to production.
3. As a developer, I want template types defined once in the domain package, so that adding a template field doesn't require updating two identical structs and a conversion function.
4. As a developer, I want to test repository query logic without setting up AES-256-GCM encryption, so that connection and credential repository tests are fast and focused on SQL correctness.
5. As an operator reviewing the codebase, I want dispatch logic concentrated in one module rather than spread across the worker's flat 280-line function, so that understanding the retry and fallback behavior doesn't require tracing through 8 injected dependencies.
6. As a developer, I want the worker consumer loop to delegate message processing to an orchestrator, so that the JetStream consumer lifecycle (connect, reconnect, drain) is separate from business logic.
7. As a developer adding a new channel adapter, I want to register it with the dispatcher registry without touching the dispatch orchestration or fallback logic.
8. As a developer debugging a credential encryption issue, I want the encryption port to be swappable in tests so that I can isolate whether the bug is in the crypto layer or the repository layer.

## Implementation Decisions

### DispatchOrchestrator (ADR-0001)

- Extract processing logic from Worker into a new DispatchOrchestrator module.
- Interface: `Process(ctx context.Context, msg DispatchMessage, qMsg *QueueMessage) error`.
- The orchestrator owns gatekeeping (idempotency, TTL, dedup), routing (fallback loop), and side effects (audit, webhooks, queue depth).
- `jetstream.Msg` is abstracted behind a `DispatchMessage` port with `Data()`, `Headers()`, `Ack()`, `NakWithDelay(time.Duration)`.
- JSON deserialization stays in the worker loop; the orchestrator receives the parsed struct.
- Transient errors NAK with delay (JetStream retries); terminal errors advance to the next fallback channel.
- Constructor reduces from 8 to 6 dependencies.

### InboundProcessor (ADR-0002)

- Extract the WhatsApp event handler (130-line anonymous function) from the Session Manager into an InboundProcessor module.
- Interface: `Handle(ctx, waMsg, media, workspaceID, senderJID) error`.
- Accepts raw `*waEvents.Message` directly — no port abstraction (one adapter, hypothetical seam).
- WhatsApp CDN media download stays in the thin event handler adapter; only the processor handles S3 upload.
- Constructor takes dedupRepo, wsRepo, s3Client, publisher, auditWriter.
- The Session Manager's `reconnectDevice` becomes a thin adapter: download from WhatsApp CDN, then delegate to InboundProcessor.

### Unify TemplateComponent (ADR-0003)

- Delete `TemplateComponent` and `TemplateParameter` from `channel/dispatcher.go`.
- `channel.MessagePayload.Components` changes type from `[]TemplateComponent` to `[]domain.TemplateComponent`.
- Delete `convertTemplateComponents` from worker.go; use `qMsg.Components` directly.
- Update `waba_test.go` type references (`channel.TemplateComponent` → `domain.TemplateComponent`).

### CredentialProvider Port (ADR-0004)

- Define `CredentialProvider` interface in `internal/repository/` with `Encrypt([]byte)` and `Decrypt([]byte)`.
- Both `ConnectionRepository` and `CredentialsRepository` take `CredentialProvider` instead of `*crypto.Encryptor`.
- Production adapter: `crypto.Encryptor` (AES-256-GCM envelope).
- Test adapter: no-op provider returning plain bytes.
- Port owned by repository package; crypto package is the adapter.

## Testing Decisions

- **Only test external behavior through module interfaces** — no assertions on internal state or implementation details.
- **Fake adapters over mocks** — use in-memory, no-op implementations rather than mock frameworks. Adapters record calls for assertion where needed (spy pattern for audit writer, publisher).
- **Table-driven tests** — one `t.Run` per scenario, consistent with existing patterns in `waba_test.go` and `dispatch_test.go`.

### DispatchOrchestrator tests

- 6 scenarios: already-sent (idempotency), TTL expired, first channel success, terminal fallback, all channels terminal, transient retry.
- Assert on: Ack vs NakWithDelay, final dispatch status, webhook events published, audit events written.

### InboundProcessor tests

- 8 scenarios: text-only, image with media, duplicate message, PII disabled, PII enabled + location, media too large (>25MB), S3 upload failure, empty message.
- Assert on: payload published to NATS, audit events, S3 upload calls.

### Unify TemplateComponent

- Update existing `waba_test.go` type references. No new tests — the types are structurally identical.

### CredentialProvider

- Repository tests use no-op provider. Assert on SQL correctness without crypto setup.
- Separate test for the `crypto.Encryptor` adapter itself (existing pattern in the codebase).

## Out of Scope

- Changes to the JetStream consumer/producer setup
- New channel adapters or provider integrations
- Admin panel changes
- Schema migrations
- Configuration or environment variable changes
- Performance benchmarking or load testing of the deepened modules
- Observability or metrics changes

## Further Notes

- ADRs 0001–0004 are in `docs/adr/` with full decision context.
- All changes are internal package restructuring — no public API changes, no breaking changes to external consumers.
- The deepened modules keep the same behavior guarantees as the current code. This is purely a refactoring to improve testability and locality.
