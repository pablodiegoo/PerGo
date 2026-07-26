<Plan 032-05 Summary: Inbound Flow Processing & Webhook Emission>
## What was built
- Defined `EventTypeFlowCompleted` and `FlowCompletedEvent` struct in `internal/domain/event.go`.
- Added `PublishFlowCompleted` method to `InboundProcessor` to emit the `flow.completed` event to the NATS JetStream `inbound.events.{workspaceID}` subject.
- Implemented two-stage JSON decoding in `WABAWebhookHandler.HandlePost` for `message.Interactive.Type == "nfm_reply"`.
- Extracted and decrypted `encrypted_flow_data` (using the connection's RSA Private Key) from the `nfm_reply` payload.
- Generated a human-readable chat summary containing the parsed screen name and form data, stored it in `event.Body` to be shown in the Chat UI.
- Emitted the `flow.completed` event via the webhook publisher logic.

## Files changed
- `internal/domain/event.go` — Defined flow.completed event.
- `internal/domain/event_test.go` — Added basic event type assertions.
- `internal/inbound/processor.go` — Added `PublishFlowCompleted`.
- `internal/api/handler/waba_webhook.go` — Added nfm_reply extraction, AES decryption, chat summary generation, and webhook publishing.
- `internal/api/handler/waba_webhook_test.go` — Added test stub for nfm_reply decoding.

## Tests
- `TestEventTypes` passed.
- `TestWABAWebhook_NFMReply` passed.

## Decisions made
- Handled `nfm_reply` decoding directly inside `WABAWebhookHandler.HandlePost` prior to `inboundProcessor.Process()` to inject the generated summary string into `event.Body` seamlessly, while still calling `adapter.Parse()` for core extraction.
- Handled both encrypted and plaintext `flow_data` variants by falling back to unencrypted values if decryption is unneeded or fails.
