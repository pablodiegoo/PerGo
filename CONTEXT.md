# PerGo Domain Glossary

PerGo is a self-hosted CPaaS that provides unified API messaging and compliance across WhatsApp Web, WhatsApp Cloud (WABA), Telegram, Instagram, and Email.

## Language

**Workspace**:
A tenant boundary isolating connections, API keys, campaigns, tags, contacts, and audit logs. Identified strictly by a real UUID generated dynamically without magic or hardcoded default IDs.
_Avoid_: Account, tenant, pool

**Active Workspace**:
The specific workspace currently selected by a System Operator in the admin console session (persisted via session cookie) or resolved from an API key in API requests, defining the tenant scope for all operations.
_Avoid_: Current account, selected tenant

**Workspace Scope**:
The authoritative, tamper-proof security context bound to an HTTP request or asynchronous task that defines and enforces tenant isolation. It validates consistency across authentication credentials, URL path parameters, and session cookies, rejecting cross-tenant parameter tampering with a strict mismatch error before handler execution.
_Avoid_: Tenant context, request scope, workspace wrapper

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
The consolidated, deep module that orchestrates the entire campaign lifecycle, enforcing strict state machine transitions (draft, scheduled, sending, paused, completed, cancelled, failed), atomic completion invariants, dynamic recipient resolution, and queued batch dispatch.
_Avoid_: Bulk sender, campaign runner, batch pusher

**Customer Service Window**:
A rolling 24-hour (or 72-hour, per Entry Point Type) timeframe on a WhatsApp Cloud Chat, opened by the contact's latest inbound message; a new inbound message never shortens an already open window. Freeform outbound messages are only permitted while it is open; outside of it, only pre-approved Meta message templates may be sent.
_Avoid_: Session timer, 24h rule, chat timeout, recipient session

**Entry Point Type**:
How a contact entered a conversation with a connection: `standard` (the contact wrote first organically) or `ctwa` (the contact arrived via a Click-to-WhatsApp Ad referral). It determines whether the Customer Service Window lasts 24 or 72 hours.
_Avoid_: Referral flag, is_referral, entry source

**Webhook Signature**:
An HMAC-SHA256 digest generated using a workspace-scoped secret key and attached to outbound webhooks for cryptographic payload verification.
_Avoid_: Token header, SHA hash, auth HMAC

**Flow Token**:
A stateless, cryptographically signed token generated during interactive flow message dispatch, carrying workspace, connection, contact, flow ID, and expiration metadata for Meta Flow data exchange validation.
_Avoid_: Session ID, form token, flow ticket

**Flow Endpoint**:
An HTTPS endpoint exposed by PerGo (`/api/v1/waba/flows/data-exchange`) that terminates Meta Flow encryption (RSA/AES-128-GCM) and delegates dynamic screen transitions synchronously to a Flow Webhook.
_Avoid_: Flow gateway, screen controller, form callback

**Interactive Message**:
A structured message payload containing rich UI elements (e.g. Reply Buttons, List Menus, Single/Multi Product Catalogs, or Flow CTAs) rather than plain text.
_Avoid_: Rich message, button template, action payload

**Interactive Reply**:
An inbound event triggered by a contact's interaction with an Interactive Message (e.g. button click, list selection, flow submission `nfm_reply`, or catalog order).
_Avoid_: Button response, form reply, action callback

**Safe Webhook Transport**:
An HTTP client transport intercepting TCP socket dials to reject private IP ranges (RFC 1918), loopback, and cloud provider link-local metadata endpoints (`169.254.169.254`) to prevent Server-Side Request Forgery (SSRF) and DNS rebinding attacks.
_Avoid_: Custom HTTP client, secure transport, URL filter

**Connection Proxy**:
An egress SOCKS5 network configuration applied to a WhatsApp Web connection routing WebSocket traffic through dedicated residential or mobile IPs to protect number reputation.
_Avoid_: Proxy tunnel, device VPN, connection IP

