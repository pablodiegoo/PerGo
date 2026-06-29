---
status: complete
date: 2026-06-29
description: Implementar inboxes simplificadas para cada canal, submenu de Audit Log separado (Inbound/Outbound) e gravação de logs de Outbound
---

# Quick Task: inboxes-outbound - Summary

## Work Done
1. **Ticker-based Buffer Flush (Latency Gap Closed)**:
   - Updated the audit `BatchWriter` worker loop inside `internal/platform/audit/batch.go` to use a `50ms` ticker.
   - Closed the architectural gap where audit logs would sit in memory indefinitely (until 100 entries were reached) instead of flushing within 50ms as specified in latency requirements.
2. **Outbound Message Auditing**:
   - Refactored `Dispatcher.Dispatch` to return `(string, error)`, allowing adapters to return raw response payloads received from Meta, Telegram, and whatsmeow.
   - Updated `queue.Worker` to receive the `auditWriter` and record `outbound_message` events in real-time when dispatch succeeds or fails.
3. **Sidebar Submenus & Channel Inboxes**:
   - Updated `sidebar.templ` navigation to group items under "Audit Logs" (Inbound, Outbound) and "Inboxes" (WhatsApp Web, WhatsApp Cloud, Telegram) section headers.
   - Added a `Channel` filter to `AuditRepository` querying `payload->>'channel'` dynamically.
   - Implemented `inbox.templ` and `inbox.go` controller to display received messages grouped by channel.
4. **Split Audit Logs (Inbound / Outbound)**:
   - Split `/admin/audit` routes in `main.go` and `audit.go` handler into `/admin/audit/inbound` and `/admin/audit/outbound` with custom table structures, filtering, and CSV exports.
   - Refactored integration tests in `admin_audit_test.go` to adapt to the split paths and event types.
5. **Verification**:
   - Successfully compiled the templates via `make generate` and verified that all tests pass (`go test ./...`).
