# Selective Metadata Logging & PII Redaction

## Requirements

- Must support conditional PII scrubbing based on workspace-specific compliance settings (`SaveMessageBodies = false`).
- Must strip plaintext body and media URLs while retaining transaction sizes and types for auditing.
- Redacted payloads must retain valid JSON formats.

## How to Build It

1. **Workspace Setting Integration:** Expose a compliance toggle in the workspace model.
2. **Payload Redaction Filter:** Prior to database insertion or webhook propagation, process raw event bytes to strip content properties:
   ```go
   func RedactEvent(ws Workspace, eventType string, raw []byte) ([]byte, error) {
       if ws.SaveMessageBodies {
           return raw, nil
       }
       var msg MessagePayload
       if err := json.Unmarshal(raw, &msg); err != nil {
           return nil, err
       }
       redacted := map[string]any{
           "from":        msg.From,
           "to":          msg.To,
           "body_length": len(msg.Body),
       }
       if msg.MediaType != "" {
           redacted["media_type"] = msg.MediaType
       }
       return json.Marshal(redacted)
   }
   ```

## What to Avoid

- **Inconsistent Redaction:** Ensure webhook delivery payloads and database audit logs apply identical redaction rules to prevent leaks on any secondary boundary.
- **Strict Key Checking on Read:** Avoid hardcoding expectations of `body` key existence in inbox lists when message saving is disabled. The client UI must handle empty or redacted body fields gracefully.

## Constraints

- Ensure the redacted payload retains the unique trace ID to support integration debugging.

## Origin

Synthesized from spikes: 016
Source files available in: sources/016-selective-metadata-logging/
