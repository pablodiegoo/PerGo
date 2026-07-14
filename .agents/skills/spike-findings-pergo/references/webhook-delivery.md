# Webhook Delivery & Signature Security

## Requirements

- Must decouple webhook delivery from raw socket listener loops (whatsmeow/WABA) to prevent targets from blocking ingestion.
- Must verify event source authenticity via custom cryptographic signatures.
- Must queue failed deliveries to a Dead Letter Queue (DLQ) for auditing.

## How to Build It

1. **Queue Decoupling:** Publish raw events immediately to a NATS JetStream subject (`webhooks.events`). Run asynchronous worker consumers that read from the queue and invoke the HTTP post request.
2. **Payload HMAC Signing:** Compute the signature using the SHA256 hash of the concatenated timestamp, delimiter (`.`), and payload bytes:
   ```go
   func SignPayload(payload []byte, secret []byte, timestamp string) string {
       mac := hmac.New(sha256.New, secret)
       mac.Write([]byte(timestamp))
       mac.Write([]byte("."))
       mac.Write(payload)
       signature := hex.EncodeToString(mac.Sum(nil))
       return fmt.Sprintf("t=%s,v1=%s", timestamp, signature)
   }
   ```
3. **HTTP Delivery Header:** Set headers before execution:
   ```go
   req.Header.Set("Content-Type", "application/json")
   req.Header.Set("X-PerGo-Signature", signature)
   if traceID != "" {
       req.Header.Set("X-Trace-ID", traceID)
   }
   ```

## What to Avoid

- **Synchronous Postbacks:** Never perform webhook HTTP POST requests within the protocol connection handlers (e.g., whatsmeow callback loops). High latency or timeouts in receiving servers will freeze connection heartbeats.
- **Plain Text Verification Secrets:** Workspace secrets should be stored encrypted at rest or strictly filtered from logs.

## Constraints

- Webhook clients must set an explicit timeout (e.g., 10 seconds) to prevent infinite socket hangs during unresponsive delivery requests.

## Origin

Synthesized from spikes: 013, 014
Source files available in: sources/013-queue-decoupled-webhook-dispatcher/, sources/014-hmac-webhook-verification/
