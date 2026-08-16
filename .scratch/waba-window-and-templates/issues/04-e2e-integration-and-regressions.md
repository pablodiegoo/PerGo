# 04 — E2E Integration Verification & Regressions

**What to build:** Run comprehensive end-to-end integration tests and regression suites covering Customer Service Window lifecycle, template modal workflows, and multi-channel communication (WhatsApp Cloud, WhatsApp Web, Telegram).

**Blocked by:** 01 — Customer Service Window Normalization, 02 — Global Modal Infrastructure & Inbox Template Triggering, 03 — Dynamic Template Parameter Resolution in Connection Testing

**Status:** implemented

- [x] All Go unit and integration tests pass with race detection (`go test ./... -race`).
- [x] Template codegen (`templ generate`) succeeds without compilation errors or diff conflicts.
- [x] WhatsApp Web (whatsmeow) and Telegram messaging paths operate without unintended customer service window blocks.
- [x] Expiry error response formatting (`422 SESSION_WINDOW_EXPIRED`) remains compliant with API contracts.
