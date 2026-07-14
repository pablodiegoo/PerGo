# Declarative Messaging Flow Verbs Engine

## Requirements

- Must support parsing and executing sequential JSON-based messaging actions (verbs).
- Must handle actions: `reply` (message response), `wait` (timed delay), and `forward` (multi-channel redirection).
- Must abort execution immediately if the execution context is cancelled or times out.

## How to Build It

1. **Verb Payload Parsing:** Define the structured payload mapping:
   ```go
   type Verb struct {
       Action string          `json:"action"`
       Params json.RawMessage `json:"params"`
   }
   ```

2. **Sequential Execution Loop:** Iterate over the verb list, unmarshal parameters on demand, and check context cancellation at each step:
   ```go
   func (e *Engine) Execute(ctx context.Context, verbs []Verb, originalBody string) error {
       for i, verb := range verbs {
           select {
           case <-ctx.Done():
               return ctx.Err()
           default:
           }

           switch verb.Action {
           case "reply":
               // Unmarshal ReplyParams and dispatch
           case "wait":
               // Parse duration, block with select over <-time.After and <-ctx.Done
           case "forward":
               // Unmarshal ForwardParams and redirect
           }
       }
       return nil
   }
   ```

## What to Avoid

- **Blocking the Main Worker Thread:** Avoid running the execution loop synchronously inside queue consumer routines. Always spawn separate worker goroutines for long-running workflows with wait states.
- **Unbounded Wait Times:** Do not permit client webhooks to return infinite/very long wait durations without an engine-enforced maximum cap (e.g., max 10 seconds).

## Constraints

- Wait durations must be parsed using standard duration formats (`time.ParseDuration`).
- Outbound actions must validate recipient numbers before dispatching.

## Origin

Synthesized from spikes: 015
Source files available in: sources/015-messaging-verbs-engine/
