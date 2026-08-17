# PerGo Domain Glossary

PerGo is a self-hosted CPaaS that provides unified API messaging and compliance across WhatsApp Web, WhatsApp Cloud (WABA), Telegram, Instagram, and Email.

## Language

**Workspace**:
A tenant boundary isolating connections, API keys, campaigns, tags, contacts, and audit logs. Identified strictly by a real UUID generated dynamically without magic or hardcoded default IDs.
_Avoid_: Account, tenant, pool

**Active Workspace**:
The specific workspace currently selected by a System Operator in the admin console session (persisted via session cookie) or resolved from an API key in API requests, defining the tenant scope for all operations.
_Avoid_: Current account, selected tenant

**System Operator**:
A global administrator authenticated to the PerGo admin console who can create, inspect, and switch between any Workspace.
_Avoid_: Superuser, root user, master admin

**Connection**:
A configured messaging provider instance linked to a specific channel and sender identity.
_Avoid_: Device, channel adapter, session instance

**Inbound Event**:
A unified payload representing a message received from a contact via an external channel.
_Avoid_: Incoming payload, webhook update, message event

**Inbound Processor**:
The consolidated module that orchestrates the ingestion logic for inbound events.
_Avoid_: Ingestion engine, webhook handler, session manager

**Outbound Processor**:
The consolidated module that orchestrates the ingestion logic for outbound messages.
_Avoid_: Message handler, API ingestor, route dispatcher

**Webhook Dispatcher**:
The consolidated module that orchestrates the dispatch logic for webhook events, handling signatures and PII redaction.
_Avoid_: Webhook worker, payload signer, HTTP poster

**Webhook Verbs Engine**:
The consolidated module that orchestrates execution of action verbs returned by webhook responses.
_Avoid_: Action runner, verb execution processor

**Inbound Channel Adapter**:
A provider-specific module that translates raw inbound messaging events (payloads, headers, and media) into a unified Inbound Event.
_Avoid_: Webhook controller, message parser, channel translator

**Media Engine**:
The consolidated module that orchestrates downloading, validation, and storage of media files across inbound and outbound channels.
_Avoid_: S3 helper, HTTP downloader, storage client

**Inbound Router**:
The consolidated module that orchestrates routing of unified inbound events to external integration syncers asynchronously.
_Avoid_: Event routing manager, syncer coordinator

**Inbound Integration Handler**:
An adapter satisfying the `IntegrationHandler` seam that syncs or forwards unified inbound events to a specific external system (e.g. Chatwoot, Typebot, N8N).
_Avoid_: Syncer adapter, integration forwarder plugin

**Campaign**:
A scheduled or immediate bulk message dispatch targeting a defined contact segment (via union of Tags) or static CSV through a specific connection slug. Recipients are dynamically evaluated at execution time. Contacts lacking an identity for the campaign's channel are explicitly recorded as `skipped` for transparency. In conflicts between Tag contacts and CSV contacts, the Tag (database) contact takes precedence for identity deduplication, while CSV variables merge on top of contact attributes. Empty dispatches are considered valid and recorded as `campaign.dispatch.completed_empty`.
_Avoid_: Broadcast job, bulk blast, mass message batch

**Tag**:
A workspace-scoped label attached to a contact for categorization and segment filtering. Multiple tags in a campaign operate as a Union (OR).
_Avoid_: Contact label, group tag, list category

**Contact**:
A single person or entity within a workspace, possessing one or more Identities (e.g. WhatsApp, Email) and arbitrary key-value custom Attributes for campaign template variable resolution.
_Avoid_: User, lead, audience member

**Identity**:
A channel-specific address (like a phone number or email) belonging to a Contact. Campaigns target the primary/deterministic identity matching the campaign's channel.
_Avoid_: Contact point, address, phone number

**Broadcaster Engine**:
The consolidated module that orchestrates the batching, rate-limiting, and queued dispatch of campaign messages.
_Avoid_: Bulk sender, campaign runner, batch pusher

**Customer Service Window**:
A rolling 24-hour (or 72-hour for Click-to-WhatsApp Ads) timeframe initiated by an inbound message from a contact to a specific WhatsApp Cloud connection. Freeform outbound messages are only permitted while this window is open; outside of it, only pre-approved Meta message templates may be sent unless a default template fallback is configured on the connection.
_Avoid_: Session timer, 24h rule, chat timeout

**Webhook Signature**:
An HMAC-SHA256 digest generated using a workspace-scoped secret key and attached to outbound webhooks for cryptographic payload verification.
_Avoid_: Token header, SHA hash, auth HMAC
