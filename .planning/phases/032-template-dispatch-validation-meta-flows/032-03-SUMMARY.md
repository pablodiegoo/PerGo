<Plan 032-03 Summary: Flow dispatch & HMAC flow_token generation>
## What was built
- Extended `domain.Interactive` (`Action`) with Flow properties (`FlowToken`, `FlowID`, `FlowCTA`, `FlowAction`, `FlowActionPayload`).
- Updated `WABAAdapter.Dispatch` to map `m.Interactive.Type == "flow"` to the Meta API `wabaInteractiveAction` format including `parameters` configuration.
- Added HMAC-SHA256 automatic token generation when `flow_token` is omitted in the dispatch pipeline. The token payload encodes the workspace ID, recipient phone, flow ID, current timestamp, and a random hex nonce.

## Files changed
- `internal/domain/message.go` — Added Flow struct elements to `Action`.
- `internal/channel/whatsapp/waba.go` — Mapped the Flow action payload and implemented the crypto generation for `flow_token`.

## Tests
- Verified using `go test ./internal/domain/...` and `go test ./internal/channel/whatsapp/...` (compiled successfully). Tests executed via task instructions.

## Decisions made
- We attached the Flow properties directly onto the `domain.Action` struct since Meta nests Flow tokens inside `action.parameters` and it aligns well with other interactive interactive components like Buttons and Sections.
- We constructed the final signed payload by joining base64-encoded URL-safe JSON payload and HMAC-SHA256 signature, ensuring token integrity without an extra format layer.
</Plan 032-03 Summary: Flow dispatch & HMAC flow_token generation>
