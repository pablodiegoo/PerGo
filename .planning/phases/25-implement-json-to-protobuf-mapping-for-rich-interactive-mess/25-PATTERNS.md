# Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages - Patterns

## Files to Create / Modify

### 1. `internal/domain/message.go`
- **Role:** Core domain models and structural validation.
- **Data Flow:** Expands `CreateMessageRequest` and `QueueMessage` schema to capture `Interactive`, `ChannelOverrides`, and `FallbackBehavior` from the incoming JSON payload. Propagates these structures down to JetStream.
- **Existing Analog:** `TemplateComponent` array and its associated validation (`TTLSeconds`, `Media`, etc.).
- **Concrete Code Excerpt:**
  ```go
  // Pattern: Unified Interactive struct definition
  type Interactive struct {
      Type   string   `json:"type"` // "button", "list"
      Header string   `json:"header,omitempty"`
      Body   string   `json:"body"`
      Footer string   `json:"footer,omitempty"`
      Action Action   `json:"action"`
  }

  type Action struct {
      Buttons []Button `json:"buttons,omitempty"`
      // Sections for list messages
  }

  // Pattern: Extensions to CreateMessageRequest / QueueMessage
  Interactive      *Interactive               `json:"interactive,omitempty"`
  ChannelOverrides map[string]json.RawMessage `json:"channel_overrides,omitempty"`
  FallbackBehavior string                     `json:"fallback_behavior,omitempty"` // "degrade", "fail"

  // Pattern: Gateway validation limits strictness (D-01)
  if req.FallbackBehavior != "" && req.FallbackBehavior != "degrade" && req.FallbackBehavior != "fail" {
      details = append(details, FieldError{
          Field:   "fallback_behavior",
          Message: "must be either 'degrade' or 'fail'",
      })
  }
  ```

### 2. `internal/domain/message_test.go`
- **Role:** Unit tests for domain layer validation.
- **Data Flow:** Validates that structural schema checks correctly pass/fail without strictly enforcing channel limits at the gateway layer.
- **Existing Analog:** Existing unit tests for `TTLSeconds` validation and `TemplateName` channel constraints.

### 3. `internal/outbound/processor.go`
- **Role:** Ingestion processing mapping.
- **Data Flow:** Extracts the new fields from `req` (`domain.CreateMessageRequest`) and maps them manually to `qMsg` (`domain.QueueMessage`) before publishing to NATS.
- **Existing Analog:** Explicit mapping of `req.TemplateName`, `req.Components` and `req.FallbackChannels` in `Processor.Ingest`.
- **Concrete Code Excerpt:**
  ```go
  // Pattern: Mapping fields for JetStream queue message construction
  qMsg := &domain.QueueMessage{
      // ... existing fields ...
      Interactive:      req.Interactive,
      ChannelOverrides: req.ChannelOverrides,
      FallbackBehavior: req.FallbackBehavior,
  }
  ```

### 4. `internal/channel/dispatcher.go`
- **Role:** Standard contract for channel adapters.
- **Data Flow:** Expands `MessagePayload` to pass `Interactive`, `ChannelOverrides`, and `FallbackBehavior` directly into `Dispatcher.Dispatch`.
- **Existing Analog:** Properties like `TemplateName` and `Components` in `MessagePayload`.
- **Concrete Code Excerpt:**
  ```go
  type MessagePayload struct {
      // ... existing fields ...
      Interactive      *domain.Interactive
      ChannelOverrides map[string]json.RawMessage
      FallbackBehavior string
  }
  ```

### 5. `internal/platform/queue/orchestrator.go`
- **Role:** Dispatch queue worker orchestration.
- **Data Flow:** Transfers new fields from the JetStream `domain.QueueMessage` to `channel.MessagePayload` during `dispatchToChannel`. Handles orchestration fallback automatically if an adapter returns `TerminalError`.
- **Existing Analog:** Field construction in `dispatchToChannel`.
- **Concrete Code Excerpt:**
  ```go
  // Pattern: Orchestrator payload mapping
  return dispatcher.Dispatch(ctx, &channel.MessagePayload{
      // ... existing fields ...
      Interactive:      qMsg.Interactive,
      ChannelOverrides: qMsg.ChannelOverrides,
      FallbackBehavior: qMsg.FallbackBehavior,
  })
  ```

### 6. `internal/channel/whatsapp/adapter.go`
- **Role:** WhatsApp Web (`whatsmeow`) Channel Adapter.
- **Data Flow:** Consumes `MessagePayload`. Maps unified `Interactive` schema to `waE2E.Message`. Interprets `channel_overrides.whatsapp` as Protobuf-encoded JSON and unmarshals it cleanly using `protojson`. Converts invalid payloads to plaintext if `fallback_behavior == "degrade"`.
- **Existing Analog:** The mapping blocks used for `waE2E.DocumentMessage` and `waE2E.ImageMessage`.
- **Concrete Code Excerpt:**
  ```go
  // Pattern: Raw JSON Protobuf Override (D-02)
  if override, ok := m.ChannelOverrides["whatsapp"]; ok {
      var msg waE2E.Message
      if err := protojson.Unmarshal(override, &msg); err != nil {
          return "", fmt.Errorf("whatsapp override unmarshal: %w", err)
      }
      // send directly bypassing unified mapping
  }

  // Pattern: Fallback Degradation (D-03)
  if isViolatingNativeLimits(m.Interactive) {
      if m.FallbackBehavior == "fail" {
          return "", channel.NewTerminalError(fmt.Errorf("native limits exceeded and fallback behavior is fail"))
      }
      // Degrade to text gracefully
      m.Body = degradeInteractiveToText(m.Interactive)
      m.Interactive = nil
  }
  ```

### 7. `internal/channel/whatsapp/waba.go`
- **Role:** WhatsApp Cloud (`whatsapp_cloud` / WABA) Channel Adapter.
- **Data Flow:** Maps `Interactive` objects into Meta's standard `"type": "interactive"` JSON structure. Treats `channel_overrides.whatsapp_cloud` as a complete override for the HTTP request body.
- **Existing Analog:** The manual marshaling done for `wabaTemplate` (`reqPayload.Type = "template"`).
- **Concrete Code Excerpt:**
  ```go
  // Pattern: Total Replacement Override for WABA
  if override, ok := m.ChannelOverrides["whatsapp_cloud"]; ok {
      return a.sendRequest(ctx, config.PhoneNumberID, config.Token, override)
  }
  ```

### 8. `internal/channel/whatsapp/adapter_test.go` and `internal/channel/whatsapp/waba_test.go`
- **Role:** Test coverage for adapter layer mappings (Nyquist verification).
- **Data Flow:** Verifies Protobuf `protojson` unmarshaling for overrides, asserts `waE2E.Message` construction for unified models, and ensures WABA formats meta JSON correctly. Validates `TerminalError` emissions on `"fail"` limits violation.
