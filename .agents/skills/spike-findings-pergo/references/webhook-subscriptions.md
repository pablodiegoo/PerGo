# Multi-Webhook Event Subscriptions

## Requirements

- Must support registering multiple webhook endpoints per workspace.
- Webhooks must filter event dispatches based on event types (e.g. `message.received`, `connection.status`).
- Must support wildcard subscriptions (`*`) to receive all workspace activities.

## How to Build It

1. **Subscription Mapping:** Configure subscription structs in the workspace schema.
   ```go
   type WebhookSubscription struct {
       URL        string
       EventTypes []string
   }
   ```

2. **Event Router Routing:** Prior to dispatching, filter the URL targets matching the event type:
   ```go
   func RouteEvent(subs []WebhookSubscription, eventType string) []string {
       var urls []string
       for _, sub := range subs {
           matches := false
           for _, t := range sub.EventTypes {
               if t == eventType || t == "*" {
                   matches = true
                   break
               }
           }
           if matches {
               urls = append(urls, sub.URL)
           }
       }
       return urls
   }
   ```

## What to Avoid

- **Synchronous Nested Loops:** Do not dispatch to multiple URLs sequentially in a blocking block. If a workspace has 5 webhooks, dispatch them concurrently to prevent HTTP delays from accumulating.
- **Redundant Signature Calculation:** Avoid re-calculating the HMAC-SHA256 signature for each target if the secret key is shared across the workspace. Compute it once and copy it.

## Constraints

- Ensure wildcard routing acts as a fallback or parallel channel.

## Origin

Synthesized from spikes: 018
Source files available in: sources/018-multi-webhook-subscriptions/
