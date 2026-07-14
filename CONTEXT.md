# PerGo Domain Glossary

PerGo is a self-hosted CPaaS that provides unified API messaging and compliance across WhatsApp Web, WhatsApp Cloud (WABA), and Telegram.

## Language

**Workspace**:
A tenant boundary isolating connections, API keys, and audit logs.
_Avoid_: Account, tenant, pool

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

**Inbound Channel Adapter**:
A provider-specific module that translates raw inbound messaging events (payloads, headers, and media) into a unified Inbound Event.
_Avoid_: Webhook controller, message parser, channel translator

**Media Engine**:
The consolidated module that orchestrates downloading, validation, and storage of media files across inbound and outbound channels.
_Avoid_: S3 helper, HTTP downloader, storage client


