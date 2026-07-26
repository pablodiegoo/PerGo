# Phase 27: Research Notes - Instagram Stories & Quick Replies

## 1. Domain Schema Updates (internal/domain/message.go)
- **ValidChannels**: Add `"instagram"` to the `ValidChannels` map so that the API accepts payloads for this new channel.
- **Interactive Schema**: The existing `domain.Interactive` struct is generic enough to support Instagram Quick Replies. We will map IG Quick Replies from this struct (e.g., `Type: "quick_reply"` or reusing `"button"` and `"list"`) to the Meta Graph API Instagram format.

## 2. Inbound Payload Extensions (internal/inbound/processor.go)
- **InboundStoryEvent**: Create a new unified struct for stories.
  ```go
  type InboundStoryEvent struct {
      Subtype  string `json:"subtype"`   // e.g., "mention" or "reply"
      MediaURL string `json:"media_url"` // Raw Meta CDN URL (no internal caching/proxying as per CONTEXT)
  }
  ```
- **InboundEvent & InboundEventPayload**: Append `Story *InboundStoryEvent` to both `InboundEvent` and `InboundEventPayload` structs.
- **Interactive**: `InboundInteractive` already exists and should be reused for mapping incoming IG Quick Reply actions. 
- **Thread ID**: IG direct does not use forum threads, so `message_thread_id` will remain unmapped/empty for this channel.

## 3. Channel Adapters (internal/channel/)
- **Instagram Channel Adapter**: Since Instagram uses the Meta Graph API (similar to WABA) but has distinct webhook formats (`messaging_product: "instagram"`, distinct story/mention payloads, generic templates vs WhatsApp templates), it is recommended to create a dedicated package `internal/channel/instagram/` with `adapter.go` and `inbound.go`.
- **Registry**: In `cmd/pergo/main.go`, instantiate `instagram.NewAdapter()` and register it to `dispatcherRegistry.Register("instagram", instagramAdapter)`.

## 4. Inbound Mapping (Webhooks)
- Parse Instagram-specific webhook payloads in `InstagramInboundAdapter.Parse()`.
- Map Instagram Story Mention and Story Reply payloads to `InboundEvent.Story`, passing the raw CDN URL directly.
- Map incoming Quick Reply interactions (when a user taps a quick reply button) to `InboundEvent.Interactive`.

## 5. Outbound Mapping (REST Adapter)
- Map `channel.MessagePayload.Interactive` into Meta Graph API JSON for Instagram Quick Replies and Generic Templates.
- Instagram's endpoint differs slightly from WABA (`https://graph.facebook.com/vX.X/{ig_user_id}/messages`). Ensure the HTTP dispatch logic points to the correct Messenger API endpoints.
