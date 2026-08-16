# 01 — Customer Service Window Normalization

**What to build:** Ensure that receiving any inbound message from a contact via WhatsApp Cloud (WABA) immediately opens the 24-hour Customer Service Window (or 72-hour CTWA window) for that specific business connection. Operators in the Inbox and API callers can send freeform replies without encountering false "24h window closed" blocks or persistent warning banners after customer interactions.

**Blocked by:** None — can start immediately

**Status:** implemented

- [x] Inbound WhatsApp Cloud events stamp `InboundEvent.To` with the Connection's canonical `SenderIdentity`.
- [x] Inbound Processor records `recipient_sessions` using the contact's phone and the connection's sender identity.
- [x] Inbox Handler queries `RecipientSessionRepository` with the contact's identity paired with the active connection's sender identity.
- [x] The Inbox chat panel accurately hides the 24h closed warning banner when an active session within 24h exists.
- [x] Freeform messages sent via API within 24h of an inbound message pass ingestion window validation without errors.