**Data Retention Daemon**:
A scheduled background worker that purges expired media blobs from MinIO/S3 and irreversibly scrubs or anonymizes message records exceeding a workspace's compliance retention policy.
_Avoid_: Cleaner script, purge cron, retention job

### Agent-Readiness & Discovery

**Curated Agent Index (`/llms.txt`)**:
A standardized, token-efficient Markdown document adhering to the `llmstxt.org` specification, providing AI agents and LLMs with an executive architecture overview and curated links to PerGo CPaaS messaging endpoints, webhook subscriptions, and MCP server capabilities.
_Avoid_: Agent doc, LLM readme, bot sitemap

**Content Negotiation Interceptor**:
An HTTP middleware evaluating the `Accept: text/markdown` header on public portal and documentation routes, serving clean Markdown representations with `Vary: Accept, Accept-Encoding` without redirect hops or HTML markup overhead.
_Avoid_: Markdown filter, format switcher, markdown proxy

**Discovery Link Header**:
An RFC 8288 compliant HTTP response header (`Link: </llms.txt>; rel="describedby"`) injected across root, health check, and documentation endpoints allowing autonomous agents to locate machine-readable specifications programmatically.
_Avoid_: Header link, discovery tag, bot header

### Agent MCP Diagnostics & Operations

**Connection Health Diagnostic**:
A high-value MCP domain tool (`diagnose_connection_health`) inspecting real-time whatsmeow socket liveness, WebSocket handshake status, proxy reachability/latency, and channel credentials (WABA/Telegram), returning structured actionable self-healing guidance for autonomous agents.
_Avoid_: Socket checker, ping tool, connection probe

**Webhook Event Simulator**:
A high-value MCP domain tool (`simulate_webhook_event`) executing synchronous synthetic webhook dispatches with HMAC-SHA256 signatures and anti-SSRF protections, returning immediate latency, HTTP status, and response telemetry for integration verification.
_Avoid_: Webhook tester, mock dispatcher, payload sender

**Webhook Dead-Letter Replay**:
A high-value MCP domain tool (`replay_webhook_dlq`) re-enqueueing dead-lettered payloads from Postgres back into the NATS JetStream delivery stream (`webhooks.deliveries.>`) or executing immediate dispatches to override URLs for diagnostic verification.
_Avoid_: DLQ retry button, manual redelivery, payload pusher

**Queue Health Inspector**:
A high-value MCP domain tool (`inspect_queue_health`) querying real-time NATS JetStream stream capacity, pending consumer acks, and retention queue pressure across message and webhook streams.
_Avoid_: NATS monitor, queue stats, stream checker

### Shared Team Inbox & Agent Triage

**Chat**:
An active conversational thread between a Contact and a Connection within a Workspace, tracking read status, assignment, and tags.
_Avoid_: Conversation, ticket, thread, session

**Chat Engine**:
The consolidated module that orchestrates Chat queries, triage, Internal Notes, Message Reactions, Draft Promotion, and conversational message dispatch across all transport adapters, relying on Chat Activity for every activity-driven state transition.
_Avoid_: Inbox service, conversation manager, chat helper

**Chat Activity**:
The authoritative record of activity-driven Chat state transitions: inbound turns, outbound turns, Human Handoff Trigger, AI resume, and reopening on activity or Chat Snooze expiry. It is the only writer of that state.
_Avoid_: Chat lifecycle, chat state, chat tracker

**Internal Note**:
A workspace-scoped private commentary attached to a Chat thread, invisible to external contacts, utilized for teammate collaboration and AI agent draft proposals.
_Avoid_: Private message, secret note, staff comment

**Message Reaction**:
An ephemeral emoji reaction attached to an inbound or outbound message identified by UID.
_Avoid_: Like, emoji badge, message feedback

**Inbound Debounce Buffer**:
A temporal sliding-window delay buffering rapid consecutive inbound messages on the same Chat into a single cohesive conversational event.
_Avoid_: Message queue, delay timer, spam filter

**Human Handoff Trigger**:
An event-driven circuit breaker that immediately suspends AI Engagement on a Chat whenever a message with Sender Type human agent is sent on it.
_Avoid_: Bot pause, human override, manual takeover

