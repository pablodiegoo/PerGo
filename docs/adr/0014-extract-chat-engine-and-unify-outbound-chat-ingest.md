# 0014. Extract Deep Chat Engine and Unify Outbound Conversational Ingest

Status: accepted

## Context

Conversational triage, thread inspection, **Chat Snooze**, **Internal Note** creation, **Message Reaction** mutation, **Human Handoff Trigger**, and conversational outbound sends were previously orchestrated directly inside three separate transport adapters (`InboxHandler` for the **Shared Team Inbox**, `ChatAPIHandler` for the **Public Chat REST API**, and `mcp.Server` for MCP tools).

Because each transport adapter coordinated `ChatRepository`, `ContactRepository`, `WorkspaceRepository`, `AuditRepository`, and NATS `Publisher` independently, six behavioral divergences emerged:
1. MCP tools omitted lazy **Chat Snooze** expiration reopen (`SnoozedUntil < now`) on read.
2. MCP status and label tools mutated `chats` without synchronizing `ContactRepository` (`CloseThread`/`ReopenThread` and `SetTags`).
3. `ChatAPIHandler.SendMessage` flipped database flags on human send without publishing NATS **Human Handoff Trigger** events (`chat.handoff.human_takeover` and `chat.handoff.<workspace_id>`).
4. `InboxHandler.SendMessage` and `ChatAPIHandler.SendMessage` bypassed `OutboundProcessor.Ingest` and published directly to `"messages.outbound"`, skipping per-workspace queue-depth backpressure and WABA template validation.
5. MCP send, reply, and **WABA Direct Chat Initiation** tools called `OutboundProcessor.Ingest` (which inserted a `ChatMessage` keyed by `UID = traceID`) and then called `ChatRepository.AddChatMessage` a second time with the same `UID`, causing `ON CONFLICT (uid) DO NOTHING` to silently drop `ReplyToUID` and template metadata.
6. `cmd/pergo/main.go` registered duplicate `/api/v1/chats/:chat_id/notes` and `/summarize` routes on `InboxHandler` alongside `ChatAPIHandler`.
7. **Message Reaction** toggle operations read existing reactions outside the `SELECT ... FOR UPDATE` transaction (TOCTOU race), `ChatAPIHandler.React` omitted NATS and webhook fan-out entirely, and `DefaultDispatcher.DispatchReaction` relied on an unexported `subscriptionLister` runtime type assertion while callers duplicated manual webhook subscription loops.
8. `InboxHandler` assembled conversation lists and message threads from `audit_logs` via `AuditRepository.ListConversations` (followed by two Go-level N+1 loops calling `FindOrCreateChat` and `ContactRepository.HasUnread`) and `AuditRepository.ListThreadByContact` (a 3-way `UNION ALL` joining `audit_logs` to `chat_messages`), while `ChatAPIHandler` and `mcp.Server` read directly from `chats` and `chat_messages`. Furthermore, `InboxHandler.ChatPanel` contained a 45-line dead loop computing **Customer Service Window** expiration from `recipient_sessions` before overwriting it with `Chat.IsServiceWindowOpen()`.
9. `cmd/pergo/main.go` instantiated `inbound.NewDebouncer(rdb)` without a `Publisher` or `CoalescedHandler` (causing `Debouncer.Flush` to silently discard coalesced turns in production), and `InboundProcessor.Process` auto-reset `contact.BotActive = true` after 12 hours of operator inactivity without resetting `chat.AIDisabled = false`, leaving `Debouncer` and `InboundRouter` permanently bypassed.
10. `cmd/pergo/main.go` duplicated core dependency wiring across its HTTP server mode and `pergo mcp` stdio mode, leaving `OutboundProcessor` in `pergo mcp` mode without `windowChecker`, `wabaTemplateRepo`, `chatRepo`, `contactRepo`, or `mediaEngine`, while `runServer` at the bottom of `main.go` was a no-op stub that forced integration tests to hand-roll route and dependency assembly.

## Decisions

1. **Extract a Deep `Chat Engine` Module (`internal/chat`)**:
   - All three transport adapters (`InboxHandler`, `ChatAPIHandler`, and `mcp.Server`) delegate **Chat** queries, triage mutations (`Assign`, `UpdateTags`, `UpdateStatus`, `SetAIEnabled`), `CreateNote`, `React`, `Summarize`, `SendMessage`, and `PromoteDraft` to `chat.Engine`.
   - `chat.Engine` accepts a polymorphic `ChatRef` (`ChatID`, `ContactID`, `MessageUID`, or `Channel + Phone`), encapsulating `FindOrCreateChat` resolution behind its seam.
   - `chat.Engine` unconditionally enforces lazy **Chat Snooze** reopen on reads, bidirectional `Chat` ↔ `Contact` synchronization on status and tag mutations, **Workspace Member** verification on assignment, and NATS + audit emission on **Human Handoff Trigger**.
