# Phase 27 Context

## Gray Areas Auto-Resolved
1. **Instagram Stories Mentions vs Replies:**
   - **Decision:** Map both to a unified `story_event` type in the `InboundEvent` struct, with a subtype field (mention or reply).
2. **Quick Replies Mapping:**
   - **Decision:** Map IG Quick Replies to the existing generic `Interactive` payload schema used by WhatsApp and Telegram.
3. **Story Media Expiration:**
   - **Decision:** Pass the raw CDN URL provided by Meta API directly in the payload. Do not build an internal media cache/proxy in this phase to maintain minimal footprint.
4. **Thread ID for IG:**
   - **Decision:** IG Direct doesn't use forum threads like Telegram, so `message_thread_id` will be left empty for IG.

## Implementation Guidelines
- Follow the patterns established in Phase 25 (WhatsApp Interactive) and Phase 26 (Telegram Inline Keyboards).