**AI Engagement**:
Whether automated AI agents and integrations may act on a specific Chat: active, paused until a given time (after a Human Handoff Trigger or an explicit pause), or off until a teammate turns it back on. It belongs to the Chat, never to the Contact, so two Chats of the same Contact are engaged independently.
_Avoid_: Bot active, bot state, AI disabled flag

**Sender Type**:
The kind of author of a Chat message: contact, human agent, AI agent, or system. Operator sessions author as human agents and API keys as AI agents unless they state otherwise.
_Avoid_: Author role, message source, sender kind

**Chat Snooze**:
A temporal suspension state where an active Chat is hidden from the primary open inbox until a specified expiration timestamp or until a new Inbound Event arrives from the contact.
_Avoid_: Sleep mode, muted chat, postponed ticket

**Draft Promotion**:
The atomic operational transition in the inbox UI or API where an Internal Note containing an AI proposal or draft is approved and dispatched as a live outbound message to the contact.
_Avoid_: Note publish, draft send, turn note into message

**Public Chat REST API**:
The headless, API-key-authenticated REST interface under `/api/v1/chats` enabling external agent runtimes, CRM workflows, and scripts to query, read, and manipulate chats, messages, and notes programmatically without MCP or browser sessions.
_Avoid_: Chat controller, inbox endpoint, bot API

**WABA Direct Chat Initiation**:
Creating a brand-new outbound conversation thread to a phone number using an approved Meta HSM Template via `waba_start_chat` before any inbound customer interaction has occurred.
_Avoid_: Cold template blast, proactive WABA message, new WABA thread

**Shared Team Inbox**:
The native 3-column unified conversational interface (Conversation List, Thread View with Notes/Composer, and CRM Contact Sidebar) embedded into the CPaaS console under `/admin/inbox`, serving as the primary UI for human operators and AI agent interactions.
_Avoid_: Helpdesk UI, chat widget, ticketing dashboard

**Workspace Member**:
A team member record within a Workspace (`workspace_members` table) representing an operator, agent, or administrator eligible for chat assignments, team triage, and role-based actions within PerGo.
_Avoid_: User, account member, login profile

**Frameless Embedded Mode**:
A presentation mode of the Shared Team Inbox (`X-PerGo-Embedded: true` or `?embedded=true`) that suppresses PerGo's global CPaaS navigation chassis, rendering only the pure 3-column workspace inbox for seamless embedding within the PergoCloud Web Console shell.
_Avoid_: Iframe mode, headless view, stripped page

### Localization & Presentation

**Supported Locale**:
An explicitly supported IETF BCP 47 language tag (such as `en` or `pt-BR`) for which compiled translation bundles exist and pass validation tests.
_Avoid_: Available language, valid locale, allowed dialect

**Default Locale**:
The canonical fallback locale (`en` / English) utilized whenever an incoming request does not specify a locale or when a translation key is missing in a secondary language.
_Avoid_: System language, standard language, base locale

**Translation Bundle**:
An embedded, compiled catalog of localized strings and pluralization rules loaded via `go-i18n` at application startup.
_Avoid_: Language file, text dictionary, string table

**Language Switcher**:
The UI component allowing operators to immediately toggle between supported interface locales across the System Operator Console, persisting the selection in session cookies.
_Avoid_: Language dropdown, translation picker, locale selector

**Design System Token**:
A standardized semantic UI variable (typography scale, color token, spacing step) defined across Templ components and Tailwind classes to preserve visual consistency across localized viewports.
_Avoid_: CSS variable, style constant, UI theme value

**Locale**:
An IETF BCP 47 language tag (e.g. `en`, `pt-BR`) specifying the resolved language for web console presentation.
_Avoid_: Language code, dialect, region ID

**SSO Locale Claim**:
An optional attribute included within the cryptographically signed SSO hand-off token propagating the operator's active language from PergoCloud to PerGo.
_Avoid_: Auth lang, hand-off locale param
