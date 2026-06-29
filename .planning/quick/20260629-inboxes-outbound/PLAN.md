---
status: complete
date: 2026-06-29
description: Implementar inboxes simplificadas para cada canal, submenu de Audit Log separado (Inbound/Outbound) e gravação de logs de Outbound
---

# Plan - Inboxes de Canais e Audit Log de Outbound

Implement channel-specific inboxes for received messages, split the audit log interface into separate Inbound and Outbound submenus, and enable real-time database audit logging for all outbound message attempts.

## Tasks
1. **Outbound Audit Logging**:
   - Update `Dispatcher.Dispatch` signature in `internal/channel/dispatcher.go` to return `(string, error)` (capturing API response raw body).
   - Update implementations in `telegram.go`, `waba.go`, `adapter.go` and their test mocks.
   - Update `queue.Worker` to receive `auditWriter` and record `outbound_message` audit events upon dispatch success and failure attempts.
2. **Channel Inboxes UI & Routing**:
   - Extend `AuditRepository` with `Channel` filter to allow querying audit logs by JSONB extracted payload channel (`payload->>'channel' = $x`).
   - Create `inbox.templ` page template and `inbox.go` controller to display simplified channel-specific lists of received messages.
   - Wire inbox routes `/admin/inbox/:channel` on the Echo router.
3. **Split Audit Log Interface**:
   - Re-wire audit routes to support `/admin/audit/inbound` and `/admin/audit/outbound` with separate list and CSV export endpoints.
   - Update `sidebar.templ` navigation to group submenus under "Audit Logs" and "Inboxes" section headers.
4. **Verification**:
   - Run `make generate` to compile the frontend templates.
   - Run `go test ./...` to verify all unit/integration tests compile and pass.