2. **Single Outbound Writer via `OutboundProcessor.Ingest`**:
   - Extend `domain.CreateMessageRequest` with optional conversational context (`ChatID`, `ReplyToUID`, `SenderType`, `SenderName`, `DisableAI`, `StrictServiceWindow`).
   - `chat.Engine.SendMessage` and `PromoteDraft` never write outbound `chat_messages` directly; they resolve the target **Chat** and **Contact** and delegate to `OutboundProcessor.Ingest`, which serves as the single writer of outbound `chat_messages` after enforcing queue-depth backpressure, **Customer Service Window**, and WABA template rules.
3. **Single REST Route Owner for `/api/v1/chats/*`**:
   - Remove `InboxHandler.APICreateNote` and duplicate `/api/v1/chats/:chat_id/*` registrations in `cmd/pergo/main.go` so `ChatAPIHandler` is the sole HTTP REST adapter for `/api/v1/chats`.
4. **Atomic Message Reaction Mutation & Typed Webhook Fan-Out**:
   - Consolidate reaction reads and updates into a single transactional `ChatRepository.MutateReaction(ctx, workspaceID, messageUID, emoji, sender, action)` that evaluates `"add" | "remove" | "toggle"` inside the `SELECT chat_id, reactions ... FOR UPDATE` lock.
   - Promote `ListByWorkspace` onto `webhook.SubscriptionStore` (removing the private `subscriptionLister` type assertion) and promote `DispatchReaction` onto `webhook.WebhookDispatcher`, so `chat.Engine.React` and `InboundProcessor` share one reaction fan-out path.
5. **Single Source of Truth for `Chat` Threads, Unread Counts, and `Customer Service Window`**:
   - Make `chats` and `chat_messages` the sole source of truth for conversation lists, unread counts (`chats.unread_count` synchronized with `contacts.last_read_at` inside `chat.Engine`), and message threads across all transport adapters, removing `ListConversations` and `ListThreadByContact` from `AuditRepository` and eliminating the N+1 queries in `InboxHandler.loadConversations`.
   - Unify **Customer Service Window** evaluation (24h standard / 72h CTWA referral) between `Chat.IsServiceWindowOpen` (`chats.service_window_expires_at`) and `session.WindowChecker` (falling back to `recipient_sessions` for stateless dispatches), and remove the dead session-loop block in `InboxHandler.ChatPanel`.
6. **Deepen `InboundProcessor` + `Debouncer` + `Human Handoff Trigger` Seam**:
   - Bind `InboundProcessor`'s `Publisher` into `Debouncer` automatically when attached (and register `Debouncer.Close()` in graceful shutdown) so `chat.inbound.coalesced` events cannot be silently dropped due to missing option wiring.
   - Synchronize both `chats.ai_disabled` and `contacts.bot_active` / `bot_paused_at` across **Human Handoff Trigger**, manual AI resume, and the 12-hour inbound auto-reset, cancelling any pending Redis debounce buffer for `(workspace_id, contact_id)` upon human takeover and gating `Debouncer` / `InboundRouter` on `!chat.AIDisabled && contact.BotActive`.
   - Delegate inbound conversational state updates (reopening `snoozed`/`closed` chats, incrementing `unread_count`, updating `last_message_at` and 24h/72h **Customer Service Window**, and recording `ChatMessage` / **Message Reaction**) to the shared `Chat` domain logic.
7. **Unified Dual-Mode Bootstrap & Testable HTTP Router Assembly**:
   - Extract a shared core dependency builder in `cmd/pergo` (initializing repositories, `encryptor`, `mediaEngine`, `windowChecker`, `webhookDispatcher`, `outboundProcessor`, `chat.Engine`, `inboundProcessor` + `debouncer`, and `mcpServer`) used by both the HTTP server mode and `pergo mcp` stdio mode.
   - Extract `*echo.Echo` route and middleware assembly into a testable function consuming the bootstrapped core so integration tests exercise the real production route tree.

## Consequences

- **High Leverage & Locality**: All conversational state transitions, **Chat Snooze** expiration, **Human Handoff Trigger** propagation, **Message Reaction** locking, and **Customer Service Window** checks live behind `chat.Engine` and `OutboundProcessor.Ingest`, shrinking `InboxHandler`, `ChatAPIHandler`, and `mcp.Server` into thin transport adapters.
- **Elimination of Silent Production Bugs**: Fixes the double-write `ON CONFLICT (uid) DO NOTHING` metadata loss on MCP sends, the silent drop of `chat.inbound.coalesced` in `Debouncer.Flush`, the stuck `chat.AIDisabled` flag after the 12-hour bot auto-reset, the TOCTOU race on reaction toggles, and the unconfigured `OutboundProcessor` in `pergo mcp` stdio mode.
- **Testability**: Tests can target the deep `chat.Engine`, `OutboundProcessor`, and `InboundProcessor` interfaces directly using `local-substitutable` PostgreSQL repositories (`testcontainers-go`) and lightweight `ports & adapters` fakes for NATS/webhooks, replacing brittle tests that previously seeded `audit_logs` to test inbox rendering.
