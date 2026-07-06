---
spike: 007
name: inbox-polling-stability
type: standard
validates: "Given an open active chat panel, when polling for new messages, then we can avoid infinite reloading loops and scroll jitter by updating the polling anchor after_id out-of-band."
verdict: VALIDATED
related: [005, 006]
tags: [ui, inbox, polling, htmx]
---

# Spike 007: Inbox Polling Stability

## What This Validates
This spike validates how to build a robust message polling loop using HTMX and Go, avoiding infinite loop redraws or duplicate event listener attachments when changing conversations.

## Research
In the initial implementation of the inbox polling, we registered a global client-side script block inside `chat_panel.templ` which listened to `DOMContentLoaded` and `htmx:afterSwap` to query DOM elements, grab the last message's UUID, rewrite the `hx-get` attribute on the poll anchor, and call `htmx.process(anchor)`.

This approach introduced two severe bugs:
1. **Accumulated Event Listeners:** Since the script block was inside the dynamically loaded chat panel content, clicking different conversations added a new global `htmx:afterSwap` event listener *every single time*. This resulted in duplicate handlers firing and initiating multiple simultaneous poll requests.
2. **Infinite Resets & Loading Loops:** Triggering `htmx.process(anchor)` inside the callback reset the internal timers and state, causing loop races and appending identical messages repeatedly when `after_id` defaulted to its raw initial value.

### Optimized Approach
1. **Server-side Initial Cursor**: When rendering the initial `ChatPanel` on the server, resolve the last message's ID and set it as `after_id` in the `hx-get` URL directly.
2. **Out-of-band Swap (OOB)**: When polling returns new messages, the server returns the message list AND an updated `<div id="chat-poll-anchor" hx-swap-oob="true" ...>` containing the updated `after_id` cursor.
3. **Pure HTMX Scroll**: Use `hx-swap="beforeend scroll:bottom"` on the poll anchor. HTMX handles scrolling the target container to the bottom automatically. No custom JS required.

## How to Run
1. Run the mock server:
   ```bash
   go run .planning/spikes/007-inbox-polling-stability/main.go
   ```
2. Open `http://localhost:8089` in your browser.
3. Click "Carlos Mendes" to load the chat panel.
4. Verify that:
   - Polling queries show `after_id` parameter containing actual message UUIDs in network logs.
   - Message list does not re-render or reload from the beginning of time.
   - Live generated messages append correctly to the bottom and scroll the window.
   - Sending replies adds outbound messages, which are immediately picked up by the next poll cycle with correct cursor progression.

## Results
**Verdict: VALIDATED**

Out-of-band (OOB) swapping of the poll anchor is extremely stable, completely eliminates custom client-side JS event listeners, and completely prevents infinite loops or duplicates.
