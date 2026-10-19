# Meta WhatsApp Cloud API: Customer Service Window vs. Free Entry Point (CTWA) Semantics

> **Research Note — Issue [#160](https://github.com/pablodiegoo/PerGo/issues/160) (follow-up from ADR 0015 / [#154](https://github.com/pablodiegoo/PerGo/issues/154))**
>
> **Primary Sources Consulted:**
> 1. **Meta WhatsApp Business Platform — Pricing (Per-Message Pricing, effective July 1, 2025; updated Sep 30, 2026):**
>    - `https://developers.facebook.com/docs/whatsapp/pricing` (`/documentation/business-messaging/whatsapp/pricing.md`)
>    - Sections: *Non-template messages*, *When Meta does not charge*, *Free entry point window (FEP)*, *Customer service window (CSW)*
> 2. **Meta WhatsApp Business Platform — Service Messages & Customer Service Windows (updated May 21, 2026):**
>    - `https://developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages#customer-service-windows`
> 3. **Meta WhatsApp Business Platform — Conversation-Based Pricing (Deprecated July 1, 2025):**
>    - `https://developers.facebook.com/documentation/business-messaging/whatsapp/pricing/conversation-based-pricing#free-entry-point-conversations`
> 4. **PerGo Codebase & Architecture Records:**
>    - [`docs/adr/0015-chat-activity-single-writer-and-chat-as-source-of-truth.md`](../adr/0015-chat-activity-single-writer-and-chat-as-source-of-truth.md)
>    - [`CONTEXT.md`](../../CONTEXT.md) (*Customer Service Window*, *Entry Point Type*)
>    - [`internal/domain/chat.go`](../../internal/domain/chat.go) (`WindowDurationFor`, `ExtendServiceWindow`, `Window`, `RequireOpenWindow`, `Apply`)
>    - [`internal/channel/whatsapp/waba_inbound.go`](../../internal/channel/whatsapp/waba_inbound.go) (`wabaReferralObj` → `entry_point_type: "ctwa"`)

---

## 1. Question Investigated

[ADR 0015](../adr/0015-chat-activity-single-writer-and-chat-as-source-of-truth.md) and [`domain.ExtendServiceWindow`](../../internal/domain/chat.go) currently model a **72-hour Customer Service Window** when `EntryPointType == "ctwa"`, measured from the contact's inbound message timestamp (`at + 72h`), and use that expiration (`chats.service_window_expires_at`) to gate freeform (non-template) outbound messages via `domain.RequireOpenWindow`.

Issue [#160](https://github.com/pablodiegoo/PerGo/issues/160) asked us to verify against Meta's primary Cloud API documentation:
1. Does a Click-to-WhatsApp Ad (`ctwa`) referral extend the **Customer Service Window** (freeform / non-template message eligibility) to 72 hours, or is the 72-hour Free Entry Point window strictly a **pricing/billing** rule?
2. Does Meta's Free Entry Point window start from the **contact's inbound message** or from the **business reply** (sent within 24 hours)?

---

## 2. Findings from Meta Primary Sources

### 2.1. The Customer Service Window (CSW) is strictly 24 hours from inbound and governs freeform eligibility

Across both the current Per-Message Pricing documentation and the Service Messages specification, Meta defines the **Customer Service Window (CSW)** as a fixed **24-hour** timer triggered and reset by the WhatsApp user's inbound message (or call), regardless of how the user entered the thread:

- **Service Messages — Customer Service Windows** (`developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages#customer-service-windows`):
  > *"When a WhatsApp user messages you or calls you, a **24-hour timer** called a customer service window starts. If the user messages or calls you again before the timer expires, the timer resets to 24 hours. While the window is open, you can send any of the following service message types to the user. When the window closes, you can only send pre-approved template messages."*

- **Pricing — Non-template messages & Customer service window (CSW)** (`developers.facebook.com/docs/whatsapp/pricing#customer-service-window-csw`):
  > *"Non-template messages (`"type":"text"`, `"type":"image"`, and so on) can only be sent in an open **24-hour customer service window**, which opens and resets with each user message. Note that a business can also choose to use template messages to respond to users; however, non-template messages cannot be used to reach out to users."*
  >
  > *"When a WhatsApp user messages you or calls you, a 24-hour timer called a customer service window starts. If the user messages or calls you again before the timer expires, the timer resets to 24 hours. Businesses can only send non-template messages to users when this window is open. If it is not open, businesses can only send template messages to reach out to users."*

### 2.2. The Free Entry Point (FEP) window governs pricing only and starts on the business's reply

Meta's **Free Entry Point (FEP)** window is an independent **billing waiver** mechanic that requires a business response within the 24-hour CSW and starts when the business responds:

- **Pricing — Free entry point window (FEP)** (`developers.facebook.com/docs/whatsapp/pricing#free-entry-point-window-fep`):
  > *"If a WhatsApp user messages you via an ad that clicks to WhatsApp (CTWA) using a device running our Android or iOS app (since our desktop and web apps are not supported):*
  > - *A **customer service window** is opened.*
  > - *If you respond within the **customer service window** with a marketing, utility, authentication or service message:*
  >   - *That message is **not** charged and opens a Free Entry Point (“FEP”) window, **starting from the time when you responded**.*
  >   - *While open, Meta does **not** charge for marketing, utility, authentication or service messages. Meta continues to charge for Meta Business Agent messages in the FEP window."*

- **Conversation-Based Pricing — Free Entry Point conversations** (`developers.facebook.com/documentation/business-messaging/whatsapp/pricing/conversation-based-pricing#free-entry-point-conversations`):
  > *"A free entry point conversation is opened if (1) a customer using a device running Android or iOS (the desktop and web clients are not supported) messages you via a Click to WhatsApp Ad or Facebook Page Call-to-Action button and (2) **you respond within 24 hours**. If you do not respond within 24 hours, a free entry point conversation is not opened and you must use a template to message the customer…*
  >
  > *The free entry point conversation is opened **as soon as your message is delivered** and lasts 72 hours…*
  >
  > *Once the free entry point conversation is opened, you can send any type of message to the customer without incurring additional charges. **However, you can only send non-template messages if there is an open customer service window between you and the customer.***
  >
  > *For example, if the customer messages you via a Click to WhatsApp Ad at 10am and you respond via a template message at 10pm the same day:*
  > - *The free entry point conversation starts at 10pm and lasts 72 hours.*
  > - *You can send template messages at no charge in those 72 hours.*
  > - ***You can send non-template messages until 10am the next day, at which point the customer service window closes, as it is independent of the free entry point conversation** (if the customer messages you again, however, it opens another 24-hour customer service window in which you can send any type of message)."*

### 2.3. Side-by-Side Comparison

| Dimension | Meta Customer Service Window (CSW) | Meta Free Entry Point (FEP) Window | Current PerGo (`ADR 0015` / `domain.ExtendServiceWindow`) |
| :--- | :--- | :--- | :--- |
| **What it governs** | **Technical permission** to send freeform (non-template) messages | **Billing waiver** (0 delivery fee for marketing, utility, authentication, and service messages) | Conflates both into `chats.service_window_expires_at` |
| **Trigger condition** | Any inbound message or call from the WhatsApp user (`standard` or `ctwa`) | User messages from Android/iOS via CTWA ad or FB Page CTA **and** business replies within 24h | Opened immediately on inbound turn |
| **Start timestamp** | User's latest inbound message (`inbound_at`) | Business's reply delivery (`reply_delivered_at`) | User's inbound message (`inbound_at`) |
| **Duration** | **24 hours** (resets to 24h on each new user message) | **72 hours** (or up to 7 days under current PMP docs; does not extend CSW) | `24h` for `standard`, `72h` for `ctwa` |
| **Hours 24–72 after a silent CTWA user's message** | **CSW is closed.** Freeform messages are rejected by Meta (`131047`); only templates can be sent (for free if FEP was opened). | **FEP is open** (if business replied within 24h). Templates are free; freeform still requires a new user reply to reopen the 24h CSW. | Treats CSW as **open**, permitting freeform sends that Meta rejects at delivery and delaying `session.expiring_soon` to hour 71. |

---

## 3. Impact on PerGo & Architectural Decision

### 3.1. Bugs Caused by the Current 72h CSW Rule in PerGo
1. **False-open window in hours 24–72:** When a contact enters via a CTWA ad at $t_0$ and does not message again, PerGo sets `service_window_expires_at = t_0 + 72h`. Between $t_0 + 24\text{h}$ and $t_0 + 72\text{h}$, `domain.Window` reports `Open: true` and `domain.RequireOpenWindow` allows freeform outbound messages through `OutboundProcessor.Ingest`, which Meta Cloud API rejects on delivery because the 24-hour CSW expired at $t_0 + 24\text{h}$.
2. **Late `session.expiring_soon` webhook:** `SessionTicker` (`ClaimExpiring`) emits `session.expiring_soon` 1 hour before `service_window_expires_at` (at $t_0 + 71\text{h}$ instead of $t_0 + 23\text{h}$), so downstream automations miss the 24-hour deadline needed both to send a freeform reply and to qualify for opening Meta's FEP window.
3. **Accidental `EntryPointType` stickiness coupling:** In [`domain.Apply`](../../internal/domain/chat.go), `EntryPointCTWA` was only preserved on subsequent `"standard"` follow-up messages because `t_0 + 72h` happened to be later than `t_1 + 24h` (`newExp.Equal(prevExp.UTC())`). In Meta's webhook payloads, only the initial ad-click message carries `referral`; follow-up messages in the same session have no `referral` object and parse as `"standard"`.

### 3.2. Agreed Follow-Up Design
Per the [#160](https://github.com/pablodiegoo/PerGo/issues/160) design review:
1. **Customer Service Window is strictly 24 hours from inbound for all entry points:**
   - Simplify `domain.ExtendServiceWindow(prev *time.Time, at time.Time) time.Time` to always compute `at.UTC().Add(StandardWindowDuration)` (`24h`), never shortening `prev` if `prev` is already further in the future.
   - Remove `CTWAWindowDuration` and `WindowDurationFor`.
   - `domain.Window` always reports `WindowDuration: StandardWindowDuration` (`24 * time.Hour`).
2. **Keep `EntryPointType` (`"standard"` | `"ctwa"`) on `Chat` as attribution metadata only (no separate FEP billing timer):**
   - Meta computes FEP billing waivers on its own ledger (requiring Android/iOS client origin + delivery confirmation within 24h) and reports `pricing.billable` on status webhooks. PerGo does not need a separate FEP timer on `Chat`.
   - `EntryPointType` remains on `chats.entry_point_type`, `domain.ChatState`, `domain.ServiceWindow`, and `session.expiring_soon` payloads so operators and webhook consumers know the conversation originated from a Click-to-WhatsApp Ad.
3. **Explicit `EntryPointType` stickiness while the 24h CSW is open:**
   - In `domain.Apply` (`InboundTurn` on `whatsapp_cloud`), if the incoming turn is `"ctwa"`, `next.EntryPointType = EntryPointCTWA`.
   - If the incoming turn is `"standard"`, preserve `next.EntryPointType = EntryPointCTWA` when the prior 24h window was still open at `at` (`prevExp != nil && prevExp.UTC().After(at)` and `next.EntryPointType == EntryPointCTWA`); otherwise set `next.EntryPointType = EntryPointStandard`.
4. **Update `CONTEXT.md` and ADR 0015:**
   - Update the `Customer Service Window` and `Entry Point Type` definitions in `CONTEXT.md` and decision #3 in ADR 0015 to reflect that the Customer Service Window is always 24 hours and `EntryPointType` is attribution metadata rather than a window-duration selector.
