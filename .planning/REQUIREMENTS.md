# Project Requirements

## Active Requirements

- **WABA-01**: Support rich interactive messages (lists/buttons) with a channel override escape hatch in the `POST /messages` payload. The API should accept unified schema for common components and pass raw JSON configurations via `channel_overrides.whatsapp` directly into WABA/WhatsApp Web.
- **TELE-01**: Support Telegram Inline Keyboards and Forum Threads routing via the generic `POST /messages` payload schema.
- ~~**INSTA-01**: Support Instagram Quick Replies, Generic Templates, and inbound Story handling within the platform's unified schema.~~ *(Completed 2026-07-20)*

## Traceability Matrix

| Requirement | Phase | Goal |
|---|---|---|
| WABA-01 | 25 | Implement JSON-to-Protobuf mapping for rich interactive messages |
| TELE-01 | 26 | Implement Telegram Inline Keyboards and Forum Threads mapping |
| INSTA-01 | 27 | Implement Instagram Stories handling and Quick Replies mapping |
