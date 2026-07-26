# Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages - Research

## 1. Schema Extensions (Unified JSON)
To support rich interactive messages and channel-specific overrides, `domain.CreateMessageRequest` and `domain.QueueMessage` must be extended:
- **`Interactive`**: A unified structure representing `button` and `list` messages to abstract away provider-specific shapes (e.g. `Type`, `Header`, `Body`, `Footer`, `Action`).
- **`ChannelOverrides`**: A map (`map[string]json.RawMessage`) to pass native configurations directly, bypassing the unified model (e.g. `whatsapp`, `whatsapp_cloud`).
- **`FallbackBehavior`**: A string (`degrade` or `fail`) governing what happens if an interactive message cannot be dispatched or is unsupported by the adapter.

## 2. Gateway & Validation (D-01)
- **`domain.ValidateMessage`**: 
  - Ensure `FallbackBehavior` is either empty, `"degrade"`, or `"fail"`.
  - Validate the structural integrity of the `Interactive` schema (e.g., if `Type == "button"`, `Action.Buttons` must be non-empty; `Body` is required).
  - **Do not** validate channel-specific limits (like maximum of 3 buttons) here. Strict limits must be deferred to downstream adapters as per D-01.

## 3. Channel Adapters & Mapping
### WhatsApp Web (`whatsmeow` adapter)
- **Override (`channel_overrides.whatsapp`)**: Unmarshal the raw JSON payload directly into a `waE2E.Message` protobuf struct. Because `whatsmeow` uses standard `proto3`, `google.golang.org/protobuf/encoding/protojson` can seamlessly unmarshal the JSON.
- **Unified Mapping**: Convert the `Interactive` domain object into `waE2E.Message`. Depending on the type, this should populate `waE2E.ButtonsMessage` (for quick replies), `waE2E.ListMessage` (for lists), or `waE2E.InteractiveMessage` (for NativeFlow messages).

### WABA (`whatsapp_cloud` adapter)
- **Override (`channel_overrides.whatsapp_cloud`)**: If provided, inject the raw JSON directly into the WABA API HTTP payload as a complete replacement (per D-02).
- **Unified Mapping**: Convert the `Interactive` domain object into Meta's standard `"type": "interactive"` JSON structure.

## 4. Fallback & Orchestration (D-03)
When `DispatchOrchestrator` delegates to a channel adapter:
- If `fallback_behavior == "degrade"` and the adapter knows it cannot safely process the interactive elements (or if the user provided too many buttons, violating native limits), the adapter should gracefully convert the interactive message into a plain-text payload (e.g. text body + numbered list of buttons) before dispatching.
- If `fallback_behavior == "fail"`, the adapter must return a `TerminalError` if it violates native limits, prompting the `DispatchOrchestrator` to try the next channel in `FallbackChannels`.

## Validation Architecture
*(Required by nyquist_validation_enabled)*

To guarantee quality and prevent regressions, this phase must implement the following tests:

1. **Schema Validation Tests**: Add unit tests in `internal/domain/message_test.go` ensuring `FallbackBehavior` logic and `Interactive` structural checks work correctly, while ensuring native limits (like 4+ buttons) are correctly allowed to pass the gateway layer.
2. **WhatsApp Web Adapter Tests (`whatsmeow`)**: 
   - Verify `channel_overrides.whatsapp` properly unmarshals via `protojson` into the expected `waE2E.Message`.
   - Verify unified `Interactive` schema translates accurately into `waE2E.Message` variants.
   - Assert `fallback_behavior == "degrade"` correctly strips/transforms interactive components into a text payload on validation failure (e.g. >3 buttons).
3. **WABA Adapter Tests (`whatsapp_cloud`)**:
   - Ensure the unified schema generates valid Meta `"type": "interactive"` JSON structures.
   - Ensure `channel_overrides.whatsapp_cloud` completely replaces the interactive payload exactly as-is.
